package main

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestRootRegistersChildrenCommand(t *testing.T) {
	root := newRootCmd(&appState{git: &fakeGitResolver{}})
	cmd, _, err := root.Find([]string{"children", "proj-abc123"})
	if err != nil {
		t.Fatalf("Find children: %v", err)
	}
	if cmd == nil || cmd.Name() != "children" {
		t.Fatalf("children command lookup = %v, want children", cmd)
	}
}

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

func TestRootCreateUsesConfiguredWorkflowDefaultStatus(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	cfgPath := filepath.Join(dir, "bn.toml")
	if err := os.WriteFile(cfgPath, []byte(`[workflow]
statuses = ["triage", "open", "closed"]
default = "triage"
active = ["triage", "open"]
terminal = ["closed"]
`), 0o644); err != nil {
		t.Fatalf("write workflow config: %v", err)
	}

	t.Setenv("BN_DRIVER", "sqlite")
	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	t.Setenv("BN_DSN", "file:"+dbName+"?mode=memory&cache=shared")
	t.Setenv(workflowEnv, cfgPath)
	t.Setenv(workflowDefaultEnv, "")

	rs := &appState{
		actor: "test",
		git:   &fakeGitResolver{},
	}
	t.Cleanup(func() {
		if rs.store != nil {
			rs.store.Close()
		}
	})
	cmd := newRootCmd(rs)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--project", "wfproj", "create", "custom workflow default", "--silent"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute create: %v", err)
	}

	issueID := strings.TrimSpace(out.String())
	got, err := rs.store.GetIssue(context.Background(), issueID)
	if err != nil {
		t.Fatalf("GetIssue(%q): %v", issueID, err)
	}
	if got.State != "triage" {
		t.Fatalf("created issue state = %q, want configured default triage", got.State)
	}
	if rs.workflow.DefaultState() != "triage" {
		t.Fatalf("app workflow default = %q, want triage", rs.workflow.DefaultState())
	}
}

func TestRootCommandsUseConfiguredWorkflowStatusVocabulary(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	cfgPath := filepath.Join(dir, "bn.toml")
	if err := os.WriteFile(cfgPath, []byte(`[workflow]
statuses = ["open", "qa", "closed"]
default = "open"
active = ["open"]
terminal = ["closed"]
`), 0o644); err != nil {
		t.Fatalf("write workflow config: %v", err)
	}

	dbPath := filepath.Join(dir, "beans.db")
	t.Setenv("BN_DRIVER", "sqlite")
	t.Setenv("BN_DSN", "file:"+dbPath)
	t.Setenv(workflowEnv, cfgPath)
	t.Setenv(workflowDefaultEnv, "")

	createState := &appState{actor: "test", git: &fakeGitResolver{}}
	createCmd := newRootCmd(createState)
	var createOut bytes.Buffer
	createCmd.SetOut(&createOut)
	createCmd.SetErr(&bytes.Buffer{})
	createCmd.SetArgs([]string{"--project", "wfproj", "create", "custom workflow update", "--silent"})
	if err := createCmd.Execute(); err != nil {
		t.Fatalf("create Execute: %v", err)
	}
	if createState.store != nil {
		defer createState.store.Close()
	}
	issueID := strings.TrimSpace(createOut.String())

	updateState := &appState{actor: "test", git: &fakeGitResolver{}}
	updateCmd := newRootCmd(updateState)
	updateCmd.SetOut(&bytes.Buffer{})
	updateCmd.SetErr(&bytes.Buffer{})
	updateCmd.SetArgs([]string{"--project", "wfproj", "update", issueID, "--status", "qa"})
	if err := updateCmd.Execute(); err != nil {
		t.Fatalf("update Execute: %v", err)
	}
	if updateState.store != nil {
		defer updateState.store.Close()
	}

	st, err := store.New(context.Background(), store.Config{
		Driver: store.DriverSQLite,
		DSN:    store.SecretDSN("file:" + dbPath),
		Workflow: model.WorkflowConfig{
			Statuses: []model.IssueState{"open", "qa", "closed"},
			Default:  "open",
			Active:   []model.IssueState{"open"},
			Terminal: []model.IssueState{"closed"},
		},
	})
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer st.Close()
	got, err := st.GetIssue(context.Background(), issueID)
	if err != nil {
		t.Fatalf("GetIssue(%q): %v", issueID, err)
	}
	if got.State != "qa" {
		t.Fatalf("updated state = %q, want qa", got.State)
	}

	listState := &appState{actor: "test", git: &fakeGitResolver{}}
	listCmd := newRootCmd(listState)
	var listErr bytes.Buffer
	listCmd.SetOut(&bytes.Buffer{})
	listCmd.SetErr(&listErr)
	listCmd.SetArgs([]string{"--project", "wfproj", "list", "--status", "ghost"})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("list Execute: %v", err)
	}
	if listState.store != nil {
		defer listState.store.Close()
	}
	warning := listErr.String()
	if !strings.Contains(warning, `unknown status "ghost"`) ||
		!strings.Contains(warning, "open, qa, closed") ||
		strings.Contains(warning, "done") {
		t.Fatalf("warning = %q, want configured status vocabulary only", warning)
	}
}

func TestRootHelpUsesConfiguredWorkflowStatusVocabulary(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	cfgPath := filepath.Join(dir, "bn.toml")
	if err := os.WriteFile(cfgPath, []byte(`[workflow]
statuses = ["open", "qa", "shipped"]
default = "open"
active = ["open", "qa"]
terminal = ["shipped"]
`), 0o644); err != nil {
		t.Fatalf("write workflow config: %v", err)
	}
	t.Setenv(workflowEnv, cfgPath)
	t.Setenv(workflowDefaultEnv, "")

	root := newRootCmd(&appState{git: &fakeGitResolver{}})
	updateCmd, _, err := root.Find([]string{"update"})
	if err != nil {
		t.Fatalf("find update command: %v", err)
	}
	if got := updateCmd.Flags().Lookup("status").Usage; !strings.Contains(got, "open, qa, shipped") || strings.Contains(got, "ready_for_review") {
		t.Fatalf("update --status usage = %q, want configured vocabulary only", got)
	}

	listCmd, _, err := root.Find([]string{"list"})
	if err != nil {
		t.Fatalf("find list command: %v", err)
	}
	if got := listCmd.Flags().Lookup("status").Usage; !strings.Contains(got, "open, qa, shipped") || strings.Contains(got, "ready_for_review") {
		t.Fatalf("list --status usage = %q, want configured vocabulary only", got)
	}
}

func TestDirectCommandsUseInjectedStoreWorkflow(t *testing.T) {
	ctx := context.Background()
	workflow := model.WorkflowConfig{
		Statuses: []model.IssueState{"backlog", "qa", "shipped"},
		Default:  "backlog",
		Active:   []model.IssueState{"qa"},
		Terminal: []model.IssueState{"shipped"},
	}
	st, err := store.New(ctx, store.Config{
		Driver:   store.DriverSQLite,
		DSN:      store.SecretDSN("file:" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()) + "?mode=memory&cache=shared"),
		Workflow: workflow,
	})
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer st.Close()
	if err := st.EnsureProject(ctx, "wfproj"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	iss, err := st.CreateIssue(ctx, store.CreateIssueInput{
		Prefix:    "wfproj",
		Title:     "custom direct command",
		Priority:  2,
		IssueType: "task",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	rs := &appState{
		store:  st,
		prefix: "wfproj",
		actor:  "test",
		git:    &fakeGitResolver{},
	}
	updateCmd := newUpdateCmd(rs)
	updateCmd.SetOut(&bytes.Buffer{})
	updateCmd.SetErr(&bytes.Buffer{})
	updateCmd.SetArgs([]string{iss.ID, "--status", "qa"})
	if err := updateCmd.Execute(); err != nil {
		t.Fatalf("direct update Execute: %v", err)
	}

	readyCmd := newReadyCmd(rs)
	var readyOut bytes.Buffer
	readyCmd.SetOut(&readyOut)
	readyCmd.SetErr(&bytes.Buffer{})
	if err := readyCmd.Execute(); err != nil {
		t.Fatalf("direct ready Execute: %v", err)
	}
	if !strings.Contains(readyOut.String(), iss.ID) {
		t.Fatalf("ready output %q missing issue %q active under injected store workflow", readyOut.String(), iss.ID)
	}
}

func TestIssueTableStatusColumnExpandsForLongConfiguredStatuses(t *testing.T) {
	cmd := newReadyCmd(&appState{})
	var out bytes.Buffer
	cmd.SetOut(&out)

	printIssueTable(cmd, []store.Issue{
		{Issue: model.Issue{ID: "wf-1", State: "waiting_for_external_validation", Priority: model.PriorityMedium, Title: "long status"}},
		{Issue: model.Issue{ID: "wf-2", State: "qa", Priority: model.PriorityHigh, Title: "short status"}},
	})

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 4 {
		t.Fatalf("table output has %d lines, want header/rule/two rows: %q", len(lines), out.String())
	}
	headerPRI := strings.Index(lines[0], "PRI")
	longPRI := strings.Index(lines[2], "P2")
	shortPRI := strings.Index(lines[3], "P1")
	if headerPRI < 0 || longPRI < 0 || shortPRI < 0 {
		t.Fatalf("table output missing PRI/priority columns: %q", out.String())
	}
	if headerPRI != longPRI || headerPRI != shortPRI {
		t.Fatalf("priority column indexes header=%d long=%d short=%d output:\n%s", headerPRI, longPRI, shortPRI, out.String())
	}
	if !strings.Contains(lines[2], "waiting_for_external_validation") {
		t.Fatalf("long status missing from table output:\n%s", out.String())
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
				CreationCommit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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
	if got.Repo.CreationCommit != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("repo creation_commit = %q, want captured commit", got.Repo.CreationCommit)
	}
	if got.Repo.Metadata["source"] != "test" {
		t.Fatalf("repo metadata[source] = %v, want test", got.Repo.Metadata["source"])
	}
}

func TestIssueJSONCommandsExposeRepoCreationCommitWithOmitEmpty(t *testing.T) {
	ctx := context.Background()
	s, repo := newTestStore(t, "", "https://github.com/alice/cli-json")
	if repo == nil {
		t.Fatal("newTestStore did not register repo")
	}
	if err := s.EnsureProject(ctx, repo.Prefix); err != nil {
		t.Fatalf("EnsureProject(%q): %v", repo.Prefix, err)
	}

	const creationCommit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	captured, err := s.CreateIssue(ctx, store.CreateIssueInput{
		Prefix: repo.Prefix,
		Title:  "captured commit",
		Actor:  "test",
		Repo: &store.IssueRepoInput{
			RepoSlug:       repo.Slug,
			CreationCommit: creationCommit,
			WorktreeSubdir: "service/api",
			RequestedRef:   "main",
			WorkBranch:     "work/captured",
		},
	})
	if err != nil {
		t.Fatalf("CreateIssue captured: %v", err)
	}
	legacy, err := s.CreateIssue(ctx, store.CreateIssueInput{
		Prefix: repo.Prefix,
		Title:  "legacy empty commit",
		Actor:  "test",
		Repo:   &store.IssueRepoInput{RepoSlug: repo.Slug},
	})
	if err != nil {
		t.Fatalf("CreateIssue legacy: %v", err)
	}

	rs := &appState{
		store:   s,
		prefix:  repo.Prefix,
		actor:   "test",
		jsonOut: true,
		git:     &fakeGitResolver{},
	}

	showCmd := newShowCmd(rs)
	var showOut bytes.Buffer
	showCmd.SetOut(&showOut)
	if err := showCmd.RunE(showCmd, []string{captured.ID}); err != nil {
		t.Fatalf("show --json RunE: %v", err)
	}
	showIssue := decodeJSONIssueMap(t, showOut.Bytes())
	assertRepoCreationCommitField(t, showIssue, creationCommit, true)

	showLegacyCmd := newShowCmd(rs)
	var showLegacyOut bytes.Buffer
	showLegacyCmd.SetOut(&showLegacyOut)
	if err := showLegacyCmd.RunE(showLegacyCmd, []string{legacy.ID}); err != nil {
		t.Fatalf("show legacy --json RunE: %v", err)
	}
	showLegacyIssue := decodeJSONIssueMap(t, showLegacyOut.Bytes())
	assertRepoCreationCommitField(t, showLegacyIssue, "", false)

	listCmd := newListCmd(rs)
	var listOut bytes.Buffer
	listCmd.SetOut(&listOut)
	if err := listCmd.RunE(listCmd, nil); err != nil {
		t.Fatalf("list --json RunE: %v", err)
	}
	listIssues := decodeJSONIssueMapList(t, listOut.Bytes())
	assertRepoCreationCommitField(t, findJSONIssueMap(t, listIssues, captured.ID), creationCommit, true)
	assertRepoCreationCommitField(t, findJSONIssueMap(t, listIssues, legacy.ID), "", false)

	readyCmd := newReadyCmd(rs)
	var readyOut bytes.Buffer
	readyCmd.SetOut(&readyOut)
	if err := readyCmd.RunE(readyCmd, nil); err != nil {
		t.Fatalf("ready --json RunE: %v", err)
	}
	readyIssues := decodeJSONIssueMapList(t, readyOut.Bytes())
	assertRepoCreationCommitField(t, findJSONIssueMap(t, readyIssues, captured.ID), creationCommit, true)
	assertRepoCreationCommitField(t, findJSONIssueMap(t, readyIssues, legacy.ID), "", false)
}

func decodeJSONIssueMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal issue JSON %q: %v", string(raw), err)
	}
	return got
}

func decodeJSONIssueMapList(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	var got []map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal issue JSON list %q: %v", string(raw), err)
	}
	return got
}

func findJSONIssueMap(t *testing.T, issues []map[string]any, id string) map[string]any {
	t.Helper()
	for _, issue := range issues {
		if issue["id"] == id {
			return issue
		}
	}
	t.Fatalf("issue %s not found in JSON output: %#v", id, issues)
	return nil
}

func assertRepoCreationCommitField(t *testing.T, issue map[string]any, want string, wantPresent bool) {
	t.Helper()
	repo, ok := issue["repo"].(map[string]any)
	if !ok {
		t.Fatalf("repo JSON = %#v, want object", issue["repo"])
	}
	got, present := repo["creation_commit"]
	if present != wantPresent {
		t.Fatalf("repo creation_commit presence = %v, want %v in %#v", present, wantPresent, repo)
	}
	if wantPresent && got != want {
		t.Fatalf("repo creation_commit = %v, want %q", got, want)
	}
}
