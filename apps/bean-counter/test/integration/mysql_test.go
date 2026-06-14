//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/testcontainers/testcontainers-go/modules/mysql"

	"github.com/mattsp1290/bean-counter/internal/handlers/deps"
	"github.com/mattsp1290/bean-counter/internal/handlers/graph"
	"github.com/mattsp1290/bean-counter/internal/handlers/issues"
	"github.com/mattsp1290/bean-counter/internal/handlers/ready"
	"github.com/mattsp1290/bean-counter/internal/server"
	appstore "github.com/mattsp1290/bean-counter/internal/store"
)

const (
	mysqlPrefix = "mysql-it"
	mysqlActor  = "mysql-integration-test"
)

func TestMySQLCRUDDepsAndReadyOverHTTP(t *testing.T) {
	app, closeStore := newMySQLApp(t)
	defer closeStore()

	parent := mysqlCreateIssue(t, app, `{"title":"Parent","description":"Root work","priority":1,"issue_type":"task","labels":["mysql"]}`)
	child := mysqlCreateIssue(t, app, `{"title":"Child","priority":2,"issue_type":"feature","labels":["mysql","deps"]}`)

	mysqlRequireIssueIDs(t, mysqlListIssues(t, app), parent.ID, child.ID)

	updated := mysqlUpdateIssue(t, app, child.ID, `{"title":"Child updated","labels":["updated"]}`)
	if updated.Title != "Child updated" || len(updated.Labels) != 1 || updated.Labels[0] != "updated" {
		t.Fatalf("updated child = %+v", updated)
	}

	mysqlAddDependency(t, app, child.ID, parent.ID)
	depsList := mysqlListDeps(t, app)
	if len(depsList.Dependencies) != 1 ||
		depsList.Dependencies[0].IssueID != child.ID ||
		depsList.Dependencies[0].BlockedByID != parent.ID {
		t.Fatalf("dependencies = %+v", depsList.Dependencies)
	}

	readyBeforeClose := mysqlReadyIssues(t, app)
	mysqlRequireIssueIDs(t, readyBeforeClose, parent.ID)
	mysqlRejectIssueID(t, readyBeforeClose, child.ID)

	closed := mysqlCloseIssue(t, app, parent.ID, `{"reason":"done"}`)
	if closed.State != "closed" {
		t.Fatalf("closed parent state = %q, want closed", closed.State)
	}

	readyAfterClose := mysqlReadyIssues(t, app)
	mysqlRequireIssueIDs(t, readyAfterClose, child.ID)
	mysqlRejectIssueID(t, readyAfterClose, parent.ID)
}

func newMySQLApp(t *testing.T) (*fiber.App, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	container, err := mysql.Run(
		ctx,
		"mysql:9.5",
		mysql.WithDatabase("bean_counter"),
		mysql.WithUsername("bean_counter"),
		mysql.WithPassword("bean_counter"),
	)
	if err != nil {
		t.Fatalf("start mysql container: %v", err)
	}
	t.Cleanup(func() {
		terminateCtx, terminateCancel := context.WithTimeout(context.Background(), time.Minute)
		defer terminateCancel()
		if err := container.Terminate(terminateCtx); err != nil {
			t.Fatalf("terminate mysql container: %v", err)
		}
	})

	dsn, err := container.ConnectionString(ctx, "parseTime=true", "loc=UTC", "multiStatements=true")
	if err != nil {
		t.Fatalf("mysql connection string: %v", err)
	}

	adapter, err := appstore.NewAdapter(ctx, appstore.AdapterConfig{
		Store: appstore.Config{
			Driver: appstore.DriverMySQL,
			DSN:    appstore.SecretDSN(dsn),
		},
		ProjectPrefix:  mysqlPrefix,
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
				ProjectPrefix: mysqlPrefix,
				Actor:         mysqlActor,
			})
			deps.Register(api, deps.Config{
				Store:         adapter.Store(),
				ProjectPrefix: mysqlPrefix,
			})
			ready.Register(api, ready.Config{
				Source: adapter,
			})
			graph.Register(api, graph.Config{
				Store:         adapter.Store(),
				ProjectPrefix: mysqlPrefix,
			})
		},
	})
	return app, adapter.Close
}

func mysqlCreateIssue(t *testing.T, app *fiber.App, body string) mysqlIssueResponse {
	t.Helper()
	var issue mysqlIssueResponse
	mysqlRequestJSON(t, app, http.MethodPost, "/api/v1/issues", body, http.StatusCreated, &issue)
	return issue
}

func mysqlListIssues(t *testing.T, app *fiber.App) []mysqlIssueResponse {
	t.Helper()
	var envelope mysqlIssueListResponse
	mysqlRequestJSON(t, app, http.MethodGet, "/api/v1/issues", "", http.StatusOK, &envelope)
	return envelope.Issues
}

func mysqlUpdateIssue(t *testing.T, app *fiber.App, id, body string) mysqlIssueResponse {
	t.Helper()
	var issue mysqlIssueResponse
	mysqlRequestJSON(t, app, http.MethodPatch, "/api/v1/issues/"+id, body, http.StatusOK, &issue)
	return issue
}

func mysqlCloseIssue(t *testing.T, app *fiber.App, id, body string) mysqlIssueResponse {
	t.Helper()
	var issue mysqlIssueResponse
	mysqlRequestJSON(t, app, http.MethodPost, "/api/v1/issues/"+id+"/close", body, http.StatusOK, &issue)
	return issue
}

func mysqlAddDependency(t *testing.T, app *fiber.App, issueID, blockedByID string) {
	t.Helper()
	var dep mysqlDependencyResponse
	body := fmt.Sprintf(`{"blocked_by_id":%q}`, blockedByID)
	mysqlRequestJSON(t, app, http.MethodPost, "/api/v1/issues/"+issueID+"/deps", body, http.StatusCreated, &dep)
	if dep.IssueID != issueID || dep.BlockedByID != blockedByID {
		t.Fatalf("created dependency = %+v", dep)
	}
}

func mysqlListDeps(t *testing.T, app *fiber.App) mysqlDependencyListResponse {
	t.Helper()
	var envelope mysqlDependencyListResponse
	mysqlRequestJSON(t, app, http.MethodGet, "/api/v1/deps", "", http.StatusOK, &envelope)
	return envelope
}

func mysqlReadyIssues(t *testing.T, app *fiber.App) []mysqlIssueResponse {
	t.Helper()
	var envelope mysqlIssueListResponse
	mysqlRequestJSON(t, app, http.MethodGet, "/api/v1/ready", "", http.StatusOK, &envelope)
	return envelope.Issues
}

func mysqlRequestJSON(t *testing.T, app *fiber.App, method, target, body string, wantStatus int, out any) {
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

func mysqlRequireIssueIDs(t *testing.T, issues []mysqlIssueResponse, wantIDs ...string) {
	t.Helper()
	seen := mysqlIssueIDSet(issues)
	for _, id := range wantIDs {
		if !seen[id] {
			t.Fatalf("issues missing %q: %+v", id, issues)
		}
	}
}

func mysqlRejectIssueID(t *testing.T, issues []mysqlIssueResponse, id string) {
	t.Helper()
	if mysqlIssueIDSet(issues)[id] {
		t.Fatalf("issues unexpectedly contain %q: %+v", id, issues)
	}
}

func mysqlIssueIDSet(issues []mysqlIssueResponse) map[string]bool {
	seen := make(map[string]bool, len(issues))
	for _, issue := range issues {
		seen[issue.ID] = true
	}
	return seen
}

type mysqlIssueResponse struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Priority int      `json:"priority"`
	State    string   `json:"state"`
	Labels   []string `json:"labels"`
}

type mysqlIssueListResponse struct {
	Issues []mysqlIssueResponse `json:"issues"`
}

type mysqlDependencyResponse struct {
	IssueID     string `json:"issue_id"`
	BlockedByID string `json:"blocked_by_id"`
}

type mysqlDependencyListResponse struct {
	Dependencies []mysqlDependencyResponse `json:"dependencies"`
}
