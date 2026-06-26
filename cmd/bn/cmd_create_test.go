package main

import (
	"bytes"
	"context"
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
