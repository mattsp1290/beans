package dto

import (
	"testing"

	appstore "github.com/mattsp1290/bean-counter/internal/store"
)

func TestDependenciesFromStore(t *testing.T) {
	got := DependenciesFromStore([]appstore.DepEdge{{IssueID: "child", BlockedByID: "parent"}})
	if len(got) != 1 || got[0].IssueID != "child" || got[0].BlockedByID != "parent" {
		t.Fatalf("dependencies = %+v", got)
	}
}

func TestGraphResponseFromStore(t *testing.T) {
	issues := []appstore.Issue{{IssueType: "task"}}
	issues[0].ID = "child"
	issues[0].Title = "Child"
	issues[0].State = "open"
	issues[0].Priority = 3
	issues[0].Labels = []string{"api"}
	deps := []appstore.DepEdge{{IssueID: "child", BlockedByID: "parent"}}

	got := GraphResponseFromStore(issues, deps)
	if len(got.Nodes) != 1 || got.Nodes[0].ID != "child" || got.Nodes[0].State != "open" {
		t.Fatalf("nodes = %+v", got.Nodes)
	}
	if got.Nodes[0].Priority != 2 {
		t.Fatalf("node priority = %d, want API priority 2", got.Nodes[0].Priority)
	}
	if len(got.Edges) != 1 || got.Edges[0].Source != "parent" || got.Edges[0].Target != "child" {
		t.Fatalf("edges = %+v", got.Edges)
	}

	issues[0].Labels[0] = "mutated"
	if got.Nodes[0].Labels[0] != "api" {
		t.Fatalf("labels were not copied: %+v", got.Nodes[0].Labels)
	}
}
