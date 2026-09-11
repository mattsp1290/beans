package plan

import (
	"os"
	"path/filepath"
	"strings"
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

func TestScaffoldSafelyEncodesSpecialTitle(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "draft")
	title := "Ship: phase #1 \"quoted\""
	if err := WriteScaffold(dir, "beans-plan-a3f2", title, time.Now()); err != nil {
		t.Fatal(err)
	}
	b, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if b.Plan.Title != title {
		t.Fatalf("title = %q, want %q", b.Plan.Title, title)
	}
}

func TestParseRejectsUnknownFrontmatter(t *testing.T) {
	data := Scaffold("beans-plan-a3f2", "x", time.Now())
	data = []byte(strings.Replace(string(data), "title:", "provenance: agent\ntitle:", 1))
	if _, err := Parse("plan.md", data); err == nil {
		t.Fatal("accepted unknown frontmatter")
	}
}

func TestLoadRetainsOrderedSectionBodies(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "draft")
	if err := WriteScaffold(dir, "beans-plan-a3f2", "x", time.Now()); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	manifest = []byte(strings.Replace(string(manifest), "updated:", "sections:\n  - sections/02.md\n  - sections/01.md\nupdated:", 1))
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sections"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ name, body string }{{"01.md", "# First\n"}, {"02.md", "# Second\n"}} {
		if err := os.WriteFile(filepath.Join(dir, "sections", item.name), []byte(item.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Plan.SectionBodies) != 2 || b.Plan.SectionBodies[0].Path != "sections/02.md" || b.Plan.SectionBodies[1].Markdown != "# First\n" {
		t.Fatalf("unexpected section bodies: %#v", b.Plan.SectionBodies)
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

func TestParseGraphRejectsUnknownFields(t *testing.T) {
	_, err := ParseGraph("plan.md", "```bn-change-graph\nversion: 1\nnodes:\n  - id: model\n    label: Model\n    kind: component\n    color: red\nedges: []\n```")
	if err == nil {
		t.Fatal("ParseGraph accepted unknown node field")
	}
}

func TestParseGraphRejectsAliasAndCustomTag(t *testing.T) {
	for _, text := range []string{
		"```bn-change-graph\nversion: 1\nnodes: &nodes []\nedges: *nodes\n```",
		"```bn-change-graph\nversion: !beans 1\nnodes: []\nedges: []\n```",
	} {
		if _, err := ParseGraph("plan.md", text); err == nil {
			t.Fatal("ParseGraph accepted unsafe YAML")
		}
	}
}
