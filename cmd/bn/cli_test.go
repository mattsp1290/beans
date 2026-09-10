package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cliEnv is a fresh BEANS_HOME, a bare remote, and a code repository to run
// commands from.
type cliEnv struct {
	t      *testing.T
	home   string
	remote string
	repo   string
	stderr bytes.Buffer
	ticks  int // fake clock: every command runs one second after the previous
}

func newCLIEnv(t *testing.T) *cliEnv {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	e := &cliEnv{t: t, home: filepath.Join(t.TempDir(), "beans-home"), remote: filepath.Join(t.TempDir(), "hub.git"), repo: filepath.Join(t.TempDir(), "myapp")}
	t.Setenv("BEANS_HOME", e.home)
	t.Setenv("BEANS_HUB", "")
	t.Setenv("BEANS_PROJECT", "")
	t.Setenv("BN_ACTOR", "tester")
	e.git("", "init", "-q", "--bare", "-b", "main", e.remote)
	e.git("", "init", "-q", "-b", "main", e.repo)
	e.git(e.repo, "remote", "add", "origin", "git@github.com:o/myapp.git")
	if err := os.WriteFile(filepath.Join(e.repo, "README"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.git(e.repo, "add", "README")
	e.git(e.repo, "-c", "user.name=t", "-c", "user.email=t@x", "commit", "-qm", "init")
	return e
}

func (e *cliEnv) git(dir string, args ...string) string {
	e.t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		e.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// run executes bn with args from inside the code repository and returns
// stdout, the error, and the exit code.
func (e *cliEnv) run(args ...string) (string, int, error) {
	e.t.Helper()
	e.ticks++
	base := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC).Add(time.Duration(e.ticks) * time.Second)
	rs := &appState{stderr: &e.stderr, cwd: e.repo, clock: func() time.Time { return base }}
	root := newRootCmd(rs)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), exitCode(err), err
}

func TestInitStatusProjectRoundTrip(t *testing.T) {
	e := newCLIEnv(t)

	out, _, err := e.run("init", e.remote)
	if err != nil || !strings.Contains(out, "branch main") {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if _, _, err := e.run("init", e.remote); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second init must refuse: %v", err)
	}

	// status through the real stdout capture is awkward; use --json to a pipe
	st := e.runJSON(t, "status", "--json")
	if st["ahead"].(float64) != 0 || st["behind"].(float64) != 0 || st["branch"] != "main" {
		t.Fatalf("status = %v", st)
	}
	if st["project"] != "myapp" || st["resolution"] != "project myapp has no issues yet" {
		t.Fatalf("before creation the candidate resolves with a notice: %v", st)
	}

	out, _, err = e.run("project", "create", "myapp", "--link")
	if err != nil || !strings.Contains(out, "created project myapp") {
		t.Fatalf("project create: %v\n%s", err, out)
	}
	log := e.git(e.remote, "log", "--format=%s", "main")
	if !strings.HasPrefix(log, "bn: project create myapp — myapp\n") {
		t.Fatalf("remote log:\n%s", log)
	}
	st = e.runJSON(t, "status", "--json")
	if st["project"] != "myapp" || st["resolution"] != "resolved" || st["repo_remote"] != "https://github.com/o/myapp" {
		t.Fatalf("status after create = %v", st)
	}
	shown := e.runJSON(t, "project", "show", "myapp", "--json")
	if shown["prefix"] != "myapp" || shown["remotes"].([]any)[0] != "https://github.com/o/myapp" {
		t.Fatalf("show = %v", shown)
	}
	if _, code, err := e.run("project", "show", "nope"); code != exitNotFound || err == nil {
		t.Fatalf("show missing: err=%v code=%d", err, code)
	}
	// link is idempotent
	out, _, err = e.run("project", "link", "myapp")
	if err != nil || !strings.Contains(out, "already linked") {
		t.Fatalf("link: %v\n%s", err, out)
	}
	out, _, err = e.run("sync")
	if err != nil || !strings.Contains(out, "ahead 0, behind 0") {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	out, _, err = e.run("cache", "clear")
	if err != nil || !strings.Contains(out, "removed last-fetch") {
		t.Fatalf("cache clear: %v\n%s", err, out)
	}
}

// runJSON runs a --json command capturing os.Stdout.
func (e *cliEnv) runJSON(t *testing.T, args ...string) map[string]any {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	_, _, runErr := e.run(args...)
	w.Close()
	os.Stdout = orig
	if runErr != nil {
		t.Fatalf("%v: %v", args, runErr)
	}
	var m map[string]any
	if err := json.NewDecoder(r).Decode(&m); err != nil {
		t.Fatalf("decode %v: %v", args, err)
	}
	return m
}

func TestCommandsWithoutHubFail(t *testing.T) {
	e := newCLIEnv(t)
	_, code, err := e.run("status")
	if err == nil || !strings.Contains(err.Error(), "bn init") || code != exitUsage {
		t.Fatalf("err=%v code=%d", err, code)
	}
}
