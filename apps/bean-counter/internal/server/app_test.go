package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	defer resp.Body.Close()

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
	app := New(Config{CORSOrigin: origin, LogOutput: bytes.NewBuffer(nil)})

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/healthz", nil)
	req.Header.Set(fiber.HeaderOrigin, origin)
	req.Header.Set(fiber.HeaderAccessControlRequestMethod, fiber.MethodGet)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get(fiber.HeaderAccessControlAllowOrigin) != origin {
		t.Fatalf("allow origin = %q, want %q", resp.Header.Get(fiber.HeaderAccessControlAllowOrigin), origin)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}

func TestCORSRejectsUnconfiguredOrigin(t *testing.T) {
	app := New(Config{CORSOrigin: "http://ui.example.test", LogOutput: bytes.NewBuffer(nil)})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/healthz", nil)
	req.Header.Set(fiber.HeaderOrigin, "http://other.example.test")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get(fiber.HeaderAccessControlAllowOrigin); got != "" {
		t.Fatalf("allow origin = %q, want empty", got)
	}
}

func TestRecoverMiddlewareReturnsServerError(t *testing.T) {
	app := New(Config{LogOutput: bytes.NewBuffer(nil)})
	app.Get("/panic", func(fiber.Ctx) error {
		panic("boom")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
}
