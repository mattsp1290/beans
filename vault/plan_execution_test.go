package vault

import (
	"encoding/json"
	"testing"

	"github.com/mattsp1290/beans/plan"
)

func TestPlanExecutionClassifiesBindingsDeterministically(t *testing.T) {
	ix, _ := loadFixture(t)
	p := &plan.Plan{ID: "a-plan-a1b2", Title: "execution", Status: plan.StatusReady, Path: "projects/a/plans/a-plan-a1b2-execution/plan.md", Graph: plan.ChangeGraph{Version: 1, Nodes: []plan.GraphNode{
		{ID: "none", Label: "None", Kind: "component"},
		{ID: "note", Label: "Note", Kind: "component", Ref: "README"},
		{ID: "missing", Label: "Missing", Kind: "component", Ref: "a-missing999"},
		{ID: "open", Label: "Open", Kind: "component", Ref: "a-open001"},
		{ID: "blocked", Label: "Blocked", Kind: "component", Ref: "b-blocked001"},
	}}}
	ix.Plans[p.ID] = p
	ix.ByPath[p.Path] = &Note{Kind: KindPlan, Project: "a", Plan: p, Path: p.Path}
	a, ok := ix.PlanExecution(p.ID)
	if !ok {
		t.Fatal("report missing")
	}
	b, _ := ix.PlanExecution(p.ID)
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Fatal("report JSON is nondeterministic")
	}
	if a.Counts.Unlinked != 1 || a.Counts.Reference != 1 || a.Counts.MissingIssue != 1 || a.Counts.Runnable != 1 || a.Counts.Blocked != 1 {
		t.Fatalf("counts = %+v", a.Counts)
	}
	if a.ExecutionState != "degraded" {
		t.Fatalf("state = %s", a.ExecutionState)
	}
	if a.Nodes[4].Binding != PlanBindingIssue || a.Nodes[4].WorkState != PlanWorkBlocked || len(a.Nodes[4].Blockers) == 0 {
		t.Fatalf("blocked node = %+v", a.Nodes[4])
	}
}
