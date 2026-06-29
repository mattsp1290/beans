package store

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mattsp1290/beans/model"
)

func TestSQLiteStoreContractIssueLifecycle(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)
	const prefix = "sqlite-issue"
	ensureContractProject(t, s, ctx, prefix)

	created, err := s.CreateIssue(ctx, CreateIssueInput{
		Prefix:      prefix,
		Title:       "Original title",
		Description: "first description",
		Priority:    1,
		IssueType:   "bug",
		Labels:      []string{"b", "a"},
		BranchName:  "feature/sqlite",
		URL:         "https://example.test/sqlite-issue",
		Actor:       "alice",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if created.ID == "" || !strings.HasPrefix(created.ID, prefix+"-") {
		t.Fatalf("created ID = %q, want %s-*", created.ID, prefix)
	}
	if created.Priority != model.PriorityHigh || created.State != "open" || created.IssueType != "bug" {
		t.Fatalf("created issue = %+v, want high/open/bug", created)
	}
	assertUTCNonZero(t, "created_at", created.CreatedAt)
	assertUTCNonZero(t, "updated_at", created.UpdatedAt)

	got, err := s.GetIssue(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Title != "Original title" || got.BranchName != "feature/sqlite" || got.URL == "" {
		t.Fatalf("GetIssue = %+v, want stored title/branch/url", got)
	}
	if !slices.Equal(got.Labels, []string{"b", "a"}) {
		t.Fatalf("labels = %v, want original order", got.Labels)
	}

	listed, err := s.ListIssues(ctx, ListFilter{Prefix: prefix, States: []model.IssueState{"open"}, Limit: 1})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("ListIssues = %+v, want created issue", listed)
	}

	waitForTimestampAdvance()
	title := "Updated title"
	desc := "second description"
	priority := 0
	state := model.IssueState("in_progress")
	empty := ""
	updated, err := s.UpdateIssue(ctx, created.ID, UpdateIssueInput{
		Title:       &title,
		Description: &desc,
		Priority:    &priority,
		State:       &state,
		Labels:      []string{},
		BranchName:  &empty,
		URL:         &empty,
		AppendNotes: &AppendNotesInput{Actor: "alice", Body: "moving"},
	})
	if err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if updated.Title != title || updated.Description != desc || updated.Priority != model.PriorityCritical {
		t.Fatalf("updated issue = %+v, want title/description/critical", updated)
	}
	if updated.State != state || updated.BranchName != "" || updated.URL != "" || len(updated.Labels) != 0 {
		t.Fatalf("updated optional fields = %+v, want state set and branch/url/labels cleared", updated)
	}
	if !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Fatalf("updated_at = %s, want after %s", updated.UpdatedAt, created.UpdatedAt)
	}

	waitForTimestampAdvance()
	if err := s.CloseIssue(ctx, created.ID, "alice", "done"); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	closed, err := s.GetIssue(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetIssue closed: %v", err)
	}
	if closed.State != "closed" {
		t.Fatalf("closed state = %q, want closed", closed.State)
	}
	if !closed.UpdatedAt.After(updated.UpdatedAt) {
		t.Fatalf("closed updated_at = %s, want after %s", closed.UpdatedAt, updated.UpdatedAt)
	}

	notes, err := s.ListIssueNotes(ctx, created.ID)
	if err != nil {
		t.Fatalf("ListIssueNotes: %v", err)
	}
	if len(notes) != 3 {
		t.Fatalf("ListIssueNotes len = %d, want 3", len(notes))
	}
	if notes[0].IssueID != created.ID || notes[0].Actor != "alice" || notes[0].Body != "created by alice" {
		t.Fatalf("first note = %+v, want create note", notes[0])
	}
	if notes[1].IssueID != created.ID || notes[1].Actor != "alice" || notes[1].Body != "moving" {
		t.Fatalf("second note = %+v, want appended update note", notes[1])
	}
	if notes[2].IssueID != created.ID || notes[2].Actor != "alice" || notes[2].Body != "done" {
		t.Fatalf("third note = %+v, want close reason note", notes[2])
	}
	if !notes[1].CreatedAt.After(notes[0].CreatedAt) || !notes[2].CreatedAt.After(notes[1].CreatedAt) {
		t.Fatalf("note order timestamps = %s, %s, %s; want append order", notes[0].CreatedAt, notes[1].CreatedAt, notes[2].CreatedAt)
	}
	latestNote, err := s.LatestIssueNote(ctx, created.ID)
	if err != nil {
		t.Fatalf("LatestIssueNote: %v", err)
	}
	if latestNote.ID != notes[2].ID || latestNote.Body != "done" {
		t.Fatalf("LatestIssueNote = %+v, want third note %+v", latestNote, notes[2])
	}

	if err := s.CloseIssue(ctx, created.ID, "alice", "duplicate close"); err != nil {
		t.Fatalf("CloseIssue idempotent: %v", err)
	}
	closedAgain, err := s.GetIssue(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetIssue closed again: %v", err)
	}
	if !closedAgain.UpdatedAt.Equal(closed.UpdatedAt) {
		t.Fatalf("idempotent close changed updated_at from %s to %s", closed.UpdatedAt, closedAgain.UpdatedAt)
	}

	withoutNotes, err := s.CreateIssue(ctx, CreateIssueInput{
		Prefix:      prefix,
		Title:       "no notes",
		Description: "empty note timeline",
		Priority:    int(model.PriorityMedium),
		IssueType:   "task",
	})
	if err != nil {
		t.Fatalf("CreateIssue without notes: %v", err)
	}
	emptyNotes, err := s.ListIssueNotes(ctx, withoutNotes.ID)
	if err != nil {
		t.Fatalf("ListIssueNotes empty: %v", err)
	}
	if len(emptyNotes) != 0 {
		t.Fatalf("ListIssueNotes empty len = %d, want 0", len(emptyNotes))
	}
	if _, err := s.LatestIssueNote(ctx, withoutNotes.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LatestIssueNote empty err = %v, want ErrNotFound", err)
	}

	if err := s.DeleteIssue(ctx, created.ID); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}
	if _, err := s.GetIssue(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetIssue after delete = %v, want ErrNotFound", err)
	}
	if err := s.DeleteIssue(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteIssue missing = %v, want ErrNotFound", err)
	}
}

func TestSQLiteStoreContractDependencies(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)
	const prefix = "sqlite-deps"
	ensureContractProject(t, s, ctx, prefix)
	parent := mustCreateContractIssue(t, s, ctx, prefix, "Parent", 0)
	child := mustCreateContractIssue(t, s, ctx, prefix, "Child", 1)
	grandchild := mustCreateContractIssue(t, s, ctx, prefix, "Grandchild", 2)

	if err := s.AddDep(ctx, child.ID, parent.ID); err != nil {
		t.Fatalf("AddDep child->parent: %v", err)
	}
	if err := s.AddDep(ctx, grandchild.ID, child.ID); err != nil {
		t.Fatalf("AddDep grandchild->child: %v", err)
	}
	if err := s.AddDep(ctx, child.ID, parent.ID); !errors.Is(err, ErrDuplicateDep) {
		t.Fatalf("duplicate AddDep = %v, want ErrDuplicateDep", err)
	}
	if err := s.AddDep(ctx, parent.ID, parent.ID); !errors.Is(err, ErrCycle) {
		t.Fatalf("self AddDep = %v, want ErrCycle", err)
	}
	if err := s.AddDep(ctx, parent.ID, grandchild.ID); !errors.Is(err, ErrCycle) {
		t.Fatalf("cycle AddDep = %v, want ErrCycle", err)
	}

	edges, err := s.ListDeps(ctx, ListFilter{Prefix: prefix})
	if err != nil {
		t.Fatalf("ListDeps: %v", err)
	}
	if !hasDepEdge(edges, child.ID, parent.ID) || !hasDepEdge(edges, grandchild.ID, child.ID) || len(edges) != 2 {
		t.Fatalf("ListDeps = %+v, want child->parent and grandchild->child", edges)
	}
	gotChild, err := s.GetIssue(ctx, child.ID)
	if err != nil {
		t.Fatalf("GetIssue child: %v", err)
	}
	if !slices.Equal(gotChild.BlockedBy, []string{parent.ID}) {
		t.Fatalf("child BlockedBy = %v, want parent", gotChild.BlockedBy)
	}
	ready, err := s.ReadyIssues(ctx, ListFilter{Prefix: prefix}, []model.IssueState{"closed"}, []model.IssueState{"open"})
	if err != nil {
		t.Fatalf("ReadyIssues initial: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != parent.ID {
		t.Fatalf("ReadyIssues initial = %+v, want parent only", ready)
	}
	if err := s.CloseIssue(ctx, parent.ID, "alice", "done"); err != nil {
		t.Fatalf("CloseIssue parent: %v", err)
	}
	ready, err = s.ReadyIssues(ctx, ListFilter{Prefix: prefix}, []model.IssueState{"closed"}, []model.IssueState{"open"})
	if err != nil {
		t.Fatalf("ReadyIssues after close: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != child.ID {
		t.Fatalf("ReadyIssues after close = %+v, want child only", ready)
	}
	if err := s.RemoveDep(ctx, grandchild.ID, child.ID); err != nil {
		t.Fatalf("RemoveDep: %v", err)
	}
	if err := s.RemoveDep(ctx, grandchild.ID, child.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RemoveDep missing = %v, want ErrNotFound", err)
	}
}

func TestSQLiteStoreContractImports(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)
	const prefix = "sqlite-import"
	ensureContractProject(t, s, ctx, prefix)
	ensureContractProject(t, s, ctx, "other-import")

	cross, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: "other-import", Title: "Other", Priority: 1, IssueType: "task"})
	if err != nil {
		t.Fatalf("CreateIssue other: %v", err)
	}
	items := []ImportInput{
		{ID: prefix + "-parent", Prefix: prefix, Title: "Parent", State: "open", Priority: 1, IssueType: "task", Labels: []string{"seed"}},
		{ID: prefix + "-child", Prefix: prefix, Title: "Child", State: "open", Priority: 2, IssueType: "bug", Deps: []string{prefix + "-parent", prefix + "-missing", prefix + "-child", prefix + "-parent"}},
		{ID: cross.ID, Prefix: prefix, Title: "Conflict", State: "open", Priority: 1, IssueType: "task"},
	}
	result, err := s.ImportIssuesFull(ctx, items, ImportOptions{Mode: ImportModeCreateOnly, TerminalStates: []model.IssueState{"closed"}})
	if err != nil {
		t.Fatalf("ImportIssuesFull create-only: %v", err)
	}
	if result.Created != 2 || result.CrossPrefixConflicts != 1 || result.DepsAdded != 1 ||
		result.DepsSkippedMissingBlocker != 1 || result.DepsSkippedSelf != 1 || result.DepsSkippedDuplicate != 1 {
		t.Fatalf("create-only result = %+v, want created=2 conflict=1 dep counters", result)
	}
	result, err = s.ImportIssuesFull(ctx, items, ImportOptions{Mode: ImportModeCreateOnly, TerminalStates: []model.IssueState{"closed"}})
	if err != nil {
		t.Fatalf("ImportIssuesFull idempotent: %v", err)
	}
	if result.Skipped != 2 || result.CrossPrefixConflicts != 1 || result.DepsAdded != 0 {
		t.Fatalf("idempotent result = %+v, want skipped=2 conflict=1 no deps", result)
	}

	if err := s.CloseIssue(ctx, prefix+"-child", "alice", "terminal"); err != nil {
		t.Fatalf("CloseIssue imported child: %v", err)
	}
	merge, err := s.ImportIssuesFull(ctx, []ImportInput{{
		ID: prefix + "-child", Prefix: prefix, Title: "Child merged", Description: "merged", State: "open", Priority: 0, IssueType: "feature", Labels: []string{"merged"},
	}}, ImportOptions{Mode: ImportModeMerge, TerminalStates: []model.IssueState{"closed"}})
	if err != nil {
		t.Fatalf("ImportIssuesFull merge: %v", err)
	}
	if merge.Updated != 1 {
		t.Fatalf("merge result = %+v, want updated=1", merge)
	}
	child, err := s.GetIssue(ctx, prefix+"-child")
	if err != nil {
		t.Fatalf("GetIssue merged child: %v", err)
	}
	if child.State != "closed" || child.Title != "Child merged" || child.Description != "merged" || child.IssueType != "feature" {
		t.Fatalf("merged child = %+v, want closed state preserved and fields updated", child)
	}
}

func TestSQLiteStoreContractReposAndAudit(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)
	const prefix = "sqlite-repos"
	ensureContractProject(t, s, ctx, prefix)

	if err := s.AddRepoAdmin(ctx, prefix, "alice", "alice", true); err != nil {
		t.Fatalf("AddRepoAdmin bootstrap: %v", err)
	}
	if err := s.AddRepoAdmin(ctx, prefix, "bob", "bob", true); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("second bootstrap = %v, want ErrUnauthorized", err)
	}
	if err := s.AddRepoAdmin(ctx, prefix, "bob", "alice", false); err != nil {
		t.Fatalf("AddRepoAdmin bob: %v", err)
	}
	admins, err := s.ListRepoAdmins(ctx, prefix)
	if err != nil {
		t.Fatalf("ListRepoAdmins: %v", err)
	}
	if !slices.Equal(admins, []string{"alice", "bob"}) {
		t.Fatalf("admins = %v, want alice,bob", admins)
	}
	if err := s.AuthorizeRepoAdmin(ctx, prefix, "mallory"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("AuthorizeRepoAdmin mallory = %v, want ErrUnauthorized", err)
	}

	_, err = s.CreateRepo(ctx, CreateRepoInput{
		Prefix:        prefix,
		Slug:          "boxy",
		RemoteURL:     "git@github.com:punk1290/boxy.git",
		AuthRef:       "ssh-key:github-default",
		DefaultBranch: "main",
		Actor:         "mallory",
	})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("CreateRepo unauthorized = %v, want ErrUnauthorized", err)
	}
	repo, err := s.CreateRepo(ctx, CreateRepoInput{
		Prefix:        prefix,
		Slug:          "boxy",
		DisplayName:   "Boxy",
		RemoteURL:     "git@github.com:punk1290/boxy.git",
		AuthRef:       "ssh-key:github-default",
		DefaultBranch: "main",
		Actor:         "alice",
		Aliases:       []string{"boxy", " GitHub.com/Punk1290/Boxy "},
		Metadata:      map[string]any{"tier": "prod"},
	})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	assertUTCNonZero(t, "repo created_at", repo.CreatedAt)
	assertUTCNonZero(t, "repo updated_at", repo.UpdatedAt)
	if repo.DisplayName != "Boxy" || !repo.Enabled || repo.Metadata["tier"] != "prod" {
		t.Fatalf("repo = %+v, want Boxy/enabled/prod metadata", repo)
	}
	got, err := s.GetRepoBySlug(ctx, prefix, "boxy")
	if err != nil {
		t.Fatalf("GetRepoBySlug: %v", err)
	}
	if got.ID != repo.ID {
		t.Fatalf("GetRepoBySlug ID = %q, want %q", got.ID, repo.ID)
	}
	byAlias, err := s.ResolveRepoAlias(ctx, prefix, "github.com/punk1290/boxy")
	if err != nil {
		t.Fatalf("ResolveRepoAlias: %v", err)
	}
	if byAlias.ID != repo.ID {
		t.Fatalf("ResolveRepoAlias ID = %q, want %q", byAlias.ID, repo.ID)
	}
	if _, err := s.CreateRepo(ctx, CreateRepoInput{
		Prefix:        prefix,
		Slug:          "dupe",
		RemoteURL:     "git@github.com:punk1290/dupe.git",
		AuthRef:       "ssh-key:github-default",
		DefaultBranch: "main",
		Actor:         "alice",
		Aliases:       []string{"boxy"},
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("CreateRepo duplicate alias = %v, want ErrConflict", err)
	}
	if _, err := s.GetRepoBySlug(ctx, prefix, "dupe"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetRepoBySlug dupe after conflict = %v, want ErrNotFound", err)
	}

	waitForTimestampAdvance()
	branch := "trunk"
	display := "Boxy Updated"
	updated, err := s.UpdateRepo(ctx, prefix, "boxy", UpdateRepoInput{
		DisplayName:   &display,
		DefaultBranch: &branch,
		Metadata:      map[string]any{"tier": "dev"},
		Actor:         "alice",
		Aliases:       []string{"new-boxy"},
	})
	if err != nil {
		t.Fatalf("UpdateRepo: %v", err)
	}
	if updated.DisplayName != display || updated.DefaultBranch != branch || updated.Metadata["tier"] != "dev" {
		t.Fatalf("updated repo = %+v, want replaced fields", updated)
	}
	if !updated.UpdatedAt.After(repo.UpdatedAt) {
		t.Fatalf("repo updated_at = %s, want after %s", updated.UpdatedAt, repo.UpdatedAt)
	}
	if _, err := s.ResolveRepoAlias(ctx, prefix, "boxy"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old alias lookup = %v, want ErrNotFound", err)
	}
	if _, err := s.ResolveRepoAlias(ctx, prefix, "new-boxy"); err != nil {
		t.Fatalf("new alias lookup: %v", err)
	}
	disabled, err := s.DisableRepo(ctx, prefix, "boxy", "alice")
	if err != nil {
		t.Fatalf("DisableRepo: %v", err)
	}
	if disabled.Enabled {
		t.Fatal("DisableRepo returned enabled repo")
	}
	enabledOnly, err := s.ListRepos(ctx, prefix, false)
	if err != nil {
		t.Fatalf("ListRepos enabled only: %v", err)
	}
	if len(enabledOnly) != 0 {
		t.Fatalf("enabled repos after disable = %+v, want none", enabledOnly)
	}
	allRepos, err := s.ListRepos(ctx, prefix, true)
	if err != nil {
		t.Fatalf("ListRepos all: %v", err)
	}
	if len(allRepos) != 1 || allRepos[0].ID != repo.ID {
		t.Fatalf("all repos = %+v, want disabled repo", allRepos)
	}

	audit, err := s.InsertRepoAudit(ctx, RepoAuditInput{
		Prefix:    prefix,
		RepoID:    repo.ID,
		Action:    "repo.contract",
		Actor:     "alice",
		NewValues: map[string]any{"ok": true},
		Command:   "test",
	})
	if err != nil {
		t.Fatalf("InsertRepoAudit: %v", err)
	}
	assertUTCNonZero(t, "audit created_at", audit.CreatedAt)
	audits, err := s.ListRepoAudit(ctx, prefix, repo.ID, 10)
	if err != nil {
		t.Fatalf("ListRepoAudit: %v", err)
	}
	if actions := repoAuditActions(audits); !slices.Equal(actions, []string{"repo.contract", "repo.update", "repo.update", "repo.create"}) {
		t.Fatalf("audit actions = %v, want contract/update/update/create", actions)
	}

	if err := s.RemoveRepoAdmin(ctx, prefix, "bob", "alice"); err != nil {
		t.Fatalf("RemoveRepoAdmin bob: %v", err)
	}
	if err := s.RemoveRepoAdmin(ctx, prefix, "bob", "alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RemoveRepoAdmin missing = %v, want ErrNotFound", err)
	}
	if err := s.RemoveRepoAdmin(ctx, prefix, "alice", "alice"); !errors.Is(err, ErrConflict) {
		t.Fatalf("RemoveRepoAdmin last = %v, want ErrConflict", err)
	}
}

func TestSQLiteStoreContractGetRepoByRemoteURL(t *testing.T) {
	t.Parallel()
	s, ctx := newSQLiteContractStore(t)
	const prefix = "sqlite-remote-url"
	ensureContractProject(t, s, ctx, prefix)

	if err := s.AddRepoAdmin(ctx, prefix, "alice", "alice", true); err != nil {
		t.Fatalf("AddRepoAdmin: %v", err)
	}

	// Register repo using the SCP transport form.  CreateRepo normalizes the
	// stored remote_url so all three transport forms share one unique slot.
	registered, err := s.CreateRepo(ctx, CreateRepoInput{
		Prefix:        prefix,
		Slug:          "myapp",
		DisplayName:   "My App",
		RemoteURL:     "git@github.com:alice/myapp.git",
		AuthRef:       "ssh-key:github-default",
		DefaultBranch: "main",
		Actor:         "alice",
	})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	// Canonical normalized URL stored in db.
	const wantCanonical = "https://github.com/alice/myapp"
	if registered.RemoteURL != wantCanonical {
		t.Fatalf("CreateRepo stored RemoteURL = %q, want %q", registered.RemoteURL, wantCanonical)
	}

	// All three transport forms must find the same row.
	forms := []struct {
		name string
		url  string
	}{
		{"scp", "git@github.com:alice/myapp.git"},
		{"ssh-url", "ssh://git@github.com/alice/myapp.git"},
		{"https-with-git", "https://github.com/alice/myapp.git"},
		{"https-no-git", "https://github.com/alice/myapp"},
	}
	for _, tc := range forms {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := s.GetRepoByRemoteURL(ctx, tc.url)
			if err != nil {
				t.Fatalf("GetRepoByRemoteURL(%q): %v", tc.url, err)
			}
			if got.ID != registered.ID {
				t.Fatalf("GetRepoByRemoteURL(%q) ID = %q, want %q", tc.url, got.ID, registered.ID)
			}
		})
	}

	// Unknown URL returns ErrNotFound.
	_, err = s.GetRepoByRemoteURL(ctx, "https://github.com/nobody/absent.git")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetRepoByRemoteURL unknown = %v, want ErrNotFound", err)
	}

	// Invalid URL returns a non-NotFound error (input error, not a missing row).
	_, err = s.GetRepoByRemoteURL(ctx, "ftp://unsupported.example.com/repo.git")
	if err == nil {
		t.Fatal("GetRepoByRemoteURL unsupported scheme: want error, got nil")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatal("GetRepoByRemoteURL unsupported scheme: want non-NotFound error")
	}

	// Empty input returns non-NotFound error (ErrNoRemote, not a missing row).
	_, err = s.GetRepoByRemoteURL(ctx, "")
	if err == nil {
		t.Fatal("GetRepoByRemoteURL empty: want error, got nil")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatal("GetRepoByRemoteURL empty: want non-NotFound error (ErrNoRemote)")
	}

	// A second CreateRepo with a different transport form for the same logical
	// remote must be rejected with ErrConflict (proves the UNIQUE index blocks
	// cross-transport duplicates once the write path normalizes).
	_, err = s.CreateRepo(ctx, CreateRepoInput{
		Prefix:        prefix,
		Slug:          "myapp-dupe",
		DisplayName:   "Dupe",
		RemoteURL:     "https://github.com/alice/myapp.git",
		AuthRef:       "ssh-key:github-default",
		DefaultBranch: "main",
		Actor:         "alice",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("CreateRepo duplicate remote (different transport form) = %v, want ErrConflict", err)
	}
}

func TestSQLiteStoreContractUpdateRepoNormalizesRemoteURL(t *testing.T) {
	t.Parallel()
	s, ctx := newSQLiteContractStore(t)
	const prefix = "sqlite-update-url"
	ensureContractProject(t, s, ctx, prefix)

	if err := s.AddRepoAdmin(ctx, prefix, "alice", "alice", true); err != nil {
		t.Fatalf("AddRepoAdmin: %v", err)
	}

	registered, err := s.CreateRepo(ctx, CreateRepoInput{
		Prefix:        prefix,
		Slug:          "app",
		DisplayName:   "App",
		RemoteURL:     "git@github.com:alice/app.git",
		AuthRef:       "ssh-key:github-default",
		DefaultBranch: "main",
		Actor:         "alice",
	})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	// Update the remote URL using a different transport form (SSH URL).
	// UpdateRepo must normalize it so GetRepoByRemoteURL still finds the repo.
	newURL := "ssh://git@github.com/alice/app.git"
	updated, err := s.UpdateRepo(ctx, prefix, "app", UpdateRepoInput{
		RemoteURL: &newURL,
		Actor:     "alice",
	})
	if err != nil {
		t.Fatalf("UpdateRepo: %v", err)
	}
	const wantCanonical = "https://github.com/alice/app"
	if updated.RemoteURL != wantCanonical {
		t.Fatalf("UpdateRepo stored RemoteURL = %q, want %q", updated.RemoteURL, wantCanonical)
	}

	// GetRepoByRemoteURL must find the repo via any transport form post-update.
	got, err := s.GetRepoByRemoteURL(ctx, "git@github.com:alice/app.git")
	if err != nil {
		t.Fatalf("GetRepoByRemoteURL after update: %v", err)
	}
	if got.ID != registered.ID {
		t.Fatalf("GetRepoByRemoteURL after update ID = %q, want %q", got.ID, registered.ID)
	}
	if got.RemoteURL != wantCanonical {
		t.Fatalf("GetRepoByRemoteURL after update RemoteURL = %q, want %q", got.RemoteURL, wantCanonical)
	}

	// Register a second repo, then attempt to update its remote to collide with
	// the first repo's canonical URL.  UpdateRepo must return ErrConflict.
	_, err = s.CreateRepo(ctx, CreateRepoInput{
		Prefix:        prefix,
		Slug:          "other",
		DisplayName:   "Other",
		RemoteURL:     "git@github.com:alice/other.git",
		AuthRef:       "ssh-key:github-default",
		DefaultBranch: "main",
		Actor:         "alice",
	})
	if err != nil {
		t.Fatalf("CreateRepo other: %v", err)
	}
	collidingURL := "https://github.com/alice/app.git"
	_, err = s.UpdateRepo(ctx, prefix, "other", UpdateRepoInput{
		RemoteURL: &collidingURL,
		Actor:     "alice",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("UpdateRepo to colliding remote = %v, want ErrConflict", err)
	}
}

func TestSQLiteStoreContractIssueRepoTarget(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)
	const prefix = "sqlite-target"
	const creationCommit = "0123456789abcdef0123456789abcdef01234567"
	ensureContractProject(t, s, ctx, prefix)
	if err := s.AddRepoAdmin(ctx, prefix, "alice", "alice", true); err != nil {
		t.Fatalf("AddRepoAdmin bootstrap: %v", err)
	}
	if _, err := s.CreateRepo(ctx, CreateRepoInput{
		Prefix:        prefix,
		Slug:          "core",
		RemoteURL:     "git@github.com:punk1290/core.git",
		AuthRef:       "ssh-key:github-default",
		DefaultBranch: "main",
		Actor:         "alice",
	}); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if _, err := s.CreateRepo(ctx, CreateRepoInput{
		Prefix:        prefix,
		Slug:          "api",
		RemoteURL:     "git@github.com:punk1290/api.git",
		AuthRef:       "ssh-key:github-default",
		DefaultBranch: "main",
		Actor:         "alice",
	}); err != nil {
		t.Fatalf("CreateRepo api: %v", err)
	}
	issue, err := s.CreateIssue(ctx, CreateIssueInput{
		Prefix: prefix,
		Title:  "Targeted",
		Repo: &IssueRepoInput{
			RepoSlug:       "core",
			RequestedRef:   "feature",
			BaseRef:        "main",
			WorkBranch:     "work/sqlite-target",
			WorktreeSubdir: "services/core",
			CreationCommit: creationCommit,
			Metadata:       map[string]any{"source": "contract"},
		},
	})
	if err != nil {
		t.Fatalf("CreateIssue with repo target: %v", err)
	}
	if issue.Repo == nil || issue.Repo.CreationCommit != creationCommit {
		t.Fatalf("CreateIssue repo creation_commit = %+v, want %q", issue.Repo, creationCommit)
	}
	got, err := s.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue targeted: %v", err)
	}
	if got.Repo == nil ||
		got.Repo.Slug != "core" ||
		got.Repo.RemoteURL != "https://github.com/punk1290/core" ||
		got.Repo.DefaultBranch != "main" ||
		got.Repo.RequestedRef != "feature" ||
		got.Repo.BaseRef != "main" ||
		got.Repo.WorkBranch != "work/sqlite-target" ||
		got.Repo.WorktreeSubdir != "services/core" ||
		got.Repo.CreationCommit != creationCommit ||
		got.Repo.AuthRef != "ssh-key:github-default" ||
		got.Repo.Metadata["source"] != "contract" {
		t.Fatalf("repo target = %+v, want populated target", got.Repo)
	}

	listed, err := s.ListIssues(ctx, ListFilter{Prefix: prefix})
	if err != nil {
		t.Fatalf("ListIssues targeted: %v", err)
	}
	if len(listed) != 1 || listed[0].Repo == nil || listed[0].Repo.CreationCommit != creationCommit {
		t.Fatalf("ListIssues repo creation_commit = %+v, want %q", listed, creationCommit)
	}

	ready, err := s.ReadyIssues(ctx, ListFilter{Prefix: prefix}, []model.IssueState{"closed"}, []model.IssueState{"open"})
	if err != nil {
		t.Fatalf("ReadyIssues targeted: %v", err)
	}
	if len(ready) != 1 || ready[0].Repo == nil || ready[0].Repo.CreationCommit != creationCommit {
		t.Fatalf("ReadyIssues repo creation_commit = %+v, want %q", ready, creationCommit)
	}

	retargeted, err := s.UpdateIssue(ctx, issue.ID, UpdateIssueInput{
		Repo: &IssueRepoInput{RepoSlug: "api"},
	})
	if err != nil {
		t.Fatalf("UpdateIssue retarget repo: %v", err)
	}
	if retargeted.Repo == nil ||
		retargeted.Repo.Slug != "api" ||
		retargeted.Repo.CreationCommit != creationCommit {
		t.Fatalf("retargeted repo = %+v, want api preserving creation_commit %q", retargeted.Repo, creationCommit)
	}

	updatedRoute, err := s.UpdateIssue(ctx, issue.ID, UpdateIssueInput{
		Repo: &IssueRepoInput{
			RepoSlug:       "api",
			RequestedRef:   "release",
			WorktreeSubdir: "services/api",
		},
	})
	if err != nil {
		t.Fatalf("UpdateIssue repo ref/subdir: %v", err)
	}
	if updatedRoute.Repo == nil ||
		updatedRoute.Repo.RequestedRef != "release" ||
		updatedRoute.Repo.WorktreeSubdir != "services/api" ||
		updatedRoute.Repo.CreationCommit != creationCommit {
		t.Fatalf("updated repo route = %+v, want ref/subdir update preserving creation_commit %q", updatedRoute.Repo, creationCommit)
	}

	differentValidCommit := "89abcdef0123456789abcdef0123456789abcdef"
	preserved, err := s.UpdateIssue(ctx, issue.ID, UpdateIssueInput{
		Repo: &IssueRepoInput{
			RepoSlug:       "core",
			CreationCommit: differentValidCommit,
			WorktreeSubdir: "services/core",
		},
	})
	if err != nil {
		t.Fatalf("UpdateIssue repo explicit replacement commit: %v", err)
	}
	if preserved.Repo == nil ||
		preserved.Repo.Slug != "core" ||
		preserved.Repo.CreationCommit != creationCommit {
		t.Fatalf("explicit replacement commit repo = %+v, want original creation_commit %q", preserved.Repo, creationCommit)
	}

	_, err = s.UpdateIssue(ctx, issue.ID, UpdateIssueInput{
		Repo: &IssueRepoInput{RepoSlug: "core", CreationCommit: "HEAD"},
	})
	if err == nil || !strings.Contains(err.Error(), "creation_commit") || !strings.Contains(err.Error(), "full lowercase 40-character hex object ID") {
		t.Fatalf("UpdateIssue invalid creation_commit error = %v, want clear validation error", err)
	}
	afterInvalidUpdate, err := s.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after invalid UpdateIssue creation_commit: %v", err)
	}
	if afterInvalidUpdate.Repo == nil ||
		afterInvalidUpdate.Repo.CreationCommit != creationCommit ||
		afterInvalidUpdate.Repo.Slug != "core" {
		t.Fatalf("repo after invalid UpdateIssue creation_commit = %+v, want unchanged core target with %q", afterInvalidUpdate.Repo, creationCommit)
	}

	emptyCommitIssue, err := s.CreateIssue(ctx, CreateIssueInput{
		Prefix: prefix,
		Title:  "Targeted empty commit",
		Repo:   &IssueRepoInput{RepoSlug: "core", CreationCommit: ""},
	})
	if err != nil {
		t.Fatalf("CreateIssue with empty creation_commit: %v", err)
	}
	if emptyCommitIssue.Repo == nil || emptyCommitIssue.Repo.CreationCommit != "" {
		t.Fatalf("empty creation_commit repo target = %+v, want empty string", emptyCommitIssue.Repo)
	}

	updateEmptyCommitIssue, err := s.CreateIssue(ctx, CreateIssueInput{
		Prefix: prefix,
		Title:  "Update adds empty commit repo",
	})
	if err != nil {
		t.Fatalf("CreateIssue update-empty target: %v", err)
	}
	updateEmptyCommitIssue, err = s.UpdateIssue(ctx, updateEmptyCommitIssue.ID, UpdateIssueInput{
		Repo: &IssueRepoInput{RepoSlug: "core", CreationCommit: ""},
	})
	if err != nil {
		t.Fatalf("UpdateIssue with empty creation_commit: %v", err)
	}
	if updateEmptyCommitIssue.Repo == nil || updateEmptyCommitIssue.Repo.CreationCommit != "" {
		t.Fatalf("UpdateIssue empty creation_commit repo target = %+v, want empty string", updateEmptyCommitIssue.Repo)
	}

	updateImportedCommitIssue, err := s.CreateIssue(ctx, CreateIssueInput{
		Prefix: prefix,
		Title:  "Update adds imported commit repo",
	})
	if err != nil {
		t.Fatalf("CreateIssue update-import target: %v", err)
	}
	updateImportedCommitIssue, err = s.UpdateIssue(ctx, updateImportedCommitIssue.ID, UpdateIssueInput{
		Repo: &IssueRepoInput{RepoSlug: "core", CreationCommit: differentValidCommit},
	})
	if err != nil {
		t.Fatalf("UpdateIssue with imported creation_commit: %v", err)
	}
	if updateImportedCommitIssue.Repo == nil || updateImportedCommitIssue.Repo.CreationCommit != differentValidCommit {
		t.Fatalf("UpdateIssue imported creation_commit repo target = %+v, want %q", updateImportedCommitIssue.Repo, differentValidCommit)
	}

	invalidCommits := []string{
		"HEAD",
		"0123456",
		"0123456789ABCDEF0123456789ABCDEF01234567",
		"not-a-commit-object-id",
	}
	for _, invalid := range invalidCommits {
		_, err := s.CreateIssue(ctx, CreateIssueInput{
			Prefix: prefix,
			Title:  "Invalid commit",
			Repo:   &IssueRepoInput{RepoSlug: "core", CreationCommit: invalid},
		})
		if err == nil || !strings.Contains(err.Error(), "creation_commit") || !strings.Contains(err.Error(), "full lowercase 40-character hex object ID") {
			t.Fatalf("CreateIssue creation_commit %q error = %v, want clear validation error", invalid, err)
		}
	}
	afterInvalid, err := s.ListIssues(ctx, ListFilter{Prefix: prefix})
	if err != nil {
		t.Fatalf("ListIssues after invalid creation_commit: %v", err)
	}
	if len(afterInvalid) != 4 {
		t.Fatalf("issue count after invalid creation_commit attempts = %d, want 4", len(afterInvalid))
	}

	_, err = s.CreateIssue(ctx, CreateIssueInput{
		Title: "Invalid remote commit",
		Repo: &IssueRepoInput{
			RemoteURL:      "https://github.com/acme/invalid-remote-commit.git",
			CreationCommit: "main",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "creation_commit") {
		t.Fatalf("CreateIssue invalid remote creation_commit error = %v, want validation error", err)
	}
	if _, err := s.GetRepoBySlug(ctx, "invalid-remote-commit", "invalid-remote-commit"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetRepoBySlug after invalid remote creation_commit = %v, want ErrNotFound", err)
	}
	remoteIssues, err := s.ListIssues(ctx, ListFilter{Prefix: "invalid-remote-commit"})
	if err != nil {
		t.Fatalf("ListIssues after invalid remote creation_commit: %v", err)
	}
	if len(remoteIssues) != 0 {
		t.Fatalf("issues after invalid remote creation_commit = %d, want 0", len(remoteIssues))
	}
}

// TestSQLiteStoreContractCreateIssueDerivesPrefix verifies the topology-a
// contract: when CreateIssue receives a RemoteURL in its IssueRepoInput, the
// issue prefix is derived from the auto-registered repo (prefix == slug), and
// two issues created in distinct repos get distinct, non-colliding prefixes.
func TestSQLiteStoreContractCreateIssueDerivesPrefix(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)

	// Issue in the first repo — auto-register via RemoteURL.
	i1, err := s.CreateIssue(ctx, CreateIssueInput{
		Title: "Issue in repo alpha",
		Repo: &IssueRepoInput{
			RemoteURL: "https://github.com/acme/alpha.git",
		},
		Actor: "system",
	})
	if err != nil {
		t.Fatalf("CreateIssue repo alpha: %v", err)
	}

	// Issue in the second repo — different canonical URL → different prefix.
	i2, err := s.CreateIssue(ctx, CreateIssueInput{
		Title: "Issue in repo beta",
		Repo: &IssueRepoInput{
			RemoteURL: "git@github.com:acme/beta.git",
		},
		Actor: "system",
	})
	if err != nil {
		t.Fatalf("CreateIssue repo beta: %v", err)
	}

	// IDs must be prefixed with their repo's slug.
	if !strings.HasPrefix(i1.ID, "alpha-") {
		t.Errorf("i1.ID = %q, want alpha-* prefix", i1.ID)
	}
	if !strings.HasPrefix(i2.ID, "beta-") {
		t.Errorf("i2.ID = %q, want beta-* prefix", i2.ID)
	}

	// Prefixes must differ.
	if i1.ID[:strings.LastIndex(i1.ID, "-")] == i2.ID[:strings.LastIndex(i2.ID, "-")] {
		t.Errorf("both issues share the same prefix segment; want distinct prefixes")
	}

	// Third issue in alpha via a different transport form — same repo, same prefix.
	i3, err := s.CreateIssue(ctx, CreateIssueInput{
		Title: "Second issue in repo alpha",
		Repo: &IssueRepoInput{
			RemoteURL: "git@github.com:acme/alpha.git",
		},
		Actor: "system",
	})
	if err != nil {
		t.Fatalf("CreateIssue repo alpha (2nd): %v", err)
	}
	if !strings.HasPrefix(i3.ID, "alpha-") {
		t.Errorf("i3.ID = %q, want alpha-* prefix (same repo as i1)", i3.ID)
	}
}

func TestSQLiteStoreContractMemories(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)
	const prefix = "sqlite-memory"
	ensureContractProject(t, s, ctx, prefix)
	ensureContractProject(t, s, ctx, "other-memory")

	global, err := s.InsertMemory(ctx, MemoryInput{Body: "global sqlite handbook", Type: "reference", Tags: []string{"shared", "sqlite", "shared"}})
	if err != nil {
		t.Fatalf("InsertMemory global: %v", err)
	}
	if !slices.Equal(global.Tags, []string{"shared", "sqlite"}) {
		t.Fatalf("global tags = %v, want normalized unique tags", global.Tags)
	}
	project, err := s.InsertMemory(ctx, MemoryInput{Prefix: prefix, Body: "project sqlite operational detail foo-bar 100%", Type: "note", Tags: []string{"sqlite", "project"}})
	if err != nil {
		t.Fatalf("InsertMemory project: %v", err)
	}
	other, err := s.InsertMemory(ctx, MemoryInput{Prefix: "other-memory", Body: "other project sqlite", Type: "note", Tags: []string{"sqlite", "other"}})
	if err != nil {
		t.Fatalf("InsertMemory other: %v", err)
	}
	assertUTCNonZero(t, "memory created_at", project.CreatedAt)

	found, err := s.SearchMemories(ctx, "", MemoryFilter{Prefix: prefix, Limit: 10})
	if err != nil {
		t.Fatalf("SearchMemories scoped empty: %v", err)
	}
	if ids := memoryIDs(found); !slices.Equal(ids, []int64{project.ID, global.ID}) {
		t.Fatalf("scoped memory IDs = %v, want project then global", ids)
	}
	found, err = s.SearchMemories(ctx, "sqlite", MemoryFilter{Prefix: prefix, Type: "note", Tags: []string{"project", "sqlite"}, Limit: 10})
	if err != nil {
		t.Fatalf("SearchMemories tags/type/query: %v", err)
	}
	if ids := memoryIDs(found); !slices.Equal(ids, []int64{project.ID}) {
		t.Fatalf("tag/type/query IDs = %v, want project", ids)
	}
	found, err = s.SearchMemories(ctx, "", MemoryFilter{All: true, Tags: []string{"sqlite"}, Limit: 10})
	if err != nil {
		t.Fatalf("SearchMemories all: %v", err)
	}
	if ids := memoryIDs(found); !slices.Equal(ids, []int64{other.ID, project.ID, global.ID}) {
		t.Fatalf("all memory IDs = %v, want newest other/project/global", ids)
	}
	for _, query := range []string{"foo-bar", `"unterminated`, "%", "_"} {
		found, err := s.SearchMemories(ctx, query, MemoryFilter{Prefix: prefix, Limit: 10})
		if err != nil {
			t.Fatalf("SearchMemories %q: %v", query, err)
		}
		if query == "foo-bar" {
			if ids := memoryIDs(found); !slices.Equal(ids, []int64{project.ID}) {
				t.Fatalf("SearchMemories %q IDs = %v, want project", query, ids)
			}
		}
	}
	if _, err := s.InsertMemory(ctx, MemoryInput{Prefix: prefix, Body: "bad tag", Tags: []string{strings.Repeat("x", maxMemoryTagLength+1)}}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatal("InsertMemory long tag succeeded, want validation error")
	}
	if _, err := s.SearchMemories(ctx, "", MemoryFilter{Prefix: prefix, Tags: []string{strings.Repeat("x", maxMemoryTagLength+1)}}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatal("SearchMemories long tag succeeded, want validation error")
	}
}

func TestSQLiteStoreContractCloseAndErrorNormalization(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)
	ensureContractProject(t, s, ctx, "sqlite-close")
	s.Close()
	s.Close()

	if err := s.EnsureProject(ctx, "sqlite-close"); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("EnsureProject after Close = %v, want ErrPoolClosed", err)
	}
	if _, err := s.ProjectExists(ctx, "sqlite-close"); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("ProjectExists after Close = %v, want ErrPoolClosed", err)
	}
	if _, err := s.ListIssues(ctx, ListFilter{Prefix: "sqlite-close"}); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("ListIssues after Close = %v, want ErrPoolClosed", err)
	}
	if _, err := s.ListRepos(ctx, "sqlite-close", true); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("ListRepos after Close = %v, want ErrPoolClosed", err)
	}
	if _, err := s.SearchMemories(ctx, "", MemoryFilter{Prefix: "sqlite-close"}); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("SearchMemories after Close = %v, want ErrPoolClosed", err)
	}
}

func newSQLiteContractStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	ctx := context.Background()
	s, err := New(ctx, Config{Driver: DriverSQLite, DSN: SecretDSN(sqliteMemoryDSN(t))})
	if err != nil {
		t.Fatalf("New sqlite: %v", err)
	}
	t.Cleanup(s.Close)
	return s, ctx
}

func ensureContractProject(t *testing.T, s *Store, ctx context.Context, prefix string) {
	t.Helper()
	if err := s.EnsureProject(ctx, prefix); err != nil {
		t.Fatalf("EnsureProject %q: %v", prefix, err)
	}
	exists, err := s.ProjectExists(ctx, prefix)
	if err != nil {
		t.Fatalf("ProjectExists %q: %v", prefix, err)
	}
	if !exists {
		t.Fatalf("ProjectExists %q = false, want true", prefix)
	}
}

func mustCreateContractIssue(t *testing.T, s *Store, ctx context.Context, prefix, title string, priority int) Issue {
	t.Helper()
	issue, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: prefix, Title: title, Priority: priority, IssueType: "task"})
	if err != nil {
		t.Fatalf("CreateIssue %q: %v", title, err)
	}
	return issue
}

func TestSQLiteStoreContractEpicMembership(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)
	const prefix = "sqlite-epic"
	ensureContractProject(t, s, ctx, prefix)

	epic, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: prefix, Title: "Epic", Priority: 1, IssueType: "epic"})
	if err != nil {
		t.Fatalf("CreateIssue epic: %v", err)
	}
	leafA := mustCreateContractIssue(t, s, ctx, prefix, "Leaf A", 1)
	leafB := mustCreateContractIssue(t, s, ctx, prefix, "Leaf B", 2)

	// Membership: leaves are children of the epic, non-blocking.
	if err := s.AddTypedDep(ctx, leafA.ID, epic.ID, DepTypeParentChild); err != nil {
		t.Fatalf("AddTypedDep leafA parent-child: %v", err)
	}
	if err := s.AddTypedDep(ctx, leafB.ID, epic.ID, DepTypeParentChild); err != nil {
		t.Fatalf("AddTypedDep leafB parent-child: %v", err)
	}

	terminal := []model.IssueState{"closed", "done"}
	active := []model.IssueState{"open"}
	ready, err := s.ReadyIssues(ctx, ListFilter{Prefix: prefix}, terminal, active)
	if err != nil {
		t.Fatalf("ReadyIssues: %v", err)
	}
	// Epic excluded (rollup); both leaves ready (membership is non-blocking).
	if len(ready) != 2 {
		t.Fatalf("ReadyIssues = %d issues, want 2 leaves: %+v", len(ready), ready)
	}
	for _, iss := range ready {
		if iss.ID == epic.ID {
			t.Fatalf("ReadyIssues included epic %s, want it excluded", epic.ID)
		}
	}

	// Epic with no terminal states configured must still exclude the epic and
	// keep leaves ready (covers the no-JOIN ready branch).
	readyNoTerm, err := s.ReadyIssues(ctx, ListFilter{Prefix: prefix}, nil, active)
	if err != nil {
		t.Fatalf("ReadyIssues no-terminal: %v", err)
	}
	if len(readyNoTerm) != 2 {
		t.Fatalf("ReadyIssues no-terminal = %d, want 2 leaves: %+v", len(readyNoTerm), readyNoTerm)
	}

	// Membership is queryable and non-blocking, so leaves carry no BlockedBy.
	gotLeaf, err := s.GetIssue(ctx, leafA.ID)
	if err != nil {
		t.Fatalf("GetIssue leafA: %v", err)
	}
	if len(gotLeaf.BlockedBy) != 0 {
		t.Fatalf("leafA BlockedBy = %v, want empty (parent-child is non-blocking)", gotLeaf.BlockedBy)
	}

	members, err := s.ListMembers(ctx, ListFilter{Prefix: prefix}, epic.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("ListMembers = %d, want 2 children", len(members))
	}

	// ListDeps surfaces the edge kind; dep cycles must ignore parent-child.
	edges, err := s.ListDeps(ctx, ListFilter{Prefix: prefix})
	if err != nil {
		t.Fatalf("ListDeps: %v", err)
	}
	for _, e := range edges {
		if e.IssueID == leafA.ID && e.BlockedByID == epic.ID && e.DepType != DepTypeParentChild {
			t.Fatalf("leafA->epic dep_type = %q, want parent-child", e.DepType)
		}
	}

	// A parent-child edge must not block via the cycle guard: adding the reverse
	// blocking edge (epic blocked by leaf) is allowed because parent-child is
	// excluded from the blocking graph.
	if err := s.AddDep(ctx, epic.ID, leafA.ID); err != nil {
		t.Fatalf("AddDep epic blocked-by leafA: %v", err)
	}
}

func TestSQLiteStoreContractCreateIssueWithParent(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)
	const prefix = "sqlite-create-parent"
	ensureContractProject(t, s, ctx, prefix)

	epic, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: prefix, Title: "Epic", Priority: 0, IssueType: "epic"})
	if err != nil {
		t.Fatalf("CreateIssue epic: %v", err)
	}
	first, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: prefix, Title: "First", Priority: 0, IssueType: "task", ParentID: epic.ID})
	if err != nil {
		t.Fatalf("CreateIssue first with parent: %v", err)
	}
	second, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: prefix, Title: "Second", Priority: 1, IssueType: "task", ParentID: epic.ID})
	if err != nil {
		t.Fatalf("CreateIssue second with parent: %v", err)
	}
	if err := s.AddDep(ctx, second.ID, first.ID); err != nil {
		t.Fatalf("AddDep second blocked by first: %v", err)
	}

	members, err := s.ListMembers(ctx, ListFilter{Prefix: prefix}, epic.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 2 || members[0].ID != first.ID || members[1].ID != second.ID {
		t.Fatalf("ListMembers = %+v, want first then second", members)
	}

	gotFirst, err := s.GetIssue(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetIssue first: %v", err)
	}
	if len(gotFirst.BlockedBy) != 0 {
		t.Fatalf("first BlockedBy = %v, want empty", gotFirst.BlockedBy)
	}
	gotSecond, err := s.GetIssue(ctx, second.ID)
	if err != nil {
		t.Fatalf("GetIssue second: %v", err)
	}
	if len(gotSecond.BlockedBy) != 1 || gotSecond.BlockedBy[0] != first.ID {
		t.Fatalf("second BlockedBy = %v, want only %s", gotSecond.BlockedBy, first.ID)
	}

	ready, err := s.ReadyIssues(ctx, ListFilter{Prefix: prefix}, []model.IssueState{"closed", "done"}, []model.IssueState{"open"})
	if err != nil {
		t.Fatalf("ReadyIssues: %v", err)
	}
	readyIDs := map[string]bool{}
	for _, iss := range ready {
		readyIDs[iss.ID] = true
	}
	if !readyIDs[first.ID] {
		t.Fatalf("ReadyIssues missing first child; membership should not block it")
	}
	if readyIDs[second.ID] {
		t.Fatalf("ReadyIssues returned second child; explicit blocks edge should block it")
	}
	if readyIDs[epic.ID] {
		t.Fatalf("ReadyIssues returned epic; epics are not dispatchable")
	}

	blocking, err := s.ListBlockingDeps(ctx, ListFilter{Prefix: prefix})
	if err != nil {
		t.Fatalf("ListBlockingDeps: %v", err)
	}
	if len(blocking) != 1 || blocking[0].IssueID != second.ID || blocking[0].BlockedByID != first.ID {
		t.Fatalf("ListBlockingDeps = %+v, want only second blocked by first", blocking)
	}
}

func TestSQLiteStoreContractCreateIssueWithParentRollsBackOnInvalidParent(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)
	const prefix = "sqlite-create-parent-rb"
	const other = "sqlite-create-parent-other"
	ensureContractProject(t, s, ctx, prefix)
	ensureContractProject(t, s, ctx, other)

	_, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: prefix, Title: "Missing parent child", Priority: 1, IssueType: "task", ParentID: prefix + "-missing"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateIssue missing parent = %v, want ErrNotFound", err)
	}
	issues, err := s.ListIssues(ctx, ListFilter{Prefix: prefix})
	if err != nil {
		t.Fatalf("ListIssues after missing parent: %v", err)
	}
	for _, iss := range issues {
		if iss.Title == "Missing parent child" {
			t.Fatalf("child was created despite missing parent: %+v", iss)
		}
	}

	foreignParent, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: other, Title: "Foreign parent", Priority: 1, IssueType: "epic"})
	if err != nil {
		t.Fatalf("CreateIssue foreign parent: %v", err)
	}
	_, err = s.CreateIssue(ctx, CreateIssueInput{Prefix: prefix, Title: "Foreign parent child", Priority: 1, IssueType: "task", ParentID: foreignParent.ID})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateIssue foreign parent = %v, want ErrNotFound", err)
	}
	issues, err = s.ListIssues(ctx, ListFilter{Prefix: prefix})
	if err != nil {
		t.Fatalf("ListIssues after foreign parent: %v", err)
	}
	for _, iss := range issues {
		if iss.Title == "Foreign parent child" {
			t.Fatalf("child was created despite cross-prefix parent: %+v", iss)
		}
	}
}

// TestSQLiteStoreContractOneEdgePerPair pins the intentional one-edge-per-pair
// behavior: the PK is (issue_id, blocked_by_id) without dep_type, so a second
// edge of a different kind for the same ordered pair is rejected as a duplicate
// (first write wins). Locks this so a future refactor can't silently change it.
func TestSQLiteStoreContractOneEdgePerPair(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)
	const prefix = "sqlite-pair"
	ensureContractProject(t, s, ctx, prefix)
	a := mustCreateContractIssue(t, s, ctx, prefix, "A", 1)
	b := mustCreateContractIssue(t, s, ctx, prefix, "B", 1)

	if err := s.AddTypedDep(ctx, a.ID, b.ID, DepTypeBlocks); err != nil {
		t.Fatalf("AddTypedDep blocks: %v", err)
	}
	// Same pair, different kind → duplicate (first edge wins).
	if err := s.AddTypedDep(ctx, a.ID, b.ID, DepTypeParentChild); !errors.Is(err, ErrDuplicateDep) {
		t.Fatalf("AddTypedDep parent-child on existing pair = %v, want ErrDuplicateDep", err)
	}

	// The surviving edge is the blocking one: A is blocked by B.
	edges, err := s.ListDeps(ctx, ListFilter{Prefix: prefix})
	if err != nil {
		t.Fatalf("ListDeps: %v", err)
	}
	if len(edges) != 1 || edges[0].DepType != DepTypeBlocks {
		t.Fatalf("edges = %+v, want a single blocks edge", edges)
	}
	// And membership did not take hold: B has no parent-child children.
	members, err := s.ListMembers(ctx, ListFilter{Prefix: prefix}, b.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("ListMembers = %d, want 0 (parent-child insert was rejected)", len(members))
	}
}

func TestSQLiteStoreContractImportParentChild(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)
	const prefix = "sqlite-import-pc"
	ensureContractProject(t, s, ctx, prefix)
	items := []ImportInput{
		{ID: prefix + "-epic", Prefix: prefix, Title: "Epic", State: "open", Priority: 1, IssueType: "epic"},
		{ID: prefix + "-leaf", Prefix: prefix, Title: "Leaf", State: "open", Priority: 1, IssueType: "task",
			// epic (real), missing (absent), self, epic (duplicate).
			ParentEdges: []string{prefix + "-epic", prefix + "-missing", prefix + "-leaf", prefix + "-epic"}},
	}
	res, err := s.ImportIssuesFull(ctx, items, ImportOptions{Mode: ImportModeCreateOnly, TerminalStates: []model.IssueState{"closed"}})
	if err != nil {
		t.Fatalf("ImportIssuesFull: %v", err)
	}
	// Dedicated parent-edge counters; self/dup stay in the shared buckets; the
	// missing parent must NOT land in DepsSkippedMissingBlocker.
	if res.ParentEdgesAdded != 1 || res.ParentEdgesSkippedMissing != 1 || res.DepsSkippedSelf != 1 || res.DepsSkippedDuplicate != 1 {
		t.Fatalf("import parent-edge counters = %+v, want added=1 missing=1 self=1 dup=1", res)
	}
	if res.DepsSkippedMissingBlocker != 0 || res.DepsAdded != 0 {
		t.Fatalf("blocking counters leaked: DepsSkippedMissingBlocker=%d DepsAdded=%d, want 0/0", res.DepsSkippedMissingBlocker, res.DepsAdded)
	}
	members, err := s.ListMembers(ctx, ListFilter{Prefix: prefix}, prefix+"-epic")
	if err != nil || len(members) != 1 || members[0].ID != prefix+"-leaf" {
		t.Fatalf("ListMembers = %+v err=%v, want the leaf", members, err)
	}
	// Membership is non-blocking, so the leaf carries no BlockedBy.
	leaf, err := s.GetIssue(ctx, prefix+"-leaf")
	if err != nil {
		t.Fatalf("GetIssue leaf: %v", err)
	}
	if len(leaf.BlockedBy) != 0 {
		t.Fatalf("leaf BlockedBy = %v, want empty (membership is non-blocking)", leaf.BlockedBy)
	}
}

func TestSQLiteStoreContractRemoveParentChild(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)
	const prefix = "sqlite-rm-pc"
	ensureContractProject(t, s, ctx, prefix)
	epic, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: prefix, Title: "Epic", Priority: 1, IssueType: "epic"})
	if err != nil {
		t.Fatalf("CreateIssue epic: %v", err)
	}
	leaf := mustCreateContractIssue(t, s, ctx, prefix, "Leaf", 1)

	if err := s.AddTypedDep(ctx, leaf.ID, epic.ID, DepTypeParentChild); err != nil {
		t.Fatalf("AddTypedDep parent-child: %v", err)
	}
	// RemoveDep is type-agnostic: it removes the membership edge by pair.
	if err := s.RemoveDep(ctx, leaf.ID, epic.ID); err != nil {
		t.Fatalf("RemoveDep parent-child: %v", err)
	}
	members, err := s.ListMembers(ctx, ListFilter{Prefix: prefix}, epic.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("ListMembers after remove = %d, want 0", len(members))
	}
	if err := s.RemoveDep(ctx, leaf.ID, epic.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RemoveDep missing = %v, want ErrNotFound", err)
	}
}

func TestSQLiteStoreContractListMembersPrefixScoped(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)
	const prefix = "sqlite-scope-a"
	const other = "sqlite-scope-b"
	ensureContractProject(t, s, ctx, prefix)
	ensureContractProject(t, s, ctx, other)

	epicA, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: prefix, Title: "Epic A", Priority: 1, IssueType: "epic"})
	if err != nil {
		t.Fatalf("CreateIssue epicA: %v", err)
	}
	leafA := mustCreateContractIssue(t, s, ctx, prefix, "Leaf A", 1)
	if err := s.AddTypedDep(ctx, leafA.ID, epicA.ID, DepTypeParentChild); err != nil {
		t.Fatalf("AddTypedDep A: %v", err)
	}

	// Querying epicA's members from the OTHER prefix must return nothing —
	// ListMembers is prefix-scoped like every other list path.
	members, err := s.ListMembers(ctx, ListFilter{Prefix: other}, epicA.ID)
	if err != nil {
		t.Fatalf("ListMembers cross-prefix: %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("cross-prefix ListMembers = %d, want 0 (no leakage)", len(members))
	}

	// ListParents is likewise scoped: the leaf's parent is invisible from other.
	parents, err := s.ListParents(ctx, ListFilter{Prefix: other}, leafA.ID)
	if err != nil {
		t.Fatalf("ListParents cross-prefix: %v", err)
	}
	if len(parents) != 0 {
		t.Fatalf("cross-prefix ListParents = %d, want 0", len(parents))
	}
	// In-prefix, the parent is visible.
	parents, err = s.ListParents(ctx, ListFilter{Prefix: prefix}, leafA.ID)
	if err != nil || len(parents) != 1 || parents[0].ID != epicA.ID {
		t.Fatalf("ListParents in-prefix = %+v err=%v, want epicA", parents, err)
	}
}

func assertUTCNonZero(t *testing.T, name string, ts time.Time) {
	t.Helper()
	if ts.IsZero() {
		t.Fatalf("%s is zero", name)
	}
	if ts.Location() != time.UTC {
		t.Fatalf("%s location = %v, want UTC", name, ts.Location())
	}
}

func waitForTimestampAdvance() {
	time.Sleep(2 * time.Millisecond)
}

func repoAuditActions(audits []RepoAudit) []string {
	actions := make([]string, 0, len(audits))
	for _, audit := range audits {
		actions = append(actions, audit.Action)
	}
	return actions
}

func memoryIDs(memories []Memory) []int64 {
	ids := make([]int64, 0, len(memories))
	for _, memory := range memories {
		ids = append(ids, memory.ID)
	}
	return ids
}

func hasDepEdge(edges []DepEdge, issueID, blockedByID string) bool {
	for _, edge := range edges {
		if edge.IssueID == issueID && edge.BlockedByID == blockedByID {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// AutoRegisterRepo contract tests
// ---------------------------------------------------------------------------

func TestSQLiteStoreContractAutoRegisterRepo(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)

	t.Run("basic registration creates project and repo", func(t *testing.T) {
		r, err := s.AutoRegisterRepo(ctx, AutoRegisterInput{
			RemoteURL: "git@github.com:alice/myapp.git",
			Actor:     "system",
		})
		if err != nil {
			t.Fatalf("AutoRegisterRepo: %v", err)
		}
		if r.RemoteURL != "https://github.com/alice/myapp" {
			t.Errorf("RemoteURL = %q, want canonical https form", r.RemoteURL)
		}
		// Slug should be "myapp" (step 1, bare repo name)
		if r.Slug != "myapp" {
			t.Errorf("Slug = %q, want %q", r.Slug, "myapp")
		}
		if r.Prefix != r.Slug {
			t.Errorf("Prefix %q != Slug %q", r.Prefix, r.Slug)
		}

		// Project must exist in bn_projects
		exists, err := s.ProjectExists(ctx, r.Slug)
		if err != nil {
			t.Fatalf("ProjectExists: %v", err)
		}
		if !exists {
			t.Errorf("project %q not created", r.Slug)
		}
	})

	t.Run("idempotent across transport forms", func(t *testing.T) {
		// First call creates the row.
		r1, err := s.AutoRegisterRepo(ctx, AutoRegisterInput{
			RemoteURL: "https://github.com/alice/repo2.git",
			Actor:     "system",
		})
		if err != nil {
			t.Fatalf("first AutoRegisterRepo: %v", err)
		}

		// Same repo via SCP form must return the identical row.
		r2, err := s.AutoRegisterRepo(ctx, AutoRegisterInput{
			RemoteURL: "git@github.com:alice/repo2.git",
			Actor:     "system",
		})
		if err != nil {
			t.Fatalf("second AutoRegisterRepo (SCP): %v", err)
		}
		if r1.ID != r2.ID {
			t.Errorf("IDs differ: %q vs %q", r1.ID, r2.ID)
		}

		// Same repo via ssh:// URL.
		r3, err := s.AutoRegisterRepo(ctx, AutoRegisterInput{
			RemoteURL: "ssh://git@github.com/alice/repo2.git",
			Actor:     "system",
		})
		if err != nil {
			t.Fatalf("third AutoRegisterRepo (ssh://): %v", err)
		}
		if r1.ID != r3.ID {
			t.Errorf("IDs differ: %q vs %q", r1.ID, r3.ID)
		}
	})

	t.Run("slug disambiguation when bare name is taken", func(t *testing.T) {
		// Register a repo that claims slug "core".
		r1, err := s.AutoRegisterRepo(ctx, AutoRegisterInput{
			RemoteURL: "https://github.com/alpha/core.git",
			Actor:     "system",
		})
		if err != nil {
			t.Fatalf("first repo: %v", err)
		}
		if r1.Slug != "core" {
			t.Errorf("expected step-1 slug %q, got %q", "core", r1.Slug)
		}

		// Second repo with same bare name → step 2 (owner-qualified).
		r2, err := s.AutoRegisterRepo(ctx, AutoRegisterInput{
			RemoteURL: "https://github.com/beta/core.git",
			Actor:     "system",
		})
		if err != nil {
			t.Fatalf("second repo: %v", err)
		}
		if r2.Slug != "beta-core" {
			t.Errorf("expected step-2 slug %q, got %q", "beta-core", r2.Slug)
		}

		// Third owner-qualified collision → step 3 (host-qualified).
		r3, err := s.AutoRegisterRepo(ctx, AutoRegisterInput{
			RemoteURL: "https://github.com/alpha/core",
			Actor:     "system",
		})
		// r1 is already registered at this canonical URL — idempotent return expected.
		if err != nil {
			t.Fatalf("idempotent third call: %v", err)
		}
		if r3.ID != r1.ID {
			t.Errorf("expected idempotent return of r1, got different ID %q", r3.ID)
		}
	})

	t.Run("local-only file:// URL", func(t *testing.T) {
		r, err := s.AutoRegisterRepo(ctx, AutoRegisterInput{
			RemoteURL: "file:///home/alice/projects/localrepo",
			Actor:     "system",
		})
		if err != nil {
			t.Fatalf("AutoRegisterRepo (file://): %v", err)
		}
		if r.RemoteURL != "file:///home/alice/projects/localrepo" {
			t.Errorf("RemoteURL = %q, want original canonical", r.RemoteURL)
		}
		if r.Slug == "" {
			t.Error("Slug is empty for file:// URL")
		}
	})

	t.Run("invalid URL returns error", func(t *testing.T) {
		_, err := s.AutoRegisterRepo(ctx, AutoRegisterInput{
			RemoteURL: "ftp://example.com/repo.git",
			Actor:     "system",
		})
		if err == nil {
			t.Error("expected error for unsupported scheme, got nil")
		}
	})

	t.Run("empty URL returns error", func(t *testing.T) {
		_, err := s.AutoRegisterRepo(ctx, AutoRegisterInput{
			RemoteURL: "",
			Actor:     "system",
		})
		if err == nil {
			t.Error("expected error for empty URL, got nil")
		}
		if errors.Is(err, ErrSlugExhausted) {
			t.Error("empty URL returned ErrSlugExhausted instead of validation error")
		}
	})
}

func TestSQLiteStoreContractListFilterAllRepos(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)

	// Create two projects/repos with issues.
	const prefixA = "list-filter-a"
	const prefixB = "list-filter-b"
	if err := s.EnsureProject(ctx, prefixA); err != nil {
		t.Fatalf("EnsureProject A: %v", err)
	}
	if err := s.EnsureProject(ctx, prefixB); err != nil {
		t.Fatalf("EnsureProject B: %v", err)
	}

	issA, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: prefixA, Title: "issue in A", Actor: "test"})
	if err != nil {
		t.Fatalf("CreateIssue A: %v", err)
	}
	issB, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: prefixB, Title: "issue in B", Actor: "test"})
	if err != nil {
		t.Fatalf("CreateIssue B: %v", err)
	}

	// (a) Default scope: prefix filter applied — each project sees only its own issues.
	listA, err := s.ListIssues(ctx, ListFilter{Prefix: prefixA})
	if err != nil {
		t.Fatalf("ListIssues A: %v", err)
	}
	if len(listA) != 1 || listA[0].ID != issA.ID {
		t.Fatalf("ListIssues A = %+v, want only issA", listA)
	}

	listB, err := s.ListIssues(ctx, ListFilter{Prefix: prefixB})
	if err != nil {
		t.Fatalf("ListIssues B: %v", err)
	}
	if len(listB) != 1 || listB[0].ID != issB.ID {
		t.Fatalf("ListIssues B = %+v, want only issB", listB)
	}

	// (b) AllRepos=true: prefix filter omitted — returns issues from all projects.
	all, err := s.ListIssues(ctx, ListFilter{AllRepos: true})
	if err != nil {
		t.Fatalf("ListIssues AllRepos: %v", err)
	}
	ids := make(map[string]bool)
	for _, iss := range all {
		ids[iss.ID] = true
	}
	if !ids[issA.ID] || !ids[issB.ID] {
		t.Fatalf("ListIssues AllRepos missing issues: got %v, want %s and %s", all, issA.ID, issB.ID)
	}

	// (c) Empty prefix with AllRepos=false: matches nothing.
	none, err := s.ListIssues(ctx, ListFilter{Prefix: ""})
	if err != nil {
		t.Fatalf("ListIssues empty prefix: %v", err)
	}
	// May include issues from other tests with prefix="", but our two issues must not appear.
	for _, iss := range none {
		if iss.ID == issA.ID || iss.ID == issB.ID {
			t.Fatalf("ListIssues empty prefix returned %s (want only prefix='' rows)", iss.ID)
		}
	}
}

func TestSQLiteStoreContractReadyIssuesAllRepos(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)

	const prefixA = "ready-all-a"
	const prefixB = "ready-all-b"
	if err := s.EnsureProject(ctx, prefixA); err != nil {
		t.Fatalf("EnsureProject A: %v", err)
	}
	if err := s.EnsureProject(ctx, prefixB); err != nil {
		t.Fatalf("EnsureProject B: %v", err)
	}

	issA, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: prefixA, Title: "ready in A", Actor: "test"})
	if err != nil {
		t.Fatalf("CreateIssue A: %v", err)
	}
	issB, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: prefixB, Title: "ready in B", Actor: "test"})
	if err != nil {
		t.Fatalf("CreateIssue B: %v", err)
	}

	term := []model.IssueState{"closed"}
	active := []model.IssueState{"open"}

	// Prefix-scoped: each project sees only its own ready issues.
	readyA, err := s.ReadyIssues(ctx, ListFilter{Prefix: prefixA}, term, active)
	if err != nil {
		t.Fatalf("ReadyIssues A: %v", err)
	}
	if len(readyA) != 1 || readyA[0].ID != issA.ID {
		t.Fatalf("ReadyIssues A = %+v, want only issA", readyA)
	}

	// AllRepos: cross-project ready list includes both.
	readyAll, err := s.ReadyIssues(ctx, ListFilter{AllRepos: true}, term, active)
	if err != nil {
		t.Fatalf("ReadyIssues AllRepos: %v", err)
	}
	ids := make(map[string]bool)
	for _, iss := range readyAll {
		ids[iss.ID] = true
	}
	if !ids[issA.ID] || !ids[issB.ID] {
		t.Fatalf("ReadyIssues AllRepos missing issues: got %v, want %s and %s", readyAll, issA.ID, issB.ID)
	}
}

func TestSQLiteStoreContractListFilterAllReposWinsOverPrefix(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)

	const prefixA = "allrepos-wins-a"
	const prefixB = "allrepos-wins-b"
	if err := s.EnsureProject(ctx, prefixA); err != nil {
		t.Fatalf("EnsureProject A: %v", err)
	}
	if err := s.EnsureProject(ctx, prefixB); err != nil {
		t.Fatalf("EnsureProject B: %v", err)
	}

	issA, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: prefixA, Title: "wins A", Actor: "test"})
	if err != nil {
		t.Fatalf("CreateIssue A: %v", err)
	}
	issB, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: prefixB, Title: "wins B", Actor: "test"})
	if err != nil {
		t.Fatalf("CreateIssue B: %v", err)
	}

	// AllRepos=true with Prefix set — AllRepos must win; both issues returned.
	all, err := s.ListIssues(ctx, ListFilter{Prefix: prefixA, AllRepos: true})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	ids := make(map[string]bool)
	for _, iss := range all {
		ids[iss.ID] = true
	}
	if !ids[issA.ID] {
		t.Fatalf("ListIssues AllRepos+Prefix missing issA")
	}
	if !ids[issB.ID] {
		t.Fatalf("ListIssues AllRepos+Prefix missing issB — AllRepos must win over Prefix")
	}
}

func TestSQLiteStoreContractReadyIssuesAllReposWithBlocker(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)

	const prefixA = "cross-ready-a"
	const prefixB = "cross-ready-b"
	if err := s.EnsureProject(ctx, prefixA); err != nil {
		t.Fatalf("EnsureProject A: %v", err)
	}
	if err := s.EnsureProject(ctx, prefixB); err != nil {
		t.Fatalf("EnsureProject B: %v", err)
	}

	// In B: parent blocks child within same project.
	parent, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: prefixB, Title: "parent B", Actor: "test"})
	if err != nil {
		t.Fatalf("CreateIssue parent: %v", err)
	}
	child, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: prefixB, Title: "child B", Actor: "test"})
	if err != nil {
		t.Fatalf("CreateIssue child: %v", err)
	}
	if err := s.AddDep(ctx, child.ID, parent.ID); err != nil {
		t.Fatalf("AddDep: %v", err)
	}

	// Unblocked issue in A.
	free, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: prefixA, Title: "free A", Actor: "test"})
	if err != nil {
		t.Fatalf("CreateIssue free: %v", err)
	}

	term := []model.IssueState{"closed"}
	active := []model.IssueState{"open"}

	// Cross-repo ready: parent (unblocked) and free (unblocked) are ready; child is not.
	readyAll, err := s.ReadyIssues(ctx, ListFilter{AllRepos: true}, term, active)
	if err != nil {
		t.Fatalf("ReadyIssues AllRepos: %v", err)
	}
	ids := make(map[string]bool)
	for _, iss := range readyAll {
		ids[iss.ID] = true
	}
	if !ids[parent.ID] {
		t.Fatalf("ReadyIssues AllRepos missing parent (expected ready)")
	}
	if !ids[free.ID] {
		t.Fatalf("ReadyIssues AllRepos missing free (expected ready)")
	}
	if ids[child.ID] {
		t.Fatalf("ReadyIssues AllRepos returned child (expected blocked, NOT-EXISTS should filter it)")
	}
}

func TestSQLiteStoreContractListDepsAllRepos(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)

	const pfxA = "deps-all-a"
	const pfxB = "deps-all-b"
	for _, pfx := range []string{pfxA, pfxB} {
		if err := s.EnsureProject(ctx, pfx); err != nil {
			t.Fatalf("EnsureProject %s: %v", pfx, err)
		}
	}

	parentA, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: pfxA, Title: "parent A", Actor: "t"})
	if err != nil {
		t.Fatalf("CreateIssue parentA: %v", err)
	}
	childA, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: pfxA, Title: "child A", Actor: "t"})
	if err != nil {
		t.Fatalf("CreateIssue childA: %v", err)
	}
	if err := s.AddDep(ctx, childA.ID, parentA.ID); err != nil {
		t.Fatalf("AddDep A: %v", err)
	}

	parentB, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: pfxB, Title: "parent B", Actor: "t"})
	if err != nil {
		t.Fatalf("CreateIssue parentB: %v", err)
	}
	childB, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: pfxB, Title: "child B", Actor: "t"})
	if err != nil {
		t.Fatalf("CreateIssue childB: %v", err)
	}
	if err := s.AddDep(ctx, childB.ID, parentB.ID); err != nil {
		t.Fatalf("AddDep B: %v", err)
	}

	// Prefix-scoped: each project sees only its own edges.
	edgesA, err := s.ListDeps(ctx, ListFilter{Prefix: pfxA})
	if err != nil {
		t.Fatalf("ListDeps A: %v", err)
	}
	for _, e := range edgesA {
		if e.IssueID == childB.ID {
			t.Fatalf("ListDeps A returned edge from pfxB: %+v", e)
		}
	}

	// AllRepos: edges from both prefixes returned.
	all, err := s.ListDeps(ctx, ListFilter{AllRepos: true})
	if err != nil {
		t.Fatalf("ListDeps AllRepos: %v", err)
	}
	byChild := make(map[string]bool)
	for _, e := range all {
		byChild[e.IssueID] = true
	}
	if !byChild[childA.ID] || !byChild[childB.ID] {
		t.Fatalf("ListDeps AllRepos missing edges: got %v", all)
	}
}

func TestSQLiteStoreContractListMembersAllRepos(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)

	const pfxA = "members-all-a"
	const pfxB = "members-all-b"
	for _, pfx := range []string{pfxA, pfxB} {
		if err := s.EnsureProject(ctx, pfx); err != nil {
			t.Fatalf("EnsureProject %s: %v", pfx, err)
		}
	}

	epicA, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: pfxA, Title: "epic A", Actor: "t", IssueType: "epic"})
	if err != nil {
		t.Fatalf("CreateIssue epicA: %v", err)
	}
	leafA, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: pfxA, Title: "leaf A", Actor: "t"})
	if err != nil {
		t.Fatalf("CreateIssue leafA: %v", err)
	}
	if err := s.AddTypedDep(ctx, leafA.ID, epicA.ID, DepTypeParentChild); err != nil {
		t.Fatalf("AddTypedDep leafA→epicA: %v", err)
	}

	epicB, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: pfxB, Title: "epic B", Actor: "t", IssueType: "epic"})
	if err != nil {
		t.Fatalf("CreateIssue epicB: %v", err)
	}
	leafB, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: pfxB, Title: "leaf B", Actor: "t"})
	if err != nil {
		t.Fatalf("CreateIssue leafB: %v", err)
	}
	if err := s.AddTypedDep(ctx, leafB.ID, epicB.ID, DepTypeParentChild); err != nil {
		t.Fatalf("AddTypedDep leafB→epicB: %v", err)
	}

	// Prefix-scoped: each project sees only its own members.
	membersA, err := s.ListMembers(ctx, ListFilter{Prefix: pfxA}, epicA.ID)
	if err != nil {
		t.Fatalf("ListMembers A: %v", err)
	}
	if len(membersA) != 1 || membersA[0].ID != leafA.ID {
		t.Fatalf("ListMembers A = %+v, want only leafA", membersA)
	}

	// AllRepos: leafA visible as member of epicA even when filter has no prefix.
	allMembers, err := s.ListMembers(ctx, ListFilter{AllRepos: true}, epicA.ID)
	if err != nil {
		t.Fatalf("ListMembers AllRepos: %v", err)
	}
	found := false
	for _, m := range allMembers {
		if m.ID == leafA.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("ListMembers AllRepos missing leafA: got %v", allMembers)
	}
}

func TestSQLiteStoreContractListParentsAllRepos(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)

	const pfxA = "parents-all-a"
	const pfxB = "parents-all-b"
	for _, pfx := range []string{pfxA, pfxB} {
		if err := s.EnsureProject(ctx, pfx); err != nil {
			t.Fatalf("EnsureProject %s: %v", pfx, err)
		}
	}

	epicA, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: pfxA, Title: "epic A", Actor: "t", IssueType: "epic"})
	if err != nil {
		t.Fatalf("CreateIssue epicA: %v", err)
	}
	leafA, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: pfxA, Title: "leaf A", Actor: "t"})
	if err != nil {
		t.Fatalf("CreateIssue leafA: %v", err)
	}
	if err := s.AddTypedDep(ctx, leafA.ID, epicA.ID, DepTypeParentChild); err != nil {
		t.Fatalf("AddTypedDep leafA→epicA: %v", err)
	}

	// Prefix-scoped: visible in own prefix.
	parentsA, err := s.ListParents(ctx, ListFilter{Prefix: pfxA}, leafA.ID)
	if err != nil {
		t.Fatalf("ListParents A: %v", err)
	}
	if len(parentsA) != 1 || parentsA[0].ID != epicA.ID {
		t.Fatalf("ListParents A = %+v, want epicA", parentsA)
	}

	// Prefix-scoped from pfxB: invisible (no leakage).
	parentsB, err := s.ListParents(ctx, ListFilter{Prefix: pfxB}, leafA.ID)
	if err != nil {
		t.Fatalf("ListParents B: %v", err)
	}
	if len(parentsB) != 0 {
		t.Fatalf("ListParents B = %+v, want 0 (no leakage)", parentsB)
	}

	// AllRepos: epicA visible as parent even without a prefix.
	parentsAll, err := s.ListParents(ctx, ListFilter{AllRepos: true}, leafA.ID)
	if err != nil {
		t.Fatalf("ListParents AllRepos: %v", err)
	}
	found := false
	for _, p := range parentsAll {
		if p.ID == epicA.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("ListParents AllRepos missing epicA: got %v", parentsAll)
	}
}

func TestSQLiteStoreContractListBlockingDepsAllRepos(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)

	const pfxA = "blocking-all-a"
	const pfxB = "blocking-all-b"
	for _, pfx := range []string{pfxA, pfxB} {
		if err := s.EnsureProject(ctx, pfx); err != nil {
			t.Fatalf("EnsureProject %s: %v", pfx, err)
		}
	}

	parentA, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: pfxA, Title: "parent A", Actor: "t"})
	if err != nil {
		t.Fatalf("CreateIssue parentA: %v", err)
	}
	childA, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: pfxA, Title: "child A", Actor: "t"})
	if err != nil {
		t.Fatalf("CreateIssue childA: %v", err)
	}
	if err := s.AddDep(ctx, childA.ID, parentA.ID); err != nil {
		t.Fatalf("AddDep A: %v", err)
	}

	parentB, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: pfxB, Title: "parent B", Actor: "t"})
	if err != nil {
		t.Fatalf("CreateIssue parentB: %v", err)
	}
	childB, err := s.CreateIssue(ctx, CreateIssueInput{Prefix: pfxB, Title: "child B", Actor: "t"})
	if err != nil {
		t.Fatalf("CreateIssue childB: %v", err)
	}
	if err := s.AddDep(ctx, childB.ID, parentB.ID); err != nil {
		t.Fatalf("AddDep B: %v", err)
	}

	// Prefix-scoped: each project sees only its own blocking edges.
	edgesA, err := s.ListBlockingDeps(ctx, ListFilter{Prefix: pfxA})
	if err != nil {
		t.Fatalf("ListBlockingDeps A: %v", err)
	}
	for _, e := range edgesA {
		if e.IssueID == childB.ID {
			t.Fatalf("ListBlockingDeps A returned edge from pfxB: %+v", e)
		}
	}
	if len(edgesA) != 1 || edgesA[0].IssueID != childA.ID {
		t.Fatalf("ListBlockingDeps A = %+v, want one edge childA→parentA", edgesA)
	}

	// AllRepos: blocking edges from both prefixes returned.
	all, err := s.ListBlockingDeps(ctx, ListFilter{AllRepos: true})
	if err != nil {
		t.Fatalf("ListBlockingDeps AllRepos: %v", err)
	}
	byChild := make(map[string]bool)
	for _, e := range all {
		byChild[e.IssueID] = true
	}
	if !byChild[childA.ID] || !byChild[childB.ID] {
		t.Fatalf("ListBlockingDeps AllRepos missing edges: got %v", all)
	}
}
