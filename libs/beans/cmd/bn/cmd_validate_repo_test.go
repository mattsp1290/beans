package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mattsp1290/beans/model"
	store "github.com/mattsp1290/beans/store"
)

// newTestStore opens a fresh in-memory SQLite store and ensures the given
// prefix exists.  It registers a repo with the provided remote URL if non-empty
// and returns both the store and the registered repo (nil when remoteURL is "").
func newTestStore(t *testing.T, prefix, remoteURL string) (*store.Store, *store.Repo) {
	t.Helper()
	ctx := context.Background()
	s, err := store.New(ctx, store.Config{
		Driver: store.DriverSQLite,
		DSN:    store.SecretDSN(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))),
	})
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(s.Close)

	if prefix != "" {
		if err := s.EnsureProject(ctx, prefix); err != nil {
			t.Fatalf("EnsureProject(%q): %v", prefix, err)
		}
	}

	if remoteURL == "" {
		return s, nil
	}
	repo, err := s.AutoRegisterRepo(ctx, store.AutoRegisterInput{RemoteURL: remoteURL, Actor: "test"})
	if err != nil {
		t.Fatalf("AutoRegisterRepo: %v", err)
	}
	return s, &repo
}

// resolvedRepoOf returns a *store.Repo with only Slug populated — enough for
// warnIfCrossRepo to compare without a real AutoRegisterRepo round-trip.
func resolvedRepoOf(slug string) *store.Repo {
	r := &store.Repo{}
	r.Slug = slug
	return r
}

// ---- warnIfCrossRepo unit tests ----

func TestWarnIfCrossRepoSilentWhenNoResolvedRepo(t *testing.T) {
	t.Parallel()

	rs := &appState{} // resolvedRepo == nil
	iss := store.Issue{
		Issue: model.Issue{
			ID: "proj-abc",
			Repo: &model.RepoTarget{
				Slug: "myapp",
			},
		},
	}
	var buf bytes.Buffer
	warnIfCrossRepo(&buf, rs, iss)
	if buf.Len() > 0 {
		t.Fatalf("warnIfCrossRepo emitted %q, want silent when resolvedRepo is nil", buf.String())
	}
}

func TestWarnIfCrossRepoSilentWhenNoIssueRepo(t *testing.T) {
	t.Parallel()

	rs := &appState{resolvedRepo: resolvedRepoOf("myapp")}
	iss := store.Issue{Issue: model.Issue{ID: "proj-abc"}} // iss.Repo == nil
	var buf bytes.Buffer
	warnIfCrossRepo(&buf, rs, iss)
	if buf.Len() > 0 {
		t.Fatalf("warnIfCrossRepo emitted %q, want silent when issue has no repo", buf.String())
	}
}

func TestWarnIfCrossRepoSilentWhenReposMatch(t *testing.T) {
	t.Parallel()

	rs := &appState{resolvedRepo: resolvedRepoOf("myapp")}
	iss := store.Issue{Issue: model.Issue{
		ID:   "proj-abc",
		Repo: &model.RepoTarget{Slug: "myapp"},
	}}
	var buf bytes.Buffer
	warnIfCrossRepo(&buf, rs, iss)
	if buf.Len() > 0 {
		t.Fatalf("warnIfCrossRepo emitted %q, want silent when repos match", buf.String())
	}
}

func TestWarnIfCrossRepoEmitsWarningOnMismatch(t *testing.T) {
	t.Parallel()

	rs := &appState{resolvedRepo: resolvedRepoOf("ctxrepo")}
	iss := store.Issue{Issue: model.Issue{
		ID:   "proj-abc",
		Repo: &model.RepoTarget{Slug: "otherrepo"},
	}}
	var buf bytes.Buffer
	warnIfCrossRepo(&buf, rs, iss)
	got := buf.String()
	if !strings.Contains(got, "proj-abc") {
		t.Errorf("warning missing issue ID: %q", got)
	}
	if !strings.Contains(got, "otherrepo") {
		t.Errorf("warning missing issue repo slug: %q", got)
	}
	if !strings.Contains(got, "ctxrepo") {
		t.Errorf("warning missing context repo slug: %q", got)
	}
}

// ---- cross-repo ID lookup integration tests ----

// TestCrossRepoPrefixGetIssueSucceeds verifies that GetIssue looks up by
// primary key globally — the issue prefix does NOT filter the result.
func TestCrossRepoPrefixGetIssueSucceeds(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t, "proj-a", "")

	// Create a second project and issue.
	if err := s.EnsureProject(ctx, "proj-b"); err != nil {
		t.Fatalf("EnsureProject proj-b: %v", err)
	}
	iss, err := s.CreateIssue(ctx, store.CreateIssueInput{Prefix: "proj-b", Title: "cross-lookup target", Actor: "test"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	// Lookup the issue from the "proj-a" context — should succeed.
	got, err := s.GetIssue(ctx, iss.ID)
	if err != nil {
		t.Fatalf("GetIssue(%q) from proj-a context: %v — cross-repo lookup must not be filtered", iss.ID, err)
	}
	if got.ID != iss.ID {
		t.Fatalf("GetIssue returned %q, want %q", got.ID, iss.ID)
	}
}

// TestCrossRepoCloseIssueSucceeds verifies that CloseIssue works for an issue
// whose prefix differs from the currently configured context prefix.
func TestCrossRepoCloseIssueSucceeds(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t, "proj-a", "")

	if err := s.EnsureProject(ctx, "proj-b"); err != nil {
		t.Fatalf("EnsureProject proj-b: %v", err)
	}
	iss, err := s.CreateIssue(ctx, store.CreateIssueInput{Prefix: "proj-b", Title: "to be closed", Actor: "test"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	// Close from proj-a context (no prefix filtering in CloseIssue).
	if err := s.CloseIssue(ctx, iss.ID, "test", "done cross-repo"); err != nil {
		t.Fatalf("CloseIssue(%q) from proj-a context: %v — cross-repo must succeed", iss.ID, err)
	}

	got, err := s.GetIssue(ctx, iss.ID)
	if err != nil {
		t.Fatalf("GetIssue after close: %v", err)
	}
	if got.State != "closed" {
		t.Fatalf("state after close = %q, want closed", got.State)
	}
}

// TestCrossRepoDeleteIssueSucceeds verifies that DeleteIssue works for an
// issue whose prefix differs from the currently active context.
func TestCrossRepoDeleteIssueSucceeds(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t, "proj-a", "")

	if err := s.EnsureProject(ctx, "proj-b"); err != nil {
		t.Fatalf("EnsureProject proj-b: %v", err)
	}
	iss, err := s.CreateIssue(ctx, store.CreateIssueInput{Prefix: "proj-b", Title: "to be deleted", Actor: "test"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	if err := s.DeleteIssue(ctx, iss.ID); err != nil {
		t.Fatalf("DeleteIssue(%q) from proj-a context: %v — cross-repo must succeed", iss.ID, err)
	}

	if _, err := s.GetIssue(ctx, iss.ID); err == nil {
		t.Fatal("GetIssue after delete: want ErrNotFound, got nil")
	}
}

// TestWarnIfCrossRepoTriggersWhenRepoAssigned creates an issue with a repo
// assignment and verifies that warnIfCrossRepo fires when the resolved repo
// differs from the issue's assigned repo.
func TestWarnIfCrossRepoTriggersWhenRepoAssigned(t *testing.T) {
	ctx := context.Background()
	s, repoA := newTestStore(t, "", "https://example.com/repo-a.git")
	if repoA == nil {
		t.Fatal("AutoRegisterRepo returned nil")
	}
	if err := s.EnsureProject(ctx, repoA.Prefix); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	// Create an issue assigned to repoA.
	iss, err := s.CreateIssue(ctx, store.CreateIssueInput{
		Prefix: repoA.Prefix,
		Title:  "assigned to repoA",
		Actor:  "test",
		Repo:   &store.IssueRepoInput{RepoSlug: repoA.Slug},
	})
	if err != nil {
		t.Fatalf("CreateIssue with repo: %v", err)
	}

	// Simulate a different resolved repo context.
	otherRepo := resolvedRepoOf("other-repo")
	rs := &appState{resolvedRepo: otherRepo}

	// Fetch the issue — should include Repo field.
	got, err := s.GetIssue(ctx, iss.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}

	var buf bytes.Buffer
	warnIfCrossRepo(&buf, rs, got)
	warning := buf.String()
	if !strings.Contains(warning, repoA.Slug) {
		t.Errorf("warning missing issue repo %q: %s", repoA.Slug, warning)
	}
	if !strings.Contains(warning, "other-repo") {
		t.Errorf("warning missing context repo %q: %s", "other-repo", warning)
	}
}
