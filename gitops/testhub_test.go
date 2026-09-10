package gitops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// RecordingRunner wraps a Runner, records every command, and lets a test run
// a callback or inject a failure before a named git subcommand.
type RecordingRunner struct {
	Inner Runner
	// Before is called with the args of every command before it runs. Return
	// an error to fail the command instead of running it.
	Before func(args []string) error

	mu       sync.Mutex
	Commands [][]string
}

func (r *RecordingRunner) Run(ctx context.Context, dir string, args ...string) (string, error) {
	r.mu.Lock()
	r.Commands = append(r.Commands, append([]string(nil), args...))
	r.mu.Unlock()
	if r.Before != nil {
		if err := r.Before(args); err != nil {
			return "", err
		}
	}
	inner := r.Inner
	if inner == nil {
		inner = ExecRunner{}
	}
	return inner.Run(ctx, dir, args...)
}

// Count returns how many recorded commands start with sub.
func (r *RecordingRunner) Count(sub string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.Commands {
		if subcommand(c) == sub {
			n++
		}
	}
	return n
}

// subcommand skips leading -c key=value pairs.
func subcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-c" {
			i++
			continue
		}
		return args[i]
	}
	return ""
}

// testHub is one clone with its own BEANS_HOME-style cache dir.
type testHub struct {
	*Hub
	Home string
	Rec  *RecordingRunner
}

// newRemote creates a bare repository and returns its path.
func newRemote(t *testing.T) string {
	t.Helper()
	requireGit(t)
	remote := filepath.Join(t.TempDir(), "hub.git")
	runGit(t, "", "init", "--quiet", "--bare", "-b", "main", remote)
	return remote
}

// newClone clones remote into a fresh home and returns the Hub.
func newClone(t *testing.T, remote, name string) *testHub {
	t.Helper()
	home := filepath.Join(t.TempDir(), name)
	rec := &RecordingRunner{}
	dir := filepath.Join(home, "hub")
	branch, err := Clone(context.Background(), rec, remote, dir)
	if err != nil {
		t.Fatalf("clone %s: %v", name, err)
	}
	return &testHub{Home: home, Rec: rec, Hub: &Hub{
		Dir:      dir,
		CacheDir: filepath.Join(home, "cache"),
		Branch:   branch,
		Runner:   rec,
		Throttle: time.Minute,
		Actor:    name,
		Stderr:   &testWriter{t: t},
	}}
}

type testWriter struct{ t *testing.T }

func (w *testWriter) Write(p []byte) (int, error) {
	w.t.Logf("stderr: %s", strings.TrimSpace(string(p)))
	return len(p), nil
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH; gitops tests need the system git")
	}
}

// remoteLog returns the one-line log of the remote's main branch.
func remoteLog(t *testing.T, remote string) []string {
	t.Helper()
	out := runGit(t, remote, "log", "--format=%s", "main")
	return strings.Split(strings.TrimSpace(out), "\n")
}

// setOp returns an operation that writes content to a hub-relative path.
func setOp(verb, id, rel, content string) Operation {
	return Operation{Verb: verb, ID: id, Summary: rel, Apply: func(hubDir string) ([]string, error) {
		p := filepath.Join(hubDir, filepath.FromSlash(rel))
		if cur, err := os.ReadFile(p); err == nil && string(cur) == content {
			return nil, nil // idempotent
		}
		if err := WriteFile(p, []byte(content)); err != nil {
			return nil, err
		}
		return []string{rel}, nil
	}}
}
