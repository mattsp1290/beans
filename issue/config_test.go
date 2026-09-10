package issue

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadHubConfigMissingPathReturnsDefaults(t *testing.T) {
	cfg, err := LoadHubConfig(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IDs.Length != 4 {
		t.Errorf("IDs.Length = %d, want 4", cfg.IDs.Length)
	}
	want := []string{"task", "bug", "feature", "epic", "chore"}
	if !equalStringSlices(cfg.Types.Names, want) {
		t.Errorf("Types.Names = %v, want %v", cfg.Types.Names, want)
	}
}

func TestLoadHubConfigMalformedTOMLNamesFileAndLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "beans.toml")
	bad := "[workflow]\nstatuses = [\"open\"\ndefault = \"open\"\n"
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadHubConfig(path)
	if err == nil {
		t.Fatal("expected an error for malformed TOML")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error = %q, want it to name the file %q", err.Error(), path)
	}
	if !strings.Contains(err.Error(), "line") {
		t.Errorf("error = %q, want it to name a line", err.Error())
	}
}

func TestLoadProjectConfigReadsNamePrefixRemotes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "beans.toml")
	data := `name = "exampleA"
prefix = "exampleA"
remotes = ["https://github.com/mattsp1290/exampleA"]
`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadProjectConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "exampleA" || cfg.Prefix != "exampleA" {
		t.Errorf("cfg = %+v", cfg)
	}
	if !equalStringSlices(cfg.Remotes, []string{"https://github.com/mattsp1290/exampleA"}) {
		t.Errorf("Remotes = %v", cfg.Remotes)
	}
}

func TestEncodeProjectConfigRoundTrip(t *testing.T) {
	cfg := ProjectConfig{
		Name:    "beanCounter",
		Prefix:  "bean-counter",
		Remotes: []string{"https://github.com/mattsp1290/beans", "git@github.com:mattsp1290/beans.git"},
		Workflow: WorkflowFile{
			Statuses: []string{"open", "closed"},
			Default:  "open",
		},
	}
	data, err := EncodeProjectConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "beans.toml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	back, err := LoadProjectConfig(path)
	if err != nil {
		t.Fatalf("LoadProjectConfig of encoded output failed: %v\ndata:\n%s", err, data)
	}
	if back.Name != cfg.Name || back.Prefix != cfg.Prefix {
		t.Errorf("round trip name/prefix = %+v, want %+v", back, cfg)
	}
	if !equalStringSlices(back.Remotes, cfg.Remotes) {
		t.Errorf("round trip remotes = %v, want %v", back.Remotes, cfg.Remotes)
	}
	if !equalStringSlices(back.Workflow.Statuses, cfg.Workflow.Statuses) || back.Workflow.Default != cfg.Workflow.Default {
		t.Errorf("round trip workflow = %+v, want %+v", back.Workflow, cfg.Workflow)
	}
}

func TestEncodeProjectConfigWithoutWorkflow(t *testing.T) {
	cfg := ProjectConfig{Name: "p", Prefix: "p", Remotes: nil}
	data, err := EncodeProjectConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "[workflow]") {
		t.Errorf("expected no [workflow] table for an empty WorkflowFile, got:\n%s", data)
	}
}

func TestUserConfigThrottleDuration(t *testing.T) {
	var cfg UserConfig
	if got := cfg.ThrottleDuration(); got != DefaultFetchThrottle {
		t.Errorf("zero-value ThrottleDuration = %v, want default %v", got, DefaultFetchThrottle)
	}
	if DefaultFetchThrottle != 60*time.Second {
		t.Errorf("DefaultFetchThrottle = %v, want 60s", DefaultFetchThrottle)
	}

	cfg.Fetch.Throttle = "5m"
	if got := cfg.ThrottleDuration(); got != 5*time.Minute {
		t.Errorf("ThrottleDuration(%q) = %v, want 5m", cfg.Fetch.Throttle, got)
	}

	cfg.Fetch.Throttle = "not-a-duration"
	if got := cfg.ThrottleDuration(); got != DefaultFetchThrottle {
		t.Errorf("invalid throttle should fall back to default, got %v", got)
	}
}

func TestLoadUserConfigMissingReturnsDefaults(t *testing.T) {
	cfg, err := LoadUserConfig(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ThrottleDuration() != DefaultFetchThrottle {
		t.Errorf("ThrottleDuration = %v, want default", cfg.ThrottleDuration())
	}
	if cfg.Actor != "" {
		t.Errorf("Actor = %q, want empty", cfg.Actor)
	}
}

func TestEncodeUserConfigDecodesBack(t *testing.T) {
	cfg := UserConfig{
		Actor: "matt",
		Hub:   UserHubConfig{Remote: "git@github.com:owner/beans-hub.git", Branch: "main"},
		Fetch: UserFetchConfig{Throttle: "90s"},
	}
	data, err := EncodeUserConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	back, err := LoadUserConfig(path)
	if err != nil {
		t.Fatalf("LoadUserConfig of encoded output failed: %v\ndata:\n%s", err, data)
	}
	if back.Actor != cfg.Actor {
		t.Errorf("Actor = %q, want %q", back.Actor, cfg.Actor)
	}
	if back.Hub != cfg.Hub {
		t.Errorf("Hub = %+v, want %+v", back.Hub, cfg.Hub)
	}
	if back.Fetch.Throttle != cfg.Fetch.Throttle {
		t.Errorf("Fetch.Throttle = %q, want %q", back.Fetch.Throttle, cfg.Fetch.Throttle)
	}
	if back.ThrottleDuration() != 90*time.Second {
		t.Errorf("ThrottleDuration = %v, want 90s", back.ThrottleDuration())
	}
}
