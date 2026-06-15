package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	store "github.com/mattsp1290/beans/store"
)

func TestCleanRepoSlug(t *testing.T) {
	t.Parallel()

	for _, slug := range []string{"boxy", "birbparty.clckr", "shady-api", "repo_1"} {
		got, err := cleanRepoSlug(slug)
		if err != nil {
			t.Fatalf("cleanRepoSlug(%q): %v", slug, err)
		}
		if got != slug {
			t.Fatalf("cleanRepoSlug(%q) = %q, want same", slug, got)
		}
	}

	for _, slug := range []string{"", "Boxy", " boxy", "boxy ", "-boxy", "boxy repo"} {
		if _, err := cleanRepoSlug(slug); err == nil {
			t.Fatalf("cleanRepoSlug(%q): want error", slug)
		}
	}
}

func TestRootCommandIncludesRepoCommand(t *testing.T) {
	t.Parallel()

	root := newRootCmd(&appState{})
	for _, cmd := range root.Commands() {
		if cmd.Name() == "repo" {
			return
		}
	}
	t.Fatal("root command missing repo subcommand")
}

func TestRepoCommandIncludesDoctor(t *testing.T) {
	t.Parallel()

	repoCmd := newRepoCmd(&appState{})
	for _, cmd := range repoCmd.Commands() {
		if cmd.Name() == "doctor" {
			if cmd.Flags().Lookup("from-orchestrator") == nil {
				t.Fatal("repo doctor missing --from-orchestrator flag")
			}
			if cmd.Flags().Lookup("allowed-host") == nil {
				t.Fatal("repo doctor missing --allowed-host flag")
			}
			return
		}
	}
	t.Fatal("repo command missing doctor subcommand")
}

func TestUpdateCommandIncludesRepoRoutingFlags(t *testing.T) {
	t.Parallel()

	cmd := newUpdateCmd(&appState{})
	for _, name := range []string{"repo", "ref", "subdir", "force"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("update command missing --%s flag", name)
		}
	}
}

// bootstrapRepoAdmin registers actor as the first admin for prefix (bootstrap mode).
func bootstrapRepoAdmin(t *testing.T, s *store.Store, prefix, actor string) {
	t.Helper()
	if err := s.AddRepoAdmin(context.Background(), prefix, actor, "", true); err != nil {
		t.Fatalf("AddRepoAdmin bootstrap(%q, %q): %v", prefix, actor, err)
	}
}

// TestRepoRemoveSoftDisablePreservesIssues verifies that repo remove without
// --purge soft-disables the repo and leaves issues intact.
func TestRepoRemoveSoftDisablePreservesIssues(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s, r := newTestStore(t, "", "https://github.com/alice/api")
	if r == nil {
		t.Fatal("newTestStore did not register repo")
	}
	if err := s.EnsureProject(ctx, r.Prefix); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	bootstrapRepoAdmin(t, s, r.Prefix, "test")
	mustCreateIssue(t, s, r.Prefix, "task one", r)

	rs := &appState{store: s, actor: "test", prefix: r.Prefix, git: &fakeGitResolver{}}
	cmd := newRepoRemoveCmd(rs)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.RunE(cmd, []string{r.Slug}); err != nil {
		t.Fatalf("repo remove RunE: %v", err)
	}

	if !strings.Contains(buf.String(), "Disabled") {
		t.Errorf("expected 'Disabled' in output, got: %q", buf.String())
	}

	// Issues must still exist after soft-disable.
	issues, err := s.ListIssues(ctx, store.ListFilter{Prefix: r.Prefix})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) == 0 {
		t.Fatal("issues were deleted by soft-disable; expected them to be preserved")
	}
}

// TestRepoRemovePurgeNoIssuesSucceeds verifies that repo remove --purge with
// no issues deletes the project cleanly.
func TestRepoRemovePurgeNoIssuesSucceeds(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s, r := newTestStore(t, "", "https://github.com/alice/api")
	if r == nil {
		t.Fatal("newTestStore did not register repo")
	}
	if err := s.EnsureProject(ctx, r.Prefix); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	bootstrapRepoAdmin(t, s, r.Prefix, "test")

	rs := &appState{store: s, actor: "test", prefix: r.Prefix, git: &fakeGitResolver{}}
	cmd := newRepoRemoveCmd(rs)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Flags().Set("purge", "true"); err != nil {
		t.Fatalf("set --purge: %v", err)
	}
	if err := cmd.RunE(cmd, []string{r.Slug}); err != nil {
		t.Fatalf("repo remove --purge RunE: %v", err)
	}

	if !strings.Contains(buf.String(), "Purged") {
		t.Errorf("expected 'Purged' in output, got: %q", buf.String())
	}

	// Project must no longer exist.
	exists, err := s.ProjectExists(ctx, r.Prefix)
	if err != nil {
		t.Fatalf("ProjectExists: %v", err)
	}
	if exists {
		t.Fatal("project still exists after --purge")
	}
}

// TestRepoRemovePurgeRefusesWhenIssuesExist verifies that --purge without
// --force returns an error when the project has issues.
func TestRepoRemovePurgeRefusesWhenIssuesExist(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s, r := newTestStore(t, "", "https://github.com/alice/api")
	if r == nil {
		t.Fatal("newTestStore did not register repo")
	}
	if err := s.EnsureProject(ctx, r.Prefix); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	bootstrapRepoAdmin(t, s, r.Prefix, "test")
	mustCreateIssue(t, s, r.Prefix, "existing task", r)

	rs := &appState{store: s, actor: "test", prefix: r.Prefix, git: &fakeGitResolver{}}
	cmd := newRepoRemoveCmd(rs)
	cmd.SetOut(new(bytes.Buffer))
	if err := cmd.Flags().Set("purge", "true"); err != nil {
		t.Fatalf("set --purge: %v", err)
	}

	err := cmd.RunE(cmd, []string{r.Slug})
	if err == nil {
		t.Fatal("expected error when purging project with issues and no --force")
	}
	if !strings.Contains(err.Error(), "issue") {
		t.Errorf("expected error to mention 'issue', got: %v", err)
	}
}

// TestRepoRemovePurgeForceDeletesIssues verifies that --purge --force deletes
// the project and all its issues even when issues exist.
func TestRepoRemovePurgeForceDeletesIssues(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s, r := newTestStore(t, "", "https://github.com/alice/api")
	if r == nil {
		t.Fatal("newTestStore did not register repo")
	}
	if err := s.EnsureProject(ctx, r.Prefix); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	bootstrapRepoAdmin(t, s, r.Prefix, "test")
	mustCreateIssue(t, s, r.Prefix, "task to delete", r)
	mustCreateIssue(t, s, r.Prefix, "another task", r)

	rs := &appState{store: s, actor: "test", prefix: r.Prefix, git: &fakeGitResolver{}}
	cmd := newRepoRemoveCmd(rs)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Flags().Set("purge", "true"); err != nil {
		t.Fatalf("set --purge: %v", err)
	}
	if err := cmd.Flags().Set("force", "true"); err != nil {
		t.Fatalf("set --force: %v", err)
	}
	if err := cmd.RunE(cmd, []string{r.Slug}); err != nil {
		t.Fatalf("repo remove --purge --force RunE: %v", err)
	}

	// Project must no longer exist.
	exists, err := s.ProjectExists(ctx, r.Prefix)
	if err != nil {
		t.Fatalf("ProjectExists: %v", err)
	}
	if exists {
		t.Fatal("project still exists after --purge --force")
	}
}
