package graph

import (
	"context"

	"github.com/gofiber/fiber/v3"

	"github.com/mattsp1290/bean-counter/internal/api/dto"
	appstore "github.com/mattsp1290/bean-counter/internal/store"
)

type Store interface {
	ListIssues(context.Context, appstore.ListFilter) ([]appstore.Issue, error)
	ListDeps(context.Context, string) ([]appstore.DepEdge, error)
}

type Config struct {
	Store         Store
	ProjectPrefix string
}

func Register(router fiber.Router, cfg Config) {
	h := Handler{cfg: cfg}
	router.Get("/graph", h.get)
}

type Handler struct {
	cfg Config
}

func (h Handler) get(c fiber.Ctx) error {
	issues, err := h.cfg.Store.ListIssues(c.Context(), appstore.ListFilter{Prefix: h.cfg.ProjectPrefix})
	if err != nil {
		return err
	}
	deps, err := h.cfg.Store.ListDeps(c.Context(), h.cfg.ProjectPrefix)
	if err != nil {
		return err
	}
	return c.JSON(dto.GraphResponseFromStore(issues, deps))
}
