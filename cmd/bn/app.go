package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/gitops"
	"github.com/mattsp1290/beans/internal/ops"
	"github.com/mattsp1290/beans/issue"
	"github.com/mattsp1290/beans/vault"
)

// Exit codes (see docs/prime.md): 0 success; 1 usage or validation error;
// 2 not found; 3 git failure; 4 lock timeout.
const (
	exitUsage    = 1
	exitNotFound = 2
)

// appState carries the flag values, injectable seams, and the hub and
// project resolved for the running command.
type appState struct {
	// resolved from flags / env
	project string
	actor   string
	jsonOut bool
	noSync  bool
	noFetch bool
	hubFlag string

	// injectable seams; defaults set in newRootCmd
	git    gitops.Resolver
	stderr io.Writer
	env    func(string) string
	cwd    string

	// set by setup
	paths    vault.Paths
	userCfg  issue.UserConfig
	hub      *gitops.Hub
	resolved vault.Resolved
	idx      *vault.Index
	clock    func() time.Time
}

func newRootCmd(rs *appState) *cobra.Command {
	if rs.clock == nil {
		rs.clock = time.Now
	}
	if rs.git == nil {
		rs.git = gitops.SystemGit{}
	}
	if rs.stderr == nil {
		rs.stderr = os.Stderr
	}
	if rs.env == nil {
		rs.env = os.Getenv
	}
	root := &cobra.Command{
		Use:           "bn",
		Short:         "Git-backed issue tracker and wiki (bn = beans)",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	pf := root.PersistentFlags()
	pf.StringVar(&rs.project, "project", "", "project name (overrides $BEANS_PROJECT and git auto-detection)")
	pf.StringVar(&rs.actor, "actor", "", "audit actor (overrides $BN_ACTOR)")
	pf.BoolVar(&rs.jsonOut, "json", false, "machine-readable JSON output")
	pf.BoolVar(&rs.noSync, "no-sync", false, "mutations: commit locally without fetching or pushing")
	pf.BoolVar(&rs.noFetch, "no-fetch", false, "reads: skip the throttled fetch")
	pf.StringVar(&rs.hubFlag, "hub", "", "hub clone directory (overrides $BEANS_HUB)")

	root.AddCommand(
		newInitCmd(rs),
		newStatusCmd(rs),
		newSyncCmd(rs),
		newCacheCmd(rs),
		newProjectCmd(rs),
		newCreateCmd(rs),
		newShowCmd(rs),
		newListCmd(rs),
		newReadyCmd(rs),
		newBlockedCmd(rs),
		newUpdateCmd(rs),
		newNoteCmd(rs),
		newCloseCmd(rs),
		newReopenCmd(rs),
		newDeleteCmd(rs),
		newDepCmd(rs),
		newChildrenCmd(rs),
		newArchiveCmd(rs),
		newHandoffCmd(rs),
		newSearchCmd(rs),
		newRememberCmd(rs),
		newMemoriesCmd(rs),
		newForgetCmd(rs),
		newDocCmd(rs),
		newRequestCmd(rs),
		newPlanCmd(rs),
		newDoctorCmd(rs),
		newPrimeCmd(),
		newImportCmd(rs),
		newServeCmd(rs),
	)
	return root
}

// setupProject resolves the project for a command. write auto-creates a
// missing project; allProjects lets a read proceed hub-wide outside a git
// repository. Notices go to stderr so --json and --silent stay clean.
func (rs *appState) setupProject(write, allProjects bool) error {
	if err := rs.setupHub(); err != nil {
		return err
	}
	res, err := vault.Resolve(rs.paths.Hub, vault.ResolveOptions{
		Cwd: rs.cwd, FlagProject: rs.project, Write: write, AllProjects: allProjects, Git: rs.git, Env: rs.env,
	})
	if err != nil {
		return err
	}
	rs.resolved = res
	if res.Notice != "" && !write {
		fmt.Fprintln(rs.stderr, res.Notice)
	}
	return nil
}

// readIndex runs the throttled fetch and loads the hub index once.
func (rs *appState) readIndex(ctx context.Context) (*vault.Index, error) {
	if rs.idx != nil {
		return rs.idx, nil
	}
	rs.fetch(ctx)
	ix, err := vault.LoadWithOptions(rs.paths.Hub, vault.LoadOptions{ExplicitWorkflow: strings.TrimSpace(rs.env("BN_CONFIG"))})
	if err != nil {
		return nil, err
	}
	rs.idx = ix
	return ix, nil
}

// opsEnv builds the environment shared by every operation.
func (rs *appState) opsEnv() ops.Env {
	hubDir := rs.paths.Hub
	explicit := strings.TrimSpace(rs.env("BN_CONFIG"))
	hubTOML, _ := os.ReadFile(filepath.Join(hubDir, "beans.toml"))
	hubCfg, _ := issue.LoadHubConfig(filepath.Join(hubDir, "beans.toml"))
	return ops.Env{
		HubDir:  hubDir,
		Project: rs.resolved.Project,
		Actor:   rs.resolveActor(),
		Repo:    baseName(rs.resolved.RepoRoot),
		SHA:     rs.resolved.RepoHead,
		Branch:  rs.resolved.RepoBranch,
		Now:     rs.clock,
		WorkflowFor: func(project string) issue.WorkflowConfig {
			projectTOML, _ := os.ReadFile(filepath.Join(hubDir, "projects", project, "beans.toml"))
			wf, err := issue.LoadWorkflow(explicit, projectTOML, hubTOML)
			if err != nil {
				return issue.DefaultWorkflowConfig()
			}
			return wf
		},
		Types:    hubCfg.Types,
		IDLength: hubCfg.IDs.Length,
	}
}

// prefixFor returns the id prefix of the resolved project.
func (rs *appState) prefixFor(project string) string {
	cfg, err := issue.LoadProjectConfig(filepath.Join(rs.paths.Hub, "projects", project, "beans.toml"))
	if err == nil && strings.TrimSpace(cfg.Prefix) != "" {
		return cfg.Prefix
	}
	return project
}

func baseName(p string) string {
	if p == "" {
		return ""
	}
	return filepath.Base(p)
}

// tableWriter returns an aligned-column writer on the command's stdout.
func tableWriter(cmd *cobra.Command) *tabwriter.Writer {
	return tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
}

// exitCode maps an error to the process exit code.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *gitops.ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	var ce *codedError
	if errors.As(err, &ce) {
		return ce.code
	}
	if errors.Is(err, ops.ErrNotFound) || errors.Is(err, ops.ErrRequestNotFound) {
		return exitNotFound
	}
	return exitUsage
}

// codedError carries a CLI exit code.
type codedError struct {
	code int
	msg  string
}

func (e *codedError) Error() string { return e.msg }

func notFound(format string, args ...any) error {
	return &codedError{code: exitNotFound, msg: fmt.Sprintf(format, args...)}
}

// setupHub locates ~/.beans, loads the user config, resolves the actor, and
// builds the Hub. It does not resolve a project.
func (rs *appState) setupHub() error {
	if rs.hub != nil {
		return nil
	}
	paths, err := vault.DefaultPaths(rs.hubFlag)
	if err != nil {
		return err
	}
	rs.paths = paths
	cfg, err := issue.LoadUserConfig(paths.Config)
	if err != nil {
		return err
	}
	rs.userCfg = cfg
	if err := vault.CheckHub(paths); err != nil {
		return err
	}
	branch := strings.TrimSpace(cfg.Hub.Branch)
	if branch == "" {
		branch = "main"
	}
	rs.hub = &gitops.Hub{
		Dir:      paths.Hub,
		CacheDir: paths.Cache,
		Branch:   branch,
		Throttle: cfg.ThrottleDuration(),
		Actor:    rs.resolveActor(),
		Stderr:   rs.stderr,
		NoSync:   rs.noSync,
	}
	return nil
}

// fetch runs the throttled read fetch unless --no-fetch was given.
func (rs *appState) fetch(ctx context.Context) {
	if rs.noFetch || rs.hub == nil {
		return
	}
	rs.hub.FetchIfStale(ctx)
}

// resolveActor applies the actor precedence: --actor > BN_ACTOR >
// ~/.beans/config.toml actor > git config user.name > USER.
func (rs *appState) resolveActor() string {
	if rs.actor != "" {
		return rs.actor
	}
	if rs.env == nil {
		rs.env = os.Getenv
	}
	if v := strings.TrimSpace(rs.env("BN_ACTOR")); v != "" {
		rs.actor = v
		return rs.actor
	}
	if v := strings.TrimSpace(rs.userCfg.Actor); v != "" {
		rs.actor = v
		return rs.actor
	}
	if out, err := exec.Command("git", "config", "user.name").Output(); err == nil {
		if name := strings.TrimSpace(string(out)); name != "" {
			rs.actor = name
			return rs.actor
		}
	}
	rs.actor = rs.env("USER")
	return rs.actor
}

// mutate runs one operation through the hub pipeline and prints the push
// notice on stderr when the change stayed local.
func (rs *appState) mutate(ctx context.Context, op gitops.Operation) (gitops.Result, error) {
	res, err := rs.hub.Mutate(ctx, op)
	if err != nil {
		return res, err
	}
	if res.Message != "" {
		fmt.Fprintln(rs.stderr, res.Message)
	}
	return res, nil
}

// ---------------------------------------------------------------------------
// JSON output helpers
// ---------------------------------------------------------------------------

func writeJSON(v any) error {
	return writeJSONTo(os.Stdout, v)
}

func writeJSONTo(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
