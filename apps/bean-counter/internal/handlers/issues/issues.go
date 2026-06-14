package issues

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/mattsp1290/bean-counter/internal/api/dto"
	"github.com/mattsp1290/bean-counter/internal/api/validate"
	appstore "github.com/mattsp1290/bean-counter/internal/store"
)

type Store interface {
	CreateIssue(context.Context, appstore.CreateIssueInput) (appstore.Issue, error)
	ListIssues(context.Context, appstore.ListFilter) ([]appstore.Issue, error)
	GetIssue(context.Context, string) (appstore.Issue, error)
	UpdateIssue(context.Context, string, appstore.UpdateIssueInput) (appstore.Issue, error)
	CloseIssue(context.Context, string, string, string) error
	DeleteIssue(context.Context, string) error
}

type Config struct {
	Store         Store
	ProjectPrefix string
	Actor         string
}

func Register(router fiber.Router, cfg Config) {
	h := Handler{cfg: cfg}
	router.Get("/issues", h.list)
	router.Post("/issues", h.create)
	router.Get("/issues/:id", h.get)
	router.Patch("/issues/:id", h.update)
	router.Post("/issues/:id/close", h.close)
	router.Delete("/issues/:id", h.delete)
}

type Handler struct {
	cfg Config
}

func (h Handler) list(c fiber.Ctx) error {
	filter, err := h.listFilter(c)
	if err != nil {
		return err
	}
	issues, err := h.cfg.Store.ListIssues(c.Context(), filter)
	if err != nil {
		return err
	}
	return c.JSON(dto.IssueListResponse{Issues: dto.IssuesFromStore(issues)})
}

func (h Handler) create(c fiber.Ctx) error {
	var req dto.CreateIssueRequest
	body := append([]byte(nil), c.Body()...)
	if err := decodeBody(body, &req); err != nil {
		return err
	}
	if err := validate.CreateIssueBody(body, req); err != nil {
		return err
	}
	issue, err := h.cfg.Store.CreateIssue(c.Context(), req.ToStoreInput(h.cfg.ProjectPrefix, h.cfg.Actor))
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(dto.IssueFromStore(issue))
}

func (h Handler) get(c fiber.Ctx) error {
	id := c.Params("id")
	if err := validate.ProjectIssueID(h.cfg.ProjectPrefix, id); err != nil {
		return err
	}
	issue, err := h.cfg.Store.GetIssue(c.Context(), id)
	if err != nil {
		return err
	}
	return c.JSON(dto.IssueFromStore(issue))
}

func (h Handler) update(c fiber.Ctx) error {
	id := c.Params("id")
	if err := validate.ProjectIssueID(h.cfg.ProjectPrefix, id); err != nil {
		return err
	}
	var req dto.UpdateIssueRequest
	body := append([]byte(nil), c.Body()...)
	if err := decodeBody(body, &req); err != nil {
		return err
	}
	if err := validate.UpdateIssueBody(body, req); err != nil {
		return err
	}
	issue, err := h.cfg.Store.UpdateIssue(c.Context(), id, req.ToStoreInput())
	if err != nil {
		return err
	}
	return c.JSON(dto.IssueFromStore(issue))
}

func (h Handler) close(c fiber.Ctx) error {
	id := c.Params("id")
	if err := validate.ProjectIssueID(h.cfg.ProjectPrefix, id); err != nil {
		return err
	}
	var req dto.CloseIssueRequest
	if len(c.Body()) > 0 {
		if err := decodeBody(c.Body(), &req); err != nil {
			return err
		}
	}
	if err := validate.CloseIssue(req); err != nil {
		return err
	}
	if err := h.cfg.Store.CloseIssue(c.Context(), id, h.cfg.Actor, req.Reason); err != nil {
		return err
	}
	issue, err := h.cfg.Store.GetIssue(c.Context(), id)
	if err != nil {
		return err
	}
	return c.JSON(dto.IssueFromStore(issue))
}

func (h Handler) delete(c fiber.Ctx) error {
	id := c.Params("id")
	if err := validate.ProjectIssueID(h.cfg.ProjectPrefix, id); err != nil {
		return err
	}
	if err := h.cfg.Store.DeleteIssue(c.Context(), id); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h Handler) listFilter(c fiber.Ctx) (appstore.ListFilter, error) {
	filter := appstore.ListFilter{Prefix: h.cfg.ProjectPrefix}
	query, err := url.ParseQuery(string(c.Request().URI().QueryString()))
	if err != nil {
		return filter, fiber.NewError(fiber.StatusBadRequest, "invalid query string")
	}
	for _, raw := range query["state"] {
		for _, state := range strings.Split(raw, ",") {
			state = strings.TrimSpace(state)
			if state != "" {
				if err := validate.IssueState("state", state); err != nil {
					return filter, err
				}
				filter.States = append(filter.States, appstore.IssueState(state))
			}
		}
	}
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 0 {
			return filter, fiber.NewError(fiber.StatusBadRequest, "limit must be a non-negative integer")
		}
		filter.Limit = limit
	}
	return filter, nil
}

func decodeBody(body []byte, out any) error {
	if err := json.Unmarshal(body, out); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON request body")
	}
	return nil
}
