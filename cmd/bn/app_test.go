package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattsp1290/beans/model"
	store "github.com/mattsp1290/beans/store"
)

func TestParseActiveProjectMarker(t *testing.T) {
	t.Parallel()

	got, err := parseActiveProjectMarker("# bn active project\nproject = smoke\n")
	if err != nil {
		t.Fatalf("parseActiveProjectMarker: %v", err)
	}
	if got != "smoke" {
		t.Fatalf("prefix = %q, want smoke", got)
	}
}

func TestParseActiveProjectConfigWithRepo(t *testing.T) {
	t.Parallel()

	got, err := parseActiveProjectConfig("# bn active project\nproject = smoke\nrepo=boxy\nremote=git@example.com:boxy.git\n")
	if err != nil {
		t.Fatalf("parseActiveProjectConfig: %v", err)
	}
	if got.Project != "smoke" || got.Repo != "boxy" || got.Remote != "git@example.com:boxy.git" {
		t.Fatalf("config = %+v, want project/repo/remote", got)
	}
}

func TestParseActiveProjectMarkerRejectsMissingProject(t *testing.T) {
	t.Parallel()

	if _, err := parseActiveProjectMarker("actor=matt\n"); err == nil {
		t.Fatal("parseActiveProjectMarker: want error for missing project")
	}
}

func TestValidateActiveProjectPrefixRejectsNonRoundTrippableValues(t *testing.T) {
	t.Parallel()

	for _, prefix := range []string{"", " smoke", "smoke ", "smoke\nprod", "smoke\rprod"} {
		if err := validateActiveProjectPrefix(prefix); err == nil {
			t.Fatalf("validateActiveProjectPrefix(%q): want error", prefix)
		}
	}
	if err := validateActiveProjectPrefix("beans"); err != nil {
		t.Fatalf("validateActiveProjectPrefix(valid): %v", err)
	}
}

func TestReadActiveProjectMarkerFindsNearestParent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, activeProjectMarker), []byte("project=parent\n"), 0o644); err != nil {
		t.Fatalf("write parent marker: %v", err)
	}
	child := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}

	got, err := readActiveProjectMarker(child)
	if err != nil {
		t.Fatalf("readActiveProjectMarker: %v", err)
	}
	if got != "parent" {
		t.Fatalf("prefix = %q, want parent", got)
	}
}

func TestReadActiveProjectConfigFindsRepo(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, activeProjectMarker), []byte("project=parent\nrepo=boxy\nremote=git@example.com:boxy.git\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	child := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}

	got, err := readActiveProjectConfig(child)
	if err != nil {
		t.Fatalf("readActiveProjectConfig: %v", err)
	}
	if got.Project != "parent" || got.Repo != "boxy" || got.Remote != "git@example.com:boxy.git" {
		t.Fatalf("config = %+v, want project/repo/remote", got)
	}
}

func TestReadActiveProjectMarkerPrefersNearestMarker(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, activeProjectMarker), []byte("project=parent\n"), 0o644); err != nil {
		t.Fatalf("write parent marker: %v", err)
	}
	child := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}
	if err := os.WriteFile(filepath.Join(child, activeProjectMarker), []byte("project=child\n"), 0o644); err != nil {
		t.Fatalf("write child marker: %v", err)
	}

	got, err := readActiveProjectMarker(child)
	if err != nil {
		t.Fatalf("readActiveProjectMarker: %v", err)
	}
	if got != "child" {
		t.Fatalf("prefix = %q, want child", got)
	}
}

func TestResolveProjectPrefixPrecedence(t *testing.T) {
	t.Setenv("BN_PROJECT", "env")

	got, err := resolveProjectPrefix("flag", false)
	if err != nil {
		t.Fatalf("resolveProjectPrefix(flag): %v", err)
	}
	if got != "flag" {
		t.Fatalf("flag prefix = %q, want flag", got)
	}

	got, err = resolveProjectPrefix("", false)
	if err != nil {
		t.Fatalf("resolveProjectPrefix(env): %v", err)
	}
	if got != "env" {
		t.Fatalf("env prefix = %q, want env", got)
	}
}

func TestStoreConfigFromEnvUsesDriverAndDSN(t *testing.T) {
	t.Setenv("BN_DRIVER", "SQLite3")
	t.Setenv("BN_DSN", "file:beans.db")

	cfg, err := storeConfigFromEnv()
	if err != nil {
		t.Fatalf("storeConfigFromEnv: %v", err)
	}
	if cfg.Driver != store.DriverSQLite {
		t.Fatalf("driver = %q, want sqlite", cfg.Driver)
	}
	if got := cfg.DSN.Reveal(); got != "file:beans.db" {
		t.Fatalf("dsn = %q, want file:beans.db", got)
	}
}

func TestStoreConfigFromEnvInfersPostgresOnlyWhenClear(t *testing.T) {
	t.Setenv("BN_DRIVER", "")
	t.Setenv("BN_DSN", "postgres://user:pass@localhost:5432/beans")

	cfg, err := storeConfigFromEnv()
	if err != nil {
		t.Fatalf("storeConfigFromEnv postgres URL: %v", err)
	}
	if cfg.Driver != store.DriverPostgres {
		t.Fatalf("driver = %q, want postgres", cfg.Driver)
	}

	t.Setenv("BN_DSN", "host=localhost user=bn dbname=beans sslmode=disable")
	cfg, err = storeConfigFromEnv()
	if err != nil {
		t.Fatalf("storeConfigFromEnv postgres keyword DSN: %v", err)
	}
	if cfg.Driver != store.DriverPostgres {
		t.Fatalf("keyword driver = %q, want postgres", cfg.Driver)
	}
}

func TestStoreConfigFromEnvRejectsMissingOrAmbiguousDriver(t *testing.T) {
	t.Setenv("BN_DRIVER", "")
	t.Setenv("BN_DSN", "file:beans.db")
	if _, err := storeConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "BN_DRIVER") {
		t.Fatalf("sqlite without BN_DRIVER error = %v, want BN_DRIVER hint", err)
	}

	t.Setenv("BN_DRIVER", "oracle")
	t.Setenv("BN_DSN", "dsn")
	if _, err := storeConfigFromEnv(); !errors.Is(err, store.ErrUnsupportedDriver) {
		t.Fatalf("unknown driver error = %v, want ErrUnsupportedDriver", err)
	}
	t.Setenv("BN_DSN", "")
	if _, err := storeConfigFromEnv(); !errors.Is(err, store.ErrUnsupportedDriver) {
		t.Fatalf("unknown driver without DSN error = %v, want ErrUnsupportedDriver", err)
	}

	t.Setenv("BN_DRIVER", "")
	t.Setenv("BN_DSN", "")
	if _, err := storeConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "BN_DRIVER and BN_DSN") {
		t.Fatalf("missing env error = %v, want both values named", err)
	}
}

func TestResolveProjectPrefixFallsBackToMarker(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, activeProjectMarker), []byte("project=marker\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Setenv("BN_PROJECT", "")

	got, err := resolveProjectPrefix("", false)
	if err != nil {
		t.Fatalf("resolveProjectPrefix(marker): %v", err)
	}
	if got != "marker" {
		t.Fatalf("marker prefix = %q, want marker", got)
	}
}

func TestResolveProjectPrefixCanSkipMalformedMarker(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, activeProjectMarker), []byte("not valid\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Setenv("BN_PROJECT", "")

	if _, err := resolveProjectPrefix("", false); err == nil {
		t.Fatal("resolveProjectPrefix: want malformed marker error")
	}
	got, err := resolveProjectPrefix("", true)
	if err != nil {
		t.Fatalf("resolveProjectPrefix(skipMarker): %v", err)
	}
	if got != "" {
		t.Fatalf("skip-marker prefix = %q, want empty", got)
	}
}

func TestReadActiveProjectMarkerUsesCurrentGitRoot(t *testing.T) {
	parent := t.TempDir()
	gitInit(t, parent)
	if err := os.WriteFile(filepath.Join(parent, activeProjectMarker), []byte("project=parent\n"), 0o644); err != nil {
		t.Fatalf("write parent marker: %v", err)
	}

	child := filepath.Join(parent, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}
	gitInit(t, child)

	got, err := readActiveProjectMarker(child)
	if err != nil {
		t.Fatalf("readActiveProjectMarker: %v", err)
	}
	if got != "" {
		t.Fatalf("prefix = %q, want empty because child repo has no marker", got)
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", "--quiet", dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git init %s: %v\n%s", dir, err, out)
	}
}

func TestIssueJSONIncludesRepoTarget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	got := toIssueJSON(store.Issue{
		Issue: model.Issue{
			ID:       "proj-abc123",
			Title:    "route it",
			Priority: model.PriorityMedium,
			State:    "open",
			Repo: &model.RepoTarget{
				ID:             "repo-1",
				Slug:           "boxy",
				RemoteURL:      "git@example.com:boxy.git",
				DefaultBranch:  "main",
				RequestedRef:   "feature/ref",
				BaseRef:        "main",
				WorktreeSubdir: "services/boxy",
				CloneStrategy:  "mirror-cache",
				AuthRef:        "ssh-key:github-default",
				Metadata:       map[string]any{"source": "test"},
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		IssueType: "task",
	})
	if got.Repo == nil {
		t.Fatal("repo JSON = nil, want route target")
	}
	if got.Repo.Slug != "boxy" || got.Repo.RequestedRef != "feature/ref" || got.Repo.WorktreeSubdir != "services/boxy" {
		t.Fatalf("repo JSON = %+v, want slug/ref/subdir", got.Repo)
	}
	if got.Repo.Metadata["source"] != "test" {
		t.Fatalf("repo metadata[source] = %v, want test", got.Repo.Metadata["source"])
	}
}
