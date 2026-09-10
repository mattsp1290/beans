package vault

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mattsp1290/beans/issue"
)

func TestReadyExcludesBlocked(t *testing.T) {
	ix, _ := loadFixture(t)
	ready := ix.Ready("b", false)
	ids := issueIDs(ready)
	if contains(ids, "b-blocked001") {
		t.Errorf("Ready(\"b\", false) should exclude b-blocked001, got %v", ids)
	}
	if !contains(ids, "b-child001") {
		t.Errorf("Ready(\"b\", false) should include b-child001, got %v", ids)
	}
}

func TestReadyExcludesEpicWithChildren(t *testing.T) {
	ix, _ := loadFixture(t)
	ready := ix.Ready("a", false)
	ids := issueIDs(ready)
	if contains(ids, "a-epic001") {
		t.Errorf("Ready(\"a\", false) should exclude the epic (it has children), got %v", ids)
	}
}

func TestChildrenAcrossProjects(t *testing.T) {
	ix, _ := loadFixture(t)
	children := ix.Children("a-epic001")
	ids := issueIDs(children)
	sort.Strings(ids)
	want := []string{"a-child001", "b-child001"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("Children(a-epic001) = %v, want %v", ids, want)
	}
}

func TestParents(t *testing.T) {
	ix, _ := loadFixture(t)
	parents := ix.Parents("b-child001")
	if len(parents) != 1 || parents[0].ID != "a-epic001" {
		t.Errorf("Parents(b-child001) = %+v, want [a-epic001]", parents)
	}
}

func TestBlockers(t *testing.T) {
	ix, _ := loadFixture(t)
	iss, ok := ix.IssueByID("b-blocked001")
	if !ok {
		t.Fatal("expected b-blocked001 to be indexed")
	}
	resolved, unresolved := ix.Blockers(iss)
	if len(unresolved) != 0 {
		t.Errorf("expected no unresolved blockers, got %v", unresolved)
	}
	if len(resolved) != 1 || resolved[0].ID != "a-open001" {
		t.Errorf("expected resolved blocker a-open001, got %+v", resolved)
	}
}

func TestBlocked(t *testing.T) {
	ix, _ := loadFixture(t)
	blocked := ix.Blocked("", true)
	var found *BlockedIssue
	for i := range blocked {
		if blocked[i].Issue.ID == "b-blocked001" {
			found = &blocked[i]
		}
	}
	if found == nil {
		t.Fatalf("expected b-blocked001 in Blocked(), got %+v", blocked)
	}
	if len(found.Blockers) != 1 || found.Blockers[0] != "a-open001" {
		t.Errorf("expected Blockers=[a-open001], got %v", found.Blockers)
	}
}

func TestGraphCrossProjectEdge(t *testing.T) {
	ix, _ := loadFixture(t)
	nodes, edges := ix.Graph("b", false)

	nodeIDs := map[string]GraphNode{}
	for _, n := range nodes {
		nodeIDs[n.ID] = n
	}
	if _, ok := nodeIDs["a-open001"]; !ok {
		t.Errorf("expected cross-project node a-open001 in Graph(\"b\", false), got %+v", nodes)
	}

	var sawEdge bool
	for _, e := range edges {
		if e.From == "b-blocked001" && e.To == "a-open001" && e.Kind == "blocks" {
			sawEdge = true
		}
	}
	if !sawEdge {
		t.Errorf("expected cross-project blocks edge b-blocked001 -> a-open001, got %+v", edges)
	}
}

func TestCyclesEmptyOnFixture(t *testing.T) {
	ix, _ := loadFixture(t)
	cycles := ix.Cycles()
	if len(cycles) != 0 {
		t.Errorf("expected no cycles on the fixture, got %v", cycles)
	}
}

func TestCyclesDetectsCrossProjectCycle(t *testing.T) {
	dir := copyFixtureHub(t)
	path := filepath.Join(dir, "projects", "a", "issues", "a-open001.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Inject a blocked_by back onto a-open001, closing a-open001 -> b-blocked001 -> a-open001.
	edited := strings.Replace(string(data), "updated: 2026-01-01T00:00:00Z\n---",
		"updated: 2026-01-01T00:00:00Z\nblocked_by:\n  - \"[[b-blocked001]]\"\n---", 1)
	if edited == string(data) {
		t.Fatal("expected to inject blocked_by into a-open001.md")
	}
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	ix, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cycles := ix.Cycles()
	if len(cycles) != 1 {
		t.Fatalf("expected exactly one cycle, got %v", cycles)
	}
	got := append([]string(nil), cycles[0]...)
	sort.Strings(got)
	want := []string{"a-open001", "b-blocked001"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("cycle = %v, want %v", got, want)
	}
}

func TestSearchParity(t *testing.T) {
	ix, _ := loadFixture(t)
	hits := ix.Search("parity", nil)
	if len(hits) < 2 {
		t.Fatalf("expected at least 2 hits for \"parity\", got %+v", hits)
	}
	var sawDoc, sawIssue bool
	for _, h := range hits {
		if h.Basename == "parity" {
			sawDoc = true
		}
		if h.ID == "a-open001" {
			sawIssue = true
		}
		if h.Score < 3 {
			t.Errorf("hit %+v should be a title match (score 3), title contains %q", h, "parity")
		}
	}
	if !sawDoc {
		t.Errorf("expected the parity doc in results, got %+v", hits)
	}
	if !sawIssue {
		t.Errorf("expected a-open001 in results, got %+v", hits)
	}
}

func issueIDs(list []*issue.Issue) []string {
	out := make([]string, 0, len(list))
	for _, iss := range list {
		out = append(out, iss.ID)
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
