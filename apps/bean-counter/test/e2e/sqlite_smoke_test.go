package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	testPrefix = "e2e"
	testActor  = "e2e-test"
)

func TestSQLiteBootCRUDDepsAndReadyOverHTTP(t *testing.T) {
	app, closeStore := newSQLiteApp(t)
	defer closeStore()

	parent := createIssue(t, app, `{"title":"Parent","description":"Root work","priority":1,"issue_type":"task","labels":["smoke"]}`)
	child := createIssue(t, app, `{"title":"Child","priority":2,"issue_type":"feature","labels":["smoke","deps"]}`)
	if parent.State != "open" || child.State != "open" {
		t.Fatalf("created states = parent %q child %q, want open", parent.State, child.State)
	}
	if parent.Priority != 1 || child.Priority != 2 {
		t.Fatalf("created priorities = parent %d child %d", parent.Priority, child.Priority)
	}

	issues := listIssues(t, app)
	assertIssueIDs(t, issues, parent.ID, child.ID)

	updated := updateIssue(t, app, child.ID, `{"title":"Child updated","labels":["updated"]}`)
	if updated.Title != "Child updated" || len(updated.Labels) != 1 || updated.Labels[0] != "updated" {
		t.Fatalf("updated child = %+v", updated)
	}

	addDependency(t, app, child.ID, parent.ID)
	depsList := listDeps(t, app)
	if len(depsList.Dependencies) != 1 ||
		depsList.Dependencies[0].IssueID != child.ID ||
		depsList.Dependencies[0].BlockedByID != parent.ID {
		t.Fatalf("dependencies = %+v", depsList.Dependencies)
	}

	readyBeforeClose := readyIssues(t, app)
	assertIssueIDs(t, readyBeforeClose, parent.ID)
	assertNoIssueID(t, readyBeforeClose, child.ID)

	closed := closeIssue(t, app, parent.ID, `{"reason":"done"}`)
	if closed.State != "closed" {
		t.Fatalf("closed parent state = %q, want closed", closed.State)
	}

	readyAfterClose := readyIssues(t, app)
	assertIssueIDs(t, readyAfterClose, child.ID)
	assertNoIssueID(t, readyAfterClose, parent.ID)
}

func newSQLiteApp(t *testing.T) (*fiber.App, func()) {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "bean-counter.sqlite")
	adapter, err := appstore.NewAdapter(ctx, appstore.AdapterConfig{
		Store: appstore.Config{
			Driver: appstore.DriverSQLite,
			DSN:    appstore.SecretDSN(fmt.Sprintf("file:%s", dbPath)),
		},
		ProjectPrefix:  testPrefix,
		TerminalStates: []appstore.IssueState{"closed", "done"},
		ActiveStates:   []appstore.IssueState{"open"},
	})
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}
	if err := adapter.EnsureProject(ctx); err != nil {
		adapter.Close()
		t.Fatalf("ensure project: %v", err)
	}

	app := server.New(server.Config{
		LogOutput: bytes.NewBuffer(nil),
		RegisterAPI: func(api fiber.Router) {
			issues.Register(api, issues.Config{
				Store:         adapter.Store(),
				ProjectPrefix: testPrefix,
				Actor:         testActor,
			})
			deps.Register(api, deps.Config{
				Store:         adapter.Store(),
				ProjectPrefix: testPrefix,
			})
			ready.Register(api, ready.Config{
				Source: adapter,
			})
			graph.Register(api, graph.Config{
				Store:         adapter.Store(),
				ProjectPrefix: testPrefix,
			})
		},
	})
	return app, adapter.Close
}

func createIssue(t *testing.T, app *fiber.App, body string) issueResponse {
	t.Helper()
	var issue issueResponse
	requestJSON(t, app, http.MethodPost, "/api/v1/issues", body, http.StatusCreated, &issue)
	return issue
}

func listIssues(t *testing.T, app *fiber.App) []issueResponse {
	t.Helper()
	var envelope issueListResponse
	requestJSON(t, app, http.MethodGet, "/api/v1/issues", "", http.StatusOK, &envelope)
	return envelope.Issues
}

func updateIssue(t *testing.T, app *fiber.App, id, body string) issueResponse {
	t.Helper()
	var issue issueResponse
	requestJSON(t, app, http.MethodPatch, "/api/v1/issues/"+id, body, http.StatusOK, &issue)
	return issue
}

func closeIssue(t *testing.T, app *fiber.App, id, body string) issueResponse {
	t.Helper()
	var issue issueResponse
	requestJSON(t, app, http.MethodPost, "/api/v1/issues/"+id+"/close", body, http.StatusOK, &issue)
	return issue
}

func addDependency(t *testing.T, app *fiber.App, issueID, blockedByID string) {
	t.Helper()
	var dep dependencyResponse
	body := fmt.Sprintf(`{"blocked_by_id":%q}`, blockedByID)
	requestJSON(t, app, http.MethodPost, "/api/v1/issues/"+issueID+"/deps", body, http.StatusCreated, &dep)
	if dep.IssueID != issueID || dep.BlockedByID != blockedByID {
		t.Fatalf("created dependency = %+v", dep)
	}
}

func listDeps(t *testing.T, app *fiber.App) dependencyListResponse {
	t.Helper()
	var envelope dependencyListResponse
	requestJSON(t, app, http.MethodGet, "/api/v1/deps", "", http.StatusOK, &envelope)
	return envelope
}

func readyIssues(t *testing.T, app *fiber.App) []issueResponse {
	t.Helper()
	var envelope issueListResponse
	requestJSON(t, app, http.MethodGet, "/api/v1/ready", "", http.StatusOK, &envelope)
	return envelope.Issues
}

func requestJSON(t *testing.T, app *fiber.App, method, target, body string, wantStatus int, out any) {
	t.Helper()
	req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read %s %s body: %v", method, target, err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d body=%s", method, target, resp.StatusCode, wantStatus, buf.Bytes())
	}
	if out != nil {
		if err := json.Unmarshal(buf.Bytes(), out); err != nil {
			t.Fatalf("decode %s %s response %s: %v", method, target, buf.Bytes(), err)
		}
	}
}

func assertIssueIDs(t *testing.T, issues []issueResponse, wantIDs ...string) {
	t.Helper()
	seen := issueIDSet(issues)
	for _, id := range wantIDs {
		if !seen[id] {
			t.Fatalf("issues missing %q: %+v", id, issues)
		}
	}
}

func assertNoIssueID(t *testing.T, issues []issueResponse, id string) {
	t.Helper()
	if issueIDSet(issues)[id] {
		t.Fatalf("issues unexpectedly contain %q: %+v", id, issues)
	}
}

func issueIDSet(issues []issueResponse) map[string]bool {
	seen := make(map[string]bool, len(issues))
	for _, issue := range issues {
		seen[issue.ID] = true
	}
	return seen
}

type issueResponse struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Priority int      `json:"priority"`
	State    string   `json:"state"`
	Labels   []string `json:"labels"`
}

type issueListResponse struct {
	Issues []issueResponse `json:"issues"`
}

type dependencyResponse struct {
	IssueID     string `json:"issue_id"`
	BlockedByID string `json:"blocked_by_id"`
}

type dependencyListResponse struct {
	Dependencies []dependencyResponse `json:"dependencies"`
}
