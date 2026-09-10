package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/gitops"
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
	paths   vault.Paths
	userCfg issue.UserConfig
	hub     *gitops.Hub
}

func newRootCmd(rs *appState) *cobra.Command {
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
	)
	return root
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
