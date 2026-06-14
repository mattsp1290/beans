package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/mattsp1290/bean-counter/internal/server"
)

func TestReadyzReturnsOKWhenStoreProjectExists(t *testing.T) {
	app := testApp(fakeReadinessStore{exists: true})

	resp := readyz(t, app)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status body = %q, want ok", body["status"])
	}
}

func TestReadyzReturnsUnavailableWhenProjectMissing(t *testing.T) {
	app := testApp(fakeReadinessStore{exists: false})

	resp := readyz(t, app)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
	var body errorResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error != "internal_error" || body.Message != "project is not registered" {
		t.Fatalf("body = %+v, want internal_error project missing", body)
	}
}

func TestReadyzReturnsStoreError(t *testing.T) {
	app := testApp(fakeReadinessStore{err: errors.New("database unavailable")})

	resp := readyz(t, app)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
	var body errorResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error != "internal_error" {
		t.Fatalf("error code = %q, want internal_error", body.Error)
	}
}

func testApp(store ReadinessStore) *fiber.App {
	return server.New(server.Config{
		RegisterAPI: func(api fiber.Router) {
			Register(api, Config{
				Store:         store,
				ProjectPrefix: "bc",
			})
		},
	})
}

func readyz(t *testing.T, app *fiber.App) *http.Response {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/readyz", nil))
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	return resp
}

type fakeReadinessStore struct {
	exists bool
	err    error
}

func (s fakeReadinessStore) ProjectExists(context.Context, string) (bool, error) {
	return s.exists, s.err
}

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}
