package server

import (
	"io"
	"os"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
)

// Config controls app-level HTTP behavior. Later config beads will populate it
// from the full application configuration.
type Config struct {
	CORSOrigin string
	LogOutput  io.Writer
}

// New builds the HTTP application with process-wide middleware and the
// versioned API route group.
func New(config ...Config) *fiber.App {
	cfg := configWithDefaults(config...)
	app := fiber.New()

	app.Use(recover.New())
	app.Use(logger.New(logger.Config{
		Format:     "${time} ${status} ${method} ${path} ${latency}\n",
		TimeFormat: "2006-01-02T15:04:05Z07:00",
		Stream:     cfg.LogOutput,
	}))
	app.Use(cors.New(cors.Config{
		AllowOrigins: []string{cfg.CORSOrigin},
		AllowMethods: []string{
			fiber.MethodGet,
			fiber.MethodPost,
			fiber.MethodPut,
			fiber.MethodPatch,
			fiber.MethodDelete,
			fiber.MethodOptions,
		},
		AllowHeaders: []string{
			fiber.HeaderAccept,
			fiber.HeaderAuthorization,
			fiber.HeaderContentType,
		},
	}))

	api := app.Group("/api/v1")
	api.Get("/healthz", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	return app
}

func configWithDefaults(config ...Config) Config {
	cfg := Config{
		CORSOrigin: "http://localhost:5173",
		LogOutput:  os.Stdout,
	}
	if len(config) == 0 {
		return cfg
	}
	if config[0].CORSOrigin != "" {
		cfg.CORSOrigin = config[0].CORSOrigin
	}
	if config[0].LogOutput != nil {
		cfg.LogOutput = config[0].LogOutput
	}
	return cfg
}
