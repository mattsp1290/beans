// Package server hosts bn serve: the JSON API over the hub and the embedded
// UI. Every read holds the index read lock; every mutation goes through the
// hub pipeline and then reloads the index so the next read sees it.
package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/static"

	"github.com/mattsp1290/beans/gitops"
	"github.com/mattsp1290/beans/internal/ops"
	"github.com/mattsp1290/beans/issue"
	"github.com/mattsp1290/beans/markdown"
	"github.com/mattsp1290/beans/vault"
)

// Config wires the server to a hub.
type Config struct {
	Hub     *gitops.Hub
	Index   *vault.Index
	HubDir  string
	Project string // initial project; the UI can switch
	Env     ops.Env
	// Prefix returns the id prefix for a project.
	Prefix func(project string) string
	// UI is the embedded Svelte app (a filesystem rooted at dist/).
	UI        fs.FS
	LogOutput io.Writer
	NoFetch   bool
}

// Server is one bn serve instance.
type Server struct {
	cfg  Config
	app  *fiber.App
	sse  *broker
	rend *markdown.Renderer
}

// New builds the Fiber application.
func New(cfg Config) *Server {
	if cfg.LogOutput == nil {
		cfg.LogOutput = os.Stderr
	}
	s := &Server{cfg: cfg, sse: newBroker()}
	s.rend = &markdown.Renderer{}
	s.rend.Resolve = s.resolveLink
	s.rend.Embed = s.embed
	s.rend.AssetHref = s.assetHref
	// UnescapePath decodes %XX in route params so an asset named "my pic.png"
	// (linked as my%20pic.png) matches the index; the traversal checks in
	// asset() run on the decoded value.
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler, UnescapePath: true})
	app.Use(logger.New(logger.Config{
		Format:     "${time} ${status} ${method} ${path} ${latency}\n",
		TimeFormat: "2006-01-02T15:04:05Z07:00",
		Stream:     cfg.LogOutput,
	}))
	app.Use(recover.New())
	s.app = app
	s.routes(app.Group("/api"))
	if cfg.UI != nil {
		app.Get("/*", static.New("", static.Config{
			FS:         cfg.UI,
			IndexNames: []string{"index.html"},
			NotFoundHandler: func(c fiber.Ctx) error {
				// SPA fallback: any non-file path gets index.html.
				if strings.HasPrefix(c.Path(), "/api/") || filepath.Ext(c.Path()) != "" {
					return fiber.NewError(fiber.StatusNotFound, "not found")
				}
				data, err := fs.ReadFile(cfg.UI, "index.html")
				if err != nil {
					return fiber.NewError(fiber.StatusNotFound, "ui not built")
				}
				c.Set(fiber.HeaderContentType, "text/html; charset=utf-8")
				return c.Status(fiber.StatusOK).Send(data)
			},
		}))
	}
	return s
}

// App exposes the Fiber app for tests and Run.
func (s *Server) App() *fiber.App { return s.app }

// Notify tells connected UIs that paths changed (from the watcher).
func (s *Server) Notify(paths []string) { s.sse.publish(paths) }

// Run serves on addr until ctx is done, with a 5 s drain.
func (s *Server) Run(ctx context.Context, addr string) error {
	errCh := make(chan error, 1)
	go func() { errCh <- s.app.Listen(addr, fiber.ListenConfig{DisableStartupMessage: true}) }()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.sse.close()
		if err := s.app.ShutdownWithTimeout(5 * time.Second); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		err := <-errCh
		if err == nil || errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
}

// ---------------------------------------------------------------------------
// errors
// ---------------------------------------------------------------------------

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorHandler renders every handler error as {error: {code, message}}.
func ErrorHandler(c fiber.Ctx, err error) error {
	status := fiber.StatusInternalServerError
	message := "internal server error"
	var fiberErr *fiber.Error
	var exitErr *gitops.ExitError
	switch {
	case errors.As(err, &fiberErr):
		status, message = fiberErr.Code, fiberErr.Message
	case errors.As(err, &exitErr):
		message = exitErr.Msg
		if exitErr.Code == gitops.ExitLock {
			status = fiber.StatusServiceUnavailable
		} else {
			status = fiber.StatusBadGateway
		}
	case errors.Is(err, ops.ErrNotFound):
		status, message = fiber.StatusNotFound, err.Error()
	case errors.Is(err, ops.ErrCycle):
		status, message = fiber.StatusConflict, err.Error()
	case err != nil:
		status, message = fiber.StatusBadRequest, err.Error()
	}
	return c.Status(status).JSON(errorBody{Error: errorDetail{Code: errorCodeForStatus(status), Message: message}})
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

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func (s *Server) index() *vault.Index { return s.cfg.Index }

// read runs fn under the index read lock after the throttled fetch.
func (s *Server) read(ctx context.Context, fn func(ix *vault.Index) error) error {
	if !s.cfg.NoFetch && s.cfg.Hub != nil {
		s.cfg.Hub.FetchIfStale(ctx)
	}
	ix := s.index()
	ix.RLock()
	defer ix.RUnlock()
	return fn(ix)
}

// mutationResult is the body of every successful mutation.
type mutationResult struct {
	ID      string `json:"id,omitempty"`
	Path    string `json:"path,omitempty"`
	Commit  string `json:"commit"`
	Pushed  bool   `json:"pushed"`
	Message string `json:"message"`
}

// mutate runs op through the pipeline and reloads the index.
func (s *Server) mutate(c fiber.Ctx, op gitops.Operation) (gitops.Result, error) {
	res, err := s.cfg.Hub.Mutate(c.Context(), op)
	if err != nil {
		return res, err
	}
	if err := s.index().ReloadAll(); err != nil {
		return res, err
	}
	return res, nil
}

func (s *Server) envFor(project string) ops.Env {
	env := s.cfg.Env
	env.Project = project
	return env
}

// projectParam reads the :p route param; "_all" means every project. Any
// other value must be a valid project name, so a crafted segment can never
// reach the filesystem.
func projectParam(c fiber.Ctx) (string, error) {
	p := c.Params("p")
	if p == "_all" {
		return "", nil
	}
	if !vault.ValidProjectName(p) {
		return "", fiber.NewError(fiber.StatusBadRequest, "invalid project name "+strconvQuote(p))
	}
	return p, nil
}

func strconvQuote(s string) string { return fmt.Sprintf("%q", s) }

// resolveLink maps a wikilink target to a UI href.
func (s *Server) resolveLink(target string) (string, bool) {
	n, ok := s.index().Lookup(target)
	if !ok {
		return "", false
	}
	return noteHref(n), true
}

func noteHref(n *vault.Note) string {
	switch n.Kind {
	case vault.KindIssue:
		if n.Issue != nil {
			return "/issues/" + n.Issue.ID
		}
	case vault.KindDoc:
		return "/wiki/" + strings.TrimSuffix(n.Path, ".md")
	case vault.KindMemory:
		return "/search?q=" + n.Basename
	case vault.KindPlan:
		return "/plans/" + n.Basename
	}
	return "/wiki/" + strings.TrimSuffix(n.Path, ".md")
}

// assetHref maps an image embed target to the asset route. Obsidian links
// images by basename anywhere in the vault, so the target is matched
// against the index's asset set by exact path, then by unique path suffix.
func (s *Server) assetHref(target string) string {
	ix := s.index()
	t := strings.Trim(target, "/")
	rel := ""
	if ix.Assets[t] {
		rel = t
	} else {
		for p := range ix.Assets {
			if strings.HasSuffix(p, "/"+t) {
				if rel != "" {
					rel = ""
					break
				}
				rel = p
			}
		}
	}
	if rel == "" {
		rel = t
	}
	segs := strings.Split(rel, "/")
	for i, seg := range segs {
		segs[i] = url.PathEscape(seg)
	}
	return "/api/assets/" + strings.Join(segs, "/")
}

// embed renders a note one level deep for ![[note]].
func (s *Server) embed(target, fragment string) (string, bool) {
	n, ok := s.index().Lookup(target)
	if !ok {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(s.cfg.HubDir, filepath.FromSlash(n.Path)))
	if err != nil {
		return "", false
	}
	src := data
	if fragment != "" {
		src = sectionOf(data, fragment)
	}
	html, _, err := s.rend.NoEmbeds().HTML(src)
	if err != nil {
		return "", false
	}
	return string(html), true
}

// sectionOf returns the markdown of the heading named fragment (matched by
// its auto id or its text) up to the next heading of the same or higher
// level; the whole document when not found.
func sectionOf(data []byte, fragment string) []byte {
	lines := strings.Split(string(data), "\n")
	want := strings.ToLower(strings.TrimSpace(fragment))
	start, level := -1, 0
	for i, l := range lines {
		if !strings.HasPrefix(l, "#") {
			continue
		}
		hashes := len(l) - len(strings.TrimLeft(l, "#"))
		text := strings.TrimSpace(l[hashes:])
		id := issue.Slug(text)
		if start < 0 {
			if strings.ToLower(text) == want || id == want || id == issue.Slug(want) {
				start, level = i, hashes
			}
			continue
		}
		if hashes <= level {
			return []byte(strings.Join(lines[start:i], "\n"))
		}
	}
	if start < 0 {
		return data
	}
	return []byte(strings.Join(lines[start:], "\n"))
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---------------------------------------------------------------------------
// SSE broker
// ---------------------------------------------------------------------------

type broker struct {
	mu      sync.Mutex
	clients map[chan []string]struct{}
	closed  bool
}

func newBroker() *broker { return &broker{clients: map[chan []string]struct{}{}} }

func (b *broker) subscribe() (chan []string, func()) {
	ch := make(chan []string, 8)
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.clients, ch)
		b.mu.Unlock()
	}
}

func (b *broker) publish(paths []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- paths:
		default:
		}
	}
}

func (b *broker) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for ch := range b.clients {
		close(ch)
		delete(b.clients, ch)
	}
}
