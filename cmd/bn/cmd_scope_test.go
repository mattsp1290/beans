package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	store "github.com/mattsp1290/beans/store"
)

// mustCreateIssue creates an issue and fatals on error.
func mustCreateIssue(t *testing.T, s *store.Store, prefix, title string, repo *store.Repo) store.Issue {
	t.Helper()
	ctx := context.Background()
	in := store.CreateIssueInput{
		Prefix: prefix,
		Title:  title,
		Actor:  "test",
	}
	if repo != nil {
		in.Repo = &store.IssueRepoInput{RepoSlug: repo.Slug}
	}
	iss, err := s.CreateIssue(ctx, in)
	if err != nil {
		t.Fatalf("CreateIssue(%q, %q): %v", prefix, title, err)
	}
	return iss
}

// TestCreateAutoDetectsAndLinksRepo verifies that bn create, with no --repo
// flag and no .bn marker, auto-detects the git repo via fakeGitResolver,
// registers it, and records the issue against that repo.
func TestCreateAutoDetectsAndLinksRepo(t *testing.T) {
	ctx := context.Background()

	// Register the repo first so we know its slug, then set rs.prefix = slug.
	s, repo := newTestStore(t, "", "https://github.com/alice/myapp")
	if repo == nil {
		t.Fatal("newTestStore did not register repo")
	}
	// EnsureProject is called by RunE, but we need the prefix to match.
	if err := s.EnsureProject(ctx, repo.Prefix); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	rs := &appState{
		store:  s,
		prefix: repo.Prefix,
		actor:  "test",
		git: &fakeGitResolver{
			toplevel:  "/home/alice/myapp",
			remoteURL: "https://github.com/alice/myapp",
		},
	}

	cmd := newCreateCmd(rs)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Flags().Set("silent", "true"); err != nil {
		t.Fatalf("set --silent: %v", err)
	}

	if err := cmd.RunE(cmd, []string{"linked-repo-issue"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	issueID := strings.TrimSpace(buf.String())
	if issueID == "" {
		t.Fatal("no issue ID in output")
	}

	got, err := s.GetIssue(ctx, issueID)
	if err != nil {
		t.Fatalf("GetIssue(%q): %v", issueID, err)
	}
	if got.Repo == nil {
		t.Fatal("issue.Repo is nil — auto-detect did not link the repo")
	}
	if got.Repo.Slug != repo.Slug {
		t.Fatalf("issue.Repo.Slug = %q, want %q", got.Repo.Slug, repo.Slug)
	}
}

// TestCreateOutsideGitRepoCreatesUnlinkedIssue verifies that create succeeds
// when not in a git repo and produces an issue with no repo link.
func TestCreateOutsideGitRepoCreatesUnlinkedIssue(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t, "myapp", "")

	rs := &appState{
		store:  s,
		prefix: "myapp",
		actor:  "test",
		git:    &fakeGitResolver{toplevel: ""}, // not in a git repo
	}

	cmd := newCreateCmd(rs)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Flags().Set("silent", "true"); err != nil {
		t.Fatalf("set --silent: %v", err)
	}

	if err := cmd.RunE(cmd, []string{"no-repo-issue"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	issueID := strings.TrimSpace(buf.String())
	got, err := s.GetIssue(ctx, issueID)
	if err != nil {
		t.Fatalf("GetIssue(%q): %v", issueID, err)
	}
	if got.Repo != nil {
		t.Fatalf("issue.Repo = %v, want nil (no git repo)", got.Repo)
	}
}

// TestCreatePrefixGuardSkipsCrossProjectRepo verifies that when the auto-
// detected repo belongs to a different project (prefix mismatch), it is NOT
// linked to the issue.
func TestCreatePrefixGuardSkipsCrossProjectRepo(t *testing.T) {
	ctx := context.Background()

	// Register repo under a DIFFERENT prefix than the current project.
	s, foreignRepo := newTestStore(t, "", "https://github.com/foreign/service")
	if foreignRepo == nil {
		t.Fatal("newTestStore did not register repo")
	}
	// Set up a project with a different prefix than the foreign repo.
	const myProject = "my-project"
	if err := s.EnsureProject(ctx, myProject); err != nil {
		t.Fatalf("EnsureProject %q: %v", myProject, err)
	}

	rs := &appState{
		store:  s,
		prefix: myProject, // does NOT match foreignRepo.Prefix
		actor:  "test",
		git: &fakeGitResolver{
			toplevel:  "/home/alice/service",
			remoteURL: "https://github.com/foreign/service",
		},
	}

	cmd := newCreateCmd(rs)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Flags().Set("silent", "true"); err != nil {
		t.Fatalf("set --silent: %v", err)
	}

	if err := cmd.RunE(cmd, []string{"cross-project-issue"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	issueID := strings.TrimSpace(buf.String())
	got, err := s.GetIssue(ctx, issueID)
	if err != nil {
		t.Fatalf("GetIssue(%q): %v", issueID, err)
	}
	// Prefix guard must block linking the foreign repo.
	if got.Repo != nil {
		t.Fatalf("issue.Repo = %v, want nil (prefix guard should block cross-project repo)", got.Repo)
	}
}

// TestListScopesToCurrentRepo verifies that bn list without --all-repos returns
// only issues belonging to the current project prefix, not issues from another repo.
func TestListScopesToCurrentRepo(t *testing.T) {
	ctx := context.Background()

	// Two repos in the same store.
	s, repoA := newTestStore(t, "", "https://github.com/alice/repo-a")
	if repoA == nil {
		t.Fatal("newTestStore did not register repoA")
	}
	repoB, err := s.AutoRegisterRepo(ctx, store.AutoRegisterInput{
		RemoteURL: "https://github.com/alice/repo-b",
		Actor:     "test",
	})
	if err != nil {
		t.Fatalf("AutoRegisterRepo repoB: %v", err)
	}

	// Ensure both projects exist.
	if err := s.EnsureProject(ctx, repoA.Prefix); err != nil {
		t.Fatalf("EnsureProject A: %v", err)
	}
	if err := s.EnsureProject(ctx, repoB.Prefix); err != nil {
		t.Fatalf("EnsureProject B: %v", err)
	}

	issA := mustCreateIssue(t, s, repoA.Prefix, "issue-in-A", repoA)
	_ = mustCreateIssue(t, s, repoB.Prefix, "issue-in-B", &repoB)

	// List with rs.prefix = repoA.Prefix; allRepos defaults to false.
	rs := &appState{
		store:  s,
		prefix: repoA.Prefix,
		actor:  "test",
		git:    &fakeGitResolver{},
	}
	cmd := newListCmd(rs)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	// --all to bypass the page cap so we see all results.
	if err := cmd.Flags().Set("all", "true"); err != nil {
		t.Fatalf("set --all: %v", err)
	}

	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("list RunE: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, issA.ID) {
		t.Errorf("list output missing repoA issue %q", issA.ID)
	}
	if strings.Contains(out, "issue-in-B") {
		t.Error("list output unexpectedly includes issue-in-B from repoB")
	}
}

// TestListAllReposReturnsAllIssues verifies that --all-repos bypasses the repo
// scope and returns issues from every project.
func TestListAllReposReturnsAllIssues(t *testing.T) {
	ctx := context.Background()

	s, repoA := newTestStore(t, "", "https://github.com/alice/all-a")
	if repoA == nil {
		t.Fatal("newTestStore did not register repoA")
	}
	repoB, err := s.AutoRegisterRepo(ctx, store.AutoRegisterInput{
		RemoteURL: "https://github.com/alice/all-b",
		Actor:     "test",
	})
	if err != nil {
		t.Fatalf("AutoRegisterRepo repoB: %v", err)
	}

	if err := s.EnsureProject(ctx, repoA.Prefix); err != nil {
		t.Fatalf("EnsureProject A: %v", err)
	}
	if err := s.EnsureProject(ctx, repoB.Prefix); err != nil {
		t.Fatalf("EnsureProject B: %v", err)
	}

	issA := mustCreateIssue(t, s, repoA.Prefix, "issue-all-A", repoA)
	issB := mustCreateIssue(t, s, repoB.Prefix, "issue-all-B", &repoB)

	// List scoped to repoA but with --all-repos override.
	rs := &appState{
		store:  s,
		prefix: repoA.Prefix,
		actor:  "test",
		git:    &fakeGitResolver{},
	}
	cmd := newListCmd(rs)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Flags().Set("all-repos", "true"); err != nil {
		t.Fatalf("set --all-repos: %v", err)
	}
	if err := cmd.Flags().Set("all", "true"); err != nil {
		t.Fatalf("set --all: %v", err)
	}

	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("list --all-repos RunE: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, issA.ID) {
		t.Errorf("--all-repos list missing issue from repoA: %q not in output", issA.ID)
	}
	if !strings.Contains(out, issB.ID) {
		t.Errorf("--all-repos list missing issue from repoB: %q not in output", issB.ID)
	}
}

// TestReadyScopesToCurrentRepo verifies that bn ready without --all-repos
// returns only unblocked open issues for the current project prefix.
func TestReadyScopesToCurrentRepo(t *testing.T) {
	ctx := context.Background()

	s, repoA := newTestStore(t, "", "https://github.com/alice/ready-a")
	if repoA == nil {
		t.Fatal("newTestStore did not register repoA")
	}
	repoB, err := s.AutoRegisterRepo(ctx, store.AutoRegisterInput{
		RemoteURL: "https://github.com/alice/ready-b",
		Actor:     "test",
	})
	if err != nil {
		t.Fatalf("AutoRegisterRepo repoB: %v", err)
	}

	if err := s.EnsureProject(ctx, repoA.Prefix); err != nil {
		t.Fatalf("EnsureProject A: %v", err)
	}
	if err := s.EnsureProject(ctx, repoB.Prefix); err != nil {
		t.Fatalf("EnsureProject B: %v", err)
	}

	issA := mustCreateIssue(t, s, repoA.Prefix, "ready-issue-A", repoA)
	_ = mustCreateIssue(t, s, repoB.Prefix, "ready-issue-B", &repoB)

	rs := &appState{
		store:  s,
		prefix: repoA.Prefix,
		actor:  "test",
		git:    &fakeGitResolver{},
	}
	cmd := newReadyCmd(rs)
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("ready RunE: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, issA.ID) {
		t.Errorf("ready output missing repoA issue %q", issA.ID)
	}
	if strings.Contains(out, "ready-issue-B") {
		t.Error("ready output unexpectedly includes issue from repoB")
	}
}

// TestReadyAllReposReturnsAllUnblocked verifies that --all-repos on ready
// returns unblocked open issues across all projects.
func TestReadyAllReposReturnsAllUnblocked(t *testing.T) {
	ctx := context.Background()

	s, repoA := newTestStore(t, "", "https://github.com/alice/rall-a")
	if repoA == nil {
		t.Fatal("newTestStore did not register repoA")
	}
	repoB, err := s.AutoRegisterRepo(ctx, store.AutoRegisterInput{
		RemoteURL: "https://github.com/alice/rall-b",
		Actor:     "test",
	})
	if err != nil {
		t.Fatalf("AutoRegisterRepo repoB: %v", err)
	}

	if err := s.EnsureProject(ctx, repoA.Prefix); err != nil {
		t.Fatalf("EnsureProject A: %v", err)
	}
	if err := s.EnsureProject(ctx, repoB.Prefix); err != nil {
		t.Fatalf("EnsureProject B: %v", err)
	}

	issA := mustCreateIssue(t, s, repoA.Prefix, "rall-A", repoA)
	issB := mustCreateIssue(t, s, repoB.Prefix, "rall-B", &repoB)

	rs := &appState{
		store:  s,
		prefix: repoA.Prefix,
		actor:  "test",
		git:    &fakeGitResolver{},
	}
	cmd := newReadyCmd(rs)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Flags().Set("all-repos", "true"); err != nil {
		t.Fatalf("set --all-repos: %v", err)
	}

	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("ready --all-repos RunE: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, issA.ID) {
		t.Errorf("ready --all-repos missing issA %q", issA.ID)
	}
	if !strings.Contains(out, issB.ID) {
		t.Errorf("ready --all-repos missing issB %q", issB.ID)
	}
}

// TestListRepoArgSlugFiltersToThatRepo verifies that when rs.repoArg is a slug
// of an existing repo, list results are scoped to that repo only.
func TestListRepoArgSlugFiltersToThatRepo(t *testing.T) {
	ctx := context.Background()

	s, repoA := newTestStore(t, "", "https://github.com/alice/slug-a")
	if repoA == nil {
		t.Fatal("newTestStore did not register repoA")
	}
	repoB, err := s.AutoRegisterRepo(ctx, store.AutoRegisterInput{
		RemoteURL: "https://github.com/alice/slug-b",
		Actor:     "test",
	})
	if err != nil {
		t.Fatalf("AutoRegisterRepo repoB: %v", err)
	}

	if err := s.EnsureProject(ctx, repoA.Prefix); err != nil {
		t.Fatalf("EnsureProject A: %v", err)
	}
	if err := s.EnsureProject(ctx, repoB.Prefix); err != nil {
		t.Fatalf("EnsureProject B: %v", err)
	}

	issA := mustCreateIssue(t, s, repoA.Prefix, "slug-issue-A", repoA)
	_ = mustCreateIssue(t, s, repoB.Prefix, "slug-issue-B", &repoB)

	// Scope to repoA via --repo slug even though rs.prefix points to repoB.
	rs := &appState{
		store:   s,
		prefix:  repoB.Prefix,
		repoArg: repoA.Slug,
		actor:   "test",
		git:     &fakeGitResolver{},
	}
	cmd := newListCmd(rs)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Flags().Set("all", "true"); err != nil {
		t.Fatalf("set --all: %v", err)
	}

	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("list --repo slug RunE: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, issA.ID) {
		t.Errorf("list --repo=%s missing issA %q", repoA.Slug, issA.ID)
	}
	if strings.Contains(out, "slug-issue-B") {
		t.Errorf("list --repo=%s unexpectedly includes issue from repoB", repoA.Slug)
	}
}

// TestIDAddressedLookupIsCrossRepo verifies that GetIssue (used by bn show,
// update, close, delete) does not filter by prefix — ID-addressed commands
// remain cross-repo addressable.
func TestIDAddressedLookupIsCrossRepo(t *testing.T) {
	ctx := context.Background()

	s, repoA := newTestStore(t, "", "https://github.com/alice/id-a")
	if repoA == nil {
		t.Fatal("newTestStore did not register repoA")
	}
	repoB, err := s.AutoRegisterRepo(ctx, store.AutoRegisterInput{
		RemoteURL: "https://github.com/alice/id-b",
		Actor:     "test",
	})
	if err != nil {
		t.Fatalf("AutoRegisterRepo repoB: %v", err)
	}

	if err := s.EnsureProject(ctx, repoA.Prefix); err != nil {
		t.Fatalf("EnsureProject A: %v", err)
	}
	if err := s.EnsureProject(ctx, repoB.Prefix); err != nil {
		t.Fatalf("EnsureProject B: %v", err)
	}

	// Create an issue in repoB.
	issB := mustCreateIssue(t, s, repoB.Prefix, "cross-repo-lookup", &repoB)

	// Get the issue while the "current context" is repoA — must succeed.
	got, err := s.GetIssue(ctx, issB.ID)
	if err != nil {
		t.Fatalf("GetIssue(%q) cross-repo: %v — ID lookup must not filter by prefix", issB.ID, err)
	}
	if got.ID != issB.ID {
		t.Fatalf("GetIssue returned %q, want %q", got.ID, issB.ID)
	}
}
