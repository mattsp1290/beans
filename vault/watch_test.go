package vault

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

type changeRecorder struct {
	mu    sync.Mutex
	calls [][]string
}

func (r *changeRecorder) onChange(paths []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, append([]string(nil), paths...))
}

func (r *changeRecorder) snapshot() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.calls...)
}

func startWatch(t *testing.T, dir string, rec *changeRecorder) {
	t.Helper()
	ix, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- Watch(ctx, ix, rec.onChange) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Error("Watch did not return after ctx cancellation")
		}
	})
	time.Sleep(50 * time.Millisecond)
}

func TestWatchSingleWrite(t *testing.T) {
	dir := copyFixtureHub(t)
	rec := &changeRecorder{}
	startWatch(t, dir, rec)
	target := filepath.Join(dir, "projects", "a", "issues", "a-open001.md")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("no onChange call: %v", rec.snapshot())
		default:
			for _, paths := range rec.snapshot() {
				for _, p := range paths {
					if p == "projects/a/issues/a-open001.md" {
						return
					}
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestWatchDebouncesBurst(t *testing.T) {
	dir := copyFixtureHub(t)
	rec := &changeRecorder{}
	startWatch(t, dir, rec)
	for _, n := range []string{"burst-a.md", "burst-b.md", "burst-c.md"} {
		if err := os.WriteFile(filepath.Join(dir, "docs", n), []byte("# "+n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)
	if calls := rec.snapshot(); len(calls) != 1 {
		t.Fatalf("expected one onChange call, got %d: %v", len(calls), calls)
	}
}

func TestWatchedPlanRootMapsSectionAndRemovedDirectory(t *testing.T) {
	hub := t.TempDir()
	section := filepath.Join(hub, "projects", "p", "plans", "p-plan-a3f2-test", "sections", "one.md")
	root, ok := watchedPlanRoot(hub, section)
	if !ok || root != "projects/p/plans/p-plan-a3f2-test" {
		t.Fatalf("section root = %q, %v", root, ok)
	}
	pending := map[string]bool{}
	if !handleEvent(nil, hub, fsnotify.Event{Name: filepath.Join(hub, "projects", "p", "plans", "p-plan-a3f2-test"), Op: fsnotify.Remove}, pending) {
		t.Fatal("directory removal was ignored")
	}
	if !pending["projects/p/plans/p-plan-a3f2-test/plan.md"] {
		t.Fatalf("pending = %#v", pending)
	}
}
