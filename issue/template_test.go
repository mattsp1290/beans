package issue

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultTemplateBuiltins(t *testing.T) {
	cases := map[string]string{
		"task":    "## Acceptance\n- [ ] \n",
		"bug":     "## Steps\n1. \n\n## Expected\n\n## Actual\n\n## Acceptance\n- [ ] \n",
		"feature": "## Motivation\n\n## Acceptance\n- [ ] \n",
		"epic":    "## Goal\n\n## Scope\n\n",
		"chore":   "",
	}
	for typ, want := range cases {
		if got := DefaultTemplate(typ); got != want {
			t.Errorf("DefaultTemplate(%q) = %q, want %q", typ, got, want)
		}
	}
}

func TestDefaultTemplateUnknownTypeFallsBackToTask(t *testing.T) {
	got := DefaultTemplate("no-such-type")
	want := DefaultTemplate("task")
	if got != want {
		t.Errorf("DefaultTemplate(unknown) = %q, want task template %q", got, want)
	}
}

func TestLoadTemplatePrefersProjectThenHubThenBuiltin(t *testing.T) {
	projectDir := t.TempDir()
	hubDir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(projectDir, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(hubDir, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}

	projectTpl := "## Project Template\n"
	hubTpl := "## Hub Template\n"
	if err := os.WriteFile(filepath.Join(projectDir, "templates", "task.md"), []byte(projectTpl), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hubDir, "templates", "task.md"), []byte(hubTpl), 0o644); err != nil {
		t.Fatal(err)
	}
	// A hub-level template for a different type, to prove the hub fallback
	// is per-type.
	if err := os.WriteFile(filepath.Join(hubDir, "templates", "bug.md"), []byte(hubTpl), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := LoadTemplate("task", projectDir, hubDir); got != projectTpl {
		t.Errorf("LoadTemplate with both dirs = %q, want project template %q", got, projectTpl)
	}
	if got := LoadTemplate("task", "", hubDir); got != hubTpl {
		t.Errorf("LoadTemplate with only hub dir = %q, want hub template %q", got, hubTpl)
	}
	if got := LoadTemplate("bug", projectDir, hubDir); got != hubTpl {
		t.Errorf("LoadTemplate(bug) = %q, want hub template %q (no project override)", got, hubTpl)
	}
	if got := LoadTemplate("task", "", ""); got != DefaultTemplate("task") {
		t.Errorf("LoadTemplate with no dirs = %q, want built-in default", got)
	}
	if got := LoadTemplate("epic", projectDir, hubDir); got != DefaultTemplate("epic") {
		t.Errorf("LoadTemplate(epic) = %q, want built-in default (no override anywhere)", got)
	}
}
