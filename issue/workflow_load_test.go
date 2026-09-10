package issue

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadWorkflowExplicitWinsOverProjectAndHub(t *testing.T) {
	dir := t.TempDir()
	explicit := filepath.Join(dir, "explicit.toml")
	explicitTOML := `[workflow]
statuses = ["draft", "live"]
default = "draft"
active = ["draft"]
terminal = ["live"]
`
	if err := os.WriteFile(explicit, []byte(explicitTOML), 0o644); err != nil {
		t.Fatal(err)
	}

	hubTOML := []byte(`[workflow]
statuses = ["open", "closed"]
default = "open"
`)
	projectTOML := []byte(`[workflow]
default = "closed"
`)

	wf, err := LoadWorkflow(explicit, projectTOML, hubTOML)
	if err != nil {
		t.Fatal(err)
	}
	if !equalStringSlices(wf.Statuses, []string{"draft", "live"}) {
		t.Errorf("Statuses = %v, want explicit file's statuses", wf.Statuses)
	}
	if wf.Default != "draft" {
		t.Errorf("Default = %q, want %q (explicit file, ignoring project/hub)", wf.Default, "draft")
	}
}

func TestLoadWorkflowProjectOverridesHubKeyByKey(t *testing.T) {
	hubTOML := []byte(`[workflow]
statuses = ["open", "in_progress", "closed"]
default = "open"
active = ["open", "in_progress"]
terminal = ["closed"]
`)
	// Project sets only default; statuses/active/terminal are inherited
	// from hub.
	projectTOML := []byte(`[workflow]
default = "in_progress"
`)

	wf, err := LoadWorkflow("", projectTOML, hubTOML)
	if err != nil {
		t.Fatal(err)
	}
	if !equalStringSlices(wf.Statuses, []string{"open", "in_progress", "closed"}) {
		t.Errorf("Statuses = %v, want hub's statuses (unset in project)", wf.Statuses)
	}
	if wf.Default != "in_progress" {
		t.Errorf("Default = %q, want project's override %q", wf.Default, "in_progress")
	}
	if !equalStringSlices(wf.Active, []string{"open", "in_progress"}) {
		t.Errorf("Active = %v, want hub's active (unset in project)", wf.Active)
	}
}

func TestLoadWorkflowMissingExplicitPathErrors(t *testing.T) {
	_, err := LoadWorkflow(filepath.Join(t.TempDir(), "does-not-exist.toml"), nil, nil)
	if err == nil {
		t.Fatal("expected an error for a missing explicit path")
	}
}

func TestLoadWorkflowInvalidVocabularyErrors(t *testing.T) {
	hubTOML := []byte(`[workflow]
statuses = ["open", "closed"]
default = "open"
`)
	// Project sets a default that is not in the (inherited) statuses list.
	projectTOML := []byte(`[workflow]
default = "nonexistent_status"
`)

	_, err := LoadWorkflow("", projectTOML, hubTOML)
	if err == nil {
		t.Fatal("expected an error for an invalid default status")
	}
	if !strings.Contains(err.Error(), "not in statuses") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "not in statuses")
	}
}

func TestLoadWorkflowTOMLWithTypesTableLoadsFine(t *testing.T) {
	hubTOML := []byte(`[workflow]
statuses = ["open", "closed"]
default = "open"
active = ["open"]
terminal = ["closed"]

[types]
names = ["task", "bug"]

[ids]
length = 5
`)

	wf, err := LoadWorkflow("", nil, hubTOML)
	if err != nil {
		t.Fatalf("unexpected error loading TOML with [types] alongside [workflow]: %v", err)
	}
	if !equalStringSlices(wf.Statuses, []string{"open", "closed"}) {
		t.Errorf("Statuses = %v", wf.Statuses)
	}
}

func TestLoadWorkflowNoConfigUsesDefaults(t *testing.T) {
	wf, err := LoadWorkflow("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := DefaultWorkflowConfig()
	if !equalStringSlices(wf.Statuses, want.Statuses) || wf.Default != want.Default {
		t.Errorf("LoadWorkflow with no config = %+v, want defaults %+v", wf, want)
	}
}
