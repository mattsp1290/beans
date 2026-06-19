package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mattsp1290/beans/model"
)

// isolateWorkflowEnv points discovery at empty/explicit locations so a stray
// bn.toml in a parent directory cannot influence loader tests.
func isolateWorkflowEnv(t *testing.T) {
	t.Helper()
	t.Setenv(workflowEnv, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Chdir(t.TempDir())
}

func TestLoadWorkflowConfigTOMLAndYAML(t *testing.T) {
	isolateWorkflowEnv(t)
	dir := t.TempDir()

	tomlPath := filepath.Join(dir, "bn.toml")
	if err := os.WriteFile(tomlPath, []byte(`
[workflow]
statuses = ["open", "in_progress", "qa", "closed"]
default = "open"
active = ["open"]
terminal = ["closed"]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	yamlPath := filepath.Join(dir, "wf.yaml")
	if err := os.WriteFile(yamlPath, []byte(`
workflow:
  statuses: [open, in_progress, qa, closed]
  default: open
  active: [open]
  terminal: [closed]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv(workflowEnv, tomlPath)
	fromTOML, err := loadWorkflowConfig()
	if err != nil {
		t.Fatalf("load TOML: %v", err)
	}

	t.Setenv(workflowEnv, yamlPath)
	fromYAML, err := loadWorkflowConfig()
	if err != nil {
		t.Fatalf("load YAML: %v", err)
	}

	if len(fromTOML.Statuses) != 4 || !fromTOML.IsValid("qa") {
		t.Errorf("TOML config wrong: %+v", fromTOML)
	}
	if !fromTOML.IsTerminal("closed") || fromTOML.IsValid("done") {
		t.Errorf("TOML buckets wrong: %+v", fromTOML)
	}
	if len(fromYAML.Statuses) != len(fromTOML.Statuses) || !fromYAML.IsValid("qa") {
		t.Errorf("YAML config should match TOML: %+v vs %+v", fromYAML, fromTOML)
	}
}

func TestLoadWorkflowConfigPartialMergeInheritsDefaults(t *testing.T) {
	isolateWorkflowEnv(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "bn.toml")
	// Only override the default status; everything else should inherit the
	// built-in default vocabulary (which still includes ready_for_*).
	if err := os.WriteFile(p, []byte("[workflow]\ndefault = \"in_progress\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(workflowEnv, p)

	wf, err := loadWorkflowConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if wf.DefaultState() != "in_progress" {
		t.Errorf("default = %q, want in_progress", wf.DefaultState())
	}
	if !wf.IsValid("ready_for_review") || !wf.IsTerminal("done") {
		t.Errorf("partial config should inherit default vocabulary: %+v", wf)
	}
}

func TestLoadWorkflowConfigNoFileReturnsDefault(t *testing.T) {
	isolateWorkflowEnv(t)
	wf, err := loadWorkflowConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	def := model.DefaultWorkflowConfig()
	if len(wf.Statuses) != len(def.Statuses) || !wf.IsValid("ready_for_merge") {
		t.Errorf("no-file load should return default config, got %+v", wf)
	}
}

func TestLoadWorkflowConfigInvalidFailsFast(t *testing.T) {
	isolateWorkflowEnv(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(p, []byte("workflow:\n  statuses: [open, closed]\n  default: ghost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(workflowEnv, p)

	if _, err := loadWorkflowConfig(); err == nil {
		t.Fatal("expected validation error for default not in statuses")
	}
}

func TestLoadWorkflowConfigUnsupportedExtension(t *testing.T) {
	isolateWorkflowEnv(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "wf.json")
	if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(workflowEnv, p)

	if _, err := loadWorkflowConfig(); err == nil {
		t.Fatal("expected error for unsupported extension")
	}
}

func TestLoadWorkflowConfigMissingExplicitPathErrors(t *testing.T) {
	isolateWorkflowEnv(t)
	t.Setenv(workflowEnv, filepath.Join(t.TempDir(), "does-not-exist.toml"))
	if _, err := loadWorkflowConfig(); err == nil {
		t.Fatal("expected error for missing BN_CONFIG path")
	}
}

func TestResolveWorkflowConfigPathBNConfigWins(t *testing.T) {
	isolateWorkflowEnv(t)
	// Place a bn.toml in cwd, but BN_CONFIG should take precedence.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cwdCfg := filepath.Join(cwd, "bn.toml")
	if err := os.WriteFile(cwdCfg, []byte("[workflow]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	explicit := filepath.Join(t.TempDir(), "explicit.toml")
	if err := os.WriteFile(explicit, []byte("[workflow]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(workflowEnv, explicit)

	path, isExplicit, err := resolveWorkflowConfigPath()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !isExplicit || path != explicit {
		t.Errorf("resolve = (%q, explicit=%v), want (%q, true)", path, isExplicit, explicit)
	}
}

func TestResolveWorkflowConfigPathWalksUpForCwdFile(t *testing.T) {
	isolateWorkflowEnv(t) // sets cwd to a temp dir and clears BN_CONFIG
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(cwd, "bn.yaml")
	if err := os.WriteFile(p, []byte("workflow:\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, isExplicit, err := resolveWorkflowConfigPath()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if isExplicit || path != p {
		t.Errorf("resolve = (%q, explicit=%v), want (%q, false)", path, isExplicit, p)
	}
}
