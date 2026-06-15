package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/mattsp1290/bean-counter/internal/handlers/deps"
	"github.com/mattsp1290/bean-counter/internal/handlers/graph"
	"github.com/mattsp1290/bean-counter/internal/handlers/issues"
	"github.com/mattsp1290/bean-counter/internal/handlers/ready"
	"github.com/mattsp1290/bean-counter/internal/server"
	appstore "github.com/mattsp1290/bean-counter/internal/store"
)

const (
	sqlitePrefix = "ht"
	sqliteActor  = "handler-test"
)

func TestSQLiteHandlersHappyPaths(t *testing.T) {
	app, closeStore := sqliteHandlersApp(t)
	defer closeStore()

	parent := sqliteCreateIssue(t, app, `{"title":"Parent","priority":1,"issue_type":"task","labels":["sqlite"]}`)
	child := sqliteCreateIssue(t, app, `{"title":"Child","priority":2,"issue_type":"feature","labels":["sqlite","deps"]}`)

	list := sqliteIssueList(t, app, "/api/v1/issues?state=open&limit=10")
	sqliteRequireIssueIDs(t, list.Issues, parent.ID, child.ID)

	got := sqliteIssue(t, app, http.MethodGet, "/api/v1/issues/"+child.ID, "", http.StatusOK)
	if got.ID != child.ID || got.Title != "Child" {
		t.Fatalf("get child = %+v", got)
	}

	updated := sqliteIssue(t, app, http.MethodPatch, "/api/v1/issues/"+child.ID, `{"title":"Child updated","labels":["updated"]}`, http.StatusOK)
	if updated.Title != "Child updated" || len(updated.Labels) != 1 || updated.Labels[0] != "updated" {
		t.Fatalf("updated child = %+v", updated)
	}

	dep := sqliteDependency(t, app, http.MethodPost, "/api/v1/issues/"+child.ID+"/deps", fmt.Sprintf(`{"blocked_by_id":%q}`, parent.ID), http.StatusCreated)
	if dep.IssueID != child.ID || dep.BlockedByID != parent.ID {
		t.Fatalf("created dependency = %+v", dep)
	}
	depList := sqliteDependencyList(t, app, "/api/v1/deps")
	if len(depList.Dependencies) != 1 || depList.Dependencies[0] != dep {
		t.Fatalf("dependencies = %+v", depList.Dependencies)
	}

	graph := sqliteGraph(t, app)
	sqliteRequireGraphNodeIDs(t, graph.Nodes, parent.ID, child.ID)
	if len(graph.Edges) != 1 || graph.Edges[0].Source != parent.ID || graph.Edges[0].Target != child.ID {
		t.Fatalf("graph edges = %+v", graph.Edges)
	}

	ready := sqliteIssueList(t, app, "/api/v1/ready")
	sqliteRequireIssueIDs(t, ready.Issues, parent.ID)
	sqliteRejectIssueID(t, ready.Issues, child.ID)

	closed := sqliteIssue(t, app, http.MethodPost, "/api/v1/issues/"+parent.ID+"/close", `{"reason":"done"}`, http.StatusOK)
	if closed.State != "closed" {
		t.Fatalf("closed parent = %+v", closed)
	}
	ready = sqliteIssueList(t, app, "/api/v1/ready")
	sqliteRequireIssueIDs(t, ready.Issues, child.ID)
	sqliteRejectIssueID(t, ready.Issues, parent.ID)

	sqliteRequest(t, app, http.MethodDelete, "/api/v1/issues/"+child.ID+"/deps/"+parent.ID, "", http.StatusNoContent, nil)
	sqliteRequest(t, app, http.MethodDelete, "/api/v1/issues/"+child.ID, "", http.StatusNoContent, nil)
	sqliteRequest(t, app, http.MethodGet, "/api/v1/issues/"+child.ID, "", http.StatusNotFound, &sqliteErrorResponse{})
}

func TestSQLiteHandlersValidationAndErrorMapping(t *testing.T) {
	app, closeStore := sqliteHandlersApp(t)
	defer closeStore()

	parent := sqliteCreateIssue(t, app, `{"title":"Parent","priority":1,"issue_type":"task"}`)
	child := sqliteCreateIssue(t, app, `{"title":"Child","priority":2,"issue_type":"task"}`)
	sqliteDependency(t, app, http.MethodPost, "/api/v1/issues/"+child.ID+"/deps", fmt.Sprintf(`{"blocked_by_id":%q}`, parent.ID), http.StatusCreated)

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantError  string
		wantField  string
	}{
		{
			name:       "create validation",
			method:     http.MethodPost,
			path:       "/api/v1/issues",
			body:       `{"title":"Missing priority","issue_type":"task"}`,
			wantStatus: http.StatusBadRequest,
			wantError:  "validation_error",
			wantField:  "priority",
		},
		{
			name:       "update validation",
			method:     http.MethodPatch,
			path:       "/api/v1/issues/" + child.ID,
			body:       `{"state":"not-a-state"}`,
			wantStatus: http.StatusBadRequest,
			wantError:  "validation_error",
			wantField:  "state",
		},
		{
			name:       "wrong project prefix",
			method:     http.MethodGet,
			path:       "/api/v1/issues/other-1",
			wantStatus: http.StatusBadRequest,
			wantError:  "validation_error",
			wantField:  "id",
		},
		{
			name:       "not found",
			method:     http.MethodGet,
			path:       "/api/v1/issues/ht-missing",
			wantStatus: http.StatusNotFound,
			wantError:  "not_found",
		},
		{
			name:       "duplicate dependency conflict",
			method:     http.MethodPost,
			path:       "/api/v1/issues/" + child.ID + "/deps",
			body:       fmt.Sprintf(`{"blocked_by_id":%q}`, parent.ID),
			wantStatus: http.StatusConflict,
			wantError:  "conflict",
		},
		{
			name:       "dependency cycle conflict",
			method:     http.MethodPost,
			path:       "/api/v1/issues/" + parent.ID + "/deps",
			body:       fmt.Sprintf(`{"blocked_by_id":%q}`, child.ID),
			wantStatus: http.StatusConflict,
			wantError:  "conflict",
		},
		{
			name:       "missing dependency target",
			method:     http.MethodDelete,
			path:       "/api/v1/issues/" + child.ID + "/deps/ht-missing",
			wantStatus: http.StatusNotFound,
			wantError:  "not_found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got sqliteErrorResponse
			sqliteRequest(t, app, tt.method, tt.path, tt.body, tt.wantStatus, &got)
			if got.Error != tt.wantError {
				t.Fatalf("error = %q, want %q; body=%+v", got.Error, tt.wantError, got)
			}
			if tt.wantField != "" && !sqliteHasField(got.Fields, tt.wantField) {
				t.Fatalf("fields = %+v, want field %q", got.Fields, tt.wantField)
			}
		})
	}
}

func sqliteHandlersApp(t *testing.T) (*fiber.App, func()) {
	t.Helper()
	app, _, closeStore := sqliteHandlersAppWithStore(t)
	return app, closeStore
}

// sqliteHandlersAppWithStore is sqliteHandlersApp but also returns the raw beans
// store, so tests can seed edge kinds (e.g. parent-child) that have no HTTP
// route.
func sqliteHandlersAppWithStore(t *testing.T) (*fiber.App, *appstore.Store, func()) {
	t.Helper()
	ctx := context.Background()
	adapter, err := appstore.NewAdapter(ctx, appstore.AdapterConfig{
		Store: appstore.Config{
			Driver: appstore.DriverSQLite,
			DSN:    appstore.SecretDSN("file:" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()) + "?mode=memory&cache=shared"),
		},
		ProjectPrefix:  sqlitePrefix,
		TerminalStates: []appstore.IssueState{"closed", "done"},
		ActiveStates:   []appstore.IssueState{"open"},
	})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	if err := adapter.EnsureProject(ctx); err != nil {
		adapter.Close()
		t.Fatalf("EnsureProject: %v", err)
	}
	app := server.New(server.Config{
		LogOutput: bytes.NewBuffer(nil),
		RegisterAPI: func(api fiber.Router) {
			issues.Register(api, issues.Config{
				Store:         adapter.Store(),
				ProjectPrefix: sqlitePrefix,
				Actor:         sqliteActor,
			})
			deps.Register(api, deps.Config{
				Store:         adapter.Store(),
				ProjectPrefix: sqlitePrefix,
			})
			ready.Register(api, ready.Config{
				Source: adapter,
			})
			graph.Register(api, graph.Config{
				Store:         adapter.Store(),
				ProjectPrefix: sqlitePrefix,
			})
		},
	})
	return app, adapter.Store(), adapter.Close
}

func sqliteCreateIssue(t *testing.T, app *fiber.App, body string) sqliteIssueResponse {
	t.Helper()
	return sqliteIssue(t, app, http.MethodPost, "/api/v1/issues", body, http.StatusCreated)
}

func sqliteIssue(t *testing.T, app *fiber.App, method, path, body string, status int) sqliteIssueResponse {
	t.Helper()
	var issue sqliteIssueResponse
	sqliteRequest(t, app, method, path, body, status, &issue)
	return issue
}

func sqliteIssueList(t *testing.T, app *fiber.App, path string) sqliteIssueListResponse {
	t.Helper()
	var envelope sqliteIssueListResponse
	sqliteRequest(t, app, http.MethodGet, path, "", http.StatusOK, &envelope)
	return envelope
}

func sqliteDependency(t *testing.T, app *fiber.App, method, path, body string, status int) sqliteDependencyResponse {
	t.Helper()
	var dep sqliteDependencyResponse
	sqliteRequest(t, app, method, path, body, status, &dep)
	return dep
}

func sqliteDependencyList(t *testing.T, app *fiber.App, path string) sqliteDependencyListResponse {
	t.Helper()
	var envelope sqliteDependencyListResponse
	sqliteRequest(t, app, http.MethodGet, path, "", http.StatusOK, &envelope)
	return envelope
}

func sqliteGraph(t *testing.T, app *fiber.App) sqliteGraphResponse {
	t.Helper()
	var graph sqliteGraphResponse
	sqliteRequest(t, app, http.MethodGet, "/api/v1/graph", "", http.StatusOK, &graph)
	return graph
}

func sqliteRequest(t *testing.T, app *fiber.App, method, path, body string, wantStatus int, out any) {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read %s %s body: %v", method, path, err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d body=%s", method, path, resp.StatusCode, wantStatus, buf.Bytes())
	}
	if out != nil {
		if err := json.Unmarshal(buf.Bytes(), out); err != nil {
			t.Fatalf("decode %s %s body=%s: %v", method, path, buf.Bytes(), err)
		}
	}
}

func sqliteRequireIssueIDs(t *testing.T, issues []sqliteIssueResponse, ids ...string) {
	t.Helper()
	seen := sqliteIssueIDSet(issues)
	for _, id := range ids {
		if !seen[id] {
			t.Fatalf("issues missing %q: %+v", id, issues)
		}
	}
}

func sqliteRejectIssueID(t *testing.T, issues []sqliteIssueResponse, id string) {
	t.Helper()
	if sqliteIssueIDSet(issues)[id] {
		t.Fatalf("issues unexpectedly contain %q: %+v", id, issues)
	}
}

func sqliteIssueIDSet(issues []sqliteIssueResponse) map[string]bool {
	seen := make(map[string]bool, len(issues))
	for _, issue := range issues {
		seen[issue.ID] = true
	}
	return seen
}

func sqliteRequireGraphNodeIDs(t *testing.T, nodes []sqliteGraphNode, ids ...string) {
	t.Helper()
	seen := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		seen[node.ID] = true
	}
	for _, id := range ids {
		if !seen[id] {
			t.Fatalf("graph nodes missing %q: %+v", id, nodes)
		}
	}
}

func sqliteHasField(fields []sqliteFieldError, field string) bool {
	for _, got := range fields {
		if got.Field == field {
			return true
		}
	}
	return false
}

type sqliteIssueResponse struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Priority int      `json:"priority"`
	State    string   `json:"state"`
	Labels   []string `json:"labels"`
}

type sqliteIssueListResponse struct {
	Issues []sqliteIssueResponse `json:"issues"`
}

type sqliteDependencyResponse struct {
	IssueID     string `json:"issue_id"`
	BlockedByID string `json:"blocked_by_id"`
}

type sqliteDependencyListResponse struct {
	Dependencies []sqliteDependencyResponse `json:"dependencies"`
}

type sqliteGraphResponse struct {
	Nodes []sqliteGraphNode `json:"nodes"`
	Edges []sqliteGraphEdge `json:"edges"`
}

type sqliteGraphNode struct {
	ID string `json:"id"`
}

type sqliteGraphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type sqliteErrorResponse struct {
	Error  string             `json:"error"`
	Fields []sqliteFieldError `json:"fields"`
}

type sqliteFieldError struct {
	Field string `json:"field"`
}
