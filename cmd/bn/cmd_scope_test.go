package main

import (
	"bytes"
	"context"
	"encoding/json"
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
//
// NOTE: readActiveProjectConfig("") is called inside RunE and walks up from the
// test CWD (cmd/bn/) looking for a .bn marker.  There is no .bn file at the
// beans repo root, so priority-2 resolution falls through to git auto-detect.
// If a .bn marker is ever created at the repo root, this test will change
// meaning — add t.Chdir(t.TempDir()) to make it hermetic at that point.
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
			toplevel:   "/home/alice/myapp",
			remoteURL:  "https://github.com/alice/myapp",
			headCommit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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
	if got.Repo.CreationCommit != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("issue.Repo.CreationCommit = %q, want cwd HEAD", got.Repo.CreationCommit)
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

	// Positive control: auto-detect must have actually run and found the foreign
	// repo.  Without this check the test would pass even if tryGitAutoDetect
	// silently no-ops (e.g. if the guard code were deleted).
	if rs.resolvedRepo == nil {
		t.Fatal("resolvedRepo is nil — tryGitAutoDetect did not run; prefix guard was never exercised")
	}
	if rs.resolvedRepo.Prefix == myProject {
		t.Fatalf("resolvedRepo.Prefix = %q == myProject — test setup error: not a cross-project scenario", myProject)
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
	issB := mustCreateIssue(t, s, repoB.Prefix, "issue-in-B", &repoB)

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
	if strings.Contains(out, issB.ID) {
		t.Errorf("list output unexpectedly includes issue from repoB: %q", issB.ID)
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

func TestChildrenListsParentChildMembers(t *testing.T) {
	ctx := context.Background()
	s, repo := newTestStore(t, "", "https://github.com/alice/children-a")
	if repo == nil {
		t.Fatal("newTestStore did not register repo")
	}
	if err := s.EnsureProject(ctx, repo.Prefix); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	epic, err := s.CreateIssue(ctx, store.CreateIssueInput{Prefix: repo.Prefix, Title: "Epic", Priority: 0, IssueType: "epic", Repo: &store.IssueRepoInput{RepoSlug: repo.Slug}})
	if err != nil {
		t.Fatalf("CreateIssue epic: %v", err)
	}
	child := mustCreateIssue(t, s, repo.Prefix, "child-A", repo)
	if err := s.AddTypedDep(ctx, child.ID, epic.ID, store.DepTypeParentChild); err != nil {
		t.Fatalf("AddTypedDep: %v", err)
	}

	rs := &appState{
		store:  s,
		prefix: repo.Prefix,
		actor:  "test",
		git:    &fakeGitResolver{},
	}
	cmd := newChildrenCmd(rs)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.RunE(cmd, []string{epic.ID}); err != nil {
		t.Fatalf("children RunE: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, child.ID) {
		t.Fatalf("children output missing child %q: %s", child.ID, out)
	}
	if strings.Contains(out, epic.ID) {
		t.Fatalf("children output included parent %q: %s", epic.ID, out)
	}
}

func TestChildrenJSONOutput(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t, "children-json", "")
	epic, err := s.CreateIssue(ctx, store.CreateIssueInput{Prefix: "children-json", Title: "Epic", Priority: 0, IssueType: "epic"})
	if err != nil {
		t.Fatalf("CreateIssue epic: %v", err)
	}
	child, err := s.CreateIssue(ctx, store.CreateIssueInput{Prefix: "children-json", Title: "Child", Priority: 1, IssueType: "task", ParentID: epic.ID})
	if err != nil {
		t.Fatalf("CreateIssue child: %v", err)
	}

	rs := &appState{
		store:   s,
		prefix:  "children-json",
		actor:   "test",
		jsonOut: true,
		git:     &fakeGitResolver{},
	}
	cmd := newChildrenCmd(rs)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.RunE(cmd, []string{epic.ID}); err != nil {
		t.Fatalf("children --json RunE: %v", err)
	}
	var got []issueJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode children JSON: %v\n%s", err, buf.String())
	}
	if len(got) != 1 || got[0].ID != child.ID {
		t.Fatalf("children JSON = %+v, want child %s", got, child.ID)
	}
}

func TestChildrenRepoScoping(t *testing.T) {
	ctx := context.Background()
	s, repoA := newTestStore(t, "", "https://github.com/alice/children-scope-a")
	if repoA == nil {
		t.Fatal("newTestStore did not register repoA")
	}
	repoB, err := s.AutoRegisterRepo(ctx, store.AutoRegisterInput{
		RemoteURL: "https://github.com/alice/children-scope-b",
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

	epicA, err := s.CreateIssue(ctx, store.CreateIssueInput{Prefix: repoA.Prefix, Title: "Epic A", Priority: 0, IssueType: "epic", Repo: &store.IssueRepoInput{RepoSlug: repoA.Slug}})
	if err != nil {
		t.Fatalf("CreateIssue epicA: %v", err)
	}
	childA, err := s.CreateIssue(ctx, store.CreateIssueInput{Prefix: repoA.Prefix, Title: "Child A", Priority: 1, IssueType: "task", ParentID: epicA.ID, Repo: &store.IssueRepoInput{RepoSlug: repoA.Slug}})
	if err != nil {
		t.Fatalf("CreateIssue childA: %v", err)
	}
	epicB, err := s.CreateIssue(ctx, store.CreateIssueInput{Prefix: repoB.Prefix, Title: "Epic B", Priority: 0, IssueType: "epic", Repo: &store.IssueRepoInput{RepoSlug: repoB.Slug}})
	if err != nil {
		t.Fatalf("CreateIssue epicB: %v", err)
	}
	childB, err := s.CreateIssue(ctx, store.CreateIssueInput{Prefix: repoB.Prefix, Title: "Child B", Priority: 1, IssueType: "task", ParentID: epicB.ID, Repo: &store.IssueRepoInput{RepoSlug: repoB.Slug}})
	if err != nil {
		t.Fatalf("CreateIssue childB: %v", err)
	}

	rs := &appState{
		store:   s,
		prefix:  repoB.Prefix,
		repoArg: repoA.Slug,
		actor:   "test",
		git:     &fakeGitResolver{},
	}
	cmd := newChildrenCmd(rs)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.RunE(cmd, []string{epicA.ID}); err != nil {
		t.Fatalf("children --repo slug RunE: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, childA.ID) {
		t.Fatalf("children --repo missing childA %q: %s", childA.ID, out)
	}
	if strings.Contains(out, childB.ID) {
		t.Fatalf("children --repo unexpectedly included childB %q: %s", childB.ID, out)
	}

	rs = &appState{
		store:  s,
		prefix: repoA.Prefix,
		actor:  "test",
		git:    &fakeGitResolver{},
	}
	cmd = newChildrenCmd(rs)
	buf.Reset()
	cmd.SetOut(&buf)
	if err := cmd.Flags().Set("all-repos", "true"); err != nil {
		t.Fatalf("set --all-repos: %v", err)
	}
	if err := cmd.RunE(cmd, []string{epicB.ID}); err != nil {
		t.Fatalf("children --all-repos RunE: %v", err)
	}
	out = buf.String()
	if !strings.Contains(out, childB.ID) {
		t.Fatalf("children --all-repos missing childB %q: %s", childB.ID, out)
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
	issB := mustCreateIssue(t, s, repoB.Prefix, "ready-issue-B", &repoB)

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
	if strings.Contains(out, issB.ID) {
		t.Errorf("ready output unexpectedly includes issue from repoB: %q", issB.ID)
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
	issB := mustCreateIssue(t, s, repoB.Prefix, "slug-issue-B", &repoB)

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
	if strings.Contains(out, issB.ID) {
		t.Errorf("list --repo=%s unexpectedly includes issue from repoB: %q", repoA.Slug, issB.ID)
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
