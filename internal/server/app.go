// Package server hosts the bn serve HTTP application: the JSON API over the
// hub and the embedded UI. WP1 carries only the Fiber construction, error
// envelope, and middleware order; routes arrive in WP6.
package server

import (
	"errors"
	"io"
	"os"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
)

// Config controls app-level HTTP behavior.
type Config struct {
	LogOutput   io.Writer
	RegisterAPI func(fiber.Router)
}

// New builds the HTTP application with process-wide middleware and the API
// route group. The server is same-origin only, so there is no CORS middleware.
func New(config ...Config) *fiber.App {
	cfg := configWithDefaults(config...)
	app := fiber.New(fiber.Config{
		ErrorHandler: ErrorHandler,
	})

	app.Use(logger.New(logger.Config{
		Format:     "${time} ${status} ${method} ${path} ${latency}\n",
		TimeFormat: "2006-01-02T15:04:05Z07:00",
		Stream:     cfg.LogOutput,
	}))
	app.Use(recover.New())

	api := app.Group("/api")
	api.Get("/health", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	if cfg.RegisterAPI != nil {
		cfg.RegisterAPI(api)
	}

	return app
}

func configWithDefaults(config ...Config) Config {
	cfg := Config{LogOutput: os.Stderr}
	if len(config) == 0 {
		return cfg
	}
	if config[0].LogOutput != nil {
		cfg.LogOutput = config[0].LogOutput
	}
	cfg.RegisterAPI = config[0].RegisterAPI
	return cfg
}

// errorBody is the JSON error envelope: {"error": {"code", "message"}}.
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorHandler renders every handler error as the JSON error envelope. A
// *fiber.Error keeps its status; anything else is a 500 with a generic
// message so internal details do not leak to the client.
func ErrorHandler(c fiber.Ctx, err error) error {
	status := fiber.StatusInternalServerError
	message := "internal server error"
	var fiberErr *fiber.Error
	if errors.As(err, &fiberErr) {
		status = fiberErr.Code
		message = fiberErr.Message
	}
	return c.Status(status).JSON(errorBody{Error: errorDetail{
		Code:    errorCodeForStatus(status),
		Message: message,
	}})
}

func errorCodeForStatus(status int) string {
	switch status {
	case fiber.StatusBadRequest:
		return "validation_error"
	case fiber.StatusNotFound:
		return "not_found"
	case fiber.StatusConflict:
		return "conflict"
	case fiber.StatusBadGateway:
		return "git_conflict"
	case fiber.StatusServiceUnavailable:
		return "lock_timeout"
	default:
		if status >= 500 {
			return "internal_error"
		}
		return "request_error"
	}
}
