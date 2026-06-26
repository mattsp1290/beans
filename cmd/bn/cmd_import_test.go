package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/beans/model"

	store "github.com/mattsp1290/beans/store"
)

func TestParseImportJSONLSkipsInvalidStatus(t *testing.T) {
	t.Parallel()

	input := strings.NewReader(strings.Join([]string{
		`{"id":"src-good","title":"good","status":"open","priority":2,"issue_type":"task"}`,
		`{"id":"src-bad","title":"bad","status":"archived","priority":2,"issue_type":"task"}`,
		`{"id":"src-done","title":"done","status":"done","priority":2,"issue_type":"task"}`,
	}, "\n"))

	items, warnings, err := parseImportJSONL(input, "dest")
	if err != nil {
		t.Fatalf("parseImportJSONL: %v", err)
	}
	if warnings != 1 {
		t.Fatalf("warnings = %d, want 1", warnings)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2: %+v", len(items), items)
	}
	if items[0].ID != "src-good" || items[0].State != "open" {
		t.Fatalf("first item = %+v, want src-good/open", items[0])
	}
	if items[1].ID != "src-done" || items[1].State != "done" {
		t.Fatalf("second item = %+v, want src-done/done", items[1])
	}
}

func TestParseImportJSONLMapsFullBDRowAndFiltersDeps(t *testing.T) {
	t.Parallel()

	input := strings.NewReader(`{"id":"src-child","title":"Child","description":"desc","status":"blocked","priority":1,"issue_type":"bug","labels":["one","two"],"branch_name":"fix/child","url":"https://example.test/src-child","dependencies":[{"issue_id":"src-child","depends_on_id":"src-parent","type":"blocks"},{"issue_id":"src-parent","depends_on_id":"src-child","type":"blocks"},{"issue_id":"src-child","depends_on_id":"src-related","type":"related"}]}`)

	items, warnings, err := parseImportJSONL(input, "dest")
	if err != nil {
		t.Fatalf("parseImportJSONL: %v", err)
	}
	if warnings != 0 {
		t.Fatalf("warnings = %d, want 0", warnings)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	got := items[0]
	if got.ID != "src-child" || got.Prefix != "dest" || got.Title != "Child" || got.Description != "desc" {
		t.Fatalf("item identity fields = %+v, want mapped bd row", got)
	}
	if got.State != "blocked" || got.Priority != 1 || got.IssueType != "bug" {
		t.Fatalf("item state fields = %+v, want blocked/P1/bug", got)
	}
	if strings.Join(got.Labels, ",") != "one,two" || got.BranchName != "fix/child" || got.URL == "" {
		t.Fatalf("item metadata fields = %+v, want labels/branch/url", got)
	}
	if len(got.Deps) != 1 || got.Deps[0] != "src-parent" {
		t.Fatalf("deps = %#v, want only src-parent", got.Deps)
	}
	if got.Repo != nil {
		t.Fatalf("repo = %+v, want nil for older JSONL without repo", got.Repo)
	}
}

func TestParseImportJSONLMapsRepoMetadata(t *testing.T) {
	t.Parallel()

	creationCommit := strings.Repeat("b", 40)
	input := strings.NewReader(`{"id":"src-linked","title":"Linked","status":"open","priority":2,"issue_type":"task","repo":{"slug":"api","remote_url":"https://github.com/acme/api","default_branch":"main","clone_strategy":"fresh-clone","requested_ref":"feature","base_ref":"main","work_branch":"work/src-linked","worktree_subdir":"services/api","auth_ref":"ssh-key:default","metadata":{"lane":"blue"},"creation_commit":"` + creationCommit + `"}}`)

	items, warnings, err := parseImportJSONL(input, "dest")
	if err != nil {
		t.Fatalf("parseImportJSONL: %v", err)
	}
	if warnings != 0 || len(items) != 1 {
		t.Fatalf("parseImportJSONL = items:%d warnings:%d, want 1/0", len(items), warnings)
	}
	got := items[0].Repo
	if got == nil {
		t.Fatal("Repo = nil, want parsed repo metadata")
	}
	if got.RepoSlug != "api" || got.RemoteURL != "https://github.com/acme/api" || got.DefaultBranch != "main" ||
		got.CloneStrategy != "fresh-clone" {
		t.Fatalf("repo identity = %+v, want slug/remote/default branch", got)
	}
	if got.CreationCommit != creationCommit {
		t.Fatalf("creation_commit = %q, want %q", got.CreationCommit, creationCommit)
	}
	if got.RequestedRef != "feature" || got.BaseRef != "main" ||
		got.WorkBranch != "work/src-linked" || got.WorktreeSubdir != "services/api" {
		t.Fatalf("repo routing fields = %+v, want refs/subdir", got)
	}
	if got.Metadata["lane"] != "blue" {
		t.Fatalf("repo metadata = %#v, want lane=blue", got.Metadata)
	}
}

func TestParseImportJSONLRejectsRepoCreationCommitWithoutIdentity(t *testing.T) {
	t.Parallel()

	creationCommit := strings.Repeat("c", 40)
	input := strings.NewReader(`{"id":"src-linked","title":"Linked","status":"open","priority":2,"issue_type":"task","repo":{"creation_commit":"` + creationCommit + `"}}`)

	_, _, err := parseImportJSONL(input, "dest")
	if err == nil || !strings.Contains(err.Error(), "creation_commit requires repo.remote_url or repo.slug") {
		t.Fatalf("parseImportJSONL error = %v, want clear repo identity error", err)
	}
}

func TestParseImportJSONLRejectsInvalidRepoCreationCommit(t *testing.T) {
	t.Parallel()

	input := strings.NewReader(`{"id":"src-linked","title":"Linked","status":"open","priority":2,"issue_type":"task","repo":{"slug":"api","creation_commit":"HEAD"}}`)

	_, _, err := parseImportJSONL(input, "dest")
	if err == nil || !strings.Contains(err.Error(), "repo.creation_commit") ||
		!strings.Contains(err.Error(), "full lowercase 40-character hex object ID") {
		t.Fatalf("parseImportJSONL error = %v, want clear creation_commit validation error", err)
	}
}

func TestParseImportJSONLRoutesParentChildEdges(t *testing.T) {
	t.Parallel()

	input := strings.NewReader(`{"id":"src-leaf","title":"Leaf","status":"open","priority":1,"issue_type":"task","dependencies":[{"issue_id":"src-leaf","depends_on_id":"src-blocker","type":"blocks"},{"issue_id":"src-leaf","depends_on_id":"src-epic","type":"parent-child"}]}`)

	items, warnings, err := parseImportJSONL(input, "dest")
	if err != nil {
		t.Fatalf("parseImportJSONL: %v", err)
	}
	if warnings != 0 {
		t.Fatalf("warnings = %d, want 0", warnings)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	got := items[0]
	if len(got.Deps) != 1 || got.Deps[0] != "src-blocker" {
		t.Fatalf("deps = %#v, want [src-blocker]", got.Deps)
	}
	if len(got.ParentEdges) != 1 || got.ParentEdges[0] != "src-epic" {
		t.Fatalf("parentEdges = %#v, want [src-epic]", got.ParentEdges)
	}
}

func TestImportGastownhallBeadsExportFixtureSmoke(t *testing.T) {
	ctx := context.Background()
	prefix := "symphony"

	f, err := os.Open(filepath.Join("testdata", "gastownhall_beads_export.jsonl"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	items, warnings, err := parseImportJSONL(f, prefix)
	if err != nil {
		t.Fatalf("parseImportJSONL: %v", err)
	}
	if warnings != 0 || len(items) != 3 {
		t.Fatalf("parsed warnings=%d len=%d, want warnings=0 len=3", warnings, len(items))
	}

	st, err := store.New(ctx, store.Config{
		Driver: store.DriverSQLite,
		DSN:    store.SecretDSN("file:" + filepath.Join(t.TempDir(), "beans.db")),
	})
	if err != nil {
		t.Fatalf("store.New sqlite: %v", err)
	}
	defer st.Close()
	if err := st.EnsureProject(ctx, prefix); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	result, err := st.ImportIssuesFull(ctx, items, store.ImportOptions{
		TerminalStates: []model.IssueState{"closed", "done"},
		Mode:           store.ImportModeCreateOnly,
	})
	if err != nil {
		t.Fatalf("ImportIssuesFull first import: %v", err)
	}
	if result.Created != 3 || result.DepsAdded != 1 {
		t.Fatalf("first import result = %+v, want created=3 deps_added=1", result)
	}

	openIssue, err := st.GetIssue(ctx, "local-symphony-smoke-open")
	if err != nil {
		t.Fatalf("GetIssue open: %v", err)
	}
	if openIssue.State != "open" ||
		openIssue.Title != "Legacy open issue" ||
		openIssue.Description != "Open issue from gastownhall/beads export" ||
		openIssue.Priority != model.PriorityMedium ||
		openIssue.IssueType != "task" ||
		!slices.Equal(openIssue.Labels, []string{"legacy", "import"}) {
		t.Fatalf("open issue = %+v, want imported bd fields", openIssue)
	}

	progressIssue, err := st.GetIssue(ctx, "local-symphony-smoke-progress")
	if err != nil {
		t.Fatalf("GetIssue progress: %v", err)
	}
	if progressIssue.State != "in_progress" ||
		progressIssue.Priority != model.PriorityHigh ||
		progressIssue.IssueType != "bug" ||
		len(progressIssue.BlockedBy) != 1 ||
		progressIssue.BlockedBy[0] != "local-symphony-smoke-open" {
		t.Fatalf("progress issue = %+v, want in-progress bug blocked by open issue", progressIssue)
	}

	closedIssue, err := st.GetIssue(ctx, "local-symphony-smoke-closed")
	if err != nil {
		t.Fatalf("GetIssue closed: %v", err)
	}
	if closedIssue.State != "closed" ||
		closedIssue.Priority != model.PriorityLow ||
		closedIssue.IssueType != "feature" ||
		!slices.Equal(closedIssue.Labels, []string{"legacy", "done"}) {
		t.Fatalf("closed issue = %+v, want closed feature with labels", closedIssue)
	}

	if err := st.CloseIssue(ctx, "local-symphony-smoke-open", "test", "terminal before rerun"); err != nil {
		t.Fatalf("CloseIssue open before rerun: %v", err)
	}
	rerun, err := st.ImportIssuesFull(ctx, items, store.ImportOptions{
		TerminalStates: []model.IssueState{"closed", "done"},
		Mode:           store.ImportModeCreateOnly,
	})
	if err != nil {
		t.Fatalf("ImportIssuesFull rerun: %v", err)
	}
	if rerun.Created != 0 || rerun.Skipped != 3 || rerun.DepsAdded != 0 {
		t.Fatalf("rerun result = %+v, want idempotent skipped=3 with no deps added", rerun)
	}
	openIssue, err = st.GetIssue(ctx, "local-symphony-smoke-open")
	if err != nil {
		t.Fatalf("GetIssue open after rerun: %v", err)
	}
	if openIssue.State != "closed" {
		t.Fatalf("open issue state after rerun = %q, want closed terminal state preserved", openIssue.State)
	}
}

func TestParseImportJSONLIgnoresBlankLinesAndComments(t *testing.T) {
	t.Parallel()

	input := strings.NewReader(strings.Join([]string{
		"",
		"# generated by bd export",
		`{"id":"src-one","title":"one","status":"open","priority":2,"issue_type":"task"}`,
	}, "\n"))

	items, warnings, err := parseImportJSONL(input, "dest")
	if err != nil {
		t.Fatalf("parseImportJSONL: %v", err)
	}
	if warnings != 0 {
		t.Fatalf("warnings = %d, want 0", warnings)
	}
	if len(items) != 1 || items[0].ID != "src-one" {
		t.Fatalf("items = %+v, want src-one only", items)
	}
}

func TestParseImportJSONLSkipsInvalidRows(t *testing.T) {
	t.Parallel()

	input := strings.NewReader(strings.Join([]string{
		`not json`,
		`{"id":"","title":"missing id","status":"open","priority":2,"issue_type":"task"}`,
		`{"id":"src-missing-title","status":"open","priority":2,"issue_type":"task"}`,
		`{"id":"src-bad-priority","title":"bad priority","status":"open","priority":7,"issue_type":"task"}`,
		`{"id":"src-good","title":"good","status":"open","priority":2,"issue_type":"task"}`,
	}, "\n"))

	items, warnings, err := parseImportJSONL(input, "dest")
	if err != nil {
		t.Fatalf("parseImportJSONL: %v", err)
	}
	if warnings != 4 {
		t.Fatalf("warnings = %d, want 4", warnings)
	}
	if len(items) != 1 || items[0].ID != "src-good" {
		t.Fatalf("items = %+v, want src-good only", items)
	}
}

func TestImportSummaryJSONIncludesDryRunMetadata(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := writeImportSummary(cmd, true, importSummary{
		DryRun:   true,
		Parsed:   3,
		Warnings: 1,
	})
	if err != nil {
		t.Fatalf("writeImportSummary: %v", err)
	}

	var got importSummary
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal summary: %v", err)
	}
	if !got.DryRun || got.Parsed != 3 || got.Warnings != 1 {
		t.Fatalf("summary = %+v, want dry-run parsed/warnings", got)
	}
}

func TestImportDryRunDoesNotRequireDSN(t *testing.T) {
	t.Setenv("BN_DSN", "")
	t.Setenv("BN_PROJECT", "dry")

	rs := &appState{}
	cmd := newRootCmd(rs)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader(`{"id":"dry-one","title":"one","status":"open","priority":2,"issue_type":"task"}`))
	cmd.SetArgs([]string{"--json", "import", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("dry-run Execute: %v", err)
	}

	var got importSummary
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal summary: %v", err)
	}
	if !got.DryRun || got.Parsed != 1 || got.Warnings != 0 {
		t.Fatalf("summary = %+v, want parse-only dry run", got)
	}
	if rs.store != nil {
		t.Fatal("dry-run initialized store; want parse-only without DB")
	}
}

// TestImportCrossRepoNoCrossPrefixConflict verifies that importing a JSONL file
// whose issue IDs carry a different prefix token does not trigger
// CrossPrefixConflicts under per-repo topology (prefix == slug).
// CrossPrefixConflict only fires when the SAME ID already exists in the DB under
// a DIFFERENT prefix — a genuine ambiguity, not a slug-derivation coincidence.
func TestImportCrossRepoNoCrossPrefixConflict(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	destStore, _ := newTestStore(t, "repo-b", "")

	// JSONL from repo-a: IDs carry "repo-a-" prefix token, differing from dest.
	jsonl := strings.Join([]string{
		`{"id":"repo-a-abc123","title":"API task","status":"open","priority":2,"issue_type":"task"}`,
		`{"id":"repo-a-def456","title":"API task 2","status":"open","priority":1,"issue_type":"feature"}`,
	}, "\n")

	items, warnings, err := parseImportJSONL(strings.NewReader(jsonl), "repo-b")
	if err != nil || warnings != 0 || len(items) != 2 {
		t.Fatalf("parseImportJSONL = items:%d warnings:%d err:%v, want 2/0/nil", len(items), warnings, err)
	}

	result, err := destStore.ImportIssuesFull(ctx, items, store.ImportOptions{
		TerminalStates: activeWorkflow.Terminal,
		Mode:           store.ImportModeCreateOnly,
	})
	if err != nil {
		t.Fatalf("ImportIssuesFull: %v", err)
	}
	if result.CrossPrefixConflicts != 0 {
		t.Errorf("CrossPrefixConflicts = %d, want 0 — cross-repo import must not false-conflict", result.CrossPrefixConflicts)
	}
	if result.Created != 2 {
		t.Errorf("Created = %d, want 2", result.Created)
	}

	// Re-import the same items: must be idempotent (skipped, no new conflicts).
	result2, err := destStore.ImportIssuesFull(ctx, items, store.ImportOptions{
		TerminalStates: activeWorkflow.Terminal,
		Mode:           store.ImportModeCreateOnly,
	})
	if err != nil {
		t.Fatalf("re-import ImportIssuesFull: %v", err)
	}
	if result2.CrossPrefixConflicts != 0 {
		t.Errorf("re-import CrossPrefixConflicts = %d, want 0", result2.CrossPrefixConflicts)
	}
	if result2.Skipped != 2 {
		t.Errorf("re-import Skipped = %d, want 2 (idempotent)", result2.Skipped)
	}
}

func TestImportJSONLOlderExportWithoutRepoStaysCompatible(t *testing.T) {
	ctx := context.Background()
	jsonl := `{"id":"src-older","title":"Older export","description":"pre repo payload","status":"open","priority":2,"issue_type":"task","labels":["legacy"],"dependencies":[]}`

	items, warnings, err := parseImportJSONL(strings.NewReader(jsonl), "dest")
	if err != nil || warnings != 0 || len(items) != 1 {
		t.Fatalf("parseImportJSONL = items:%d warnings:%d err:%v, want 1/0/nil", len(items), warnings, err)
	}
	if items[0].Repo != nil {
		t.Fatalf("parsed repo = %+v, want nil for older export", items[0].Repo)
	}

	st, _ := newTestStore(t, "dest", "")
	result, err := st.ImportIssuesFull(ctx, items, store.ImportOptions{
		TerminalStates: activeWorkflow.Terminal,
		Mode:           store.ImportModeCreateOnly,
	})
	if err != nil {
		t.Fatalf("ImportIssuesFull: %v", err)
	}
	if result.Created != 1 {
		t.Fatalf("Created = %d, want 1", result.Created)
	}
	got, err := st.GetIssue(ctx, "src-older")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Repo != nil {
		t.Fatalf("imported repo = %+v, want nil for older export", got.Repo)
	}
}

func TestImportJSONLRepoRemoteURLCreatesRepoLinkWithCreationCommit(t *testing.T) {
	ctx := context.Background()
	creationCommit := strings.Repeat("d", 40)
	jsonl := `{"id":"src-linked","title":"Linked","status":"open","priority":2,"issue_type":"task","repo":{"remote_url":"https://github.com/acme/widgets","clone_strategy":"fresh-clone","requested_ref":"feature","base_ref":"main","work_branch":"work/src-linked","worktree_subdir":"services/api","metadata":{"lane":"blue"},"creation_commit":"` + creationCommit + `"}}`

	items, warnings, err := parseImportJSONL(strings.NewReader(jsonl), "dest")
	if err != nil || warnings != 0 || len(items) != 1 {
		t.Fatalf("parseImportJSONL = items:%d warnings:%d err:%v, want 1/0/nil", len(items), warnings, err)
	}

	st, _ := newTestStore(t, "dest", "")
	result, err := st.ImportIssuesFull(ctx, items, store.ImportOptions{
		TerminalStates: activeWorkflow.Terminal,
		Mode:           store.ImportModeCreateOnly,
	})
	if err != nil {
		t.Fatalf("ImportIssuesFull: %v", err)
	}
	if result.Created != 1 {
		t.Fatalf("Created = %d, want 1", result.Created)
	}
	got, err := st.GetIssue(ctx, "src-linked")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Repo == nil {
		t.Fatal("Repo = nil, want imported repo link")
	}
	if got.Repo.CreationCommit != creationCommit {
		t.Fatalf("creation_commit = %q, want %q", got.Repo.CreationCommit, creationCommit)
	}
	if !strings.Contains(got.Repo.RemoteURL, "github.com/acme/widgets") {
		t.Fatalf("remote_url = %q, want acme/widgets repo", got.Repo.RemoteURL)
	}
	if got.Repo.RequestedRef != "feature" || got.Repo.BaseRef != "main" ||
		got.Repo.WorkBranch != "work/src-linked" || got.Repo.WorktreeSubdir != "services/api" {
		t.Fatalf("repo routing fields = %+v, want imported refs/subdir", got.Repo)
	}
	if got.Repo.Metadata["lane"] != "blue" {
		t.Fatalf("repo metadata = %#v, want lane=blue", got.Repo.Metadata)
	}
	if got.Repo.CloneStrategy != "fresh-clone" {
		t.Fatalf("clone_strategy = %q, want fresh-clone", got.Repo.CloneStrategy)
	}
}

func TestImportJSONLExistingRepoSlugCreatesRepoLinkWithCreationCommit(t *testing.T) {
	ctx := context.Background()
	creationCommit := strings.Repeat("a", 40)
	st, repo := newTestStore(t, "", "https://github.com/acme/api")
	jsonl := `{"id":"src-slug-linked","title":"Slug linked","status":"open","priority":2,"issue_type":"task","repo":{"slug":"` + repo.Slug + `","requested_ref":"release","base_ref":"main","work_branch":"work/src-slug-linked","worktree_subdir":"services/api","metadata":{"lane":"green"},"creation_commit":"` + creationCommit + `"}}`

	items, warnings, err := parseImportJSONL(strings.NewReader(jsonl), repo.Prefix)
	if err != nil || warnings != 0 || len(items) != 1 {
		t.Fatalf("parseImportJSONL = items:%d warnings:%d err:%v, want 1/0/nil", len(items), warnings, err)
	}

	result, err := st.ImportIssuesFull(ctx, items, store.ImportOptions{
		TerminalStates: activeWorkflow.Terminal,
		Mode:           store.ImportModeCreateOnly,
	})
	if err != nil {
		t.Fatalf("ImportIssuesFull: %v", err)
	}
	if result.Created != 1 {
		t.Fatalf("Created = %d, want 1", result.Created)
	}
	got, err := st.GetIssue(ctx, "src-slug-linked")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Repo == nil {
		t.Fatal("Repo = nil, want imported repo link")
	}
	if got.Repo.Slug != repo.Slug || got.Repo.RemoteURL != repo.RemoteURL {
		t.Fatalf("repo identity = %+v, want existing slug %q", got.Repo, repo.Slug)
	}
	if got.Repo.CreationCommit != creationCommit {
		t.Fatalf("creation_commit = %q, want %q", got.Repo.CreationCommit, creationCommit)
	}
	if got.Repo.RequestedRef != "release" || got.Repo.BaseRef != "main" ||
		got.Repo.WorkBranch != "work/src-slug-linked" || got.Repo.WorktreeSubdir != "services/api" {
		t.Fatalf("repo routing fields = %+v, want imported refs/subdir", got.Repo)
	}
	if got.Repo.Metadata["lane"] != "green" {
		t.Fatalf("repo metadata = %#v, want lane=green", got.Repo.Metadata)
	}
}

func TestImportJSONLMergeReplacesRepoRoutingAndPreservesCreationCommit(t *testing.T) {
	ctx := context.Background()
	creationCommit := strings.Repeat("f", 40)
	st, repoA := newTestStore(t, "", "https://github.com/acme/api")
	if err := st.AddRepoAdmin(ctx, repoA.Prefix, "test", "test", true); err != nil {
		t.Fatalf("AddRepoAdmin bootstrap: %v", err)
	}
	repoB, err := st.CreateRepo(ctx, store.CreateRepoInput{
		Prefix:        repoA.Prefix,
		Slug:          "worker",
		RemoteURL:     "https://github.com/acme/worker",
		CloneStrategy: "fresh-clone",
		AuthRef:       "test:none",
		Actor:         "test",
	})
	if err != nil {
		t.Fatalf("CreateRepo(worker): %v", err)
	}

	created, err := st.CreateIssue(ctx, store.CreateIssueInput{
		Prefix: repoA.Prefix,
		Title:  "Linked",
		Actor:  "test",
		Repo: &store.IssueRepoInput{
			RepoSlug:       repoA.Slug,
			RequestedRef:   "old-ref",
			CreationCommit: creationCommit,
			Metadata:       map[string]any{"lane": "old"},
		},
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	jsonl := `{"id":"` + created.ID + `","title":"Linked merged","status":"open","priority":2,"issue_type":"task","repo":{"slug":"` + repoB.Slug + `","requested_ref":"new-ref","base_ref":"main","work_branch":"work/merged","worktree_subdir":"services/worker","metadata":{"lane":"new"}}}`
	items, warnings, err := parseImportJSONL(strings.NewReader(jsonl), repoA.Prefix)
	if err != nil || warnings != 0 || len(items) != 1 {
		t.Fatalf("parseImportJSONL = items:%d warnings:%d err:%v, want 1/0/nil", len(items), warnings, err)
	}

	result, err := st.ImportIssuesFull(ctx, items, store.ImportOptions{
		TerminalStates: activeWorkflow.Terminal,
		Mode:           store.ImportModeMerge,
	})
	if err != nil {
		t.Fatalf("ImportIssuesFull merge: %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("Updated = %d, want 1", result.Updated)
	}
	got, err := st.GetIssue(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Repo == nil || got.Repo.Slug != repoB.Slug || got.Repo.RequestedRef != "new-ref" ||
		got.Repo.WorkBranch != "work/merged" || got.Repo.WorktreeSubdir != "services/worker" {
		t.Fatalf("repo after merge = %+v, want replacement worker routing", got.Repo)
	}
	if got.Repo.CreationCommit != creationCommit {
		t.Fatalf("creation_commit = %q, want preserved %q", got.Repo.CreationCommit, creationCommit)
	}
	if got.Repo.Metadata["lane"] != "new" {
		t.Fatalf("repo metadata = %#v, want lane=new", got.Repo.Metadata)
	}
}

func TestImportJSONLRepoSlugCreationCommitRequiresRegisteredRepo(t *testing.T) {
	ctx := context.Background()
	creationCommit := strings.Repeat("e", 40)
	jsonl := `{"id":"src-linked","title":"Linked","status":"open","priority":2,"issue_type":"task","repo":{"slug":"missing","creation_commit":"` + creationCommit + `"}}`

	items, warnings, err := parseImportJSONL(strings.NewReader(jsonl), "dest")
	if err != nil || warnings != 0 || len(items) != 1 {
		t.Fatalf("parseImportJSONL = items:%d warnings:%d err:%v, want 1/0/nil", len(items), warnings, err)
	}

	st, _ := newTestStore(t, "dest", "")
	_, err = st.ImportIssuesFull(ctx, items, store.ImportOptions{
		TerminalStates: activeWorkflow.Terminal,
		Mode:           store.ImportModeCreateOnly,
	})
	if err == nil || !strings.Contains(err.Error(), "creation_commit requires resolvable repo.slug") ||
		!strings.Contains(err.Error(), "missing") {
		t.Fatalf("ImportIssuesFull error = %v, want clear unresolved repo slug creation_commit error", err)
	}
}

func TestImportJSONLInvalidStateDoesNotAutoRegisterRepo(t *testing.T) {
	ctx := context.Background()
	st, _ := newTestStore(t, "dest", "")

	result, err := st.ImportIssuesFull(ctx, []store.ImportInput{{
		ID:        "src-invalid",
		Prefix:    "dest",
		Title:     "Invalid",
		State:     "archived",
		Priority:  2,
		IssueType: "task",
		Repo: &store.ImportRepoInput{
			RemoteURL: "https://github.com/acme/side-effect",
		},
	}}, store.ImportOptions{
		TerminalStates: activeWorkflow.Terminal,
		Mode:           store.ImportModeCreateOnly,
	})
	if err != nil {
		t.Fatalf("ImportIssuesFull: %v", err)
	}
	if result.Skipped != 1 {
		t.Fatalf("Skipped = %d, want 1", result.Skipped)
	}
	_, err = st.GetRepoByRemoteURL(ctx, "https://github.com/acme/side-effect")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetRepoByRemoteURL after skipped invalid import = %v, want ErrNotFound", err)
	}
}

func TestImportSummaryFromResultIncludesSkippedEdgeCounts(t *testing.T) {
	t.Parallel()

	got := importSummaryFromResult(store.ImportResult{
		Created:                   1,
		Updated:                   2,
		Skipped:                   3,
		CrossPrefixConflicts:      4,
		DepsAdded:                 5,
		DepsSkippedMissingBlocker: 6,
		DepsSkippedDuplicate:      7,
		DepsSkippedSelf:           8,
		DepsSkippedCycle:          9,
	}, 10, 11)

	if got.Created != 1 || got.Updated != 2 || got.Skipped != 3 || got.CrossPrefixConflicts != 4 {
		t.Fatalf("issue summary = %+v, want result counts", got)
	}
	if got.DepsAdded != 5 || got.DepsSkippedMissingBlocker != 6 || got.DepsSkippedDuplicate != 7 ||
		got.DepsSkippedSelf != 8 || got.DepsSkippedCycle != 9 {
		t.Fatalf("dep summary = %+v, want result counts", got)
	}
	if got.Parsed != 10 || got.Warnings != 11 {
		t.Fatalf("parse summary = %+v, want parsed/warnings", got)
	}
}
