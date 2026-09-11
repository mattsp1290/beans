package vault

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattsp1290/beans/issue"
)

type fakeGit struct {
	toplevel, remote, head, branch string
}

func (f fakeGit) Toplevel(string) (string, bool, error)   { return f.toplevel, f.toplevel != "", nil }
func (f fakeGit) RemoteURL(string) (string, bool, error)  { return f.remote, f.remote != "", nil }
func (f fakeGit) HeadCommit(string) (string, bool, error) { return f.head, f.head != "", nil }
func (f fakeGit) Branch(string) (string, bool, error)     { return f.branch, f.branch != "", nil }

func newHub(t *testing.T) string {
	t.Helper()
	hub := t.TempDir()
	if err := os.MkdirAll(filepath.Join(hub, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	return hub
}

func addProject(t *testing.T, hub, name string, remotes ...string) {
	t.Helper()
	data, err := issue.EncodeProjectConfig(issue.ProjectConfig{Name: name, Prefix: name, Remotes: remotes})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(hub, "projects", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "beans.toml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func noEnv(string) string { return "" }

func TestResolveBasenameMatch(t *testing.T) {
	hub := newHub(t)
	addProject(t, hub, "exa", "https://github.com/o/exa")
	git := fakeGit{toplevel: "/code/exa", remote: "git@github.com:o/exa.git", head: "0123456789abcdef", branch: "feature/x"}
	res, err := Resolve(hub, ResolveOptions{Cwd: "/code/exa/sub", Git: git, Env: noEnv})
	if err != nil {
		t.Fatal(err)
	}
	if res.Project != "exa" || res.Created || res.RepoHead != "0123456" || res.RepoBranch != "feature/x" || res.RepoRemote != "https://github.com/o/exa" {
		t.Errorf("res = %+v", res)
	}
}

func TestResolveRemoteFallback(t *testing.T) {
	hub := newHub(t)
	addProject(t, hub, "renamed", "https://github.com/o/exa")
	git := fakeGit{toplevel: "/code/exa", remote: "https://github.com/o/exa.git"}
	res, err := Resolve(hub, ResolveOptions{Git: git, Env: noEnv, Cwd: "/code/exa"})
	if err != nil || res.Project != "renamed" {
		t.Fatalf("res = %+v err = %v", res, err)
	}
}

func TestResolveCollisionErrors(t *testing.T) {
	hub := newHub(t)
	addProject(t, hub, "exa", "https://github.com/other/exa")
	git := fakeGit{toplevel: "/code/exa", remote: "https://github.com/o/exa"}
	_, err := Resolve(hub, ResolveOptions{Git: git, Env: noEnv, Cwd: "/code/exa"})
	if err == nil || !strings.Contains(err.Error(), "projects/exa belongs to https://github.com/other/exa") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveOutsideGit(t *testing.T) {
	hub := newHub(t)
	_, err := Resolve(hub, ResolveOptions{Git: fakeGit{}, Env: noEnv, Cwd: "/nowhere"})
	if !errors.Is(err, ErrOutsideRepo) {
		t.Fatalf("err = %v", err)
	}
	res, err := Resolve(hub, ResolveOptions{Git: fakeGit{}, Env: noEnv, Cwd: "/nowhere", AllProjects: true})
	if err != nil || res.Project != "" {
		t.Fatalf("all-projects read should proceed hub-wide: %+v %v", res, err)
	}
}

func TestResolveOverrides(t *testing.T) {
	hub := newHub(t)
	addProject(t, hub, "flagged")
	git := fakeGit{toplevel: "/code/exa"}
	res, err := Resolve(hub, ResolveOptions{Git: git, Env: noEnv, Cwd: "/code/exa", FlagProject: "flagged"})
	if err != nil || res.Project != "flagged" || res.Created {
		t.Fatalf("%+v %v", res, err)
	}
	env := func(k string) string {
		if k == EnvProject {
			return "flagged"
		}
		return ""
	}
	res, err = Resolve(hub, ResolveOptions{Git: git, Env: env, Cwd: "/code/exa"})
	if err != nil || res.Project != "flagged" {
		t.Fatalf("%+v %v", res, err)
	}
	// explicit missing project: reads error, writes auto-create
	if _, err := Resolve(hub, ResolveOptions{Git: git, Env: noEnv, FlagProject: "nope", Cwd: "/x"}); err == nil {
		t.Error("read of a missing explicit project must error")
	}
	res, err = Resolve(hub, ResolveOptions{Git: git, Env: noEnv, FlagProject: "nope", Cwd: "/x", Write: true})
	if err != nil || !res.Created || res.Project != "nope" {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := Resolve(hub, ResolveOptions{Git: git, Env: noEnv, FlagProject: "Bad Name", Cwd: "/x"}); err == nil {
		t.Error("invalid project name must error")
	}
}

func TestResolveAutoCreateOnWriteNotRead(t *testing.T) {
	hub := newHub(t)
	git := fakeGit{toplevel: "/code/My Repo", remote: "https://github.com/o/my-repo"}
	res, err := Resolve(hub, ResolveOptions{Git: git, Env: noEnv, Cwd: "/code/My Repo"})
	if err != nil || res.Created || res.Project != "my-repo" || res.Notice != "project my-repo has no issues yet" {
		t.Fatalf("read: %+v %v", res, err)
	}
	res, err = Resolve(hub, ResolveOptions{Git: git, Env: noEnv, Cwd: "/code/My Repo", Write: true})
	if err != nil || !res.Created || res.Project != "my-repo" {
		t.Fatalf("write: %+v %v", res, err)
	}
	paths, err := CreateProjectFiles(hub, res.Project, res.RepoRemote)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 6 || paths[0] != "projects/my-repo/beans.toml" || paths[5] != "projects/my-repo/requests/.gitkeep" {
		t.Errorf("paths = %v", paths)
	}
	cfg, err := issue.LoadProjectConfig(filepath.Join(hub, "projects", "my-repo", "beans.toml"))
	if err != nil || cfg.Prefix != "my-repo" || len(cfg.Remotes) != 1 || cfg.Remotes[0] != "https://github.com/o/my-repo" {
		t.Errorf("cfg = %+v %v", cfg, err)
	}
	again, err := CreateProjectFiles(hub, res.Project, res.RepoRemote)
	if err != nil || len(again) != 0 {
		t.Errorf("second create should write nothing: %v %v", again, err)
	}
	names, _ := ProjectDirs(hub)
	if len(names) != 1 || names[0] != "my-repo" {
		t.Errorf("ProjectDirs = %v", names)
	}
}

func TestProjectName(t *testing.T) {
	for in, want := range map[string]string{"Beans": "beans", "My Repo": "my-repo", "a_b.c": "a-b-c", "--x--": "x", "ok-name": "ok-name"} {
		if got := ProjectName(in); got != want {
			t.Errorf("ProjectName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDefaultPaths(t *testing.T) {
	t.Setenv(EnvHome, "/tmp/bh")
	t.Setenv(EnvHub, "")
	p, err := DefaultPaths("")
	if err != nil || p.Hub != "/tmp/bh/hub" || p.Cache != "/tmp/bh/cache" || p.Config != "/tmp/bh/config.toml" {
		t.Fatalf("%+v %v", p, err)
	}
	t.Setenv(EnvHub, "/elsewhere/hub")
	p, _ = DefaultPaths("")
	if p.Hub != "/elsewhere/hub" {
		t.Errorf("BEANS_HUB ignored: %+v", p)
	}
	p, _ = DefaultPaths("/flag/hub")
	if p.Hub != "/flag/hub" {
		t.Errorf("--hub ignored: %+v", p)
	}
	if err := CheckHub(p); !errors.Is(err, ErrNoHub) {
		t.Errorf("CheckHub on a missing dir = %v", err)
	}
}
