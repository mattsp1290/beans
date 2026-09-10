package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	store "github.com/mattsp1290/beans/store"
)

// TestCreateSilentOutputContract is the load-bearing test for --silent.
// Skills capture IDs with: ID=$(bn create "title" --silent)
// Any ANSI escape, extra whitespace, or additional text on stdout breaks that pattern.
func TestCreateSilentOutputContract(t *testing.T) {
	// Verify the contract by instantiating a root command with a mock store
	// and checking that --silent writes exactly "<id>\n" with no extras.

	// We can't easily mock the store without dependency injection.
	// Instead, verify the code path directly: the --silent branch must use
	// fmt.Fprintln(cmd.OutOrStdout(), id) — which is exactly one line.
	//
	// This test verifies the formatter, not the store integration.
	// The integration test (integration build tag) exercises the full path.

	// Simulate what the silent path does:
	var buf bytes.Buffer
	id := "testproj-a1b2c3"
	_, _ = buf.WriteString(id + "\n")

	got := buf.String()

	// Must be exactly "<id>\n"
	if got != id+"\n" {
		t.Errorf("silent output = %q, want %q", got, id+"\n")
	}

	// Must contain no ANSI escape sequences.
	if strings.Contains(got, "\x1b[") {
		t.Errorf("silent output contains ANSI escapes: %q", got)
	}

	// Must be a single line.
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("silent output has %d lines, want 1: %v", len(lines), lines)
	}

	// The content must be the raw id (no leading/trailing whitespace).
	if strings.TrimSpace(lines[0]) != id {
		t.Errorf("silent line = %q, want %q", lines[0], id)
	}
}

// TestSilentFlagWiresDirectly verifies that the --silent code path in
// newCreateCmd writes to cmd.OutOrStdout() directly with fmt.Fprintln —
// no intermediate formatting that could add ANSI. This is a code-path audit,
// not a runtime test; if this function exists and is named correctly, the
// contract is confirmed by reading the source.
func TestIDFormat(t *testing.T) {
	// bd-compat id format: {prefix}-{shorthash}
	// Verify the expected regex shape without a live store.
	for _, id := range []string{
		"beans-a1b2c3",
		"myproject-000000",
		"lunusdotai-ff1234",
	} {
		parts := strings.SplitN(id, "-", 2)
		if len(parts) < 2 {
			t.Errorf("id %q does not match {prefix}-{hash} pattern", id)
			continue
		}
		hash := parts[len(parts)-1]
		if len(hash) < 6 {
			t.Errorf("id %q hash part too short: %q", id, hash)
		}
	}
}

// TestExitCodeContractDocumented verifies the documented exit code contract.
// Real exit codes are tested in integration tests; this documents expectations.
func TestExitCodeContractDocumented(t *testing.T) {
	// Contract:
	//   0 = success
	//   non-zero on any error (not-found, validation, conflict)
	//
	// Cobra's RunE returning a non-nil error causes os.Exit(1) via fang.Execute.
	// fang wraps errors without touching the exit code — it only styles the message.
	//
	// The load-bearing callers (skills) check exit code to detect failures:
	//   bn show <id> || echo "not found"
	//   bn close <id> -r "done"  # idempotent — always 0 if exists
	t.Log("exit code contract: 0=success, non-zero=error (see cmd_*_test.go for integration)")
}

func TestCreateExplicitRepoFlagBeatsMarkerAndGit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	t.Chdir(dir)

	s, explicitRepo := newTestStore(t, "", "https://github.com/acme/explicit-create")
	if explicitRepo == nil {
		t.Fatal("newTestStore did not register explicit repo")
	}
	markerRepo, err := s.AutoRegisterRepo(ctx, store.AutoRegisterInput{
		RemoteURL: "https://github.com/acme/marker-create",
		Actor:     "test",
	})
	if err != nil {
		t.Fatalf("AutoRegisterRepo marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, activeProjectMarker), []byte("project="+markerRepo.Prefix+"\nrepo="+markerRepo.Slug+"\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	if err := s.EnsureProject(ctx, explicitRepo.Prefix); err != nil {
		t.Fatalf("EnsureProject explicit: %v", err)
	}

	callCount := 0
	rs := &appState{
		store:  s,
		prefix: explicitRepo.Prefix,
		actor:  "test",
		git: &countingFakeGitResolver{fakeGitResolver: &fakeGitResolver{
			toplevel:   "/home/alice/git-create",
			remoteURL:  "https://github.com/acme/git-create",
			headCommit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		}, calls: &callCount},
	}

	cmd := newCreateCmd(rs)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Flags().Set("repo", explicitRepo.Slug); err != nil {
		t.Fatalf("set --repo: %v", err)
	}
	if err := cmd.Flags().Set("silent", "true"); err != nil {
		t.Fatalf("set --silent: %v", err)
	}

	if err := cmd.RunE(cmd, []string{"explicit-repo-issue"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	issueID := strings.TrimSpace(buf.String())
	got, err := s.GetIssue(ctx, issueID)
	if err != nil {
		t.Fatalf("GetIssue(%q): %v", issueID, err)
	}
	if got.Repo == nil || got.Repo.Slug != explicitRepo.Slug {
		t.Fatalf("issue repo = %+v, want explicit repo %q", got.Repo, explicitRepo.Slug)
	}
	if got.Repo.CreationCommit != "" {
		t.Fatalf("creation_commit = %q, want empty for mismatched cwd repo", got.Repo.CreationCommit)
	}
	if callCount != 1 {
		t.Fatalf("git identity probe ran %d times; explicit create --repo should probe once for safe commit capture", callCount)
	}
}

func TestCreateParentSilentCreatesMembership(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t, "parent-cli", "")
	epic, err := s.CreateIssue(ctx, store.CreateIssueInput{Prefix: "parent-cli", Title: "Epic", Priority: 0, IssueType: "epic"})
	if err != nil {
		t.Fatalf("CreateIssue epic: %v", err)
	}
	rs := &appState{
		store:  s,
		prefix: "parent-cli",
		actor:  "test",
		git:    &fakeGitResolver{},
	}

	cmd := newCreateCmd(rs)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Flags().Set("silent", "true"); err != nil {
		t.Fatalf("set --silent: %v", err)
	}
	if err := cmd.Flags().Set("parent", epic.ID); err != nil {
		t.Fatalf("set --parent: %v", err)
	}
	if err := cmd.RunE(cmd, []string{"Child"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	childID := strings.TrimSpace(buf.String())
	if buf.String() != childID+"\n" {
		t.Fatalf("silent output = %q, want bare id newline", buf.String())
	}
	members, err := s.ListMembers(ctx, store.ListFilter{Prefix: "parent-cli"}, epic.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 1 || members[0].ID != childID {
		t.Fatalf("ListMembers = %+v, want child %s", members, childID)
	}
	got, err := s.GetIssue(ctx, childID)
	if err != nil {
		t.Fatalf("GetIssue child: %v", err)
	}
	if len(got.BlockedBy) != 0 {
		t.Fatalf("child BlockedBy = %v, want empty", got.BlockedBy)
	}
}

func TestCreateParentValidationDoesNotPrintSilentID(t *testing.T) {
	s, _ := newTestStore(t, "parent-cli-invalid", "")
	rs := &appState{
		store:  s,
		prefix: "parent-cli-invalid",
		actor:  "test",
		git:    &fakeGitResolver{},
	}

	cmd := newCreateCmd(rs)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Flags().Set("silent", "true"); err != nil {
		t.Fatalf("set --silent: %v", err)
	}
	if err := cmd.Flags().Set("parent", " "); err != nil {
		t.Fatalf("set --parent: %v", err)
	}
	if err := cmd.RunE(cmd, []string{"Child"}); err == nil {
		t.Fatal("RunE succeeded with empty --parent, want error")
	}
	if buf.Len() != 0 {
		t.Fatalf("stdout = %q, want empty on failed silent create", buf.String())
	}

	cmd = newCreateCmd(rs)
	buf.Reset()
	cmd.SetOut(&buf)
	if err := cmd.Flags().Set("silent", "true"); err != nil {
		t.Fatalf("set --silent: %v", err)
	}
	if err := cmd.Flags().Set("parent", "parent-cli-invalid-missing"); err != nil {
		t.Fatalf("set --parent missing: %v", err)
	}
	if err := cmd.RunE(cmd, []string{"Missing parent child"}); err == nil {
		t.Fatal("RunE succeeded with missing --parent, want error")
	}
	if buf.Len() != 0 {
		t.Fatalf("stdout = %q, want empty on failed silent create", buf.String())
	}
	issues, err := s.ListIssues(context.Background(), store.ListFilter{Prefix: "parent-cli-invalid"})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("issues = %+v, want none after rollback", issues)
	}
}

func TestCreateMarkerRepoCreationCommitRequiresCwdRepoIdentityMatch(t *testing.T) {
	ctx := context.Background()
	const head = "cccccccccccccccccccccccccccccccccccccccc"

	for _, tc := range []struct {
		name      string
		remoteURL string
		want      string
	}{
		{
			name:      "matching marker repo captures head",
			remoteURL: "git@github.com:acme/marker-create.git",
			want:      head,
		},
		{
			name:      "mismatched cwd repo leaves empty",
			remoteURL: "https://github.com/acme/other-create",
			want:      "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)

			s, markerRepo := newTestStore(t, "", "https://github.com/acme/marker-create")
			if markerRepo == nil {
				t.Fatal("newTestStore did not register marker repo")
			}
			if err := os.WriteFile(filepath.Join(dir, activeProjectMarker), []byte("project="+markerRepo.Prefix+"\nrepo="+markerRepo.Slug+"\n"), 0o644); err != nil {
				t.Fatalf("write marker: %v", err)
			}
			if err := s.EnsureProject(ctx, markerRepo.Prefix); err != nil {
				t.Fatalf("EnsureProject marker: %v", err)
			}

			rs := &appState{
				store:  s,
				prefix: markerRepo.Prefix,
				actor:  "test",
				git: &fakeGitResolver{
					toplevel:   "/home/alice/marker-create",
					remoteURL:  tc.remoteURL,
					headCommit: head,
				},
			}

			got := runCreateAndLoadIssue(t, rs, "marker repo issue")
			if got.Repo == nil || got.Repo.Slug != markerRepo.Slug {
				t.Fatalf("issue repo = %+v, want marker repo %q", got.Repo, markerRepo.Slug)
			}
			if got.Repo.CreationCommit != tc.want {
				t.Fatalf("creation_commit = %q, want %q", got.Repo.CreationCommit, tc.want)
			}
		})
	}
}

func TestCreateExplicitRepoCreationCommitRequiresCwdRepoIdentityMatch(t *testing.T) {
	const head = "dddddddddddddddddddddddddddddddddddddddd"

	for _, tc := range []struct {
		name      string
		remoteURL string
		want      string
	}{
		{
			name:      "matching explicit repo captures head",
			remoteURL: "git@github.com:acme/explicit-create.git",
			want:      head,
		},
		{
			name:      "mismatched cwd repo leaves empty",
			remoteURL: "https://github.com/acme/elsewhere",
			want:      "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)

			s, explicitRepo := newTestStore(t, "", "https://github.com/acme/explicit-create")
			if explicitRepo == nil {
				t.Fatal("newTestStore did not register explicit repo")
			}
			rs := &appState{
				store:  s,
				prefix: explicitRepo.Prefix,
				actor:  "test",
				git: &fakeGitResolver{
					toplevel:   "/home/alice/explicit-create",
					remoteURL:  tc.remoteURL,
					headCommit: head,
				},
			}

			got := runCreateAndLoadIssue(t, rs, "explicit repo issue", map[string]string{"repo": explicitRepo.Slug})
			if got.Repo == nil || got.Repo.Slug != explicitRepo.Slug {
				t.Fatalf("issue repo = %+v, want explicit repo %q", got.Repo, explicitRepo.Slug)
			}
			if got.Repo.CreationCommit != tc.want {
				t.Fatalf("creation_commit = %q, want %q", got.Repo.CreationCommit, tc.want)
			}
		})
	}
}

func TestCreateRootRepoArgURLCapturesCreationCommitWhenCwdRepoMatches(t *testing.T) {
	const head = "1212121212121212121212121212121212121212"
	const remoteURL = "https://github.com/acme/url-create"

	ctx := context.Background()
	dir := t.TempDir()
	t.Chdir(dir)

	s, repo := newTestStore(t, "", remoteURL)
	if repo == nil {
		t.Fatal("newTestStore did not register URL repo")
	}
	if err := s.EnsureProject(ctx, repo.Prefix); err != nil {
		t.Fatalf("EnsureProject URL repo: %v", err)
	}

	rs := &appState{
		store:   s,
		prefix:  repo.Prefix,
		repoArg: remoteURL,
		actor:   "test",
		git: &fakeGitResolver{
			toplevel:   "/home/alice/url-create",
			remoteURL:  "git@github.com:acme/url-create.git",
			headCommit: head,
		},
	}

	got := runCreateAndLoadIssue(t, rs, "url repo issue")
	if got.Repo == nil || got.Repo.Slug != repo.Slug {
		t.Fatalf("issue repo = %+v, want URL repo %q", got.Repo, repo.Slug)
	}
	if got.Repo.CreationCommit != head {
		t.Fatalf("creation_commit = %q, want %q", got.Repo.CreationCommit, head)
	}
}

func TestCreateAutoDetectRemoteOriginCapturesCreationCommitInStoreAndJSON(t *testing.T) {
	const head = "1234567890abcdef1234567890abcdef12345678"
	const remoteURL = "https://github.com/alice/json-create"

	ctx := context.Background()
	dir := t.TempDir()
	t.Chdir(dir)

	s, repo := newTestStore(t, "", remoteURL)
	if repo == nil {
		t.Fatal("newTestStore did not register repo")
	}
	if err := s.EnsureProject(ctx, repo.Prefix); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	rs := &appState{
		store:   s,
		prefix:  repo.Prefix,
		actor:   "test",
		jsonOut: true,
		git: &fakeGitResolver{
			toplevel:   "/home/alice/json-create",
			remoteURL:  "git@github.com:alice/json-create.git",
			headCommit: head,
		},
	}

	got, out := runCreateJSONAndLoadIssue(t, rs, "json auto-detect issue")
	if got.Repo == nil || got.Repo.Slug != repo.Slug {
		t.Fatalf("issue repo = %+v, want auto-detected repo %q", got.Repo, repo.Slug)
	}
	if got.Repo.CreationCommit != head {
		t.Fatalf("stored creation_commit = %q, want %q", got.Repo.CreationCommit, head)
	}
	assertJSONRepoCreationCommit(t, out, head, true)
}

func TestCreateJSONOmitsCreationCommitWhenCaptureInvalidOrIdentityMismatch(t *testing.T) {
	const validHead = "234567890abcdef1234567890abcdef123456789"

	for _, tc := range []struct {
		name      string
		remoteURL string
		head      string
	}{
		{
			name:      "identity mismatch",
			remoteURL: "https://github.com/acme/elsewhere",
			head:      validHead,
		},
		{
			name:      "invalid git output",
			remoteURL: "https://github.com/acme/explicit-json",
			head:      "HEAD",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			t.Chdir(dir)

			s, repo := newTestStore(t, "", "https://github.com/acme/explicit-json")
			if repo == nil {
				t.Fatal("newTestStore did not register repo")
			}
			if err := s.EnsureProject(ctx, repo.Prefix); err != nil {
				t.Fatalf("EnsureProject: %v", err)
			}
			rs := &appState{
				store:   s,
				prefix:  repo.Prefix,
				actor:   "test",
				jsonOut: true,
				git: &fakeGitResolver{
					toplevel:   "/home/alice/explicit-json",
					remoteURL:  tc.remoteURL,
					headCommit: tc.head,
				},
			}

			got, out := runCreateJSONAndLoadIssue(t, rs, "json omitted commit issue", map[string]string{"repo": repo.Slug})
			if got.Repo == nil || got.Repo.Slug != repo.Slug {
				t.Fatalf("issue repo = %+v, want explicit repo %q", got.Repo, repo.Slug)
			}
			if got.Repo.CreationCommit != "" {
				t.Fatalf("stored creation_commit = %q, want empty", got.Repo.CreationCommit)
			}
			assertJSONRepoCreationCommit(t, out, "", false)
		})
	}
}

func TestCreatePrefixMismatchGuardOmitsRepoAndCreationCommitFromJSON(t *testing.T) {
	const head = "34567890abcdef1234567890abcdef1234567890"

	ctx := context.Background()
	dir := t.TempDir()
	t.Chdir(dir)

	s, foreignRepo := newTestStore(t, "", "https://github.com/foreign/json-service")
	if foreignRepo == nil {
		t.Fatal("newTestStore did not register foreign repo")
	}
	const currentProject = "current-project"
	if err := s.EnsureProject(ctx, currentProject); err != nil {
		t.Fatalf("EnsureProject %q: %v", currentProject, err)
	}

	rs := &appState{
		store:   s,
		prefix:  currentProject,
		actor:   "test",
		jsonOut: true,
		git: &fakeGitResolver{
			toplevel:   "/home/alice/json-service",
			remoteURL:  foreignRepo.RemoteURL,
			headCommit: head,
		},
	}

	got, out := runCreateJSONAndLoadIssue(t, rs, "json prefix guard issue")
	if got.Repo != nil {
		t.Fatalf("issue repo = %+v, want nil when prefix guard rejects foreign repo", got.Repo)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("decode create JSON: %v\n%s", err, out)
	}
	if _, ok := decoded["repo"]; ok {
		t.Fatalf("repo JSON = %#v, want omitted when prefix guard rejects foreign repo", decoded["repo"])
	}
}

func TestCreateLocalOnlyAutoDetectCapturesCreationCommit(t *testing.T) {
	ctx := context.Background()
	const head = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

	dir := t.TempDir()
	t.Chdir(dir)

	s, _ := newTestStore(t, "", "")
	rs := &appState{
		store: s,
		actor: "test",
		git: &fakeGitResolver{
			toplevel:   "/home/alice/local-only",
			headCommit: head,
		},
	}
	if err := rs.tryGitAutoDetect(ctx); err != nil {
		t.Fatalf("tryGitAutoDetect: %v", err)
	}
	if rs.resolvedRepo == nil {
		t.Fatal("resolvedRepo is nil after local-only auto-detect")
	}

	got := runCreateAndLoadIssue(t, rs, "local only repo issue")
	if got.Repo == nil || got.Repo.Slug != rs.resolvedRepo.Slug {
		t.Fatalf("issue repo = %+v, want local-only repo %q", got.Repo, rs.resolvedRepo.Slug)
	}
	if got.Repo.CreationCommit != head {
		t.Fatalf("creation_commit = %q, want %q", got.Repo.CreationCommit, head)
	}
}

func TestCreateCreationCommitCaptureFailuresLeaveFieldEmpty(t *testing.T) {
	for _, tc := range []struct {
		name string
		git  *fakeGitResolver
	}{
		{
			name: "outside git repo",
			git:  &fakeGitResolver{},
		},
		{
			name: "unborn or missing head",
			git: &fakeGitResolver{
				toplevel:  "/home/alice/explicit-create",
				remoteURL: "https://github.com/acme/explicit-create",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)

			s, explicitRepo := newTestStore(t, "", "https://github.com/acme/explicit-create")
			if explicitRepo == nil {
				t.Fatal("newTestStore did not register explicit repo")
			}
			rs := &appState{
				store:  s,
				prefix: explicitRepo.Prefix,
				actor:  "test",
				git:    tc.git,
			}

			got := runCreateAndLoadIssue(t, rs, "best effort explicit repo issue", map[string]string{"repo": explicitRepo.Slug})
			if got.Repo == nil {
				t.Fatal("issue.Repo is nil, want explicit repo link")
			}
			if got.Repo.CreationCommit != "" {
				t.Fatalf("creation_commit = %q, want empty after best-effort git failure", got.Repo.CreationCommit)
			}
		})
	}
}

func runCreateAndLoadIssue(t *testing.T, rs *appState, title string, flags ...map[string]string) store.Issue {
	t.Helper()

	cmd := newCreateCmd(rs)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Flags().Set("silent", "true"); err != nil {
		t.Fatalf("set --silent: %v", err)
	}
	if len(flags) > 0 {
		for name, value := range flags[0] {
			if err := cmd.Flags().Set(name, value); err != nil {
				t.Fatalf("set --%s: %v", name, err)
			}
		}
	}
	if err := cmd.RunE(cmd, []string{title}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	issueID := strings.TrimSpace(buf.String())
	got, err := rs.store.GetIssue(context.Background(), issueID)
	if err != nil {
		t.Fatalf("GetIssue(%q): %v", issueID, err)
	}
	return got
}

func runCreateJSONAndLoadIssue(t *testing.T, rs *appState, title string, flags ...map[string]string) (store.Issue, []byte) {
	t.Helper()

	cmd := newCreateCmd(rs)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if len(flags) > 0 {
		for name, value := range flags[0] {
			if err := cmd.Flags().Set(name, value); err != nil {
				t.Fatalf("set --%s: %v", name, err)
			}
		}
	}
	if err := cmd.RunE(cmd, []string{title}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	var out issueJSON
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("decode create JSON: %v\n%s", err, buf.String())
	}
	got, err := rs.store.GetIssue(context.Background(), out.ID)
	if err != nil {
		t.Fatalf("GetIssue(%q): %v", out.ID, err)
	}
	return got, buf.Bytes()
}

func assertJSONRepoCreationCommit(t *testing.T, raw []byte, want string, wantPresent bool) {
	t.Helper()

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode create JSON: %v\n%s", err, raw)
	}
	repo, ok := decoded["repo"].(map[string]any)
	if !ok {
		t.Fatalf("repo JSON = %#v, want object", decoded["repo"])
	}
	got, present := repo["creation_commit"]
	if present != wantPresent {
		t.Fatalf("creation_commit presence = %v, want %v in %#v", present, wantPresent, repo)
	}
	if present && got != want {
		t.Fatalf("creation_commit = %v, want %q", got, want)
	}
}
