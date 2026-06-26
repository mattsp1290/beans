package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// compile-time interface satisfaction check for the test double
var _ gitResolver = (*fakeGitResolver)(nil)

// fakeGitResolver is a test double that returns predetermined values without
// touching the filesystem or spawning processes.
type fakeGitResolver struct {
	toplevel   string
	remoteURL  string
	headCommit string
	// err values are returned for every call when non-nil
	toplevelErr   error
	remoteURLErr  error
	headCommitErr error
	// last* fields record the arguments of the most recent call so tests can
	// assert correct wiring (e.g. RemoteURL receives the root returned by
	// Toplevel, not cwd).
	lastToplevelDir    string
	lastRemoteURLRoot  string
	lastHeadCommitRoot string
}

func (f *fakeGitResolver) Toplevel(dir string) (string, bool, error) {
	f.lastToplevelDir = dir
	if f.toplevelErr != nil {
		return "", false, f.toplevelErr
	}
	if f.toplevel == "" {
		return "", false, nil
	}
	return f.toplevel, true, nil
}

func (f *fakeGitResolver) RemoteURL(root string) (string, bool, error) {
	f.lastRemoteURLRoot = root
	if f.remoteURLErr != nil {
		return "", false, f.remoteURLErr
	}
	if f.remoteURL == "" {
		return "", false, nil
	}
	return f.remoteURL, true, nil
}

func (f *fakeGitResolver) HeadCommit(root string) (string, bool, error) {
	f.lastHeadCommitRoot = root
	if f.headCommitErr != nil {
		return "", false, f.headCommitErr
	}
	if f.headCommit == "" {
		return "", false, nil
	}
	return f.headCommit, true, nil
}

func TestFakeGitResolverHeadCommitRecordsCallArgs(t *testing.T) {
	t.Parallel()

	fake := &fakeGitResolver{
		headCommit: "0123456789abcdef0123456789abcdef01234567",
	}

	sha, ok, err := fake.HeadCommit("/repo/root")
	if err != nil || !ok || sha != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("HeadCommit: got (%q, %v, %v), want configured sha", sha, ok, err)
	}
	if fake.lastHeadCommitRoot != "/repo/root" {
		t.Errorf("lastHeadCommitRoot = %q, want /repo/root", fake.lastHeadCommitRoot)
	}
}

func TestRealGitResolverHeadCommitReturnsExactHeadInCommonRepoStates(t *testing.T) {
	root := initGitRepo(t)
	first := commitFile(t, root, "tracked.txt", "base\n", "base")

	assertHeadCommit(t, realGitResolver{}, root, first)

	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatalf("dirty tracked file: %v", err)
	}
	assertHeadCommit(t, realGitResolver{}, root, first)

	runGit(t, root, "checkout", "--detach", "HEAD")
	assertHeadCommit(t, realGitResolver{}, root, first)

	runGit(t, root, "checkout", "main")
	linkedRoot := filepath.Join(t.TempDir(), "linked")
	runGit(t, root, "worktree", "add", "-b", "linked-branch", linkedRoot, "HEAD")
	assertHeadCommit(t, realGitResolver{}, linkedRoot, first)

	submoduleSource := initGitRepo(t)
	submoduleHead := commitFile(t, submoduleSource, "sub.txt", "sub\n", "sub")
	runGit(t, root, "-c", "protocol.file.allow=always", "submodule", "add", submoduleSource, "deps/sub")
	submoduleRoot := filepath.Join(root, "deps", "sub")
	assertHeadCommit(t, realGitResolver{}, submoduleRoot, submoduleHead)
}

func TestRealGitResolverHeadCommitReturnsExactHeadDuringMergeState(t *testing.T) {
	root := initGitRepo(t)
	commitFile(t, root, "conflict.txt", "base\n", "base")
	runGit(t, root, "checkout", "-b", "side")
	side := commitFile(t, root, "conflict.txt", "side\n", "side")
	runGit(t, root, "checkout", "main")
	main := commitFile(t, root, "conflict.txt", "main\n", "main")

	cmd := gitCommand(root, "merge", "side")
	if err := cmd.Run(); err == nil {
		t.Fatal("merge side: want conflict, got success")
	}
	if got := revParseHead(t, root); got != main {
		t.Fatalf("merge state HEAD = %s, want main commit %s; side commit was %s", got, main, side)
	}
	assertHeadCommit(t, realGitResolver{}, root, main)
}

func TestRealGitResolverHeadCommitReturnsExactHeadDuringRebaseState(t *testing.T) {
	root := initGitRepo(t)
	base := commitFile(t, root, "conflict.txt", "base\n", "base")
	runGit(t, root, "checkout", "-b", "topic")
	topic := commitFile(t, root, "conflict.txt", "topic\n", "topic")
	runGit(t, root, "checkout", "main")
	main := commitFile(t, root, "conflict.txt", "main\n", "main")
	runGit(t, root, "checkout", "topic")

	cmd := gitCommand(root, "rebase", "main")
	if err := cmd.Run(); err == nil {
		t.Fatal("rebase main: want conflict, got success")
	}
	want := revParseHead(t, root)
	if want == "" || want == topic || want == base {
		t.Fatalf("rebase state HEAD = %s, want an active rebase HEAD at or from main %s", want, main)
	}
	assertHeadCommit(t, realGitResolver{}, root, want)
}

func TestRealGitResolverHeadCommitBestEffortFailures(t *testing.T) {
	resolver := realGitResolver{}

	for _, tc := range []struct {
		name string
		root string
	}{
		{name: "outside git repo", root: t.TempDir()},
		{name: "unborn head", root: initGitRepo(t)},
		{name: "missing root", root: filepath.Join(t.TempDir(), "missing")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sha, ok, err := resolver.HeadCommit(tc.root)
			if err != nil || ok || sha != "" {
				t.Fatalf("HeadCommit(%s): got (%q, %v, %v), want empty ok=false nil", tc.root, sha, ok, err)
			}
		})
	}
}

func TestIsFullLowercaseHexCommit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		sha  string
		want bool
	}{
		{name: "valid", sha: "0123456789abcdef0123456789abcdef01234567", want: true},
		{name: "short", sha: "0123456789abcdef0123456789abcdef0123456", want: false},
		{name: "long", sha: "0123456789abcdef0123456789abcdef012345678", want: false},
		{name: "uppercase", sha: "0123456789ABCDEF0123456789abcdef01234567", want: false},
		{name: "non hex", sha: "0123456789abcdef0123456789abcdef0123456g", want: false},
		{name: "with newline", sha: "0123456789abcdef0123456789abcdef01234567\n", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isFullLowercaseHexCommit(tc.sha); got != tc.want {
				t.Fatalf("isFullLowercaseHexCommit(%q) = %v, want %v", tc.sha, got, tc.want)
			}
		})
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	return root
}

func commitFile(t *testing.T, root, name, contents, message string) string {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	runGit(t, root, "add", name)
	runGit(t, root, "commit", "-m", message)
	return revParseHead(t, root)
}

func assertHeadCommit(t *testing.T, resolver gitResolver, root, want string) {
	t.Helper()

	sha, ok, err := resolver.HeadCommit(root)
	if err != nil || !ok || sha != want {
		t.Fatalf("HeadCommit(%s): got (%q, %v, %v), want (%q, true, nil)", root, sha, ok, err, want)
	}
}

func revParseHead(t *testing.T, root string) string {
	t.Helper()

	out := runGit(t, root, "rev-parse", "HEAD")
	return strings.TrimSpace(out)
}

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()

	cmd := gitCommand(root, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}

func gitCommand(root string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Beans Test",
		"GIT_AUTHOR_EMAIL=beans@example.invalid",
		"GIT_COMMITTER_NAME=Beans Test",
		"GIT_COMMITTER_EMAIL=beans@example.invalid",
	)
	return cmd
}
