package server

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	appstore "github.com/mattsp1290/bean-counter/internal/store"
)

func TestErrorHandlerMapsSentinels(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"not found", appstore.ErrNotFound, http.StatusNotFound, "not_found"},
		{"cycle", appstore.ErrCycle, http.StatusConflict, "conflict"},
		{"duplicate dep", appstore.ErrDuplicateDep, http.StatusConflict, "conflict"},
		{"conflict", appstore.ErrConflict, http.StatusConflict, "conflict"},
		{"empty dsn", appstore.ErrEmptyDSN, http.StatusInternalServerError, "store_configuration_error"},
		{"unsupported driver", appstore.ErrUnsupportedDriver, http.StatusInternalServerError, "store_configuration_error"},
		{"validation sentinel", ErrValidation, http.StatusBadRequest, "validation_error"},
		{"validation details", ValidationError{Message: "bad input", Fields: []FieldError{{Field: "title", Message: "required"}}}, http.StatusBadRequest, "validation_error"},
		{"fiber error", fiber.NewError(fiber.StatusBadRequest, "bad request"), http.StatusBadRequest, "bad_request"},
		{"unknown", errors.New("database password leaked"), http.StatusInternalServerError, "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := New(Config{LogOutput: bytes.NewBuffer(nil)})
			app.Get("/err", func(fiber.Ctx) error {
				return tt.err
			})

			resp, body := testErrorRequest(t, app)
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, tt.wantStatus, body)
			}
			if !strings.Contains(body, `"error":"`+tt.wantCode+`"`) {
				t.Fatalf("body = %s, want error code %q", body, tt.wantCode)
			}
			if !strings.Contains(body, `"message":`) {
				t.Fatalf("body = %s, want message field", body)
			}
		})
	}
}

func TestErrorHandlerPreservesValidationFields(t *testing.T) {
	app := New(Config{LogOutput: bytes.NewBuffer(nil)})
	app.Get("/err", func(fiber.Ctx) error {
		return ValidationError{
			Message: "invalid issue",
			Fields: []FieldError{
				{Field: "title", Message: "required"},
			},
		}
	})

	resp, body := testErrorRequest(t, app)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, http.StatusBadRequest, body)
	}
	for _, want := range []string{`"error":"validation_error"`, `"field":"title"`, `"message":"required"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %s, want %s", body, want)
		}
	}
}

func testErrorRequest(t *testing.T, app *fiber.App) (*http.Response, string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/err", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer resp.Body.Close()

	var body bytes.Buffer
	if _, err := body.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, body.String()
}
