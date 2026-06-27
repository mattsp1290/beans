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
	t.Setenv(workflowDefaultEnv, "")
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

func TestLoadWorkflowConfigDefaultEnvOverrideWins(t *testing.T) {
	isolateWorkflowEnv(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "bn.toml")
	if err := os.WriteFile(p, []byte(`
[workflow]
statuses = ["open", "triage", "closed"]
default = "open"
active = ["open"]
terminal = ["closed"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(workflowEnv, p)
	t.Setenv(workflowDefaultEnv, "triage")

	wf, err := loadWorkflowConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if wf.DefaultState() != "triage" {
		t.Errorf("%s override = %q, want triage", workflowDefaultEnv, wf.DefaultState())
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

func TestLoadWorkflowConfigInvalidDefaultOverrideFailsFast(t *testing.T) {
	isolateWorkflowEnv(t)
	t.Setenv(workflowDefaultEnv, "ghost")

	if _, err := loadWorkflowConfig(); err == nil {
		t.Fatalf("expected validation error for invalid %s", workflowDefaultEnv)
	}
}

func TestLoadWorkflowConfigInvalidBucketFailsFast(t *testing.T) {
	isolateWorkflowEnv(t)
	p := filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(p, []byte(`
[workflow]
statuses = ["open", "closed"]
default = "open"
active = ["ghost"]
terminal = ["closed"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(workflowEnv, p)

	if _, err := loadWorkflowConfig(); err == nil {
		t.Fatal("expected validation error for active status not in statuses")
	}
}

func TestLoadWorkflowConfigUnknownKeysFailFast(t *testing.T) {
	tests := []struct {
		name string
		file string
		body string
	}{
		{
			name: "toml",
			file: "bad.toml",
			body: "[workflow]\ndefualt = \"open\"\n",
		},
		{
			name: "yaml",
			file: "bad.yaml",
			body: "workflow:\n  defualt: open\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateWorkflowEnv(t)
			p := filepath.Join(t.TempDir(), tt.file)
			if err := os.WriteFile(p, []byte(tt.body), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Setenv(workflowEnv, p)

			if _, err := loadWorkflowConfig(); err == nil {
				t.Fatal("expected error for unknown workflow config key")
			}
		})
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

func TestLoadWorkflowConfigDiscoveredReadErrorFailsFast(t *testing.T) {
	isolateWorkflowEnv(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(cwd, "bn.toml")
	if err := os.WriteFile(p, []byte("[workflow]\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(p, 0o644)
	})
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(p); err == nil {
		t.Skip("test environment can read chmod 000 files")
	}

	if _, err := loadWorkflowConfig(); err == nil {
		t.Fatal("expected read error for discovered unreadable config")
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

func TestResolveWorkflowConfigPathFindsCwdFile(t *testing.T) {
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

func TestResolveWorkflowConfigPathCwdWinsOverMarker(t *testing.T) {
	isolateWorkflowEnv(t)
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	markerCfg := filepath.Join(parent, "bn.toml")
	if err := os.WriteFile(filepath.Join(parent, activeProjectMarker), []byte("project=marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markerCfg, []byte("[workflow]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwdCfg := filepath.Join(child, "bn.yaml")
	if err := os.WriteFile(cwdCfg, []byte("workflow:\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(child)

	path, isExplicit, err := resolveWorkflowConfigPath()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if isExplicit || path != cwdCfg {
		t.Errorf("resolve = (%q, explicit=%v), want (%q, false)", path, isExplicit, cwdCfg)
	}
}

func TestResolveWorkflowConfigPathFindsMarkerConfig(t *testing.T) {
	isolateWorkflowEnv(t)
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, activeProjectMarker), []byte("project=marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(parent, "bn.toml")
	if err := os.WriteFile(p, []byte("[workflow]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(child)

	path, isExplicit, err := resolveWorkflowConfigPath()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if isExplicit || path != p {
		t.Errorf("resolve = (%q, explicit=%v), want (%q, false)", path, isExplicit, p)
	}
}

func TestResolveWorkflowConfigPathSkipsParentConfigWithoutMarker(t *testing.T) {
	isolateWorkflowEnv(t)
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "bn.toml"), []byte("[workflow]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(child)

	path, isExplicit, err := resolveWorkflowConfigPath()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if isExplicit || path != "" {
		t.Errorf("resolve = (%q, explicit=%v), want no config", path, isExplicit)
	}
}

func TestResolveWorkflowConfigPathMarkerWinsOverXDG(t *testing.T) {
	isolateWorkflowEnv(t)
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	xdg := t.TempDir()
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, activeProjectMarker), []byte("project=marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	markerCfg := filepath.Join(parent, "bn.toml")
	if err := os.WriteFile(markerCfg, []byte("[workflow]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	xdgDir := filepath.Join(xdg, "bn")
	if err := os.MkdirAll(xdgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdgDir, "config.toml"), []byte("[workflow]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Chdir(child)

	path, isExplicit, err := resolveWorkflowConfigPath()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if isExplicit || path != markerCfg {
		t.Errorf("resolve = (%q, explicit=%v), want (%q, false)", path, isExplicit, markerCfg)
	}
}
