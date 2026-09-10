package vault

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type changeRecorder struct {
	mu    sync.Mutex
	calls [][]string
}

func (r *changeRecorder) onChange(paths []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := append([]string(nil), paths...)
	r.calls = append(r.calls, cp)
}

func (r *changeRecorder) snapshot() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.calls...)
}

func startWatch(t *testing.T, dir string, rec *changeRecorder) context.CancelFunc {
	t.Helper()
	ix, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- Watch(ctx, ix, rec.onChange)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Error("Watch did not return after ctx cancellation")
		}
	})
	// Give the watcher time to register its directories before the test
	// starts writing files.
	time.Sleep(50 * time.Millisecond)
	return cancel
}

func TestWatchSingleWrite(t *testing.T) {
	dir := copyFixtureHub(t)
	rec := &changeRecorder{}
	startWatch(t, dir, rec)

	target := filepath.Join(dir, "projects", "a", "issues", "a-open001.md")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.After(1 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline:
			t.Fatalf("no onChange call within 1s, calls so far: %v", rec.snapshot())
		case <-tick.C:
			calls := rec.snapshot()
			if len(calls) == 0 {
				continue
			}
			for _, paths := range calls {
				for _, p := range paths {
					if p == "projects/a/issues/a-open001.md" {
						return
					}
				}
			}
		}
	}
}

func TestWatchDebouncesBurst(t *testing.T) {
	dir := copyFixtureHub(t)
	rec := &changeRecorder{}
	startWatch(t, dir, rec)

	docsDir := filepath.Join(dir, "docs")
	names := []string{"burst-a.md", "burst-b.md", "burst-c.md"}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(docsDir, n), []byte("# "+n+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", n, err)
		}
		time.Sleep(10 * time.Millisecond) // three writes inside ~30ms, well under the 100ms burst window
	}

	// Wait for the debounce window (200ms) plus slack, then make sure no
	// further calls land after that.
	time.Sleep(500 * time.Millisecond)

	calls := rec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one onChange call for the burst, got %d: %v", len(calls), calls)
	}
	got := map[string]bool{}
	for _, p := range calls[0] {
		got[p] = true
	}
	for _, n := range names {
		if !got["docs/"+n] {
			t.Errorf("expected docs/%s in the single onChange call, got %v", n, calls[0])
		}
	}
}
