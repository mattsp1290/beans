package deps

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/mattsp1290/bean-counter/internal/server"
	appstore "github.com/mattsp1290/bean-counter/internal/store"
)

type fakeStore struct {
	addedIssueID       string
	addedBlockedByID   string
	removedIssueID     string
	removedBlockedByID string
	listPrefix         string
	deps               []appstore.DepEdge
	err                error
}

func (s *fakeStore) AddDep(_ context.Context, issueID, blockedByID string) error {
	s.addedIssueID = issueID
	s.addedBlockedByID = blockedByID
	return s.err
}

func (s *fakeStore) RemoveDep(_ context.Context, issueID, blockedByID string) error {
	s.removedIssueID = issueID
	s.removedBlockedByID = blockedByID
	return s.err
}

func (s *fakeStore) ListDeps(_ context.Context, prefix string) ([]appstore.DepEdge, error) {
	s.listPrefix = prefix
	return s.deps, s.err
}

func TestListDependencies(t *testing.T) {
	store := &fakeStore{deps: []appstore.DepEdge{{IssueID: "bc-2", BlockedByID: "bc-1"}}}
	resp, body := request(t, testApp(store), http.MethodGet, "/api/v1/deps", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if store.listPrefix != "bc" {
		t.Fatalf("listPrefix = %q, want bc", store.listPrefix)
	}
	if !bytes.Contains(body, []byte(`"dependencies"`)) || !bytes.Contains(body, []byte(`"blocked_by_id":"bc-1"`)) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestAddDependency(t *testing.T) {
	store := &fakeStore{}
	resp, body := request(t, testApp(store), http.MethodPost, "/api/v1/issues/bc-2/deps", `{"blocked_by_id":"bc-1"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if store.addedIssueID != "bc-2" || store.addedBlockedByID != "bc-1" {
		t.Fatalf("added = %q -> %q", store.addedIssueID, store.addedBlockedByID)
	}
}

func TestAddDependencyRejectsSelfDependency(t *testing.T) {
	resp, body := request(t, testApp(&fakeStore{}), http.MethodPost, "/api/v1/issues/bc-1/deps", `{"blocked_by_id":"bc-1"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(`"field":"blocked_by_id"`)) {
		t.Fatalf("body missing blocked_by_id field error: %s", body)
	}
}

func TestAddDependencyRejectsWrongPathProjectPrefix(t *testing.T) {
	resp, body := request(t, testApp(&fakeStore{}), http.MethodPost, "/api/v1/issues/other-2/deps", `{"blocked_by_id":"bc-1"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(`"field":"id"`)) {
		t.Fatalf("body missing id field error: %s", body)
	}
}

func TestAddDependencyRejectsWrongBlockedByProjectPrefix(t *testing.T) {
	resp, body := request(t, testApp(&fakeStore{}), http.MethodPost, "/api/v1/issues/bc-2/deps", `{"blocked_by_id":"other-1"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(`"field":"blocked_by_id"`)) {
		t.Fatalf("body missing blocked_by_id field error: %s", body)
	}
}

func TestAddDependencyRejectsMalformedJSON(t *testing.T) {
	resp, body := request(t, testApp(&fakeStore{}), http.MethodPost, "/api/v1/issues/bc-2/deps", `{"blocked_by_id":`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(`"error":"bad_request"`)) {
		t.Fatalf("body missing bad_request: %s", body)
	}
}

func TestAddDependencyRejectsEmptyBody(t *testing.T) {
	resp, body := request(t, testApp(&fakeStore{}), http.MethodPost, "/api/v1/issues/bc-2/deps", `{}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(`"field":"blocked_by_id"`)) {
		t.Fatalf("body missing blocked_by_id field error: %s", body)
	}
}

func TestRemoveDependency(t *testing.T) {
	store := &fakeStore{}
	resp, body := request(t, testApp(store), http.MethodDelete, "/api/v1/issues/bc-2/deps/bc-1", "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if store.removedIssueID != "bc-2" || store.removedBlockedByID != "bc-1" {
		t.Fatalf("removed = %q -> %q", store.removedIssueID, store.removedBlockedByID)
	}
}

func TestRemoveDependencyRejectsWrongBlockedByProjectPrefix(t *testing.T) {
	resp, body := request(t, testApp(&fakeStore{}), http.MethodDelete, "/api/v1/issues/bc-2/deps/other-1", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(`"field":"blocked_by_id"`)) {
		t.Fatalf("body missing blocked_by_id field error: %s", body)
	}
}

func TestStoreConflictUsesCentralErrorHandler(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "cycle", err: appstore.ErrCycle},
		{name: "duplicate", err: appstore.ErrDuplicateDep},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, body := request(t, testApp(&fakeStore{err: tt.err}), http.MethodPost, "/api/v1/issues/bc-2/deps", `{"blocked_by_id":"bc-1"}`)
			if resp.StatusCode != http.StatusConflict {
				t.Fatalf("status = %d body=%s", resp.StatusCode, body)
			}
			if !bytes.Contains(body, []byte(`"error":"conflict"`)) {
				t.Fatalf("body missing conflict: %s", body)
			}
		})
	}
}

func TestStoreNotFoundUsesCentralErrorHandler(t *testing.T) {
	resp, body := request(t, testApp(&fakeStore{err: appstore.ErrNotFound}), http.MethodDelete, "/api/v1/issues/bc-2/deps/bc-1", "")
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
			Register(api, Config{Store: store, ProjectPrefix: "bc"})
		},
	})
}

func request(t *testing.T, app *fiber.App, method, target, body string) (*http.Response, []byte) {
	t.Helper()
	reader := bytes.NewReader([]byte(body))
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
