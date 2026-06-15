package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mattsp1290/beans/model"
	store "github.com/mattsp1290/beans/store"
)

func TestWriteExportJSONLEmitsBDCompatibleLines(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	var out strings.Builder
	depsByChild := map[string][]store.DepEdge{
		"proj-child": {{IssueID: "proj-child", BlockedByID: "proj-parent", DepType: store.DepTypeBlocks}},
	}
	err := writeExportJSONL(&out, []store.Issue{
		{
			Issue: model.Issue{
				ID:          "proj-child",
				Title:       "child",
				Description: "blocked work",
				Priority:    model.PriorityHigh,
				State:       "open",
				Labels:      []string{"backend"},
				BlockedBy:   []string{"proj-parent"},
				BranchName:  "fix/child",
				URL:         "https://example.test/proj-child",
				CreatedAt:   now,
				UpdatedAt:   now,
			},
			IssueType: "task",
		},
	}, depsByChild)
	if err != nil {
		t.Fatalf("writeExportJSONL: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("line count = %d, want 1: %q", len(lines), out.String())
	}

	var got bdExportLine
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("unmarshal export line: %v", err)
	}
	if got.ID != "proj-child" || got.Status != "open" || got.Priority != 1 || got.IssueType != "task" {
		t.Fatalf("export line core fields = %+v, want id/status/priority/issue_type", got)
	}
	if len(got.Labels) != 1 || got.Labels[0] != "backend" {
		t.Fatalf("labels = %#v, want [backend]", got.Labels)
	}
	if len(got.Dependencies) != 1 {
		t.Fatalf("dependencies = %#v, want 1 edge", got.Dependencies)
	}
	dep := got.Dependencies[0]
	if dep.IssueID != "proj-child" || dep.DependsOn != "proj-parent" || dep.Type != "blocks" {
		t.Fatalf("dependency = %+v, want child -> parent blocks edge", dep)
	}
}

// TestExportImportRoundTripsMixedEdges locks the serialization boundary: a child
// carrying BOTH a blocks and a parent-child edge is exported with each edge's
// type in deterministic order, and re-parsing the line routes them back to the
// blocking (Deps) and membership (ParentEdges) buckets respectively.
func TestExportImportRoundTripsMixedEdges(t *testing.T) {
	t.Parallel()

	iss := store.Issue{
		Issue: model.Issue{
			ID:       "proj-leaf",
			Title:    "Leaf",
			Priority: model.PriorityMedium,
			State:    "open",
		},
		IssueType: "task",
	}
	// As ListDeps returns them: ordered by (blocked_by_id, dep_type).
	edges := []store.DepEdge{
		{IssueID: "proj-leaf", BlockedByID: "proj-blocker", DepType: store.DepTypeBlocks},
		{IssueID: "proj-leaf", BlockedByID: "proj-epic", DepType: store.DepTypeParentChild},
	}

	line := toBDExportLine(iss, edges)
	if len(line.Dependencies) != 2 {
		t.Fatalf("dependencies = %#v, want 2 edges", line.Dependencies)
	}
	// Deterministic order preserved from the input edge slice.
	if line.Dependencies[0].Type != store.DepTypeBlocks || line.Dependencies[0].DependsOn != "proj-blocker" {
		t.Fatalf("dep[0] = %+v, want blocks->proj-blocker", line.Dependencies[0])
	}
	if line.Dependencies[1].Type != store.DepTypeParentChild || line.Dependencies[1].DependsOn != "proj-epic" {
		t.Fatalf("dep[1] = %+v, want parent-child->proj-epic", line.Dependencies[1])
	}

	// Round-trip: marshal the line and feed it back through the import parser.
	var buf strings.Builder
	if err := writeExportJSONL(&buf, []store.Issue{iss}, map[string][]store.DepEdge{iss.ID: edges}); err != nil {
		t.Fatalf("writeExportJSONL: %v", err)
	}
	items, warnings, err := parseImportJSONL(strings.NewReader(buf.String()), "dest")
	if err != nil || warnings != 0 || len(items) != 1 {
		t.Fatalf("parseImportJSONL = items:%d warnings:%d err:%v, want 1/0/nil", len(items), warnings, err)
	}
	got := items[0]
	if len(got.Deps) != 1 || got.Deps[0] != "proj-blocker" {
		t.Fatalf("Deps = %#v, want [proj-blocker]", got.Deps)
	}
	if len(got.ParentEdges) != 1 || got.ParentEdges[0] != "proj-epic" {
		t.Fatalf("ParentEdges = %#v, want [proj-epic]", got.ParentEdges)
	}
}

func TestToBDExportLineUsesEmptySlices(t *testing.T) {
	t.Parallel()

	got := toBDExportLine(store.Issue{
		Issue: model.Issue{
			ID:       "proj-empty",
			Title:    "empty",
			Priority: model.PriorityMedium,
			State:    "open",
		},
		IssueType: "task",
	}, nil)

	if got.Labels == nil {
		t.Fatal("labels = nil, want empty slice")
	}
	if got.Dependencies == nil {
		t.Fatal("dependencies = nil, want empty slice")
	}
}
