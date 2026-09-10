package server

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestHealthAndErrorEnvelope(t *testing.T) {
	t.Parallel()
	app := New(Config{LogOutput: io.Discard, RegisterAPI: func(r fiber.Router) {
		r.Get("/boom", func(fiber.Ctx) error { return fiber.NewError(fiber.StatusNotFound, "no such thing") })
	}})

	resp, err := app.Test(httptest.NewRequest("GET", "/api/health", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("health status = %d", resp.StatusCode)
	}

	resp, err = app.Test(httptest.NewRequest("GET", "/api/boom", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 404 {
		t.Fatalf("boom status = %d, want 404", resp.StatusCode)
	}
	var body errorBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "not_found" || body.Error.Message != "no such thing" {
		t.Errorf("envelope = %+v", body)
	}
}
