package handlers_test

import (
	"context"
	"net/http"
	"testing"

	appstore "github.com/mattsp1290/bean-counter/internal/store"
)

// deptypeIssue captures the blocked_by array of GET /api/v1/issues/:id, which
// the shared sqliteIssueResponse omits.
type deptypeIssue struct {
	ID        string   `json:"id"`
	BlockedBy []string `json:"blocked_by"`
}

// TestSQLiteDepsAndGraphIgnoreParentChildEdges proves the beans 0008 upgrade is
// behavior-preserving for bean-counter: /deps, /graph, and an issue's blocked_by
// surface only blocking edges, never the parent-child membership edges beans now
// stores. Uses two DISTINCT ordered pairs to avoid the (issue_id, blocked_by_id)
// primary-key collision (one edge per pair, any kind).
func TestSQLiteDepsAndGraphIgnoreParentChildEdges(t *testing.T) {
	app, store, closeStore := sqliteHandlersAppWithStore(t)
	defer closeStore()
	ctx := context.Background()

	// Blocking edge via the HTTP route (always dep_type="blocks").
	blockParent := sqliteCreateIssue(t, app, `{"title":"Block parent","priority":1,"issue_type":"task"}`)
	blockChild := sqliteCreateIssue(t, app, `{"title":"Block child","priority":2,"issue_type":"task"}`)
	sqliteDependency(t, app, http.MethodPost, "/api/v1/issues/"+blockChild.ID+"/deps",
		`{"blocked_by_id":"`+blockParent.ID+`"}`, http.StatusCreated)

	// Parent-child membership edge — distinct pair, seeded directly (no HTTP route).
	epic := sqliteCreateIssue(t, app, `{"title":"Epic","priority":1,"issue_type":"epic"}`)
	leaf := sqliteCreateIssue(t, app, `{"title":"Leaf","priority":2,"issue_type":"task"}`)
	if err := store.AddTypedDep(ctx, leaf.ID, epic.ID, appstore.DepTypeParentChild); err != nil {
		t.Fatalf("seed parent-child edge: %v", err)
	}

	// /deps shows only the blocking edge.
	deps := sqliteDependencyList(t, app, "/api/v1/deps")
	if len(deps.Dependencies) != 1 {
		t.Fatalf("deps = %+v, want only the blocking edge", deps.Dependencies)
	}
	if d := deps.Dependencies[0]; d.IssueID != blockChild.ID || d.BlockedByID != blockParent.ID {
		t.Fatalf("deps[0] = %+v, want %s blocked_by %s", d, blockChild.ID, blockParent.ID)
	}

	// /graph shows only the blocking edge (all four issues are still nodes).
	graph := sqliteGraph(t, app)
	if len(graph.Edges) != 1 {
		t.Fatalf("graph edges = %+v, want only the blocking edge", graph.Edges)
	}
	if e := graph.Edges[0]; e.Source != blockParent.ID || e.Target != blockChild.ID {
		t.Fatalf("graph edge = %+v, want %s -> %s", e, blockParent.ID, blockChild.ID)
	}
	sqliteRequireGraphNodeIDs(t, graph.Nodes, blockParent.ID, blockChild.ID, epic.ID, leaf.ID)

	// The leaf's blocked_by must not include its epic (populateBlockedBy is now
	// blocks-only).
	var got deptypeIssue
	sqliteRequest(t, app, http.MethodGet, "/api/v1/issues/"+leaf.ID, "", http.StatusOK, &got)
	for _, b := range got.BlockedBy {
		if b == epic.ID {
			t.Fatalf("leaf blocked_by = %v, must not include epic %s", got.BlockedBy, epic.ID)
		}
	}
}

// TestSQLiteReadyExcludesEpics proves the new ReadyIssues behavior: an epic with
// no blockers is excluded from /ready, while a normal unblocked issue is ready.
func TestSQLiteReadyExcludesEpics(t *testing.T) {
	app, closeStore := sqliteHandlersApp(t)
	defer closeStore()

	epic := sqliteCreateIssue(t, app, `{"title":"Epic","priority":1,"issue_type":"epic"}`)
	task := sqliteCreateIssue(t, app, `{"title":"Task","priority":2,"issue_type":"task"}`)

	ready := sqliteIssueList(t, app, "/api/v1/ready")
	sqliteRequireIssueIDs(t, ready.Issues, task.ID)
	sqliteRejectIssueID(t, ready.Issues, epic.ID)
}
