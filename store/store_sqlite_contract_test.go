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

	time.Sleep(time.Millisecond)
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

	time.Sleep(time.Millisecond)
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

	edges, err := s.ListDeps(ctx, prefix)
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
	ready, err := s.ReadyIssues(ctx, prefix, []model.IssueState{"closed"}, []model.IssueState{"open"})
	if err != nil {
		t.Fatalf("ReadyIssues initial: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != parent.ID {
		t.Fatalf("ReadyIssues initial = %+v, want parent only", ready)
	}
	if err := s.CloseIssue(ctx, parent.ID, "alice", "done"); err != nil {
		t.Fatalf("CloseIssue parent: %v", err)
	}
	ready, err = s.ReadyIssues(ctx, prefix, []model.IssueState{"closed"}, []model.IssueState{"open"})
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

	time.Sleep(time.Millisecond)
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
	if len(audits) != 4 || audits[0].Action != "repo.contract" {
		t.Fatalf("audits = %+v, want custom audit plus create/update/disable", audits)
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

func TestSQLiteStoreContractIssueRepoTarget(t *testing.T) {
	s, ctx := newSQLiteContractStore(t)
	const prefix = "sqlite-target"
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
	issue, err := s.CreateIssue(ctx, CreateIssueInput{
		Prefix: prefix,
		Title:  "Targeted",
		Repo: &IssueRepoInput{
			RepoSlug:       "core",
			RequestedRef:   "feature",
			BaseRef:        "main",
			WorkBranch:     "work/sqlite-target",
			WorktreeSubdir: "services/core",
			Metadata:       map[string]any{"source": "contract"},
		},
	})
	if err != nil {
		t.Fatalf("CreateIssue with repo target: %v", err)
	}
	got, err := s.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue targeted: %v", err)
	}
	if got.Repo == nil || got.Repo.Slug != "core" || got.Repo.WorkBranch != "work/sqlite-target" || got.Repo.Metadata["source"] != "contract" {
		t.Fatalf("repo target = %+v, want populated target", got.Repo)
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
		if _, err := s.SearchMemories(ctx, query, MemoryFilter{Prefix: prefix, Limit: 10}); err != nil {
			t.Fatalf("SearchMemories %q: %v", query, err)
		}
	}
	if _, err := s.InsertMemory(ctx, MemoryInput{Prefix: prefix, Body: "bad tag", Tags: []string{strings.Repeat("x", maxMemoryTagLength+1)}}); err == nil {
		t.Fatal("InsertMemory long tag succeeded, want validation error")
	}
	if _, err := s.SearchMemories(ctx, "", MemoryFilter{Prefix: prefix, Tags: []string{strings.Repeat("x", maxMemoryTagLength+1)}}); err == nil {
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

func assertUTCNonZero(t *testing.T, name string, ts time.Time) {
	t.Helper()
	if ts.IsZero() {
		t.Fatalf("%s is zero", name)
	}
	if ts.Location() != time.UTC {
		t.Fatalf("%s location = %v, want UTC", name, ts.Location())
	}
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
