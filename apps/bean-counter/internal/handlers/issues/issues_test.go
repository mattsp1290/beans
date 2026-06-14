package issues

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mattsp1290/bean-counter/internal/server"
	appstore "github.com/mattsp1290/bean-counter/internal/store"
)

type fakeStore struct {
	created      appstore.CreateIssueInput
	listFilter   appstore.ListFilter
	updatedID    string
	updated      appstore.UpdateIssueInput
	closedID     string
	closedActor  string
	closedReason string
	deletedID    string
	issue        appstore.Issue
	issues       []appstore.Issue
	err          error
}

func (s *fakeStore) CreateIssue(_ context.Context, in appstore.CreateIssueInput) (appstore.Issue, error) {
	s.created = in
	return s.issue, s.err
}

func (s *fakeStore) ListIssues(_ context.Context, filter appstore.ListFilter) ([]appstore.Issue, error) {
	s.listFilter = filter
	return s.issues, s.err
}

func (s *fakeStore) GetIssue(context.Context, string) (appstore.Issue, error) {
	return s.issue, s.err
}

func (s *fakeStore) UpdateIssue(_ context.Context, id string, in appstore.UpdateIssueInput) (appstore.Issue, error) {
	s.updatedID = id
	s.updated = in
	return s.issue, s.err
}

func (s *fakeStore) CloseIssue(_ context.Context, id, actor, reason string) error {
	s.closedID = id
	s.closedActor = actor
	s.closedReason = reason
	return s.err
}

func (s *fakeStore) DeleteIssue(_ context.Context, id string) error {
	s.deletedID = id
	return s.err
}

func TestCreateIssue(t *testing.T) {
	store := &fakeStore{issue: testIssue("bc-1")}
	app := testApp(store)

	resp, body := request(t, app, http.MethodPost, "/api/v1/issues", `{"title":"New","priority":2,"issue_type":"feature","labels":["api"]}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if store.created.Prefix != "bc" || store.created.Actor != "tester" || store.created.Title != "New" {
		t.Fatalf("created input = %+v", store.created)
	}
}

func TestCreateIssueValidationError(t *testing.T) {
	resp, body := request(t, testApp(&fakeStore{}), http.MethodPost, "/api/v1/issues", `{"title":"New","issue_type":"feature"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(`"field":"priority"`)) {
		t.Fatalf("body missing priority field error: %s", body)
	}
}

func TestListIssues(t *testing.T) {
	store := &fakeStore{issues: []appstore.Issue{testIssue("bc-1")}}
	resp, body := request(t, testApp(store), http.MethodGet, "/api/v1/issues?state=open,blocked&state=done&limit=10", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if store.listFilter.Prefix != "bc" || store.listFilter.Limit != 10 {
		t.Fatalf("filter = %+v", store.listFilter)
	}
	if got := statesToStrings(store.listFilter.States); len(got) != 3 || got[0] != "open" || got[1] != "blocked" || got[2] != "done" {
		t.Fatalf("states = %v", got)
	}
	if !bytes.Contains(body, []byte(`"issues"`)) {
		t.Fatalf("body missing issues envelope: %s", body)
	}
}

func TestGetIssue(t *testing.T) {
	resp, body := request(t, testApp(&fakeStore{issue: testIssue("bc-1")}), http.MethodGet, "/api/v1/issues/bc-1", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
}

func TestUpdateIssue(t *testing.T) {
	store := &fakeStore{issue: testIssue("bc-1")}
	resp, body := request(t, testApp(store), http.MethodPatch, "/api/v1/issues/bc-1", `{"title":"Updated","labels":[]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if store.updatedID != "bc-1" || store.updated.Title == nil || *store.updated.Title != "Updated" || store.updated.Labels == nil {
		t.Fatalf("update = id %q input %+v", store.updatedID, store.updated)
	}
}

func TestUpdateIssueRejectsNullLabels(t *testing.T) {
	resp, body := request(t, testApp(&fakeStore{}), http.MethodPatch, "/api/v1/issues/bc-1", `{"title":"Updated","labels":null}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(`"field":"labels"`)) {
		t.Fatalf("body missing labels field error: %s", body)
	}
}

func TestCloseIssue(t *testing.T) {
	store := &fakeStore{issue: testIssue("bc-1")}
	resp, body := request(t, testApp(store), http.MethodPost, "/api/v1/issues/bc-1/close", `{"reason":"done"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if store.closedID != "bc-1" || store.closedActor != "tester" || store.closedReason != "done" {
		t.Fatalf("close = id %q actor %q reason %q", store.closedID, store.closedActor, store.closedReason)
	}
}

func TestDeleteIssue(t *testing.T) {
	store := &fakeStore{}
	resp, body := request(t, testApp(store), http.MethodDelete, "/api/v1/issues/bc-1", "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if store.deletedID != "bc-1" {
		t.Fatalf("deletedID = %q", store.deletedID)
	}
}

func TestStoreErrorUsesCentralErrorHandler(t *testing.T) {
	resp, body := request(t, testApp(&fakeStore{err: appstore.ErrNotFound}), http.MethodGet, "/api/v1/issues/missing", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(`"error":"not_found"`)) {
		t.Fatalf("body missing not_found: %s", body)
	}
}

func testApp(store *fakeStore) *fiber.App {
	return server.New(server.Config{
		LogOutput: bytes.NewBuffer(nil),
		RegisterAPI: func(api fiber.Router) {
			Register(api, Config{Store: store, ProjectPrefix: "bc", Actor: "tester"})
		},
	})
}

func request(t *testing.T, app *fiber.App, method, target, body string) (*http.Response, []byte) {
	t.Helper()
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, target, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, buf.Bytes()
}

func testIssue(id string) appstore.Issue {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	issue := appstore.Issue{IssueType: "feature"}
	issue.ID = id
	issue.Identifier = id
	issue.Title = "Issue"
	issue.Priority = 3
	issue.State = "open"
	issue.Labels = []string{}
	issue.BlockedBy = []string{}
	issue.CreatedAt = now
	issue.UpdatedAt = now
	return issue
}

func statesToStrings(states []appstore.IssueState) []string {
	result := make([]string, 0, len(states))
	for _, state := range states {
		result = append(result, string(state))
	}
	return result
}
