package server

import "github.com/gofiber/fiber/v3"

// New builds the HTTP application. Later beads add config, storage, middleware,
// and resource route groups behind this constructor.
func New() *fiber.App {
	app := fiber.New()

	api := app.Group("/api/v1")
	api.Get("/healthz", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	return app
}
