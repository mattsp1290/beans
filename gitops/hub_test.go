package gitops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCloneEmptyRemoteInitializesAndMutateSucceeds(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	if a.Branch != "main" {
		t.Fatalf("branch = %q, want main", a.Branch)
	}
	if got := remoteLog(t, remote); len(got) != 1 || got[0] != initialCommitMsg {
		t.Fatalf("remote log = %v", got)
	}
	res, err := a.Mutate(context.Background(), setOp("create", "p-1", "projects/p/issues/p-1.md", "one\n"))
	if err != nil || !res.Pushed || res.SHA == "" {
		t.Fatalf("mutate: %+v, %v", res, err)
	}
	if got := remoteLog(t, remote); got[0] != "bn: create p-1 — projects/p/issues/p-1.md" {
		t.Fatalf("remote log = %v", got)
	}
	st, err := a.Status(context.Background())
	if err != nil || st.Ahead != 0 || st.Behind != 0 || len(st.Dirty) != 0 {
		t.Fatalf("status = %+v, %v", st, err)
	}
	if _, err := os.Stat(a.JournalPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("journal should be cleared")
	}
}

func TestCloneNonEmptyRemoteReadsDefaultBranch(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	newClone(t, remote, "seed")
	b := newClone(t, remote, "b")
	if b.Branch != "main" {
		t.Fatalf("branch = %q", b.Branch)
	}
	if _, err := os.Stat(filepath.Join(b.Dir, "beans.toml")); err != nil {
		t.Fatal("second clone should see the initial commit")
	}
}

func TestRaceRetriesOnceAndKeepsBothOperations(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	b := newClone(t, remote, "b")
	pushed := false
	a.Rec.Before = func(args []string) error {
		if subcommand(args) == "push" && !pushed {
			pushed = true
			if _, err := b.Mutate(context.Background(), setOp("create", "p-b", "projects/p/issues/p-b.md", "b\n")); err != nil {
				t.Fatalf("b mutate: %v", err)
			}
		}
		return nil
	}
	res, err := a.Mutate(context.Background(), setOp("create", "p-a", "projects/p/issues/p-a.md", "a\n"))
	if err != nil || !res.Pushed {
		t.Fatalf("a mutate: %+v %v", res, err)
	}
	if res.Retries != 1 {
		t.Errorf("retries = %d, want 1", res.Retries)
	}
	log := remoteLog(t, remote)
	if len(log) != 3 || !strings.Contains(log[0], "p-a") || !strings.Contains(log[1], "p-b") {
		t.Fatalf("remote log = %v", log)
	}
	if n := strings.Count(strings.Join(log, "\n"), "create p-a"); n != 1 {
		t.Errorf("a's commit appears %d times", n)
	}
}

func TestConflictInReplayIsIdempotent(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	b := newClone(t, remote, "b")
	if _, err := a.Mutate(context.Background(), setOp("create", "p-1", "projects/p/issues/p-1.md", "status: open\n")); err != nil {
		t.Fatal(err)
	}
	b.FetchIfStale(context.Background())
	closeOp := setOp("close", "p-1", "projects/p/issues/p-1.md", "status: closed\n")
	pushed := false
	a.Rec.Before = func(args []string) error {
		if subcommand(args) == "push" && !pushed {
			pushed = true
			if _, err := b.Mutate(context.Background(), closeOp); err != nil {
				t.Fatalf("b close: %v", err)
			}
		}
		return nil
	}
	res, err := a.Mutate(context.Background(), closeOp)
	if err != nil {
		t.Fatalf("a close: %v", err)
	}
	if res.SHA != "" {
		t.Errorf("replay found the file already closed; expected no second commit, got %s", res.SHA)
	}
	log := remoteLog(t, remote)
	if n := strings.Count(strings.Join(log, "\n"), "close p-1"); n != 1 {
		t.Fatalf("expected one close commit, log = %v", log)
	}
}

func TestOfflinePushLeavesLocalCommitAndSyncPushes(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	a.Rec.Before = func(args []string) error {
		if subcommand(args) == "push" {
			return &GitError{Args: args, Code: 128, Stderr: "fatal: unable to access 'https://x/': Could not resolve host: x"}
		}
		return nil
	}
	res, err := a.Mutate(context.Background(), setOp("create", "p-1", "projects/p/issues/p-1.md", "one\n"))
	if err != nil || res.Pushed || res.SHA == "" || !strings.Contains(res.Message, "run bn sync") {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	st, _ := a.Status(context.Background())
	if st.Ahead != 1 {
		t.Fatalf("ahead = %d, want 1", st.Ahead)
	}
	a.Rec.Before = nil
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, _ = a.Status(context.Background())
	if st.Ahead != 0 {
		t.Fatalf("after sync ahead = %d", st.Ahead)
	}
}

func TestThrottleFetchesOncePerWindow(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	a.Clock = func() time.Time { return now }
	a.Throttle = time.Minute
	before := a.Rec.Count("fetch")
	a.FetchIfStale(context.Background())
	a.FetchIfStale(context.Background())
	if got := a.Rec.Count("fetch") - before; got != 1 {
		t.Fatalf("fetches in window = %d, want 1", got)
	}
	now = now.Add(2 * time.Minute)
	a.FetchIfStale(context.Background())
	if got := a.Rec.Count("fetch") - before; got != 2 {
		t.Fatalf("fetches after window = %d, want 2", got)
	}
}

func TestHandEditIsCommittedBeforeOperation(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	if err := os.WriteFile(filepath.Join(a.Dir, "README.md"), []byte("edited by hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Mutate(context.Background(), setOp("create", "p-1", "projects/p/issues/p-1.md", "one\n")); err != nil {
		t.Fatal(err)
	}
	log := remoteLog(t, remote)
	if len(log) != 3 || log[1] != "bn: hand edits" || !strings.HasPrefix(log[0], "bn: create p-1") {
		t.Fatalf("remote log = %v", log)
	}
}

func TestHandEditPlusRaceKeepsHandEdit(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	b := newClone(t, remote, "b")
	if err := os.WriteFile(filepath.Join(a.Dir, "README.md"), []byte("edited by hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pushed := false
	a.Rec.Before = func(args []string) error {
		if subcommand(args) == "push" && !pushed {
			pushed = true
			if _, err := b.Mutate(context.Background(), setOp("create", "p-b", "projects/p/issues/p-b.md", "b\n")); err != nil {
				t.Fatal(err)
			}
		}
		return nil
	}
	res, err := a.Mutate(context.Background(), setOp("create", "p-a", "projects/p/issues/p-a.md", "a\n"))
	if err != nil || !res.Pushed {
		t.Fatalf("%+v %v", res, err)
	}
	log := strings.Join(remoteLog(t, remote), "\n")
	for _, want := range []string{"bn: hand edits", "p-a", "p-b"} {
		if !strings.Contains(log, want) {
			t.Errorf("remote log missing %q:\n%s", want, log)
		}
	}
	content := runGit(t, remote, "show", "main:README.md")
	if content != "edited by hand\n" {
		t.Errorf("hand edit lost at remote: %q", content)
	}
	for _, c := range a.Rec.Commands {
		if subcommand(c) == "reset" {
			t.Errorf("reset must not run when a hand-edit commit exists: %v", c)
		}
	}
}

func TestConflictingOperationWithHandEditExits3WithoutReset(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	b := newClone(t, remote, "b")
	if _, err := a.Mutate(context.Background(), setOp("create", "p-1", "projects/p/issues/p-1.md", "v0\n")); err != nil {
		t.Fatal(err)
	}
	b.FetchIfStale(context.Background())
	if err := os.WriteFile(filepath.Join(a.Dir, "README.md"), []byte("hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pushed := false
	a.Rec.Before = func(args []string) error {
		if subcommand(args) == "push" && !pushed {
			pushed = true
			if _, err := b.Mutate(context.Background(), setOp("update", "p-1", "projects/p/issues/p-1.md", "vB\n")); err != nil {
				t.Fatal(err)
			}
		}
		return nil
	}
	_, err := a.Mutate(context.Background(), setOp("update", "p-1", "projects/p/issues/p-1.md", "vA\n"))
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != ExitGit || !strings.Contains(ee.Msg, "run bn sync") {
		t.Fatalf("err = %v", err)
	}
	local := runGit(t, a.Dir, "log", "--format=%s")
	if !strings.Contains(local, "bn: hand edits") {
		t.Errorf("hand-edit commit lost locally:\n%s", local)
	}
	for _, c := range a.Rec.Commands {
		if subcommand(c) == "reset" {
			t.Errorf("reset --hard ran: %v", c)
		}
	}
}

func TestRetryExhaustionExits3AndKeepsCommit(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	a.Rec.Before = func(args []string) error {
		if subcommand(args) == "push" {
			return &GitError{Args: args, Code: 1, Stderr: " ! [rejected] main -> main (fetch first)\nerror: failed to push some refs"}
		}
		return nil
	}
	_, err := a.Mutate(context.Background(), setOp("create", "p-1", "projects/p/issues/p-1.md", "one\n"))
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != ExitGit || !strings.Contains(ee.Msg, "push rejected 3 times") {
		t.Fatalf("err = %v", err)
	}
	if pushes := a.Rec.Count("push") - 1; pushes != 3 { // minus the initial clone push
		t.Errorf("push attempts = %d, want 3", pushes)
	}
	head := runGit(t, a.Dir, "log", "-1", "--format=%s")
	if !strings.HasPrefix(head, "bn: create p-1") {
		t.Errorf("HEAD = %q, operation commit must remain", head)
	}
}

func TestPartialWriteRecovery(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	panicky := Operation{Verb: "archive", ID: "p-1", Apply: func(hubDir string) ([]string, error) {
		_ = WriteFile(filepath.Join(hubDir, "projects/p/archive/2026/p-1.md"), []byte("moved\n"))
		_ = WriteFile(filepath.Join(hubDir, "projects/p/archive/2026/p-2.md"), []byte("moved\n"))
		panic("simulated kill")
	}}
	func() {
		defer func() { _ = recover() }()
		_, _ = a.Mutate(context.Background(), panicky)
	}()
	if _, err := os.Stat(a.JournalPath()); err != nil {
		t.Fatal("journal must survive the kill")
	}
	// The lock is released with the goroutine's defer even on panic.
	var stderr strings.Builder
	a.Stderr = &stderr
	if _, err := a.Mutate(context.Background(), setOp("create", "p-3", "projects/p/issues/p-3.md", "three\n")); err != nil {
		t.Fatal(err)
	}
	log := remoteLog(t, remote)
	if len(log) != 3 || log[1] != "bn: recovered partial archive p-1" {
		t.Fatalf("remote log = %v", log)
	}
	if !strings.Contains(stderr.String(), "interrupted") {
		t.Errorf("expected a warning, got %q", stderr.String())
	}
}

func TestSameCloneReadDoesNotMergeWhileWriteHoldsLock(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	b := newClone(t, remote, "b")
	if _, err := a.Mutate(context.Background(), setOp("create", "p-1", "projects/p/issues/p-1.md", "v0\n")); err != nil {
		t.Fatal(err)
	}
	b.FetchIfStale(context.Background())
	if _, err := b.Mutate(context.Background(), setOp("update", "p-1", "projects/p/issues/p-1.md", "vB\n")); err != nil {
		t.Fatal(err)
	}
	// A's write blocks inside Apply; a concurrent read must skip the fetch.
	release := make(chan struct{})
	inApply := make(chan struct{})
	blocking := Operation{Verb: "update", ID: "p-1", Apply: func(hubDir string) ([]string, error) {
		close(inApply)
		<-release
		if err := WriteFile(filepath.Join(hubDir, "projects/p/issues/p-1.md"), []byte("vA\n")); err != nil {
			return nil, err
		}
		return []string{"projects/p/issues/p-1.md"}, nil
	}}
	// Make A's clone fetch nothing during Mutate's own pre-rebase by pointing
	// the throttle at a stale state; A's pipeline still fetches B's commit
	// first, so this operation is a conflicting replay on vB.
	done := make(chan error, 1)
	go func() {
		_, err := a.Mutate(context.Background(), blocking)
		done <- err
	}()
	<-inApply
	a.Throttle = 0
	fetchesBefore := a.Rec.Count("fetch")
	a.FetchIfStale(context.Background())
	if a.Rec.Count("fetch") != fetchesBefore {
		t.Error("FetchIfStale must skip the fetch while the lock is held")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("a mutate: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(a.Dir, "projects/p/issues/p-1.md"))
	if string(content) != "vA\n" {
		t.Fatalf("content = %q, want vA", content)
	}
	a.FetchIfStale(context.Background())
	if a.Rec.Count("fetch") != fetchesBefore+1 {
		t.Error("FetchIfStale should fetch once the lock is free")
	}
}

func TestInterruptedRebaseExits3UntilAborted(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	b := newClone(t, remote, "b")
	if _, err := a.Mutate(context.Background(), setOp("create", "p-1", "projects/p/issues/p-1.md", "v0\n")); err != nil {
		t.Fatal(err)
	}
	b.FetchIfStale(context.Background())
	if _, err := b.Mutate(context.Background(), setOp("update", "p-1", "projects/p/issues/p-1.md", "vB\n")); err != nil {
		t.Fatal(err)
	}
	// Create a conflicting local commit in A by hand, then start a rebase and
	// leave it interrupted.
	if err := os.WriteFile(filepath.Join(a.Dir, "projects/p/issues/p-1.md"), []byte("vA\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, a.Dir, "commit", "-aqm", "local")
	runGit(t, a.Dir, "fetch", "-q", "origin")
	if out, err := (ExecRunner{}).Run(context.Background(), a.Dir, "rebase", "origin/main"); err == nil {
		t.Fatalf("rebase should conflict: %s", out)
	}
	_, err := a.Mutate(context.Background(), setOp("create", "p-2", "projects/p/issues/p-2.md", "two\n"))
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != ExitGit || !strings.Contains(ee.Msg, "interrupted rebase") {
		t.Fatalf("err = %v", err)
	}
	if err := a.Preflight(context.Background()); err == nil {
		t.Error("Preflight should report the interrupted rebase for doctor")
	}
	runGit(t, a.Dir, "rebase", "--abort")
	runGit(t, a.Dir, "reset", "-q", "--hard", "origin/main")
	if _, err := a.Mutate(context.Background(), setOp("create", "p-2", "projects/p/issues/p-2.md", "two\n")); err != nil {
		t.Fatalf("after abort: %v", err)
	}
}

func TestOrphanedTmpFilesAreDeletedNotCommitted(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	tmp := filepath.Join(a.Dir, "projects/p/issues/x.md.tmp")
	if err := os.MkdirAll(filepath.Dir(tmp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Mutate(context.Background(), setOp("create", "p-1", "projects/p/issues/p-1.md", "one\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tmp); !errors.Is(err, os.ErrNotExist) {
		t.Error("tmp file should be deleted")
	}
	files := runGit(t, remote, "ls-tree", "-r", "--name-only", "main")
	if strings.Contains(files, ".tmp") {
		t.Errorf("tmp path committed:\n%s", files)
	}
}

func TestDetachedHeadErrorsWithoutCommit(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	runGit(t, a.Dir, "checkout", "-q", "--detach", "HEAD")
	_, err := a.Mutate(context.Background(), setOp("create", "p-1", "projects/p/issues/p-1.md", "one\n"))
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != ExitGit || !strings.Contains(ee.Msg, "detached HEAD") {
		t.Fatalf("err = %v", err)
	}
	if n := len(remoteLog(t, remote)); n != 1 {
		t.Errorf("remote should have only the initial commit, got %d", n)
	}
	runGit(t, a.Dir, "checkout", "-q", "-b", "other")
	_, err = a.Mutate(context.Background(), setOp("create", "p-1", "projects/p/issues/p-1.md", "one\n"))
	if !errors.As(err, &ee) || !strings.Contains(ee.Msg, "hub is on other, expected main") {
		t.Fatalf("err = %v", err)
	}
}

func TestLockTimeoutExits4(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	unlock, err := a.lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	// Use a second Hub value on the same cache dir with a short deadline.
	b := *a.Hub
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err = b.Mutate(ctx, setOp("create", "p-1", "projects/p/issues/p-1.md", "one\n"))
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != ExitLock {
		t.Fatalf("err = %v", err)
	}
}

func TestNoSyncCommitsLocallyOnly(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	a.NoSync = true
	pushes, fetches := a.Rec.Count("push"), a.Rec.Count("fetch")
	res, err := a.Mutate(context.Background(), setOp("create", "p-1", "projects/p/issues/p-1.md", "one\n"))
	if err != nil || res.Pushed || res.Message != "committed locally (--no-sync)" {
		t.Fatalf("%+v %v", res, err)
	}
	if a.Rec.Count("push") != pushes || a.Rec.Count("fetch") != fetches {
		t.Error("no-sync must not fetch or push")
	}
}

func TestSyncConflictAbortsRebaseAndExits3(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	b := newClone(t, remote, "b")
	if _, err := a.Mutate(context.Background(), setOp("create", "p-1", "projects/p/issues/p-1.md", "v0\n")); err != nil {
		t.Fatal(err)
	}
	b.FetchIfStale(context.Background())
	if _, err := b.Mutate(context.Background(), setOp("update", "p-1", "projects/p/issues/p-1.md", "vB\n")); err != nil {
		t.Fatal(err)
	}
	// A commits a conflicting change offline, then syncs.
	a.NoSync = true
	if _, err := a.Mutate(context.Background(), setOp("update", "p-1", "projects/p/issues/p-1.md", "vA\n")); err != nil {
		t.Fatal(err)
	}
	a.NoSync = false
	err := a.Sync(context.Background())
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != ExitGit || !strings.Contains(ee.Msg, "run bn sync") {
		t.Fatalf("err = %v", err)
	}
	if err := a.Preflight(context.Background()); err != nil {
		t.Fatalf("rebase must be aborted so the clone is usable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(a.Dir, ".git", "rebase-merge")); err == nil {
		t.Fatal("rebase-merge left behind")
	}
	head := runGit(t, a.Dir, "log", "-1", "--format=%s")
	if !strings.HasPrefix(head, "bn: update p-1") {
		t.Errorf("local commit lost: %q", head)
	}
}

// TestHandEditSurvivesDroppedOperationCommit is the regression for the
// mislabeled-opCommit bug: A has a hand edit plus an operation, B pushes the
// identical operation first (A's commit becomes empty in the rebase and git
// drops it), then C pushes a conflicting change to the hand-edited file.
// bn must not reset the hand-edit commit away.
func TestHandEditSurvivesDroppedOperationCommit(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	b := newClone(t, remote, "b")
	c := newClone(t, remote, "c")
	if _, err := a.Mutate(context.Background(), setOp("create", "p-1", "projects/p/issues/p-1.md", "open\n")); err != nil {
		t.Fatal(err)
	}
	b.FetchIfStale(context.Background())
	c.FetchIfStale(context.Background())
	if err := os.WriteFile(filepath.Join(a.Dir, "README.md"), []byte("edited by hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	closeOp := setOp("close", "p-1", "projects/p/issues/p-1.md", "closed\n")
	pushes := 0
	a.Rec.Before = func(args []string) error {
		if subcommand(args) != "push" {
			return nil
		}
		pushes++
		switch pushes {
		case 1:
			if _, err := b.Mutate(context.Background(), closeOp); err != nil {
				t.Errorf("b: %v", err)
			}
		case 2:
			if _, err := c.Mutate(context.Background(), setOp("update", "readme", "README.md", "C changed the readme\n")); err != nil {
				t.Errorf("c: %v", err)
			}
		}
		return nil
	}
	_, err := a.Mutate(context.Background(), closeOp)
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != ExitGit {
		t.Fatalf("expected a conflict for the user to resolve, got %v", err)
	}
	for _, cmd := range a.Rec.Commands {
		if subcommand(cmd) == "reset" {
			t.Fatalf("reset --hard ran and would have discarded the hand edit: %v", cmd)
		}
	}
	local := runGit(t, a.Dir, "log", "--format=%s")
	if !strings.Contains(local, "bn: hand edits") {
		t.Fatalf("hand-edit commit lost:\n%s", local)
	}
	if content, _ := os.ReadFile(filepath.Join(a.Dir, "README.md")); string(content) != "edited by hand\n" {
		t.Fatalf("hand edit content lost: %q", content)
	}
}

func TestStrandedCommitIsPushedByNextIdempotentMutate(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	op := setOp("create", "p-1", "projects/p/issues/p-1.md", "one\n")
	// Simulate a kill between commit and push: the commit exists locally,
	// the journal is still on disk, nothing was pushed.
	a.Rec.Before = func(args []string) error {
		if subcommand(args) == "push" {
			panic("killed before push")
		}
		return nil
	}
	func() {
		defer func() { _ = recover() }()
		_, _ = a.Mutate(context.Background(), op)
	}()
	a.Rec.Before = nil
	st, _ := a.Status(context.Background())
	if st.Ahead != 1 {
		t.Fatalf("precondition: ahead = %d", st.Ahead)
	}
	res, err := a.Mutate(context.Background(), op) // idempotent re-run
	if err != nil || !res.Pushed {
		t.Fatalf("stranded commit not pushed: %+v %v", res, err)
	}
	st, _ = a.Status(context.Background())
	if st.Ahead != 0 {
		t.Fatalf("ahead = %d after push", st.Ahead)
	}
	if log := remoteLog(t, remote); !strings.HasPrefix(log[0], "bn: create p-1") {
		t.Fatalf("remote log = %v", log)
	}
}

func TestNoSyncApplyErrorLeavesCleanTree(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	a.NoSync = true
	if err := os.WriteFile(filepath.Join(a.Dir, "README.md"), []byte("hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	failing := Operation{Verb: "create", ID: "p-1", Apply: func(hubDir string) ([]string, error) {
		_ = WriteFile(filepath.Join(hubDir, "projects/p/issues/p-1.md"), []byte("half\n"))
		return nil, errors.New("boom")
	}}
	if _, err := a.Mutate(context.Background(), failing); err == nil {
		t.Fatal("expected the Apply error")
	}
	if clean, _ := a.isClean(context.Background()); !clean {
		st, _ := a.Status(context.Background())
		t.Fatalf("tree left dirty: %v", st.Dirty)
	}
	if _, err := os.Stat(a.JournalPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("journal left behind")
	}
	if content, _ := os.ReadFile(filepath.Join(a.Dir, "README.md")); string(content) != "hand\n" {
		t.Fatalf("hand edit lost: %q", content)
	}
	if local := runGit(t, a.Dir, "log", "-1", "--format=%s"); local != "bn: hand edits\n" {
		t.Fatalf("hand edit should be committed locally, got %q", local)
	}
}

// TestSubjectCollisionCannotDiscardHandEdit: an operation whose subject
// renders exactly as the reserved "bn: hand edits" must not let the discard
// check mistake a user's hand-edit commit for the operation commit.
func TestSubjectCollisionCannotDiscardHandEdit(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	b := newClone(t, remote, "b")
	c := newClone(t, remote, "c")
	collide := func(content string) Operation {
		return Operation{Verb: "hand", ID: "edits", Apply: func(hubDir string) ([]string, error) {
			p := filepath.Join(hubDir, "projects/p/issues/p-1.md")
			if cur, err := os.ReadFile(p); err == nil && string(cur) == content {
				return nil, nil
			}
			return []string{"projects/p/issues/p-1.md"}, WriteFile(p, []byte(content))
		}}
	}
	if _, err := a.Mutate(context.Background(), collide("v0\n")); err != nil {
		t.Fatal(err)
	}
	b.FetchIfStale(context.Background())
	c.FetchIfStale(context.Background())
	if err := os.WriteFile(filepath.Join(a.Dir, "README.md"), []byte("edited by hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pushes := 0
	a.Rec.Before = func(args []string) error {
		if subcommand(args) != "push" {
			return nil
		}
		pushes++
		switch pushes {
		case 1:
			if _, err := b.Mutate(context.Background(), collide("v1\n")); err != nil {
				t.Errorf("b: %v", err)
			}
		case 2:
			if _, err := c.Mutate(context.Background(), setOp("update", "readme", "README.md", "C changed the readme\n")); err != nil {
				t.Errorf("c: %v", err)
			}
		}
		return nil
	}
	_, err := a.Mutate(context.Background(), collide("v1\n"))
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != ExitGit {
		t.Fatalf("expected a conflict, got %v", err)
	}
	for _, cmd := range a.Rec.Commands {
		if subcommand(cmd) == "reset" {
			t.Fatalf("reset --hard ran: %v", cmd)
		}
	}
	if content, _ := os.ReadFile(filepath.Join(a.Dir, "README.md")); string(content) != "edited by hand\n" {
		t.Fatalf("hand edit content lost: %q", content)
	}
}

func TestOperationCommitCarriesNonceAndSurvivesRebaseDiscardCheck(t *testing.T) {
	t.Parallel()
	remote := newRemote(t)
	a := newClone(t, remote, "a")
	res, err := a.Mutate(context.Background(), setOp("create", "p-1", "projects/p/issues/p-1.md", "one\n"))
	if err != nil {
		t.Fatal(err)
	}
	body := runGit(t, a.Dir, "log", "-1", "--format=%B", res.SHA)
	if !strings.Contains(body, nonceTrailer+": ") || !strings.HasPrefix(body, "bn: create p-1") {
		t.Fatalf("commit message = %q", body)
	}
}
