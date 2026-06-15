package main

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	store "github.com/mattsp1290/beans/store"
)

// insertMemoryDirect inserts a memory directly via the store (bypassing RunE)
// and fatals on error.
func insertMemoryDirect(t *testing.T, s *store.Store, prefix, body string) store.Memory {
	t.Helper()
	ctx := context.Background()
	mem, err := s.InsertMemory(ctx, store.MemoryInput{Prefix: prefix, Body: body})
	if err != nil {
		t.Fatalf("InsertMemory(prefix=%q, body=%q): %v", prefix, body, err)
	}
	return mem
}

// TestRememberCmdStoresPerRepo verifies that the remember command without
// --global scopes the memory to rs.prefix, making it visible when queried
// from that project but not from another project.
func TestRememberCmdStoresPerRepo(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s, _ := newTestStore(t, "proj-a", "")
	if err := s.EnsureProject(ctx, "proj-b"); err != nil {
		t.Fatalf("EnsureProject(proj-b): %v", err)
	}

	rs := &appState{store: s, actor: "test", prefix: "proj-a", git: &fakeGitResolver{}}
	remCmd := newRememberCmd(rs)
	remCmd.SetOut(new(bytes.Buffer))
	if err := remCmd.RunE(remCmd, []string{"proj-a insight"}); err != nil {
		t.Fatalf("remember RunE: %v", err)
	}

	// Visible from proj-a.
	memsA, err := s.SearchMemories(ctx, "", store.MemoryFilter{Prefix: "proj-a"})
	if err != nil {
		t.Fatalf("SearchMemories(proj-a): %v", err)
	}
	if len(memsA) == 0 {
		t.Fatal("SearchMemories(proj-a): got 0 memories, want 1")
	}
	found := false
	for _, m := range memsA {
		if m.Body == "proj-a insight" {
			found = true
			if m.Prefix == nil || *m.Prefix != "proj-a" {
				t.Errorf("memory prefix = %v, want 'proj-a'", m.Prefix)
			}
		}
	}
	if !found {
		t.Error("memory 'proj-a insight' not found in proj-a search results")
	}

	// Not visible from proj-b alone (SearchMemories with proj-b prefix shows
	// proj-b rows + globals; proj-a row should be absent).
	memsB, err := s.SearchMemories(ctx, "", store.MemoryFilter{Prefix: "proj-b"})
	if err != nil {
		t.Fatalf("SearchMemories(proj-b): %v", err)
	}
	for _, m := range memsB {
		if m.Body == "proj-a insight" {
			t.Error("proj-a memory leaked into proj-b search results")
		}
	}
}

// TestRememberCmdGlobalStoresGlobally verifies that --global stores the memory
// with a NULL prefix, making it visible from any project.
func TestRememberCmdGlobalStoresGlobally(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s, _ := newTestStore(t, "proj-a", "")
	if err := s.EnsureProject(ctx, "proj-b"); err != nil {
		t.Fatalf("EnsureProject(proj-b): %v", err)
	}

	rs := &appState{store: s, actor: "test", prefix: "proj-a", git: &fakeGitResolver{}}
	remCmd := newRememberCmd(rs)
	remCmd.SetOut(new(bytes.Buffer))
	if err := remCmd.Flags().Set("global", "true"); err != nil {
		t.Fatalf("set --global: %v", err)
	}
	if err := remCmd.RunE(remCmd, []string{"global insight"}); err != nil {
		t.Fatalf("remember --global RunE: %v", err)
	}

	// Global memory must have nil prefix in the DB.
	allMems, err := s.SearchMemories(ctx, "", store.MemoryFilter{All: true})
	if err != nil {
		t.Fatalf("SearchMemories(all): %v", err)
	}
	found := false
	for _, m := range allMems {
		if m.Body == "global insight" {
			found = true
			if m.Prefix != nil {
				t.Errorf("global memory has non-nil prefix %q, want nil", *m.Prefix)
			}
		}
	}
	if !found {
		t.Error("global memory not found in SearchMemories(all)")
	}

	// Also visible from proj-b (globals appear in all project-scoped searches).
	memsB, err := s.SearchMemories(ctx, "", store.MemoryFilter{Prefix: "proj-b"})
	if err != nil {
		t.Fatalf("SearchMemories(proj-b): %v", err)
	}
	visibleFromB := false
	for _, m := range memsB {
		if m.Body == "global insight" {
			visibleFromB = true
		}
	}
	if !visibleFromB {
		t.Error("global memory not visible from proj-b scoped search")
	}
}

// TestMemoriesCmdDefaultScopesToCurrentRepo verifies that memories without
// --all returns the current repo's memories + globals but not another repo's.
func TestMemoriesCmdDefaultScopesToCurrentRepo(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s, _ := newTestStore(t, "proj-a", "")
	if err := s.EnsureProject(ctx, "proj-b"); err != nil {
		t.Fatalf("EnsureProject(proj-b): %v", err)
	}

	insertMemoryDirect(t, s, "proj-a", "a only memory")
	insertMemoryDirect(t, s, "proj-b", "b only memory")
	insertMemoryDirect(t, s, "", "global memory") // NULL prefix

	rs := &appState{store: s, actor: "test", prefix: "proj-a", git: &fakeGitResolver{}}
	memCmd := newMemoriesCmd(rs)
	var buf bytes.Buffer
	memCmd.SetOut(&buf)
	if err := memCmd.RunE(memCmd, []string{}); err != nil {
		t.Fatalf("memories RunE: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "a only memory") {
		t.Error("proj-a memory not visible in default scope")
	}
	if !strings.Contains(out, "global memory") {
		t.Error("global memory not visible in default scope")
	}
	if strings.Contains(out, "b only memory") {
		t.Error("proj-b memory leaked into proj-a default scope")
	}

	// Test --all-repos also works via direct store call for the DB invariant.
	allMems, err := s.SearchMemories(ctx, "", store.MemoryFilter{All: true})
	if err != nil {
		t.Fatalf("SearchMemories(all): %v", err)
	}
	bodies := make([]string, 0, len(allMems))
	for _, m := range allMems {
		bodies = append(bodies, m.Body)
	}
	for _, want := range []string{"a only memory", "b only memory", "global memory"} {
		if !slices.Contains(bodies, want) {
			t.Errorf("SearchMemories(all) missing %q; got: %v", want, bodies)
		}
	}
}

// TestMemoriesCmdAllReposAliasMatchesAll verifies that passing --all-repos
// produces the same results as passing --all on the memories command.
func TestMemoriesCmdAllReposAliasMatchesAll(t *testing.T) {
	t.Parallel()

	s, _ := newTestStore(t, "proj-a", "")
	ctx := context.Background()
	if err := s.EnsureProject(ctx, "proj-b"); err != nil {
		t.Fatalf("EnsureProject(proj-b): %v", err)
	}

	insertMemoryDirect(t, s, "proj-a", "memory from a")
	insertMemoryDirect(t, s, "proj-b", "memory from b")

	run := func(t *testing.T, flagName string) string {
		t.Helper()
		rs := &appState{store: s, actor: "test", prefix: "proj-a", git: &fakeGitResolver{}}
		cmd := newMemoriesCmd(rs)
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		if err := cmd.Flags().Set(flagName, "true"); err != nil {
			t.Fatalf("set --%s: %v", flagName, err)
		}
		if err := cmd.RunE(cmd, []string{}); err != nil {
			t.Fatalf("memories --%s RunE: %v", flagName, err)
		}
		return buf.String()
	}

	outAll := run(t, "all")
	outAllRepos := run(t, "all-repos")

	if outAll != outAllRepos {
		t.Errorf("--all and --all-repos produced different output:\n--all:\n%s\n--all-repos:\n%s", outAll, outAllRepos)
	}
	if !strings.Contains(outAll, "memory from a") || !strings.Contains(outAll, "memory from b") {
		t.Errorf("--all output missing memories: %q", outAll)
	}
}
