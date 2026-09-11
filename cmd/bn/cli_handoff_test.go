package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandoffCLIWorkflow(t *testing.T) {
	e := newCLIEnv(t)
	e.mustRun(t, "init", e.remote)
	issueID := strings.TrimSpace(e.mustRun(t, "create", "govern handoff", "--silent"))
	source := filepath.Join(e.repo, "handoff.md")
	body := "# Continue validation\n\nunique handoff search phrase\n\n"
	if err := os.WriteFile(source, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	handoffID := strings.TrimSpace(e.mustRun(t, "handoff", "create", "--file", source, "--issue", issueID, "--silent"))
	if !strings.HasPrefix(handoffID, "myapp-") {
		t.Fatalf("handoff id = %q", handoffID)
	}

	out := e.mustRun(t, "handoff", "list", "--json")
	var rows []handoffJSON
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != handoffID || rows[0].Issue != issueID {
		t.Fatalf("list = %+v", rows)
	}
	if got := e.mustRun(t, "search", "unique handoff search phrase"); !strings.Contains(got, "handoff") || !strings.Contains(got, handoffID) {
		t.Fatalf("search = %s", got)
	}
	if got := e.mustRun(t, "handoff", "show", handoffID, "--raw"); !strings.HasSuffix(got, body) {
		t.Fatalf("raw body missing: %q", got)
	}

	e.mustRun(t, "handoff", "archive", handoffID)
	if got := e.mustRun(t, "handoff", "list", "--json"); got != "[]\n" {
		t.Fatalf("live list after archive = %q", got)
	}
	if got := e.mustRun(t, "search", "unique handoff search phrase"); strings.Contains(got, handoffID) {
		t.Fatalf("default search exposed archive: %s", got)
	}
	if got := e.mustRun(t, "search", "unique handoff search phrase", "--include-archived-handoffs"); !strings.Contains(got, handoffID) {
		t.Fatalf("archived search = %s", got)
	}
	e.mustRun(t, "handoff", "restore", handoffID)
	if got := e.mustRun(t, "handoff", "show", handoffID, "--raw"); !strings.HasSuffix(got, body) {
		t.Fatalf("restored raw = %q", got)
	}
}
