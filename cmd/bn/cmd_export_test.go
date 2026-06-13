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
	})
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
	})

	if got.Labels == nil {
		t.Fatal("labels = nil, want empty slice")
	}
	if got.Dependencies == nil {
		t.Fatal("dependencies = nil, want empty slice")
	}
}
