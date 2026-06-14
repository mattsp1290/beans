package main

import (
	"bytes"
	"context"
	"encoding/json"
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
