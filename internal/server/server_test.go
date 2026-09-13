package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mattsp1290/beans/gitops"
	"github.com/mattsp1290/beans/internal/ops"
	"github.com/mattsp1290/beans/issue"
	"github.com/mattsp1290/beans/vault"
)

type env struct {
	t   *testing.T
	srv *Server
	hub string
}

func TestHandoffWikiRenderingAndArchivedSearch(t *testing.T) {
	e := newEnv(t)
	write := func(rel, content string) {
		p := filepath.Join(e.hub, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	live := "projects/p/handoffs/p-handoff1-live.md"
	archived := "projects/p/handoffs/archive/2026/p-handoff2-old.md"
	write(live, "---\nid: p-handoff1\ntitle: Live handoff\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n# Live continuation\n\nneedle live\n")
	write(archived, "---\nid: p-handoff2\ntitle: Archived handoff\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n# Archived continuation\n\nneedle archive\n")
	if err := e.srv.index().Reload(live, archived); err != nil {
		t.Fatal(err)
	}
	code, page, _ := e.do("GET", "/api/docs/projects/p/handoffs/p-handoff1-live", nil)
	if code != 200 || page["kind"] != "handoff" || !strings.Contains(page["html"].(string), "Live continuation") || strings.Contains(page["html"].(string), "created:") {
		t.Fatalf("handoff page: %d %#v", code, page)
	}
	code, _, raw := e.do("GET", "/api/search?q=needle+archive", nil)
	if code != 200 || strings.Contains(string(raw), "p-handoff2") {
		t.Fatalf("default archived search: %d %s", code, raw)
	}
	code, _, raw = e.do("GET", "/api/search?q=needle+archive&include_archived_handoffs=true", nil)
	if code != 200 || !strings.Contains(string(raw), "p-handoff2") {
		t.Fatalf("explicit archived search: %d %s", code, raw)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@x", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@x")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	remote := filepath.Join(t.TempDir(), "hub.git")
	runGit(t, t.TempDir(), "init", "-q", "--bare", "-b", "main", remote)
	home := t.TempDir()
	dir := filepath.Join(home, "hub")
	branch, err := gitops.Clone(context.Background(), gitops.ExecRunner{}, remote, dir)
	if err != nil {
		t.Fatal(err)
	}
	hub := &gitops.Hub{Dir: dir, CacheDir: filepath.Join(home, "cache"), Branch: branch, Throttle: time.Minute, Actor: "tester", Stderr: io.Discard}
	if _, err := vault.CreateProjectFiles(dir, "p", ""); err != nil {
		t.Fatal(err)
	}
	write := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("projects/p/issues/p-aaaa-first.md", "---\nid: p-aaaa\naliases: [p-aaaa]\ntitle: First\ntype: task\nstatus: open\npriority: 1\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\nSee [[guide]] and ![[img.png]].\n\n## Log\n- 2026-01-01T00:00:00Z t: created\n")
	write("projects/p/issues/p-bbbb-second.md", "---\nid: p-bbbb\naliases: [p-bbbb]\ntitle: Second\ntype: task\nstatus: open\npriority: 2\nblocked_by:\n  - \"[[p-aaaa-first]]\"\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n")
	write("projects/p/requests/p-r-a3f2-first-request.md", "---\nid: p-r-a3f2\naliases: [p-r-a3f2]\ntitle: First request\nstatus: open\npriority: 2\nrequested_by: tester\nissues:\n  - \"[[p-aaaa]]\"\n  - \"[[missing-a1b2]]\"\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\nRequest body with [[p-bbbb]].\n\n## Log\n- 2026-01-01T00:00:00Z t: created\n")
	write("projects/p/docs/guide.md", "# Guide\n\nLinks to [[p-aaaa-first]] and [[missing-page]].\n\n## Section\n\ntext\n")
	write("projects/p/docs/img.png", "PNG")
	write("projects/p/plans/p-plan-a3f2-add/plan.md", "---\nid: p-plan-a3f2\naliases: [p-plan-a3f2]\ntitle: Add plan support\nslug: add\nstatus: draft\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\nsections:\n  - sections/context.md\n---\n## Summary\n\n### Outcome\n\n<!-- bn:todo -->\n\n### Affected areas\n\n<!-- bn:todo -->\n\n### Execution order\n\n<!-- bn:todo -->\n\n### Risks\n\n<!-- bn:todo -->\n\n### Change graph\n\n```bn-change-graph\nversion: 1\nnodes: []\nedges: []\n```\n")
	write("projects/p/plans/p-plan-a3f2-add/sections/context.md", "# Context\n\nPlan details.\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-qm", "seed")
	runGit(t, dir, "push", "-q", "origin", "HEAD:main")
	ix, err := vault.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := ops.Env{HubDir: dir, Project: "p", Actor: "tester", Types: issue.DefaultTypesConfig(), IDLength: 4,
		WorkflowFor: func(string) issue.WorkflowConfig { return issue.DefaultWorkflowConfig() }}
	ui := fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}, "assets/app.js": {Data: []byte("js")}}
	srv := New(Config{Hub: hub, Index: ix, HubDir: dir, Project: "p", Env: e, Prefix: func(string) string { return "p" }, UI: ui, LogOutput: io.Discard, NoFetch: true})
	return &env{t: t, srv: srv, hub: dir}
}

func (e *env) do(method, path string, body any) (int, map[string]any, []byte) {
	e.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := e.srv.App().Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	if err != nil {
		e.t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp.StatusCode, m, raw
}

func TestReadsAndMutations(t *testing.T) {
	e := newEnv(t)
	code, _, raw := e.do("GET", "/api/projects", nil)
	if code != 200 || !strings.Contains(string(raw), `"name":"p"`) || !strings.Contains(string(raw), `"open":2`) {
		t.Fatalf("projects: %d %s", code, raw)
	}
	code, _, raw = e.do("GET", "/api/projects/p/ready", nil)
	if code != 200 || strings.Count(string(raw), `"id":"p-`) != 1 || !strings.Contains(string(raw), `"id":"p-aaaa"`) {
		t.Fatalf("ready: %d %s", code, raw)
	}
	code, m, _ := e.do("GET", "/api/issues/p-aaaa", nil)
	if code != 200 || !strings.Contains(m["html"].(string), `class="wikilink" href="/wiki/projects/p/docs/guide"`) || !strings.Contains(m["html"].(string), `src="/api/assets/projects/p/docs/img.png"`) {
		t.Fatalf("show: %d %v", code, m["html"])
	}
	if wf := m["workflow"].(map[string]any); len(wf["statuses"].([]any)) == 0 {
		t.Fatal("workflow missing")
	}
	code, m, _ = e.do("POST", "/api/projects/p/issues", map[string]any{"title": "Third", "priority": 0, "blocked_by": []string{"p-bbbb"}})
	if code != 200 || m["id"] == nil || m["pushed"] != true {
		t.Fatalf("create: %d %v", code, m)
	}
	id := m["id"].(string)
	code, m, _ = e.do("GET", "/api/issues/"+id, nil)
	if code != 200 || m["title"] != "Third" || m["blocked_by"].([]any)[0] != "p-bbbb" {
		t.Fatalf("fetch after create: %d %v", code, m)
	}
	code, _, _ = e.do("PATCH", "/api/issues/"+id, map[string]any{"status": "in_progress", "add_labels": []string{"x"}})
	if code != 200 {
		t.Fatalf("patch: %d", code)
	}
	code, _, _ = e.do("POST", "/api/issues/"+id+"/notes", map[string]any{"text": "hello"})
	if code != 200 {
		t.Fatalf("note: %d", code)
	}
	code, m, _ = e.do("POST", "/api/issues/p-aaaa/close", map[string]any{"reason": "done"})
	if code != 200 || m["pushed"] != true {
		t.Fatalf("close: %d %v", code, m)
	}
	code, _, raw = e.do("GET", "/api/projects/p/ready", nil)
	if code != 200 || !strings.Contains(string(raw), `"id":"p-bbbb"`) {
		t.Fatalf("ready after close: %s", raw)
	}
	code, _, _ = e.do("POST", "/api/issues/p-aaaa/reopen", nil)
	if code != 200 {
		t.Fatalf("reopen: %d", code)
	}
	code, m, _ = e.do("POST", "/api/issues/p-aaaa/deps", map[string]any{"target": "p-bbbb", "type": "blocks"})
	if code != 409 || m["error"].(map[string]any)["code"] != "conflict" {
		t.Fatalf("cycle: %d %v", code, m)
	}
	code, _, _ = e.do("DELETE", "/api/issues/p-bbbb/deps/p-aaaa", nil)
	if code != 200 {
		t.Fatalf("dep remove: %d", code)
	}
	code, m, _ = e.do("GET", "/api/issues/nope", nil)
	if code != 404 || m["error"].(map[string]any)["code"] != "not_found" {
		t.Fatalf("404: %d %v", code, m)
	}
	code, _, _ = e.do("PATCH", "/api/issues/nope", map[string]any{"status": "open"})
	if code != 404 {
		t.Fatalf("patch unknown: %d", code)
	}
	code, _, _ = e.do("POST", "/api/projects/p/issues", map[string]any{"title": ""})
	if code != 400 {
		t.Fatalf("empty title: %d", code)
	}
}

func TestPlanReadEndpointsAreGetOnly(t *testing.T) {
	e := newEnv(t)
	code, _, raw := e.do("GET", "/api/projects/p/plans", nil)
	if code != 200 || !strings.Contains(string(raw), `"id":"p-plan-a3f2"`) {
		t.Fatalf("plan list: %d %s", code, raw)
	}
	code, m, _ := e.do("GET", "/api/plans/p-plan-a3f2", nil)
	if code != 200 || m["summary"] == nil || len(m["sections"].([]any)) != 1 {
		t.Fatalf("plan detail: %d %v", code, m)
	}
	code, _, _ = e.do("POST", "/api/plans/p-plan-a3f2", map[string]any{})
	if code != 405 {
		t.Fatalf("plan mutation status = %d, want 405", code)
	}
}

func TestDocsGraphSearchAssetsAndSPA(t *testing.T) {
	e := newEnv(t)
	code, m, _ := e.do("GET", "/api/docs/tree?project=p", nil)
	if code != 200 || len(m["docs"].([]any)) != 1 {
		t.Fatalf("tree: %d %v", code, m)
	}
	code, m, _ = e.do("GET", "/api/docs/projects/p/docs/guide", nil)
	if code != 200 || m["title"] != "Guide" || !strings.Contains(m["html"].(string), "is-unresolved") || len(m["toc"].([]any)) != 2 {
		t.Fatalf("doc: %d %v", code, m)
	}
	if bl := m["backlinks"].([]any); len(bl) != 1 || bl[0].(map[string]any)["from"] != "p-aaaa" || bl[0].(map[string]any)["note_kind"] != "issue" {
		t.Fatalf("backlinks: %v", m["backlinks"])
	}
	code, _, raw := e.do("GET", "/api/graph?project=p", nil)
	if code != 200 || !strings.Contains(string(raw), `"kind":"blocks"`) {
		t.Fatalf("graph: %d %s", code, raw)
	}
	code, _, raw = e.do("GET", "/api/search?q=first", nil)
	if code != 200 || !strings.Contains(string(raw), `"id":"p-aaaa"`) {
		t.Fatalf("search: %d %s", code, raw)
	}
	code, _, raw = e.do("GET", "/api/assets/projects/p/docs/img.png", nil)
	if code != 200 || string(raw) != "PNG" {
		t.Fatalf("asset: %d %q", code, raw)
	}
	for _, p := range []string{"/api/assets/../go.mod", "/api/assets/projects/p/docs/../../../beans.toml"} {
		if code, _, _ := e.do("GET", p, nil); code != 400 && code != 404 {
			t.Fatalf("traversal %s: %d", p, code)
		}
	}
	if code, _, _ := e.do("GET", "/api/assets/projects/p/issues/p-aaaa-first.md", nil); code != 404 {
		t.Fatalf("non-asset must be 404: %d", code)
	}
	code, _, raw = e.do("GET", "/issues/abc", nil)
	if code != 200 || string(raw) != "<html>app</html>" {
		t.Fatalf("spa fallback: %d %q", code, raw)
	}
	code, _, raw = e.do("GET", "/assets/app.js", nil)
	if code != 200 || string(raw) != "js" {
		t.Fatalf("static file: %d %q", code, raw)
	}
	if code, _, _ := e.do("GET", "/api/nope", nil); code != 404 {
		t.Fatalf("api 404: %d", code)
	}
	if code, _, _ := e.do("GET", "/missing.css", nil); code != 404 {
		t.Fatalf("missing file: %d", code)
	}
	if code, _, _ := e.do("GET", "/api/health", nil); code != 200 {
		t.Fatalf("health: %d", code)
	}
}

func TestRequestReadAPIAndRelationships(t *testing.T) {
	e := newEnv(t)
	code, _, raw := e.do("GET", "/api/projects/p/requests", nil)
	if code != 200 || !strings.Contains(string(raw), `"id":"p-r-a3f2"`) || !strings.Contains(string(raw), `"issue_count":2`) {
		t.Fatalf("request list: %d %s", code, raw)
	}
	code, m, _ := e.do("GET", "/api/requests/p-r-a3f2", nil)
	if code != 200 || len(m["issues"].([]any)) != 2 || !strings.Contains(m["html"].(string), `/issues/p-bbbb`) {
		t.Fatalf("request detail: %d %v", code, m)
	}
	issues := m["issues"].([]any)
	if !issues[1].(map[string]any)["missing"].(bool) {
		t.Fatalf("missing issue was not retained: %v", issues)
	}
	code, m, _ = e.do("GET", "/api/issues/p-aaaa", nil)
	if code != 200 || len(m["requests"].([]any)) != 1 || m["requests"].([]any)[0].(map[string]any)["id"] != "p-r-a3f2" {
		t.Fatalf("issue requests: %d %v", code, m["requests"])
	}
	for _, backlink := range m["backlinks"].([]any) {
		if backlink.(map[string]any)["kind"] == "request_issue" {
			t.Fatalf("canonical request relationship duplicated as backlink: %v", m["backlinks"])
		}
	}
	code, _, raw = e.do("GET", "/api/search?q=first%20request&kind=request", nil)
	if code != 200 || !strings.Contains(string(raw), `"kind":"request"`) {
		t.Fatalf("request search: %d %s", code, raw)
	}
	if code, _, _ = e.do("GET", "/api/docs/projects/p/requests/p-r-a3f2-first-request", nil); code != 404 {
		t.Fatalf("request must not be a docs route: %d", code)
	}
	if code, _, _ = e.do("POST", "/api/requests/p-r-a3f2", map[string]any{}); code != 405 && code != 404 {
		t.Fatalf("request mutation route must not exist: %d", code)
	}
}

func TestEventsStreamReload(t *testing.T) {
	e := newEnv(t)
	req := httptest.NewRequest("GET", "/api/events", nil)
	go func() {
		time.Sleep(200 * time.Millisecond)
		e.srv.Notify([]string{"projects/p/issues/p-aaaa-first.md"})
	}()
	resp, err := e.srv.App().Test(req, fiber.TestConfig{Timeout: 2 * time.Second, FailOnTimeout: false})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 512)
	n, _ := io.ReadAtLeast(resp.Body, buf, 20)
	body := string(buf[:n])
	if !strings.Contains(body, "event: reload") || !strings.Contains(body, "p-aaaa-first.md") {
		t.Fatalf("sse body: %q", body)
	}
}

func TestProjectNameIsValidatedAndCycleIs409NotValidation(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/api/projects/../issues", "/api/projects/Bad%20Name/issues", "/api/projects/.hidden/issues"} {
		code, m, _ := e.do("POST", p, map[string]any{"title": "pwn"})
		if code != 400 && code != 404 {
			t.Fatalf("%s: %d %v", p, code, m)
		}
	}
	if _, err := os.Stat(filepath.Join(e.hub, "issues")); err == nil {
		t.Fatal("a crafted project name created a directory in the hub root")
	}
	// A validation error whose message contains "cycle" is 400, not 409.
	status := "recycle-bin"
	code, m, _ := e.do("PATCH", "/api/issues/p-aaaa", map[string]any{"status": status})
	if code != 400 || m["error"].(map[string]any)["code"] != "validation_error" {
		t.Fatalf("recycle-bin: %d %v", code, m)
	}
}

func TestAssetWithSpaceInName(t *testing.T) {
	e := newEnv(t)
	p := filepath.Join(e.hub, "projects", "p", "docs", "my pic.png")
	if err := os.WriteFile(p, []byte("PNG2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.srv.index().ReloadAll(); err != nil {
		t.Fatal(err)
	}
	code, _, raw := e.do("GET", "/api/assets/projects/p/docs/my%20pic.png", nil)
	if code != 200 || string(raw) != "PNG2" {
		t.Fatalf("encoded asset name: %d %q", code, raw)
	}
	if code, _, _ := e.do("GET", "/api/assets/projects/p/docs/%2e%2e/%2e%2e/beans.toml", nil); code != 400 && code != 404 {
		t.Fatalf("encoded traversal: %d", code)
	}
}

func TestSSEHeartbeatReapsClosedClient(t *testing.T) {
	old := sseHeartbeat
	sseHeartbeat = 50 * time.Millisecond
	defer func() { sseHeartbeat = old }()
	e := newEnv(t)
	req := httptest.NewRequest("GET", "/api/events", nil)
	resp, err := e.srv.App().Test(req, fiber.TestConfig{Timeout: 300 * time.Millisecond, FailOnTimeout: false})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 256)
	n, _ := io.ReadAtLeast(resp.Body, buf, 20)
	if !strings.Contains(string(buf[:n]), ": ping") {
		t.Fatalf("no heartbeat: %q", buf[:n])
	}
	resp.Body.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		e.srv.sse.mu.Lock()
		n := len(e.srv.sse.clients)
		e.srv.sse.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("subscription not reaped after the client went away")
}

func TestParentChildDepsOverAPI(t *testing.T) {
	e := newEnv(t)
	// The URL id is the child; the target is the parent.
	code, _, _ := e.do("POST", "/api/issues/p-bbbb/deps", map[string]any{"target": "p-aaaa", "type": "parent-child"})
	if code != 200 {
		t.Fatalf("add parent: %d", code)
	}
	code, m, _ := e.do("GET", "/api/issues/p-aaaa", nil)
	if code != 200 {
		t.Fatal(code)
	}
	children := m["children"].([]any)
	if len(children) != 1 || children[0].(map[string]any)["id"] != "p-bbbb" || children[0].(map[string]any)["title"] != "Second" || children[0].(map[string]any)["project"] != "p" {
		t.Fatalf("children = %v", children)
	}
	_, m, _ = e.do("GET", "/api/issues/p-bbbb", nil)
	if m["parent"] != "p-aaaa" {
		t.Fatalf("parent = %v", m["parent"])
	}
	code, _, _ = e.do("DELETE", "/api/issues/p-bbbb/deps/p-aaaa?type=parent-child", nil)
	if code != 200 {
		t.Fatalf("remove parent: %d", code)
	}
	_, m, _ = e.do("GET", "/api/issues/p-bbbb", nil)
	if m["parent"] != "" {
		t.Fatalf("parent still set: %v", m["parent"])
	}
}
