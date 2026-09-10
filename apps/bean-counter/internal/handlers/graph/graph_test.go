package graph

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mattsp1290/beans/apps/bean-counter/internal/server"
	appstore "github.com/mattsp1290/beans/apps/bean-counter/internal/store"
)

type fakeStore struct {
	issues    []appstore.Issue
	deps      []appstore.DepEdge
	issuesErr error
	depsErr   error

	listFilter appstore.ListFilter
	depsFilter appstore.ListFilter
	depsCalled bool
}

func (s *fakeStore) ListIssues(_ context.Context, filter appstore.ListFilter) ([]appstore.Issue, error) {
	s.listFilter = filter
	return s.issues, s.issuesErr
}

func (s *fakeStore) ListBlockingDeps(_ context.Context, filter appstore.ListFilter) ([]appstore.DepEdge, error) {
	s.depsCalled = true
	s.depsFilter = filter
	return s.deps, s.depsErr
}

func TestGraphReturnsNodesAndEdges(t *testing.T) {
	store := &fakeStore{
		issues: []appstore.Issue{
			testIssue("bc-1", "Root"),
			testIssue("bc-2", "Blocked"),
		},
		deps: []appstore.DepEdge{{IssueID: "bc-2", BlockedByID: "bc-1"}},
	}
	resp, body := request(t, testApp(store), http.MethodGet, "/api/v1/graph")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if store.listFilter.Prefix != "bc" {
		t.Fatalf("list filter prefix = %q, want bc", store.listFilter.Prefix)
	}
	if store.depsFilter.Prefix != "bc" {
		t.Fatalf("deps filter prefix = %q, want bc", store.depsFilter.Prefix)
	}
	// AllRepos is the field that would silently drop the prefix WHERE clause
	// and return every project's edges.
	if store.listFilter.AllRepos || store.depsFilter.AllRepos {
		t.Fatal("AllRepos = true, want false: both queries must stay prefix-scoped")
	}
	for _, want := range []string{
		`"nodes"`,
		`"edges"`,
		`"id":"bc-1"`,
		`"title":"Root"`,
		`"source":"bc-1"`,
		`"target":"bc-2"`,
	} {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}
}

func TestGraphStopsWhenListIssuesFails(t *testing.T) {
	store := &fakeStore{issuesErr: errors.New("list failed")}
	resp, body := request(t, testApp(store), http.MethodGet, "/api/v1/graph")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	// An explicit flag, not an empty-prefix proxy: a future ListFilter default
	// could make the zero value indistinguishable from a real call.
	if store.depsCalled {
		t.Fatal("ListBlockingDeps was called after ListIssues failed")
	}
	if !bytes.Contains(body, []byte(`"error":"internal_error"`)) {
		t.Fatalf("body missing internal_error: %s", body)
	}
}

func TestGraphUsesCentralErrorHandlerForDepsFailure(t *testing.T) {
	store := &fakeStore{depsErr: errors.New("deps failed")}
	resp, body := request(t, testApp(store), http.MethodGet, "/api/v1/graph")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(`"error":"internal_error"`)) {
		t.Fatalf("body missing internal_error: %s", body)
	}
}

func testApp(store *fakeStore) *fiber.App {
	return server.New(server.Config{
		LogOutput: bytes.NewBuffer(nil),
		RegisterAPI: func(api fiber.Router) {
			Register(api, Config{Store: store, ProjectPrefix: "bc"})
		},
	})
}

func request(t *testing.T, app *fiber.App, method, target string) (*http.Response, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, buf.Bytes()
}

func testIssue(id, title string) appstore.Issue {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	issue := appstore.Issue{IssueType: "feature"}
	issue.ID = id
	issue.Identifier = id
	issue.Title = title
	issue.Priority = 3
	issue.State = "open"
	issue.Labels = []string{"api"}
	issue.BlockedBy = []string{}
	issue.CreatedAt = now
	issue.UpdatedAt = now
	return issue
}
