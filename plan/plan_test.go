package plan

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScaffoldLoadsAsDraft(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "draft")
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	if err := WriteScaffold(dir, "beans-plan-a3f2", "Add plan artifacts", now); err != nil {
		t.Fatal(err)
	}
	b, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if b.Plan.ID != "beans-plan-a3f2" || b.Plan.Slug != "add-plan-artifacts" || b.Plan.Status != StatusDraft {
		t.Fatalf("unexpected plan: %#v", b.Plan)
	}
}

func TestLoadRejectsUnlistedSection(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "draft")
	if err := WriteScaffold(dir, "beans-plan-a3f2", "x", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sections"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sections", "extra.md"), []byte("# extra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("Load accepted unlisted section")
	}
}

func TestReadyRequiresStructuredSummary(t *testing.T) {
	p := &Plan{Status: StatusReady, Summary: Summary{Outcome: "outcome", AffectedAreas: "- area", ExecutionOrder: "1. step", Risks: "- None."}, Graph: ChangeGraph{Version: 1, Nodes: []GraphNode{{ID: "model", Label: "Model", Kind: "component"}}}}
	if err := Validate(p); err != nil {
		t.Fatal(err)
	}
	p.Summary.ExecutionOrder = "prose"
	if err := Validate(p); err == nil {
		t.Fatal("ready plan accepted prose execution order")
	}
}
