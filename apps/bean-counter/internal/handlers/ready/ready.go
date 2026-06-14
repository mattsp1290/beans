package ready

import (
	"context"

	"github.com/gofiber/fiber/v3"

	"github.com/mattsp1290/bean-counter/internal/api/dto"
	appstore "github.com/mattsp1290/bean-counter/internal/store"
)

type Source interface {
	ReadyIssues(context.Context) ([]appstore.Issue, error)
}

type Config struct {
	Source Source
}

func Register(router fiber.Router, cfg Config) {
	h := Handler{cfg: cfg}
	router.Get("/ready", h.get)
}

type Handler struct {
	cfg Config
}

func (h Handler) get(c fiber.Ctx) error {
	issues, err := h.cfg.Source.ReadyIssues(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(dto.ReadyResponseFromStore(issues))
}
