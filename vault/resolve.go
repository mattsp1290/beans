package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mattsp1290/beans/gitops"
	"github.com/mattsp1290/beans/issue"
)

// Environment variables.
const (
	EnvHome    = "BEANS_HOME"    // default ~/.beans
	EnvHub     = "BEANS_HUB"     // default $BEANS_HOME/hub
	EnvProject = "BEANS_PROJECT" // overrides project resolution
)

// Paths locates the per-user directories.
type Paths struct {
	Home   string // $BEANS_HOME
	Hub    string // $BEANS_HUB
	Cache  string // $BEANS_HOME/cache
	Config string // $BEANS_HOME/config.toml
}

// DefaultPaths resolves BEANS_HOME and BEANS_HUB. flagHub, when non-empty,
// overrides BEANS_HUB.
func DefaultPaths(flagHub string) (Paths, error) {
	home := strings.TrimSpace(os.Getenv(EnvHome))
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve home directory: %w", err)
		}
		home = filepath.Join(userHome, ".beans")
	}
	hub := strings.TrimSpace(flagHub)
	if hub == "" {
		hub = strings.TrimSpace(os.Getenv(EnvHub))
	}
	if hub == "" {
		hub = filepath.Join(home, "hub")
	}
	if !filepath.IsAbs(hub) {
		abs, err := filepath.Abs(hub)
		if err != nil {
			return Paths{}, err
		}
		hub = abs
	}
	return Paths{Home: home, Hub: hub, Cache: filepath.Join(home, "cache"), Config: filepath.Join(home, "config.toml")}, nil
}

// ErrNoHub is returned when the hub directory is not a git clone.
var ErrNoHub = errors.New("no hub found; run bn init <remote>")

// CheckHub verifies the hub clone exists.
func CheckHub(p Paths) error {
	if fi, err := os.Stat(filepath.Join(p.Hub, ".git")); err != nil || !fi.IsDir() {
		return fmt.Errorf("%w (expected a clone at %s)", ErrNoHub, p.Hub)
	}
	return nil
}

// Resolved is the outcome of project resolution for one command.
type Resolved struct {
	HubDir     string
	Project    string // "" when no project resolved (hub-wide reads)
	ProjectDir string
	RepoRoot   string
	RepoRemote string // normalized origin URL, "" when none
	RepoHead   string // short sha
	RepoBranch string
	Created    bool   // the project was auto-created by this resolution
	Notice     string // one-line stderr notice for reads ("project x has no issues yet")
	Candidate  string // name derived from the repository when no project exists yet
}

// ResolveOptions controls Resolve.
type ResolveOptions struct {
	Cwd         string // working directory; "" means os.Getwd
	FlagProject string // --project
	Write       bool   // the command mutates; a missing project is auto-created
	AllProjects bool   // a read that may proceed hub-wide outside a repository
	Git         gitops.Resolver
	Env         func(string) string // defaults to os.Getenv
}

var projectNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ProjectName lowercases s and replaces characters outside [a-z0-9-] with "-".
func ProjectName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// ValidProjectName reports whether name matches the project grammar.
func ValidProjectName(name string) bool { return projectNameRe.MatchString(name) }

// ErrOutsideRepo is returned for commands that need a project outside a git
// repository.
var ErrOutsideRepo = errors.New("not inside a git repository; pass --project <name> or run inside the project's repository")

// Resolve finds the project for the current command.
func Resolve(hubDir string, opts ResolveOptions) (Resolved, error) {
	if opts.Git == nil {
		opts.Git = gitops.SystemGit{}
	}
	if opts.Env == nil {
		opts.Env = os.Getenv
	}
	cwd := opts.Cwd
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return Resolved{}, err
		}
		cwd = wd
	}
	res := Resolved{HubDir: hubDir}
	res.fillRepo(cwd, opts.Git)

	// 1. explicit project
	name := strings.TrimSpace(opts.FlagProject)
	if name == "" {
		name = strings.TrimSpace(opts.Env(EnvProject))
	}
	if name != "" {
		if !ValidProjectName(name) {
			return Resolved{}, fmt.Errorf("invalid project name %q (use [a-z0-9-])", name)
		}
		res.Project = name
		res.ProjectDir = filepath.Join(hubDir, "projects", name)
		if !projectExists(hubDir, name) {
			if !opts.Write {
				return Resolved{}, fmt.Errorf("project %s does not exist in the hub", name)
			}
			res.Created = true
		}
		return res, nil
	}

	// 2. git toplevel
	if res.RepoRoot == "" {
		if opts.AllProjects {
			return res, nil
		}
		return Resolved{}, ErrOutsideRepo
	}
	candidate := ProjectName(filepath.Base(res.RepoRoot))
	res.Candidate = candidate

	// 3. basename match
	if projectExists(hubDir, candidate) {
		cfg, err := issue.LoadProjectConfig(filepath.Join(hubDir, "projects", candidate, "beans.toml"))
		if err != nil {
			return Resolved{}, err
		}
		if res.RepoRemote != "" && len(cfg.Remotes) > 0 && !containsRemote(cfg.Remotes, res.RepoRemote) {
			return Resolved{}, fmt.Errorf("projects/%s belongs to %s; run bn project create <other-name> --link to use a different name", candidate, strings.Join(cfg.Remotes, ", "))
		}
		res.Project = candidate
		res.ProjectDir = filepath.Join(hubDir, "projects", candidate)
		return res, nil
	}

	// 4. remote match
	if res.RepoRemote != "" {
		matches, err := projectsWithRemote(hubDir, res.RepoRemote)
		if err != nil {
			return Resolved{}, err
		}
		switch len(matches) {
		case 0:
		case 1:
			res.Project = matches[0]
			res.ProjectDir = filepath.Join(hubDir, "projects", matches[0])
			return res, nil
		default:
			return Resolved{}, fmt.Errorf("remote %s is linked to more than one project: %s; pass --project", res.RepoRemote, strings.Join(matches, ", "))
		}
	}

	// 5. no project yet
	res.Project = candidate
	res.ProjectDir = filepath.Join(hubDir, "projects", candidate)
	if opts.Write {
		res.Created = true
		return res, nil
	}
	res.Notice = fmt.Sprintf("project %s has no issues yet", candidate)
	return res, nil
}

func (r *Resolved) fillRepo(cwd string, git gitops.Resolver) {
	root, ok, _ := git.Toplevel(cwd)
	if !ok {
		return
	}
	r.RepoRoot = root
	if url, ok, _ := git.RemoteURL(root); ok {
		if norm, err := NormalizeRemoteURL(url); err == nil {
			r.RepoRemote = norm
		}
	}
	if sha, ok, _ := git.HeadCommit(root); ok && len(sha) >= 7 {
		r.RepoHead = sha[:7]
	}
	if b, ok := branchOf(git, root); ok {
		r.RepoBranch = b
	}
}

// brancher is implemented by resolvers that can report the checked-out
// branch; SystemGit does.
type brancher interface {
	Branch(root string) (string, bool, error)
}

func branchOf(git gitops.Resolver, root string) (string, bool) {
	if b, ok := git.(brancher); ok {
		name, found, _ := b.Branch(root)
		return name, found
	}
	return "", false
}

func projectExists(hubDir, name string) bool {
	_, err := os.Stat(filepath.Join(hubDir, "projects", name, "beans.toml"))
	return err == nil
}

func containsRemote(remotes []string, want string) bool {
	for _, r := range remotes {
		norm, err := NormalizeRemoteURL(r)
		if err != nil {
			norm = r
		}
		if norm == want {
			return true
		}
	}
	return false
}

func projectsWithRemote(hubDir, remote string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(hubDir, "projects"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var matches []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		cfg, err := issue.LoadProjectConfig(filepath.Join(hubDir, "projects", e.Name(), "beans.toml"))
		if err != nil {
			return nil, err
		}
		if containsRemote(cfg.Remotes, remote) {
			matches = append(matches, e.Name())
		}
	}
	sort.Strings(matches)
	return matches, nil
}

// ProjectDirs lists the project names in the hub.
func ProjectDirs(hubDir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(hubDir, "projects"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && projectExists(hubDir, e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// CreateProjectFiles writes projects/<name>/beans.toml and the empty
// directories with .gitkeep files. It returns the hub-relative paths written
// so an Operation can stage them. Existing files are left alone.
func CreateProjectFiles(hubDir, name, remote string) ([]string, error) {
	dir := filepath.Join(hubDir, "projects", name)
	var paths []string
	cfgPath := filepath.Join(dir, "beans.toml")
	if _, err := os.Stat(cfgPath); errors.Is(err, os.ErrNotExist) {
		cfg := issue.ProjectConfig{Name: name, Prefix: name}
		if remote != "" {
			cfg.Remotes = []string{remote}
		}
		data, err := issue.EncodeProjectConfig(cfg)
		if err != nil {
			return nil, err
		}
		if err := gitops.WriteFile(cfgPath, data); err != nil {
			return nil, err
		}
		paths = append(paths, filepath.ToSlash(filepath.Join("projects", name, "beans.toml")))
	}
	for _, sub := range []string{"issues", "archive", "docs", "memories", "requests", "handoffs", "handoffs/archive"} {
		keep := filepath.Join(dir, sub, ".gitkeep")
		if _, err := os.Stat(keep); errors.Is(err, os.ErrNotExist) {
			if err := gitops.WriteFile(keep, nil); err != nil {
				return nil, err
			}
			paths = append(paths, filepath.ToSlash(filepath.Join("projects", name, sub, ".gitkeep")))
		}
	}
	return paths, nil
}
