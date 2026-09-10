package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestHealthz(t *testing.T) {
	app := New(Config{LogOutput: bytes.NewBuffer(nil)})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/healthz", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
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

func TestCORSAllowsConfiguredOrigin(t *testing.T) {
	const origin = "http://ui.example.test"
	app := New(Config{CORSOrigin: origin, CORSOriginSet: true, LogOutput: bytes.NewBuffer(nil)})

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/healthz", nil)
	req.Header.Set(fiber.HeaderOrigin, origin)
	req.Header.Set(fiber.HeaderAccessControlRequestMethod, fiber.MethodGet)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.Header.Get(fiber.HeaderAccessControlAllowOrigin) != origin {
		t.Fatalf("allow origin = %q, want %q", resp.Header.Get(fiber.HeaderAccessControlAllowOrigin), origin)
	}
	if got := resp.Header.Values(fiber.HeaderAccessControlAllowMethods); !containsHeaderValue(got, fiber.MethodGet) {
		t.Fatalf("allow methods = %q, want %q", got, fiber.MethodGet)
	}
	if got := resp.Header.Values(fiber.HeaderAccessControlAllowHeaders); !containsHeaderValue(got, fiber.HeaderContentType) {
		t.Fatalf("allow headers = %q, want %q", got, fiber.HeaderContentType)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}

func TestCORSRejectsUnconfiguredOrigin(t *testing.T) {
	app := New(Config{CORSOrigin: "http://ui.example.test", CORSOriginSet: true, LogOutput: bytes.NewBuffer(nil)})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/healthz", nil)
	req.Header.Set(fiber.HeaderOrigin, "http://other.example.test")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := resp.Header.Get(fiber.HeaderAccessControlAllowOrigin); got != "" {
		t.Fatalf("allow origin = %q, want empty", got)
	}
}

func TestCORSRejectsUnconfiguredPreflightOrigin(t *testing.T) {
	app := New(Config{CORSOrigin: "http://ui.example.test", CORSOriginSet: true, LogOutput: bytes.NewBuffer(nil)})

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/healthz", nil)
	req.Header.Set(fiber.HeaderOrigin, "http://other.example.test")
	req.Header.Set(fiber.HeaderAccessControlRequestMethod, fiber.MethodGet)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := resp.Header.Get(fiber.HeaderAccessControlAllowOrigin); got != "" {
		t.Fatalf("allow origin = %q, want empty", got)
	}
}

func TestCORSAllowsNoOriginsWhenExplicitOriginIsEmpty(t *testing.T) {
	app := New(Config{CORSOrigin: "", CORSOriginSet: true, LogOutput: bytes.NewBuffer(nil)})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/healthz", nil)
	req.Header.Set(fiber.HeaderOrigin, "http://localhost:5173")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := resp.Header.Get(fiber.HeaderAccessControlAllowOrigin); got != "" {
		t.Fatalf("allow origin = %q, want empty", got)
	}
}

func TestRecoverMiddlewareReturnsServerErrorAndLogsRequest(t *testing.T) {
	var logs bytes.Buffer
	app := New(Config{LogOutput: &logs})
	app.Get("/panic", func(fiber.Ctx) error {
		panic("boom")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
	for _, want := range []string{"500", "GET", "/panic"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log output %q does not contain %q", logs.String(), want)
		}
	}
}

func containsHeaderValue(values []string, want string) bool {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), want) {
				return true
			}
		}
	}
	return false
}
