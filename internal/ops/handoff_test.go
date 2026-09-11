package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHandoffCreateMissingIssueDoesNotBootstrapProject(t *testing.T) {
	hub := t.TempDir()
	env := Env{HubDir: hub, Project: "alpha", Now: func() time.Time { return time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC) }}
	op, _ := HandoffCreate(env, HandoffCreateInput{Title: "Context", Body: "# Context\n", Issue: "alpha-nope"}, "alpha")
	if _, err := op.Apply(hub); err == nil {
		t.Fatal("missing issue unexpectedly succeeded")
	}
	if _, err := os.Stat(filepath.Join(hub, "projects", "alpha")); !os.IsNotExist(err) {
		t.Fatalf("missing issue bootstrapped project: %v", err)
	}
}

func TestHandoffCreatePreservesExistingFinalNewlines(t *testing.T) {
	hub := t.TempDir()
	env := Env{HubDir: hub, Project: "alpha", Now: func() time.Time { return time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC) }}
	op, result := HandoffCreate(env, HandoffCreateInput{Title: "Context", Body: "# Context\n\n\n"}, "alpha")
	if _, err := op.Apply(hub); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(hub, filepath.FromSlash(result.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "# Context\n\n\n") {
		t.Fatalf("trailing newlines changed: %q", data)
	}
}
