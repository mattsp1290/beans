package deps

import (
	"context"
	"encoding/json"

	"github.com/gofiber/fiber/v3"

	"github.com/mattsp1290/bean-counter/internal/api/dto"
	"github.com/mattsp1290/bean-counter/internal/api/validate"
	appstore "github.com/mattsp1290/bean-counter/internal/store"
)

type Store interface {
	AddDep(context.Context, string, string) error
	RemoveDep(context.Context, string, string) error
	// ListBlockingDeps returns only blocking (dep_type="blocks") edges. beans
	// 0008 added parent-child membership edges that ListDeps now also returns;
	// the dependency views deliberately ignore non-blocking edges.
	ListBlockingDeps(context.Context, string) ([]appstore.DepEdge, error)
}

type Config struct {
	Store         Store
	ProjectPrefix string
}

func Register(router fiber.Router, cfg Config) {
	h := Handler{cfg: cfg}
	router.Get("/deps", h.list)
	router.Post("/issues/:id/deps", h.add)
	router.Delete("/issues/:id/deps/:blocked_by_id", h.remove)
}

type Handler struct {
	cfg Config
}

func (h Handler) list(c fiber.Ctx) error {
	deps, err := h.cfg.Store.ListBlockingDeps(c.Context(), h.cfg.ProjectPrefix)
	if err != nil {
		return err
	}
	return c.JSON(dto.DependencyListResponse{Dependencies: dto.DependenciesFromStore(deps)})
}

func (h Handler) add(c fiber.Ctx) error {
	id := c.Params("id")
	var req dto.AddDependencyRequest
	if err := decodeBody(c.Body(), &req); err != nil {
		return err
	}
	if err := validate.AddDependency(id, req); err != nil {
		return err
	}
	if err := validate.ProjectIssueID(h.cfg.ProjectPrefix, id); err != nil {
		return err
	}
	if err := validate.ProjectIssueIDField("blocked_by_id", h.cfg.ProjectPrefix, req.BlockedByID); err != nil {
		return err
	}
	if err := h.cfg.Store.AddDep(c.Context(), id, req.BlockedByID); err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(dto.Dependency{IssueID: id, BlockedByID: req.BlockedByID})
}

func (h Handler) remove(c fiber.Ctx) error {
	id := c.Params("id")
	blockedByID := c.Params("blocked_by_id")
	if err := validate.RemoveDependency(id, blockedByID); err != nil {
		return err
	}
	if err := validate.ProjectIssueID(h.cfg.ProjectPrefix, id); err != nil {
		return err
	}
	if err := validate.ProjectIssueIDField("blocked_by_id", h.cfg.ProjectPrefix, blockedByID); err != nil {
		return err
	}
	if err := h.cfg.Store.RemoveDep(c.Context(), id, blockedByID); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func decodeBody(body []byte, out any) error {
	if err := json.Unmarshal(body, out); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON request body")
	}
	return nil
}
