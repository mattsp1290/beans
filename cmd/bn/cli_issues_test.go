package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/mattsp1290/beans/internal/ops"
)

func hubManifest(t *testing.T, root string) map[string]string {
	t.Helper()
	entries := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." || rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			return err
		}
		if d.IsDir() {
			entries[rel] = "dir"
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		entries[rel] = "file:" + string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// runOut runs bn capturing os.Stdout (commands write JSON and some tables
// there) and returns stdout, exit code, and error.
func (e *cliEnv) runOut(t *testing.T, args ...string) (string, int, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = buf.ReadFrom(r)
		close(done)
	}()
	cmdOut, code, runErr := e.run(args...)
	w.Close()
	os.Stdout = orig
	<-done
	return cmdOut + buf.String(), code, runErr
}

func (e *cliEnv) mustRun(t *testing.T, args ...string) string {
	t.Helper()
	out, code, err := e.runOut(t, args...)
	if err != nil {
		t.Fatalf("bn %s: exit %d: %v\n%s\nstderr:\n%s", strings.Join(args, " "), code, err, out, e.stderr.String())
	}
	return out
}

func (e *cliEnv) ids(t *testing.T, args ...string) []string {
	t.Helper()
	out := e.mustRun(t, append(args, "--json")...)
	var rows []issueJSON
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("decode %v: %v\n%s", args, err, out)
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids
}

func TestCreateReadyCloseRoundTrip(t *testing.T) {
	e := newCLIEnv(t)
	e.mustRun(t, "init", e.remote)
	before := strings.Count(e.git(e.remote, "log", "--format=%s", "main"), "\n")

	a := strings.TrimSpace(e.mustRun(t, "create", "a", "--silent"))
	b := strings.TrimSpace(e.mustRun(t, "create", "b", "--blocked-by", a, "--silent", "-p", "1"))
	if !strings.HasPrefix(a, "myapp-") || len(a) != len("myapp-")+4 || !strings.HasPrefix(b, "myapp-") {
		t.Fatalf("ids = %q %q", a, b)
	}
	if got := e.ids(t, "ready"); len(got) != 1 || got[0] != a {
		t.Fatalf("ready before close = %v", got)
	}
	if got := e.ids(t, "list"); len(got) != 2 {
		t.Fatalf("list = %v", got)
	}
	out := e.mustRun(t, "close", a, "-r", "x", "--suggest-next")
	if !strings.Contains(out, "closed "+a) || !strings.Contains(out, "now ready: "+b) {
		t.Fatalf("close output:\n%s", out)
	}
	if got := e.ids(t, "ready"); len(got) != 1 || got[0] != b {
		t.Fatalf("ready after close = %v", got)
	}
	if got := e.ids(t, "list", "--closed"); len(got) != 1 || got[0] != a {
		t.Fatalf("closed list = %v", got)
	}
	after := strings.Count(e.git(e.remote, "log", "--format=%s", "main"), "\n")
	if after-before != 3 {
		t.Fatalf("commits grew by %d, want 3 (create, create, close)", after-before)
	}
	log := e.git(e.remote, "log", "--format=%s", "main")
	for _, want := range []string{"bn: create " + a + " — a", "bn: create " + b + " — b", "bn: close " + a + " — x"} {
		if !strings.Contains(log, want) {
			t.Errorf("remote log missing %q:\n%s", want, log)
		}
	}
	// idempotent close
	if out := e.mustRun(t, "close", a, "-r", "again"); !strings.Contains(out, a+" already closed") {
		t.Fatalf("second close: %s", out)
	}
	// show --json carries blockers and log
	shown := e.mustRun(t, "show", b, "--json")
	var detail issueJSON
	if err := json.Unmarshal([]byte(shown), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Blockers) != 1 || detail.Blockers[0].ID != a || detail.Blockers[0].Status != "closed" || len(detail.BlockedBy) != 1 || detail.BlockedBy[0] != a || len(detail.Log) != 1 {
		t.Fatalf("show = %+v", detail)
	}
	// update, note, reopen, delete refusal and force
	e.mustRun(t, "update", b, "--claim", "--label", "x", "--note", "working")
	e.mustRun(t, "note", b, "second", "note")
	if got := e.ids(t, "list", "--status", "in_progress", "--label", "x"); len(got) != 1 {
		t.Fatalf("filtered list = %v", got)
	}
	e.mustRun(t, "reopen", a)
	if _, code, err := e.runOut(t, "delete", a); err == nil || code != exitUsage {
		t.Fatalf("delete with backlinks must refuse: %v %d", err, code)
	}
	e.mustRun(t, "delete", a, "--force")
	if _, code, err := e.runOut(t, "show", a); code != exitNotFound || err == nil {
		t.Fatalf("show deleted: code=%d err=%v", code, err)
	}
	shown = e.mustRun(t, "show", b, "--json")
	_ = json.Unmarshal([]byte(shown), &detail)
	if len(detail.BlockedBy) != 0 {
		t.Fatalf("forced delete must remove the link: %+v", detail.BlockedBy)
	}
	if out := e.mustRun(t, "doctor"); !strings.Contains(out, "healthy") {
		t.Fatalf("doctor: %s", out)
	}
}

func TestRequestCLIWorkflow(t *testing.T) {
	e := newCLIEnv(t)
	e.mustRun(t, "init", e.remote)
	issueID := strings.TrimSpace(e.mustRun(t, "create", "linked work", "--silent"))
	requestID := strings.TrimSpace(e.mustRun(t, "request", "create", "A request", "--issue", issueID, "--requested-by", "tester", "--silent"))
	if !strings.HasPrefix(requestID, "myapp-r-") {
		t.Fatalf("request id = %q", requestID)
	}
	if out := e.mustRun(t, "request", "list"); !strings.Contains(out, requestID) {
		t.Fatalf("request list:\n%s", out)
	}
	shown := e.mustRun(t, "request", "show", requestID, "--json")
	var detail map[string]any
	if err := json.Unmarshal([]byte(shown), &detail); err != nil {
		t.Fatal(err)
	}
	linked := detail["linked_issues"].([]any)
	if len(linked) != 1 || linked[0].(map[string]any)["id"] != issueID {
		t.Fatalf("request show: %#v", detail)
	}
	e.mustRun(t, "request", "update", requestID, "--status", "accepted")
	e.mustRun(t, "request", "update", requestID, "--status", "in_progress")
	e.mustRun(t, "request", "update", requestID, "--status", "resolved")
	if out := e.mustRun(t, "request", "list"); strings.Contains(out, requestID) {
		t.Fatalf("terminal request was listed by default:\n%s", out)
	}
	if out := e.mustRun(t, "request", "list", "--terminal"); !strings.Contains(out, requestID) {
		t.Fatalf("terminal request missing:\n%s", out)
	}
	if _, code, err := e.runOut(t, "request", "update", requestID, "--status", "open"); err == nil || code != exitUsage {
		t.Fatalf("terminal reopen without force: %d %v", code, err)
	}
	e.mustRun(t, "request", "update", requestID, "--status", "open", "--force")
	e.mustRun(t, "request", "unlink", requestID, issueID)
	if out := e.mustRun(t, "request", "show", requestID); strings.Contains(out, "linked issues:") {
		t.Fatalf("unlink did not remove issue:\n%s", out)
	}
}

func TestDepCyclesAndTree(t *testing.T) {
	e := newCLIEnv(t)
	e.mustRun(t, "init", e.remote)
	a := strings.TrimSpace(e.mustRun(t, "create", "a", "--silent"))
	b := strings.TrimSpace(e.mustRun(t, "create", "b", "--silent"))
	e.mustRun(t, "dep", "add", b, a)
	if _, code, err := e.runOut(t, "dep", "add", a, b); err == nil || !strings.Contains(err.Error(), "cycle") || code != exitUsage {
		t.Fatalf("cycle must be refused: %v", err)
	}
	if out := e.mustRun(t, "dep", "cycles"); !strings.Contains(out, "no cycles") {
		t.Fatalf("cycles: %s", out)
	}
	tree := e.mustRun(t, "dep", "tree", b)
	if !strings.Contains(tree, b) || !strings.Contains(tree, "  "+a) {
		t.Fatalf("tree:\n%s", tree)
	}
	e.mustRun(t, "dep", "add", b, a, "-t", "parent-child")
	if got := e.ids(t, "children", a); len(got) != 1 || got[0] != b {
		t.Fatalf("children = %v", got)
	}
	e.mustRun(t, "dep", "remove", b, a)
	if got := e.ids(t, "blocked"); len(got) != 0 {
		t.Fatalf("blocked = %v", got)
	}
}

func TestMemoriesDocsSearchArchive(t *testing.T) {
	e := newCLIEnv(t)
	e.mustRun(t, "init", e.remote)
	e.mustRun(t, "remember", "the deploy host is 10.0.0.106", "--type", "reference", "--tag", "deploy")
	e.mustRun(t, "remember", "hub wide fact", "--global", "--key", "hub-fact")
	out := e.mustRun(t, "memories", "deploy")
	if !strings.Contains(out, "the-deploy-host-is-10-0") || strings.Contains(out, "hub-fact") {
		t.Fatalf("memories:\n%s", out)
	}
	if out := e.mustRun(t, "memories", "--all"); !strings.Contains(out, "hub-fact") {
		t.Fatalf("memories --all:\n%s", out)
	}
	e.mustRun(t, "forget", "hub-fact", "--global")
	e.mustRun(t, "doc", "new", "design/overview")
	if out := e.mustRun(t, "doc", "list"); !strings.Contains(out, "projects/myapp/docs/design/overview.md") {
		t.Fatalf("doc list:\n%s", out)
	}
	if out := e.mustRun(t, "search", "deploy"); !strings.Contains(out, "memory") {
		t.Fatalf("search:\n%s", out)
	}
	id := strings.TrimSpace(e.mustRun(t, "create", "old", "--silent"))
	e.mustRun(t, "close", id, "-r", "done")
	if out := e.mustRun(t, "archive", "--older-than", "0s", "--dry-run"); !strings.Contains(out, "would archive 1") {
		t.Fatalf("archive dry run: %s", out)
	}
	e.mustRun(t, "archive", "--older-than", "0s")
	if got := e.ids(t, "list", "--closed"); len(got) != 1 {
		t.Fatalf("archived issue missing from --closed: %v", got)
	}
	if got := e.ids(t, "list", "--archived", "--status", "closed"); len(got) != 1 {
		t.Fatalf("archived issue missing from --archived: %v", got)
	}
}

func TestImportBDFromLiveExport(t *testing.T) {
	e := newCLIEnv(t)
	e.mustRun(t, "init", e.remote)
	dry := e.mustRun(t, "import", "bd", "testdata/beans_export_2026-09-10.jsonl", "--project", "beans", "--dry-run")
	if !strings.Contains(dry, "issues: 178 (157 archived)  memories: 2  blocks: 263  parents: 28") || !strings.Contains(dry, "dry run") {
		t.Fatalf("dry run report:\n%s", dry)
	}
	e.mustRun(t, "import", "bd", "testdata/beans_export_2026-09-10.jsonl", "--project", "beans")
	if got := e.ids(t, "list", "--all-projects", "--archived", "-n", "0"); len(got) != 178 {
		t.Fatalf("all issues = %d", len(got))
	}
	if got := e.ids(t, "list", "--all-projects", "--closed", "-n", "0"); len(got) != 157 {
		t.Fatalf("closed = %d", len(got))
	}
	if got := e.ids(t, "list", "--all-projects", "-n", "0"); len(got) != 21 {
		t.Fatalf("open = %d", len(got))
	}
	dotted := 0
	for _, id := range e.ids(t, "list", "--all-projects", "--archived", "-n", "0") {
		if strings.Contains(id, ".") {
			dotted++
		}
	}
	if dotted != 21 {
		t.Fatalf("dotted ids = %d", dotted)
	}
	if out := e.mustRun(t, "dep", "cycles", "--project", "beans"); !strings.Contains(out, "no cycles") {
		t.Fatalf("cycles: %s", out)
	}
	if out := e.mustRun(t, "doctor", "--all-projects"); !strings.Contains(out, "healthy") {
		t.Fatalf("doctor:\n%s", out)
	}
	if out := e.mustRun(t, "memories", "--all"); !strings.Contains(out, "bean-counter-prod-10-0-0-106-beans") {
		t.Fatalf("memories:\n%s", out)
	}
	log := e.git(e.remote, "log", "-1", "--format=%s", "main")
	if !strings.HasPrefix(log, "bn: import bd (178 issues, 2 memories) — beans") {
		t.Fatalf("commit subject: %s", log)
	}
	if _, code, err := e.runOut(t, "import", "bd", "testdata/beans_export_2026-09-10.jsonl", "--project", "beans"); err == nil || code != exitUsage || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("re-import must refuse: %v", err)
	}
}

func TestCLIImportBDReportsUnmappedDependencyTypes(t *testing.T) {
	e := newCLIEnv(t)
	e.mustRun(t, "init", e.remote)
	hub := filepath.Join(e.home, "hub")
	assertUnchanged := func(before map[string]string, head, status, remoteHead string) {
		t.Helper()
		if got := hubManifest(t, hub); !reflect.DeepEqual(got, before) {
			t.Fatalf("hub changed: got=%v want=%v", got, before)
		}
		if got := strings.TrimSpace(e.git(hub, "rev-parse", "HEAD")); got != head {
			t.Fatalf("hub HEAD changed: got=%s want=%s", got, head)
		}
		if got := e.git(hub, "status", "--porcelain=v1", "--untracked-files=all"); got != status {
			t.Fatalf("hub status changed: got=%q want=%q", got, status)
		}
		if got := strings.TrimSpace(e.git(e.remote, "rev-parse", "main")); got != remoteHead {
			t.Fatalf("remote HEAD changed: got=%s want=%s", got, remoteHead)
		}
	}
	before := hubManifest(t, hub)
	head := strings.TrimSpace(e.git(hub, "rev-parse", "HEAD"))
	status := e.git(hub, "status", "--porcelain=v1", "--untracked-files=all")
	remoteHead := strings.TrimSpace(e.git(e.remote, "rev-parse", "main"))
	args := []string{"import", "bd", "testdata/import_bd_unmapped_dependencies.jsonl", "--project", "myapp", "--dry-run"}
	out, code, err := e.runOut(t, args...)
	if err != nil || code != 0 || !strings.Contains(out, `warning: dropped 1 "discovered-from" edge(s); not mapped by bn import bd`) || !strings.Contains(out, "issues: 2 (0 archived)  memories: 0  blocks: 1  parents: 0") || !strings.Contains(out, "dry run: nothing written") {
		t.Fatalf("text import: code=%d err=%v out=%s", code, err, out)
	}
	assertUnchanged(before, head, status, remoteHead)

	out, code, err = e.runOut(t, append(args, "--json")...)
	if err != nil || code != 0 {
		t.Fatalf("json import: code=%d err=%v out=%s", code, err, out)
	}
	var result struct {
		Report ops.ImportReport `json:"report"`
		DryRun bool             `json:"dry_run"`
		Commit string           `json:"commit"`
		Pushed bool             `json:"pushed"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("complete JSON output: %v\n%s", err, out)
	}
	if !result.DryRun || result.Commit != "" || result.Pushed || result.Report.Blocks != 1 || !reflect.DeepEqual(result.Report.Warnings, []string{`dropped 1 "discovered-from" edge(s); not mapped by bn import bd`}) || strings.Contains(out, "warning:") || strings.Contains(out, "dry run:") {
		t.Fatalf("JSON import result=%+v out=%s", result, out)
	}
	assertUnchanged(before, head, status, remoteHead)
}

func TestPrimeMatchesDocs(t *testing.T) {
	want, err := os.ReadFile("../../docs/prime.md")
	if err != nil {
		t.Fatal(err)
	}
	e := newCLIEnv(t)
	out := e.mustRun(t, "prime")
	if out != string(want) {
		t.Fatal("bn prime output differs from docs/prime.md")
	}
	for _, sentence := range []string{
		"bn stores issues as markdown files in the hub repository at ~/.beans/hub.",
		"Every mutating command commits and pushes to that repository; you never commit hub files yourself.",
		"Reads use the local clone and fetch at most once a minute; run bn sync to force it.",
		"The project is the name of the git repository you are in",
		"Every read accepts `--json`",
		"the next bn command commits them as `bn: hand edits`",
	} {
		if !strings.Contains(out, sentence) {
			t.Errorf("prime is missing %q", sentence)
		}
	}
}

func TestExitCodes(t *testing.T) {
	e := newCLIEnv(t)
	if _, code, _ := e.runOut(t, "ready"); code != exitUsage {
		t.Errorf("no hub: code %d", code)
	}
	e.mustRun(t, "init", e.remote)
	if _, code, _ := e.runOut(t, "show", "nope-1234"); code != exitNotFound {
		t.Errorf("unknown id: code %d", code)
	}
	if _, code, _ := e.runOut(t, "close", "nope-1234", "-r", "x"); code != exitNotFound {
		t.Errorf("close unknown id: code %d", code)
	}
	if _, code, _ := e.runOut(t, "create", "x", "-p", "9"); code != exitUsage {
		t.Errorf("bad priority: code %d", code)
	}
	if _, code, _ := e.runOut(t, "update", "nope-1234"); code != exitUsage {
		t.Errorf("nothing to update: code %d", code)
	}
}

func TestCloseMultipleIDsPartialFailure(t *testing.T) {
	e := newCLIEnv(t)
	e.mustRun(t, "init", e.remote)
	a := strings.TrimSpace(e.mustRun(t, "create", "a", "--silent"))
	b := strings.TrimSpace(e.mustRun(t, "create", "b", "--silent"))
	out, code, err := e.runOut(t, "close", a, b, "nope-9999", "-r", "batch")
	if err == nil || code != exitNotFound {
		t.Fatalf("expected not-found exit, got %d %v", code, err)
	}
	if !strings.Contains(out, "closed "+a) || !strings.Contains(out, "closed "+b) {
		t.Fatalf("earlier closes must be reported:\n%s", out)
	}
	log := e.git(e.remote, "log", "--format=%s", "main")
	if !strings.Contains(log, "bn: close "+a+" — batch") || !strings.Contains(log, "bn: close "+b+" — batch") {
		t.Fatalf("earlier closes must be pushed:\n%s", log)
	}
}

func TestImportForceRerunIsNoChange(t *testing.T) {
	e := newCLIEnv(t)
	e.mustRun(t, "init", e.remote)
	e.mustRun(t, "import", "bd", "testdata/gastownhall_beads_export.jsonl", "--project", "gth")
	out := e.mustRun(t, "import", "bd", "testdata/gastownhall_beads_export.jsonl", "--project", "gth", "--force")
	if !strings.Contains(out, "no change") {
		t.Fatalf("force re-run should report no change, not crash:\n%s", out)
	}
}

// normalize replaces random ids and timestamps so text output can be
// compared with a golden.
func normalize(s string, ids ...string) string {
	for i, id := range ids {
		s = strings.ReplaceAll(s, id, "ID"+string(rune('A'+i)))
	}
	re := regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}(:\d{2}Z?)?`)
	return re.ReplaceAllString(s, "<time>")
}

func TestTextOutputGoldens(t *testing.T) {
	e := newCLIEnv(t)
	e.mustRun(t, "init", e.remote)
	a := strings.TrimSpace(e.mustRun(t, "create", "First thing", "--silent", "-p", "1", "-l", "infra", "-d", "The description."))
	b := strings.TrimSpace(e.mustRun(t, "create", "Second thing", "--silent", "--blocked-by", a))
	got := map[string]string{
		"list":     normalize(e.mustRun(t, "list"), a, b),
		"ready":    normalize(e.mustRun(t, "ready"), a, b),
		"show":     normalize(e.mustRun(t, "show", b), a, b),
		"dep-tree": normalize(e.mustRun(t, "dep", "tree", b), a, b),
	}
	for name, out := range got {
		path := "testdata/golden/" + name + ".txt"
		if *updateGoldens {
			if err := os.MkdirAll("testdata/golden", 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v (run with -update)", name, err)
		}
		if string(want) != out {
			t.Errorf("%s differs from golden:\n--- got ---\n%s\n--- want ---\n%s", name, out, want)
		}
	}
}

var updateGoldens = flag.Bool("update", false, "rewrite golden files")
