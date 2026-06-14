package ready

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

type fakeSource struct {
	issues []appstore.Issue
	err    error
	calls  int
}

func (s *fakeSource) ReadyIssues(context.Context) ([]appstore.Issue, error) {
	s.calls++
	return s.issues, s.err
}

func TestReadyReturnsIssueEnvelope(t *testing.T) {
	source := &fakeSource{issues: []appstore.Issue{testIssue("bc-1")}}
	resp, body := request(t, testApp(source), http.MethodGet, "/api/v1/ready")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if source.calls != 1 {
		t.Fatalf("ReadyIssues calls = %d, want 1", source.calls)
	}
	if !bytes.Contains(body, []byte(`"issues"`)) || !bytes.Contains(body, []byte(`"id":"bc-1"`)) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestReadyUsesCentralErrorHandler(t *testing.T) {
	resp, body := request(t, testApp(&fakeSource{err: appstore.ErrNotFound}), http.MethodGet, "/api/v1/ready")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(`"error":"not_found"`)) {
		t.Fatalf("body missing not_found: %s", body)
	}
}

func testApp(source *fakeSource) *fiber.App {
	return server.New(server.Config{
		LogOutput: bytes.NewBuffer(nil),
		RegisterAPI: func(api fiber.Router) {
			Register(api, Config{Source: source})
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
