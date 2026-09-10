package vault

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const dupIssue = `---
id: b-dup001
title: Duplicate basename
type: task
status: open
priority: 2
created: 2026-01-01T00:00:00Z
updated: 2026-01-01T00:00:00Z
---
body
`

func TestDuplicateBasenameKeepsEveryIssue(t *testing.T) {
	dir := copyFixtureHub(t)
	// projects/b/issues/a-open001.md collides with project a's basename.
	dupPath := filepath.Join(dir, "projects", "b", "issues", "a-open001.md")
	writeFile(t, dupPath, dupIssue)
	writeFile(t, filepath.Join(dir, "projects", "b", "docs", "parity.md"), "# B parity\n")
	ix, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ix.Issues["b-dup001"]; !ok {
		t.Fatal("the losing file's issue must still be indexed by id")
	}
	if n, ok := ix.Lookup("a-open001"); !ok || n.Project != "a" {
		t.Fatalf("basename must resolve to the first in walk order: %+v", n)
	}
	if n, ok := ix.Lookup("projects/b/docs/parity.md"); !ok || n.Path != "projects/b/docs/parity.md" {
		t.Fatalf("path-qualified lookup must not fall back to another note: %+v", n)
	}
	if n, ok := ix.Lookup("b/docs/parity"); !ok || n.Path != "projects/b/docs/parity.md" {
		t.Fatalf("unique path suffix must resolve: %+v", n)
	}
	dups := 0
	for _, w := range ix.Warnings {
		if strings.Contains(w.Err.Error(), "duplicate note basename") {
			dups++
		}
	}
	if dups != 2 {
		t.Fatalf("expected 2 duplicate warnings, got %d: %v", dups, ix.Warnings)
	}
	// Removing the winner promotes the loser.
	if err := os.Remove(filepath.Join(dir, "projects", "a", "issues", "a-open001.md")); err != nil {
		t.Fatal(err)
	}
	if err := ix.Reload(filepath.Join(dir, "projects", "a", "issues", "a-open001.md")); err != nil {
		t.Fatal(err)
	}
	if n, ok := ix.Lookup("a-open001"); !ok || n.Project != "b" || n.Issue.ID != "b-dup001" {
		t.Fatalf("loser must take over the basename: %+v", n)
	}
	if _, ok := ix.Issues["a-open001"]; ok {
		t.Fatal("removed issue still indexed")
	}
}

func TestReloadBeansTOMLRefreshesWorkflow(t *testing.T) {
	ix, dir := loadFixture(t)
	if ix.WorkflowFor("a").IsActive("blocked") {
		t.Fatal("precondition: blocked is a hold state")
	}
	tomlPath := filepath.Join(dir, "projects", "a", "beans.toml")
	data, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, tomlPath, string(data)+"\n[workflow]\nactive = [\"open\", \"blocked\"]\n")
	if err := ix.Reload(tomlPath); err != nil {
		t.Fatal(err)
	}
	if !ix.WorkflowFor("a").IsActive("blocked") {
		t.Fatal("project workflow not refreshed after beans.toml reload")
	}
	if len(ix.Issues) == 0 || len(ix.Notes) == 0 {
		t.Fatal("full reload lost the notes")
	}
}

func TestReloadRejectsPathsOutsideHub(t *testing.T) {
	ix, _ := loadFixture(t)
	if err := ix.Reload(filepath.Join(t.TempDir(), "elsewhere.md")); err == nil {
		t.Fatal("a path outside the hub must be an error")
	}
	if err := ix.Reload("../escape.md"); err == nil {
		t.Fatal("a relative path escaping the hub must be an error")
	}
}

func TestConcurrentReloadAndLockedReads(t *testing.T) {
	ix, dir := loadFixture(t)
	path := filepath.Join(dir, "projects", "a", "issues", "a-open001.md")
	data, _ := os.ReadFile(path)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			ix.RLock()
			_ = ix.Ready("b", false)
			_, _ = ix.Lookup("parity")
			_ = len(ix.Notes)
			ix.RUnlock()
		}
	}()
	for i := 0; i < 20; i++ {
		writeFile(t, path, string(data))
		if err := ix.Reload(path); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
}

func TestWatchPicksUpBeansTOMLAndNewDirectoryContents(t *testing.T) {
	ix, dir := loadFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan []string, 10)
	go func() { _ = Watch(ctx, ix, func(paths []string) { events <- paths }) }()
	time.Sleep(150 * time.Millisecond) // let the watcher arm

	writeFile(t, filepath.Join(dir, "projects", "a", "beans.toml"), "name = \"a\"\nprefix = \"a\"\nremotes = []\n\n[workflow]\nactive = [\"open\", \"blocked\"]\n")
	select {
	case paths := <-events:
		if len(paths) != 1 || paths[0] != "projects/a/beans.toml" {
			t.Fatalf("paths = %v", paths)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event for beans.toml")
	}
	ix.RLock()
	active := ix.WorkflowFor("a").IsActive("blocked")
	ix.RUnlock()
	if !active {
		t.Fatal("workflow not reloaded from the watcher")
	}

	newDir := filepath.Join(dir, "projects", "a", "docs", "sub")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(newDir, "late.md"), "# Late\n")
	select {
	case paths := <-events:
		found := false
		for _, p := range paths {
			if p == "projects/a/docs/sub/late.md" {
				found = true
			}
		}
		if !found {
			t.Fatalf("new directory contents missed: %v", paths)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event for the new directory")
	}
}
