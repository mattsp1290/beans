package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mattsp1290/beans/gitops"

	"github.com/mattsp1290/beans/internal/ops"
	"github.com/mattsp1290/beans/issue"
	"github.com/mattsp1290/beans/plan"
	"github.com/mattsp1290/beans/vault"
)

// sseHeartbeat is the interval between keep-alive comments on /api/events.
var sseHeartbeat = 15 * time.Second

func (s *Server) routes(api fiber.Router) {
	api.Get("/health", s.health)
	api.Get("/projects", s.listProjects)
	api.Get("/projects/:p/issues", s.listIssues)
	api.Get("/projects/:p/requests", s.listRequests)
	api.Get("/projects/:p/plans", s.listPlans)
	api.Get("/projects/:p/ready", s.ready)
	api.Post("/projects/:p/issues", s.createIssue)
	api.Get("/issues/:id", s.showIssue)
	api.Get("/requests/:id", s.showRequest)
	api.Get("/plans/:id", s.showPlan)
	api.Patch("/issues/:id", s.updateIssue)
	api.Post("/issues/:id/notes", s.addNote)
	api.Post("/issues/:id/close", s.closeIssue)
	api.Post("/issues/:id/reopen", s.reopenIssue)
	api.Post("/issues/:id/deps", s.addDep)
	api.Delete("/issues/:id/deps/:target", s.removeDep)
	api.Get("/graph", s.graph)
	api.Get("/docs/tree", s.docsTree)
	api.Get("/docs/*", s.docPage)
	api.Get("/assets/*", s.asset)
	api.Get("/search", s.search)
	api.Get("/events", s.events)
}

// ---------------------------------------------------------------------------
// JSON shapes (mirrors cmd/bn/json.go)
// ---------------------------------------------------------------------------

type issueJSON struct {
	ID          string               `json:"id"`
	Title       string               `json:"title"`
	Type        string               `json:"type"`
	Status      string               `json:"status"`
	Priority    int                  `json:"priority"`
	Labels      []string             `json:"labels"`
	Assignee    string               `json:"assignee"`
	Parent      string               `json:"parent"`
	BlockedBy   []string             `json:"blocked_by"`
	URL         string               `json:"url"`
	Created     string               `json:"created"`
	Updated     string               `json:"updated"`
	Project     string               `json:"project"`
	Path        string               `json:"path"`
	Archived    bool                 `json:"archived"`
	Description string               `json:"description,omitempty"`
	Log         []logJSON            `json:"log,omitempty"`
	Children    []childJSON          `json:"children,omitempty"`
	Backlinks   []backlinkJSON       `json:"backlinks,omitempty"`
	Blockers    []blockerJSON        `json:"blockers,omitempty"`
	HTML        string               `json:"html,omitempty"`
	Workflow    *workflowJSON        `json:"workflow,omitempty"`
	Requests    []requestSummaryJSON `json:"requests,omitempty"`
}

type requestSummaryJSON struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority int    `json:"priority"`
	Project  string `json:"project"`
}
type linkedIssueJSON struct {
	ID       string `json:"id"`
	Title    string `json:"title,omitempty"`
	Status   string `json:"status,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Project  string `json:"project,omitempty"`
	Archived bool   `json:"archived,omitempty"`
	Missing  bool   `json:"missing,omitempty"`
}
type requestJSON struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Status      string            `json:"status"`
	Priority    int               `json:"priority"`
	Labels      []string          `json:"labels"`
	RequestedBy string            `json:"requested_by"`
	Created     string            `json:"created"`
	Updated     string            `json:"updated"`
	Project     string            `json:"project"`
	Path        string            `json:"path"`
	IssueCount  int               `json:"issue_count"`
	Issues      []linkedIssueJSON `json:"issues,omitempty"`
	Backlinks   []backlinkJSON    `json:"backlinks,omitempty"`
	Log         []logJSON         `json:"log,omitempty"`
	Toc         []headingJSON     `json:"toc,omitempty"`
	HTML        string            `json:"html,omitempty"`
}

type planListJSON struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	Project      string `json:"project"`
	Path         string `json:"path"`
	Created      string `json:"created"`
	Updated      string `json:"updated"`
	SectionCount int    `json:"section_count"`
}
type planSectionJSON struct {
	Path string `json:"path"`
	HTML string `json:"html"`
}
type planDetailJSON struct {
	planListJSON
	Summary   planSummaryJSON     `json:"summary"`
	Sections  []planSectionJSON   `json:"sections"`
	Backlinks []backlinkJSON      `json:"backlinks"`
	Execution vault.PlanExecution `json:"execution"`
}
type planSummaryJSON struct {
	Status             string `json:"status"`
	OutcomeHTML        string `json:"outcome_html"`
	AffectedAreasHTML  string `json:"affected_areas_html"`
	ExecutionOrderHTML string `json:"execution_order_html"`
	RisksHTML          string `json:"risks_html"`
	Graph              any    `json:"graph"`
}

func planList(project string, p *plan.Plan) planListJSON {
	return planListJSON{ID: p.ID, Title: p.Title, Status: string(p.Status), Project: project, Path: p.Path, Created: p.Created.UTC().Format(time.RFC3339), Updated: p.Updated.UTC().Format(time.RFC3339), SectionCount: len(p.Sections)}
}

type logJSON struct {
	At     string `json:"at"`
	Actor  string `json:"actor"`
	Repo   string `json:"repo,omitempty"`
	SHA    string `json:"sha,omitempty"`
	Branch string `json:"branch,omitempty"`
	Event  string `json:"event"`
	Raw    string `json:"raw,omitempty"`
}

// backlinkJSON is one note linking to the current one. From is the issue id
// when the referring note is an issue, else its basename; NoteKind says
// which ("issue", "doc", "memory") so the UI can route without guessing.
type backlinkJSON struct {
	From     string `json:"from"`
	Kind     string `json:"kind"`
	NoteKind string `json:"note_kind"`
	Path     string `json:"path,omitempty"`
	Title    string `json:"title,omitempty"`
}

// childJSON is a child issue summary.
type childJSON struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Project string `json:"project"`
	Title   string `json:"title"`
}

type blockerJSON struct {
	ID      string `json:"id"`
	Status  string `json:"status,omitempty"`
	Project string `json:"project,omitempty"`
	Title   string `json:"title,omitempty"`
	Missing bool   `json:"missing,omitempty"`
}

type workflowJSON struct {
	Statuses []string `json:"statuses"`
	Active   []string `json:"active"`
	Terminal []string `json:"terminal"`
}

// backlinksOf lists the notes linking to basename, sorted, with the
// referring note's kind so the UI can route to /issues/<id> or /wiki/<path>.
func backlinksOf(ix *vault.Index, basename string) []backlinkJSON {
	refs := append([]vault.LinkRef(nil), ix.Backlinks[basename]...)
	sort.Slice(refs, func(i, j int) bool { return refs[i].From < refs[j].From })
	out := make([]backlinkJSON, 0, len(refs))
	for _, r := range refs {
		bl := backlinkJSON{From: r.From, Kind: string(r.Kind), NoteKind: string(vault.KindDoc)}
		if fn, ok := ix.Notes[r.From]; ok {
			bl.Path = fn.Path
			bl.Title = fn.Title
			bl.NoteKind = string(fn.Kind)
			if fn.Issue != nil {
				bl.From = fn.Issue.ID
			}
		}
		out = append(out, bl)
	}
	return out
}

func resolveID(ix *vault.Index, target string) string {
	if n, ok := ix.Lookup(target); ok && n.Issue != nil {
		return n.Issue.ID
	}
	return target
}

func toIssueJSON(ix *vault.Index, iss *issue.Issue) issueJSON {
	out := issueJSON{
		ID: iss.ID, Title: iss.Title, Type: iss.Type, Status: iss.Status, Priority: iss.Priority,
		Labels: iss.Labels, Assignee: iss.Assignee, URL: iss.URL,
		Created: iss.Created.UTC().Format("2006-01-02T15:04:05Z07:00"), Updated: iss.Updated.UTC().Format("2006-01-02T15:04:05Z07:00"),
		Project: iss.Project, Path: iss.Path, Archived: iss.Archived, BlockedBy: []string{},
	}
	if out.Labels == nil {
		out.Labels = []string{}
	}
	if !iss.Parent.IsZero() {
		out.Parent = resolveID(ix, iss.Parent.Target)
	}
	for _, b := range iss.BlockedBy {
		out.BlockedBy = append(out.BlockedBy, resolveID(ix, b.Target))
	}
	return out
}

func (s *Server) toIssueDetail(ix *vault.Index, iss *issue.Issue) issueJSON {
	out := toIssueJSON(ix, iss)
	out.Description = iss.Description
	for _, e := range iss.Log {
		out.Log = append(out.Log, logJSON{At: e.At.UTC().Format("2006-01-02T15:04:05Z07:00"), Actor: e.Actor, Repo: e.Repo, SHA: e.SHA, Branch: e.Branch, Event: e.Event, Raw: e.Raw})
	}
	for _, c := range ix.Children(iss.ID) {
		out.Children = append(out.Children, childJSON{ID: c.ID, Status: c.Status, Project: c.Project, Title: c.Title})
	}
	base := strings.TrimSuffix(filepath.Base(iss.Path), ".md")
	for _, link := range backlinksOf(ix, base) {
		if link.Kind != string(vault.LinkRequestIssue) {
			out.Backlinks = append(out.Backlinks, link)
		}
	}
	resolved, unresolved := ix.Blockers(iss)
	for _, b := range resolved {
		out.Blockers = append(out.Blockers, blockerJSON{ID: b.ID, Status: b.Status, Project: b.Project, Title: b.Title})
	}
	for _, u := range unresolved {
		out.Blockers = append(out.Blockers, blockerJSON{ID: u, Missing: true})
	}
	html, _, err := s.rend.HTML([]byte(iss.Description + iss.Body))
	if err == nil {
		out.HTML = string(html)
	}
	for _, link := range ix.Backlinks[base] {
		if link.Kind != vault.LinkRequestIssue {
			continue
		}
		note, ok := ix.Notes[link.From]
		if !ok || note.Request == nil {
			continue
		}
		req := note.Request
		out.Requests = append(out.Requests, requestSummaryJSON{ID: req.ID, Title: req.Title, Status: req.Status, Priority: req.Priority, Project: req.Project})
	}
	sort.Slice(out.Requests, func(i, j int) bool { return out.Requests[i].ID < out.Requests[j].ID })
	wf := ix.WorkflowFor(iss.Project)
	out.Workflow = &workflowJSON{Statuses: wf.Statuses, Active: wf.Active, Terminal: wf.Terminal}
	return out
}

func toRequestJSON(ix *vault.Index, req *issue.Request) requestJSON {
	out := requestJSON{ID: req.ID, Title: req.Title, Status: req.Status, Priority: req.Priority, Labels: req.Labels, RequestedBy: req.RequestedBy, Created: req.Created.UTC().Format(time.RFC3339), Updated: req.Updated.UTC().Format(time.RFC3339), Project: req.Project, Path: req.Path, IssueCount: len(req.Issues)}
	if out.Labels == nil {
		out.Labels = []string{}
	}
	return out
}

func (s *Server) toRequestDetail(ix *vault.Index, req *issue.Request) requestJSON {
	out := toRequestJSON(ix, req)
	for _, link := range req.Issues {
		if n, ok := ix.Lookup(link.Target); ok && n.Issue != nil {
			i := n.Issue
			out.Issues = append(out.Issues, linkedIssueJSON{ID: i.ID, Title: i.Title, Status: i.Status, Priority: i.Priority, Project: i.Project, Archived: i.Archived})
		} else {
			out.Issues = append(out.Issues, linkedIssueJSON{ID: link.Target, Missing: true})
		}
	}
	for _, e := range req.Log {
		out.Log = append(out.Log, logJSON{At: e.At.UTC().Format(time.RFC3339), Actor: e.Actor, Repo: e.Repo, SHA: e.SHA, Branch: e.Branch, Event: e.Event, Raw: e.Raw})
	}
	base := strings.TrimSuffix(filepath.Base(req.Path), ".md")
	for _, b := range backlinksOf(ix, base) {
		if b.Kind != string(vault.LinkRequestIssue) {
			out.Backlinks = append(out.Backlinks, b)
		}
	}
	if html, toc, err := s.rend.HTML([]byte(req.Body)); err == nil {
		out.HTML = string(html)
		for _, h := range toc {
			out.Toc = append(out.Toc, headingJSON{Level: h.Level, ID: h.ID, Text: h.Text})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// handlers: reads
// ---------------------------------------------------------------------------

func (s *Server) health(c fiber.Ctx) error {
	out := fiber.Map{"status": "ok", "hub": s.cfg.HubDir, "project": s.cfg.Project, "ahead": 0, "behind": 0}
	if s.cfg.Hub != nil {
		if st, err := s.cfg.Hub.Status(c.Context()); err == nil {
			out["ahead"], out["behind"] = st.Ahead, st.Behind
		}
	}
	return c.JSON(out)
}

type projectJSON struct {
	Name     string         `json:"name"`
	Prefix   string         `json:"prefix"`
	Counts   map[string]int `json:"counts"`
	Workflow workflowJSON   `json:"workflow"`
}

func (s *Server) listProjects(c fiber.Ctx) error {
	return s.read(c.Context(), func(ix *vault.Index) error {
		out := make([]projectJSON, 0, len(ix.Projects))
		for _, name := range sortedKeys(ix.Projects) {
			p := ix.Projects[name]
			counts := map[string]int{"open": 0, "in_progress": 0, "closed": 0}
			for _, iss := range ix.Issues {
				if iss.Project != name {
					continue
				}
				switch {
				case p.Workflow.IsTerminal(iss.Status):
					counts["closed"]++
				case iss.Status == "in_progress":
					counts["in_progress"]++
				default:
					counts["open"]++
				}
			}
			prefix := p.Config.Prefix
			if prefix == "" {
				prefix = name
			}
			out = append(out, projectJSON{Name: name, Prefix: prefix, Counts: counts, Workflow: workflowJSON{Statuses: p.Workflow.Statuses, Active: p.Workflow.Active, Terminal: p.Workflow.Terminal}})
		}
		return c.JSON(out)
	})
}

func (s *Server) listIssues(c fiber.Ctx) error {
	project, err := projectParam(c)
	if err != nil {
		return err
	}
	status, typ, label := c.Query("status"), c.Query("type"), c.Query("label")
	archived := c.Query("archived") == "true"
	q := strings.ToLower(c.Query("q"))
	return s.read(c.Context(), func(ix *vault.Index) error {
		var out []issueJSON
		for _, iss := range ix.ProjectIssues(project, archived) {
			wf := ix.WorkflowFor(iss.Project)
			if !archived && status == "" && wf.IsTerminal(iss.Status) {
				continue
			}
			if status != "" && iss.Status != status {
				continue
			}
			if typ != "" && iss.Type != typ {
				continue
			}
			if label != "" && !contains(iss.Labels, label) {
				continue
			}
			if q != "" && !strings.Contains(strings.ToLower(iss.Title+" "+iss.ID+" "+strings.Join(iss.Labels, " ")), q) {
				continue
			}
			out = append(out, toIssueJSON(ix, iss))
		}
		if out == nil {
			out = []issueJSON{}
		}
		return c.JSON(out)
	})
}

func (s *Server) listRequests(c fiber.Ctx) error {
	project, err := projectParam(c)
	if err != nil {
		return err
	}
	status, label, q := c.Query("status"), c.Query("label"), c.Query("q")
	if status != "" && !issue.ValidRequestStatus(status) {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request status")
	}
	terminal := c.Query("terminal") == "true"
	var priority *int
	if raw := c.Query("priority"); raw != "" {
		var n int
		if _, err := fmt.Sscan(raw, &n); err != nil || n < 0 || n > 4 {
			return fiber.NewError(fiber.StatusBadRequest, "invalid priority")
		}
		priority = &n
	}
	return s.read(c.Context(), func(ix *vault.Index) error {
		reqs := ix.ProjectRequests(project, status, label, priority, q, terminal)
		out := make([]requestJSON, 0, len(reqs))
		for _, r := range reqs {
			out = append(out, toRequestJSON(ix, r))
		}
		return c.JSON(out)
	})
}

func (s *Server) ready(c fiber.Ctx) error {
	project, err := projectParam(c)
	if err != nil {
		return err
	}
	return s.read(c.Context(), func(ix *vault.Index) error {
		out := []issueJSON{}
		for _, iss := range ix.Ready(project, project == "") {
			out = append(out, toIssueJSON(ix, iss))
		}
		return c.JSON(out)
	})
}

func (s *Server) showIssue(c fiber.Ctx) error {
	id := c.Params("id")
	return s.read(c.Context(), func(ix *vault.Index) error {
		iss, ok := ix.Issues[id]
		if !ok {
			return fiber.NewError(fiber.StatusNotFound, "issue "+id+" not found")
		}
		return c.JSON(s.toIssueDetail(ix, iss))
	})
}

func (s *Server) showRequest(c fiber.Ctx) error {
	id := c.Params("id")
	return s.read(c.Context(), func(ix *vault.Index) error {
		req, ok := ix.RequestByID(id)
		if !ok {
			return fiber.NewError(fiber.StatusNotFound, "request "+id+" not found")
		}
		return c.JSON(s.toRequestDetail(ix, req))
	})
}

func (s *Server) listPlans(c fiber.Ctx) error {
	project, err := projectParam(c)
	if err != nil {
		return err
	}
	status := c.Query("status")
	return s.read(c.Context(), func(ix *vault.Index) error {
		out := []planListJSON{}
		for _, p := range ix.ProjectPlans(project) {
			if status != "" && string(p.Status) != status {
				continue
			}
			n := ix.ByPath[p.Path]
			out = append(out, planList(n.Project, p))
		}
		return c.JSON(out)
	})
}
func (s *Server) showPlan(c fiber.Ctx) error {
	id := c.Params("id")
	if strings.TrimSpace(id) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "invalid plan id")
	}
	return s.read(c.Context(), func(ix *vault.Index) error {
		p, ok := ix.PlanByID(id)
		if !ok {
			return fiber.NewError(fiber.StatusNotFound, "plan "+id+" not found")
		}
		n := ix.ByPath[p.Path]
		out := planDetailJSON{planListJSON: planList(n.Project, p), Backlinks: backlinksOf(ix, p.ID)}
		out.Execution, _ = ix.PlanExecution(id)
		html := func(md string) string {
			b, _, e := s.rend.HTML([]byte(md))
			if e != nil {
				return ""
			}
			return string(b)
		}
		out.Summary = planSummaryJSON{Status: string(p.Status), OutcomeHTML: html(p.Summary.Outcome), AffectedAreasHTML: html(p.Summary.AffectedAreas), ExecutionOrderHTML: html(p.Summary.ExecutionOrder), RisksHTML: html(p.Summary.Risks), Graph: p.Graph}
		for _, section := range p.SectionBodies {
			out.Sections = append(out.Sections, planSectionJSON{Path: section.Path, HTML: html(section.Markdown)})
		}
		return c.JSON(out)
	})
}

type graphNode struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority int    `json:"priority"`
	Type     string `json:"type"`
	Project  string `json:"project"`
	Archived bool   `json:"archived"`
}

type graphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

func (s *Server) graph(c fiber.Ctx) error {
	project := c.Query("project")
	all := c.Query("all") == "true" || project == "" || project == "_all"
	if all {
		project = ""
	}
	return s.read(c.Context(), func(ix *vault.Index) error {
		nodes, edges := ix.Graph(project, all)
		outN := make([]graphNode, 0, len(nodes))
		for _, n := range nodes {
			outN = append(outN, graphNode{ID: n.ID, Title: n.Title, Status: n.Status, Priority: n.Priority, Type: n.Type, Project: n.Project, Archived: n.Archived})
		}
		outE := make([]graphEdge, 0, len(edges))
		for _, e := range edges {
			outE = append(outE, graphEdge{From: e.From, To: e.To, Kind: e.Kind})
		}
		return c.JSON(fiber.Map{"nodes": outN, "edges": outE})
	})
}

type docEntry struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Project string `json:"project"`
}

func (s *Server) docsTree(c fiber.Ctx) error {
	project := c.Query("project")
	if project == "_all" {
		project = ""
	}
	return s.read(c.Context(), func(ix *vault.Index) error {
		docs := []docEntry{}
		for _, n := range ix.Notes {
			if n.Kind != vault.KindDoc {
				continue
			}
			if project != "" && n.Project != "" && n.Project != project {
				continue
			}
			docs = append(docs, docEntry{Path: n.Path, Title: n.Title, Project: n.Project})
		}
		sort.Slice(docs, func(i, j int) bool { return docs[i].Path < docs[j].Path })
		return c.JSON(fiber.Map{"docs": docs})
	})
}

type headingJSON struct {
	Level int    `json:"level"`
	ID    string `json:"id"`
	Text  string `json:"text"`
}

func (s *Server) docPage(c fiber.Ctx) error {
	path := strings.Trim(c.Params("*"), "/")
	if !strings.HasSuffix(path, ".md") {
		path += ".md"
	}
	return s.read(c.Context(), func(ix *vault.Index) error {
		n, ok := ix.ByPath[path]
		if !ok || n.Kind != vault.KindDoc {
			return fiber.NewError(fiber.StatusNotFound, "doc "+path+" not found")
		}
		data, err := os.ReadFile(filepath.Join(s.cfg.HubDir, filepath.FromSlash(n.Path)))
		if err != nil {
			return fiber.NewError(fiber.StatusNotFound, "doc "+path+" not found")
		}
		html, toc, err := s.rend.HTML(data)
		if err != nil {
			return err
		}
		headings := make([]headingJSON, 0, len(toc))
		for _, h := range toc {
			headings = append(headings, headingJSON{Level: h.Level, ID: h.ID, Text: h.Text})
		}
		backlinks := backlinksOf(ix, n.Basename)
		outlinks := make([]fiber.Map, 0, len(n.Outlinks))
		for _, l := range n.Outlinks {
			outlinks = append(outlinks, fiber.Map{"to": l.To, "kind": l.Kind})
		}
		fm := n.Frontmatter
		if fm == nil {
			fm = map[string]any{}
		}
		return c.JSON(fiber.Map{"path": n.Path, "title": n.Title, "project": n.Project, "frontmatter": fm, "html": string(html), "toc": headings, "backlinks": backlinks, "outlinks": outlinks})
	})
}

// asset serves an image referenced by an embed. The path is looked up in the
// index's asset set (never resolved against the filesystem from client
// input), then cleaned, joined, and checked to stay inside the hub; symlinks
// are refused.
func (s *Server) asset(c fiber.Ctx) error {
	raw := c.Params("*")
	if filepath.IsAbs(raw) || strings.Contains(raw, "..") {
		return fiber.NewError(fiber.StatusBadRequest, "invalid asset path")
	}
	rel := filepath.ToSlash(filepath.Clean(raw))
	var known bool
	_ = s.read(c.Context(), func(ix *vault.Index) error {
		known = ix.Assets[rel]
		return nil
	})
	if !known {
		return fiber.NewError(fiber.StatusNotFound, "asset not found")
	}
	abs := filepath.Join(s.cfg.HubDir, filepath.FromSlash(rel))
	if r, err := filepath.Rel(s.cfg.HubDir, abs); err != nil || strings.HasPrefix(r, "..") {
		return fiber.NewError(fiber.StatusBadRequest, "invalid asset path")
	}
	fi, err := os.Lstat(abs)
	if err != nil || fi.Mode()&os.ModeSymlink != 0 || fi.IsDir() {
		return fiber.NewError(fiber.StatusNotFound, "asset not found")
	}
	return c.SendFile(abs)
}

type hitJSON struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Basename string `json:"basename"`
	Title    string `json:"title"`
	Project  string `json:"project"`
	Path     string `json:"path"`
	Score    int    `json:"score"`
}

func (s *Server) search(c fiber.Ctx) error {
	q := c.Query("q")
	project := c.Query("project")
	if project == "_all" {
		project = ""
	}
	var kinds []vault.Kind
	if k := c.Query("kind"); k != "" {
		kinds = []vault.Kind{vault.Kind(k)}
	}
	return s.read(c.Context(), func(ix *vault.Index) error {
		out := []hitJSON{}
		for _, h := range ix.Search(q, kinds) {
			if project != "" && h.Project != "" && h.Project != project {
				continue
			}
			out = append(out, hitJSON{Kind: string(h.Kind), ID: h.ID, Basename: h.Basename, Title: h.Title, Project: h.Project, Path: h.Path, Score: h.Score})
		}
		return c.JSON(out)
	})
}

// events streams reload notifications as Server-Sent Events.
func (s *Server) events(c fiber.Ctx) error {
	c.Set(fiber.HeaderContentType, "text/event-stream")
	c.Set(fiber.HeaderCacheControl, "no-cache")
	c.Set("Connection", "keep-alive")
	ch, unsubscribe := s.sse.subscribe()
	ctx := c.Context()
	c.SendStreamWriter(func(w *bufio.Writer) {
		defer unsubscribe()
		_, _ = fmt.Fprint(w, ": connected\n\n")
		if err := w.Flush(); err != nil {
			return
		}
		// A heartbeat makes a closed browser tab fail the next flush, so its
		// goroutine and subscription are reaped even when no file changes.
		ticker := time.NewTicker(sseHeartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = fmt.Fprint(w, ": ping\n\n")
				if err := w.Flush(); err != nil {
					return
				}
			case paths, ok := <-ch:
				if !ok {
					return
				}
				data, _ := json.Marshal(paths)
				_, _ = fmt.Fprintf(w, "event: reload\ndata: %s\n\n", data)
				if err := w.Flush(); err != nil {
					return
				}
			}
		}
	})
	return nil
}

// ---------------------------------------------------------------------------
// handlers: mutations
// ---------------------------------------------------------------------------

type createBody struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Priority    *int     `json:"priority"`
	Type        string   `json:"type"`
	Labels      []string `json:"labels"`
	Parent      string   `json:"parent"`
	Assignee    string   `json:"assignee"`
	BlockedBy   []string `json:"blocked_by"`
	URL         string   `json:"url"`
}

func (s *Server) createIssue(c fiber.Ctx) error {
	project, err := projectParam(c)
	if err != nil {
		return err
	}
	if project == "" {
		return fiber.NewError(fiber.StatusBadRequest, "a project is required to create an issue")
	}
	var body createBody
	if err := c.Bind().JSON(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	if strings.TrimSpace(body.Title) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "title is required")
	}
	in := ops.CreateInput{Title: body.Title, Description: body.Description, Priority: 2, Type: body.Type, Labels: body.Labels, Parent: body.Parent, Assignee: body.Assignee, BlockedBy: body.BlockedBy, URL: body.URL}
	if body.Priority != nil {
		in.Priority = *body.Priority
	}
	op, res := ops.Create(s.envFor(project), in, s.cfg.Prefix(project))
	out, err := s.mutate(c, op)
	if err != nil {
		return err
	}
	return c.JSON(mutationResult{ID: res.ID, Path: res.Path, Commit: out.SHA, Pushed: out.Pushed, Message: out.Message})
}

type updateBody struct {
	Status       *string  `json:"status"`
	Title        *string  `json:"title"`
	Description  *string  `json:"description"`
	Priority     *int     `json:"priority"`
	Type         *string  `json:"type"`
	Assignee     *string  `json:"assignee"`
	Parent       *string  `json:"parent"`
	AddLabels    []string `json:"add_labels"`
	RemoveLabels []string `json:"remove_labels"`
	Note         string   `json:"note"`
	Claim        bool     `json:"claim"`
	Force        bool     `json:"force"`
}

func (s *Server) updateIssue(c fiber.Ctx) error {
	id := c.Params("id")
	var body updateBody
	if err := c.Bind().JSON(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	in := ops.UpdateInput{Status: body.Status, Title: body.Title, Description: body.Description, Priority: body.Priority, Type: body.Type,
		Assignee: body.Assignee, Parent: body.Parent, AddLabels: body.AddLabels, RemoveLabel: body.RemoveLabels, Note: body.Note, Claim: body.Claim, Force: body.Force}
	if in.IsEmpty() {
		return fiber.NewError(fiber.StatusBadRequest, "nothing to update")
	}
	return s.runMutation(c, ops.Update(s.cfg.Env, id, in))
}

func (s *Server) addNote(c fiber.Ctx) error {
	var body struct {
		Text string `json:"text"`
	}
	if err := c.Bind().JSON(&body); err != nil || strings.TrimSpace(body.Text) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "text is required")
	}
	return s.runMutation(c, ops.Update(s.cfg.Env, c.Params("id"), ops.UpdateInput{Note: body.Text}))
}

func (s *Server) closeIssue(c fiber.Ctx) error {
	var body struct {
		Reason string `json:"reason"`
	}
	if err := c.Bind().JSON(&body); err != nil || strings.TrimSpace(body.Reason) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "reason is required")
	}
	return s.runMutation(c, ops.Close(s.cfg.Env, c.Params("id"), body.Reason))
}

func (s *Server) reopenIssue(c fiber.Ctx) error {
	return s.runMutation(c, ops.Reopen(s.cfg.Env, c.Params("id")))
}

func (s *Server) addDep(c fiber.Ctx) error {
	var body struct {
		Target string `json:"target"`
		Type   string `json:"type"`
	}
	if err := c.Bind().JSON(&body); err != nil || strings.TrimSpace(body.Target) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "target is required")
	}
	return s.runMutation(c, ops.DepAdd(s.cfg.Env, c.Params("id"), body.Target, body.Type))
}

func (s *Server) removeDep(c fiber.Ctx) error {
	return s.runMutation(c, ops.DepRemove(s.cfg.Env, c.Params("id"), c.Params("target"), c.Query("type")))
}

// runMutation checks the id exists (404) before running a mutation.
func (s *Server) runMutation(c fiber.Ctx, op gitops.Operation) error {
	id := c.Params("id")
	var exists bool
	_ = s.read(c.Context(), func(ix *vault.Index) error {
		_, exists = ix.Issues[id]
		return nil
	})
	if !exists {
		if _, err := ops.Find(s.cfg.HubDir, id); err != nil {
			return fiber.NewError(fiber.StatusNotFound, "issue "+id+" not found")
		}
	}
	out, err := s.mutate(c, op)
	if err != nil {
		return err
	}
	return c.JSON(mutationResult{Commit: out.SHA, Pushed: out.Pushed, Message: out.Message})
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
