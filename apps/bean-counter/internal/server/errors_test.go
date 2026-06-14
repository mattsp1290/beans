package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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
		{"wrapped not found", fmt.Errorf("lookup issue: %w", appstore.ErrNotFound), http.StatusNotFound, "not_found"},
		{"cycle", appstore.ErrCycle, http.StatusConflict, "conflict"},
		{"duplicate dep", appstore.ErrDuplicateDep, http.StatusConflict, "conflict"},
		{"conflict", appstore.ErrConflict, http.StatusConflict, "conflict"},
		{"disabled", appstore.ErrDisabled, http.StatusConflict, "conflict"},
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
				t.Fatalf("status = %d, want %d; body=%+v", resp.StatusCode, tt.wantStatus, body)
			}
			if body.Error != tt.wantCode {
				t.Fatalf("error = %q, want %q; body=%+v", body.Error, tt.wantCode, body)
			}
			if body.Message == "" {
				t.Fatalf("message is empty; body=%+v", body)
			}
		})
	}
}

func TestErrorHandlerPreservesValidationFields(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "value",
			err: ValidationError{
				Message: "invalid issue",
				Fields: []FieldError{
					{Field: "title", Message: "required"},
				},
			},
		},
		{
			name: "pointer",
			err: &ValidationError{
				Message: "invalid issue",
				Fields: []FieldError{
					{Field: "title", Message: "required"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := New(Config{LogOutput: bytes.NewBuffer(nil)})
			app.Get("/err", func(fiber.Ctx) error {
				return tt.err
			})

			resp, body := testErrorRequest(t, app)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%+v", resp.StatusCode, http.StatusBadRequest, body)
			}
			if body.Error != "validation_error" {
				t.Fatalf("error = %q, want validation_error", body.Error)
			}
			if body.Message != "invalid issue" {
				t.Fatalf("message = %q, want invalid issue", body.Message)
			}
			if len(body.Fields) != 1 || body.Fields[0].Field != "title" || body.Fields[0].Message != "required" {
				t.Fatalf("fields = %+v, want title required", body.Fields)
			}
		})
	}
}

func testErrorRequest(t *testing.T, app *fiber.App) (*http.Response, errorResponse) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/err", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer resp.Body.Close()

	var body errorResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return resp, body
}
