package health

import (
	"context"

	"github.com/gofiber/fiber/v3"
)

type ReadinessStore interface {
	ProjectExists(context.Context, string) (bool, error)
}

type Config struct {
	Store         ReadinessStore
	ProjectPrefix string
}

func Register(router fiber.Router, cfg Config) {
	h := Handler{cfg: cfg}
	router.Get("/readyz", h.ready)
}

type Handler struct {
	cfg Config
}

func (h Handler) ready(c fiber.Ctx) error {
	if h.cfg.Store == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "store is not configured")
	}
	exists, err := h.cfg.Store.ProjectExists(c.Context(), h.cfg.ProjectPrefix)
	if err != nil {
		return err
	}
	if !exists {
		return fiber.NewError(fiber.StatusServiceUnavailable, "project is not registered")
	}
	return c.JSON(fiber.Map{"status": "ok"})
}
