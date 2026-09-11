package gitops

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"
)

// Exit codes shared with the CLI.
const (
	ExitGit  = 3 // conflict, push failure after retries, detached HEAD, interrupted rebase
	ExitLock = 4 // another bn holds the hub lock
)

// ExitError is an error that maps to a specific process exit code.
type ExitError struct {
	Code int
	Msg  string
}

func (e *ExitError) Error() string { return e.Msg }

// Cache file names under CacheDir.
const (
	lockFile         = "hub.lock"
	lastFetchFile    = "last-fetch"
	lastAttemptFile  = "last-fetch-attempt"
	journalFile      = "op-journal.json"
	maxPushAttempts  = 3
	lockTimeout      = 30 * time.Second
	lockRetryDelay   = 50 * time.Millisecond
	initialCommitMsg = "bn: initialize hub"
)

// Hub is one clone of the hub repository plus the cache directory that holds
// its lock and fetch bookkeeping.
type Hub struct {
	Dir      string        // the clone
	CacheDir string        // ~/.beans/cache
	Branch   string        // default branch recorded at clone time
	Runner   Runner        // defaults to ExecRunner
	Throttle time.Duration // minimum interval between fetches on reads
	Clock    func() time.Time
	Actor    string    // commit author name
	Stderr   io.Writer // notices; defaults to os.Stderr
	NoSync   bool      // skip fetch/rebase before and push after a mutation

	identOnce  sync.Once
	identName  string
	identEmail string
}

// Operation is one mutation of the hub. Apply must be idempotent and must
// re-read the files it changes every time it runs, because the pipeline can
// run it again on a different tree after a push race.
type Operation struct {
	Verb    string // create, update, close, ...
	ID      string // issue id or other subject; may be empty
	Summary string // one line for the commit message
	Apply   func(hubDir string) (changedPaths []string, err error)
	// RequireFreshBase rejects offline publication. It is for replacement
	// operations whose snapshot cannot safely be replayed later.
	RequireFreshBase bool
}

// Result reports what Mutate did.
type Result struct {
	SHA     string // operation commit; empty when Apply changed nothing
	Pushed  bool
	Message string // notice for the user when not pushed
	Retries int    // push rejections that were recovered from
}

// Status is the hub clone's state for bn status.
type Status struct {
	Dir       string
	Remote    string
	Branch    string
	Ahead     int
	Behind    int
	Dirty     []string
	LastFetch time.Time
}

type journal struct {
	Verb    string    `json:"verb"`
	ID      string    `json:"id"`
	Summary string    `json:"summary"`
	Started time.Time `json:"started"`
}

func (h *Hub) runner() Runner {
	if h.Runner == nil {
		return ExecRunner{}
	}
	return h.Runner
}

func (h *Hub) now() time.Time {
	if h.Clock != nil {
		return h.Clock()
	}
	return time.Now()
}

func (h *Hub) stderr() io.Writer {
	if h.Stderr == nil {
		return os.Stderr
	}
	return h.Stderr
}

// git runs a git command in the hub with the actor's identity configured,
// because rebases and commits need a committer and a fresh machine (or a CI
// runner) may have no global user.name/user.email.
func (h *Hub) git(ctx context.Context, args ...string) (string, error) {
	name, email := h.identity(ctx)
	full := append([]string{"-c", "user.name=" + name, "-c", "user.email=" + email}, args...)
	return h.runner().Run(ctx, h.Dir, full...)
}

// identity returns the commit identity: the sanitized actor as the name and
// the hub clone's user.email when set, else <actor>@bn.local.
func (h *Hub) identity(ctx context.Context) (name, email string) {
	h.identOnce.Do(func() {
		n := strings.Map(func(r rune) rune {
			if r == '<' || r == '>' || r == '\n' || r == '\r' {
				return -1
			}
			return r
		}, h.Actor)
		n = strings.TrimSpace(n)
		if n == "" {
			n = "bn"
		}
		e := ""
		if out, err := h.runner().Run(ctx, h.Dir, "config", "--get", "user.email"); err == nil {
			e = strings.TrimSpace(out)
		}
		if e == "" {
			e = strings.ReplaceAll(n, " ", "-") + "@bn.local"
		}
		h.identName, h.identEmail = n, e
	})
	return h.identName, h.identEmail
}

func (h *Hub) cachePath(name string) string { return filepath.Join(h.CacheDir, name) }

// LockPath is the flock target.
func (h *Hub) LockPath() string { return h.cachePath(lockFile) }

// JournalPath is where an in-flight operation is recorded.
func (h *Hub) JournalPath() string { return h.cachePath(journalFile) }

// ---------------------------------------------------------------------------
// Clone
// ---------------------------------------------------------------------------

// Clone clones remote into dir and returns the default branch name. An empty
// remote receives an initial commit (README, .gitignore, beans.toml, and the
// top-level directories) pushed with -u. Clone fails when no branch name can
// be determined.
func Clone(ctx context.Context, r Runner, remote, dir string) (branch string, err error) {
	if r == nil {
		r = ExecRunner{}
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	if _, err := r.Run(ctx, filepath.Dir(dir), "clone", "--quiet", remote, dir); err != nil {
		return "", fmt.Errorf("clone %s: %w", remote, err)
	}
	if out, err := r.Run(ctx, dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		branch = strings.TrimPrefix(strings.TrimSpace(out), "origin/")
		if branch != "" {
			return branch, nil
		}
	}
	// Empty remote: no origin/HEAD. Use the unborn branch git chose.
	if _, err := r.Run(ctx, dir, "rev-parse", "--verify", "--quiet", "HEAD"); err == nil {
		return "", errors.New("clone: remote has commits but no default branch; set origin/HEAD with git remote set-head origin -a")
	}
	if out, err := r.Run(ctx, dir, "config", "--get", "init.defaultBranch"); err == nil {
		branch = strings.TrimSpace(out)
	}
	if branch == "" {
		branch = "main"
	}
	if _, err := r.Run(ctx, dir, "checkout", "--quiet", "-B", branch); err != nil {
		return "", err
	}
	if err := writeInitialHub(dir); err != nil {
		return "", err
	}
	if _, err := r.Run(ctx, dir, "add", "-A"); err != nil {
		return "", err
	}
	if _, err := r.Run(ctx, dir, "-c", "user.name=bn", "-c", "user.email=bn@bn.local", "commit", "--quiet", "-m", initialCommitMsg); err != nil {
		return "", err
	}
	if _, err := r.Run(ctx, dir, "push", "--quiet", "-u", "origin", "HEAD"); err != nil {
		return "", fmt.Errorf("push initial commit: %w", err)
	}
	return branch, nil
}

// InitialHubFiles lists what an empty hub is seeded with.
var InitialHubFiles = map[string]string{
	"README.md":         "# beans hub\n\nThis repository is managed by `bn`. Issues live under `projects/<name>/issues/`,\ndocs under `docs/`, memories under `memories/`. Edit files freely; the next\n`bn` command commits hand edits as `bn: hand edits`.\n\nOpen this directory as an Obsidian vault to browse it.\n",
	".gitignore":        "*.tmp\n.obsidian/workspace*.json\n.DS_Store\n",
	"beans.toml":        "[workflow]\nstatuses = [\"open\", \"in_progress\", \"ready_for_review\", \"ready_for_validation\", \"ready_for_merge\", \"blocked\", \"closed\", \"done\"]\ndefault = \"open\"\nactive = [\"open\"]\nterminal = [\"closed\", \"done\"]\n\n[types]\nnames = [\"task\", \"bug\", \"feature\", \"epic\", \"chore\"]\n\n[ids]\nlength = 4\n",
	"docs/.gitkeep":     "",
	"memories/.gitkeep": "",
	"projects/.gitkeep": "",
}

func writeInitialHub(dir string) error {
	for name, content := range InitialHubFiles {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Locking and cache files
// ---------------------------------------------------------------------------

func (h *Hub) newLock() (*flock.Flock, error) {
	if err := os.MkdirAll(h.CacheDir, 0o755); err != nil {
		return nil, err
	}
	return flock.New(h.LockPath()), nil
}

// lock blocks up to lockTimeout for the exclusive hub lock.
func (h *Hub) lock(ctx context.Context) (func(), error) {
	fl, err := h.newLock()
	if err != nil {
		return nil, err
	}
	lctx, cancel := context.WithTimeout(ctx, lockTimeout)
	defer cancel()
	ok, err := fl.TryLockContext(lctx, lockRetryDelay)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	if !ok {
		return nil, &ExitError{Code: ExitLock, Msg: "another bn is running on this hub (lock held for more than 30s)"}
	}
	return func() { _ = fl.Unlock() }, nil
}

// tryLock acquires the lock without waiting; ok is false when it is busy.
func (h *Hub) tryLock() (unlock func(), ok bool, err error) {
	fl, err := h.newLock()
	if err != nil {
		return nil, false, err
	}
	ok, err = fl.TryLock()
	if err != nil || !ok {
		return nil, false, err
	}
	return func() { _ = fl.Unlock() }, true, nil
}

func (h *Hub) readTime(name string) time.Time {
	data, err := os.ReadFile(h.cachePath(name))
	if err != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return time.Time{}
	}
	return t
}

func (h *Hub) writeTime(name string, t time.Time) {
	_ = os.MkdirAll(h.CacheDir, 0o755)
	_ = os.WriteFile(h.cachePath(name), []byte(t.UTC().Format(time.RFC3339)+"\n"), 0o644)
}

// LastFetch returns the time of the last successful fetch, zero when none.
func (h *Hub) LastFetch() time.Time { return h.readTime(lastFetchFile) }

func (h *Hub) readJournal() (journal, bool) {
	data, err := os.ReadFile(h.JournalPath())
	if err != nil {
		return journal{}, false
	}
	var j journal
	if err := json.Unmarshal(data, &j); err != nil {
		return journal{Verb: "operation"}, true
	}
	return j, true
}

func (h *Hub) writeJournal(op Operation) error {
	data, err := json.Marshal(journal{Verb: op.Verb, ID: op.ID, Summary: op.Summary, Started: h.now().UTC()})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(h.CacheDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(h.JournalPath(), data, 0o644)
}

func (h *Hub) clearJournal() { _ = os.Remove(h.JournalPath()) }

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// FetchIfStale fetches origin and fast-forwards a clean worktree when the
// last fetch attempt is older than Throttle. It never blocks on the lock and
// never fails a read: network errors are one line on stderr.
func (h *Hub) FetchIfStale(ctx context.Context) {
	if h.Throttle > 0 {
		if last := h.readTime(lastAttemptFile); !last.IsZero() && h.now().Sub(last) < h.Throttle {
			return
		}
	}
	unlock, ok, err := h.tryLock()
	if err != nil || !ok {
		return
	}
	defer unlock()
	h.writeTime(lastAttemptFile, h.now())
	if _, err := h.git(ctx, "fetch", "--quiet", "origin"); err != nil {
		fmt.Fprintf(h.stderr(), "bn: fetch failed, using local hub: %s\n", firstLine(err.Error()))
		return
	}
	if clean, _ := h.isClean(ctx); clean {
		if _, err := h.git(ctx, "merge", "--ff-only", "--quiet", "origin/"+h.Branch); err != nil {
			fmt.Fprintf(h.stderr(), "bn: local hub has diverged from origin; run bn sync (%s)\n", firstLine(err.Error()))
		}
	}
	h.writeTime(lastFetchFile, h.now())
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func (h *Hub) isClean(ctx context.Context) (bool, error) {
	out, err := h.git(ctx, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "", nil
}

// ---------------------------------------------------------------------------
// Preflight
// ---------------------------------------------------------------------------

// Preflight reports an interrupted rebase or merge, a detached HEAD, or a
// checkout of the wrong branch. bn doctor calls it too.
func (h *Hub) Preflight(ctx context.Context) error {
	for _, marker := range []string{"rebase-merge", "rebase-apply", "MERGE_HEAD"} {
		if _, err := os.Stat(filepath.Join(h.Dir, ".git", marker)); err == nil {
			return &ExitError{Code: ExitGit, Msg: fmt.Sprintf("hub has an interrupted rebase or merge; run git -C %s rebase --abort (or merge --abort) and retry", h.Dir)}
		}
	}
	out, err := h.git(ctx, "symbolic-ref", "--short", "-q", "HEAD")
	branch := strings.TrimSpace(out)
	if err != nil || branch == "" {
		return &ExitError{Code: ExitGit, Msg: fmt.Sprintf("hub is on a detached HEAD, expected %s; run git -C %s checkout %s", h.Branch, h.Dir, h.Branch)}
	}
	if branch != h.Branch {
		return &ExitError{Code: ExitGit, Msg: fmt.Sprintf("hub is on %s, expected %s; run git -C %s checkout %s", branch, h.Branch, h.Dir, h.Branch)}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Mutate
// ---------------------------------------------------------------------------

var errConflict = &ExitError{Code: ExitGit, Msg: "hub has local commits that conflict with origin; run bn sync"}

// Mutate runs the write pipeline: lock, commit stray hand edits, rebase onto
// origin, apply, commit, push with replay on rejection.
//
// Only a commit this run created can ever be discarded. After a successful
// rebase the operation is re-derived by running Apply again on the rebased
// tree: if the operation commit survived, Apply changes nothing; if git
// dropped it as empty because another writer made the same change, Apply
// changes nothing either and the remaining local commits (hand edits,
// offline work) are pushed as they are.
func (h *Hub) Mutate(ctx context.Context, op Operation) (Result, error) {
	if op.Apply == nil {
		return Result{}, errors.New("operation has no Apply")
	}
	unlock, err := h.lock(ctx)
	if err != nil {
		return Result{}, err
	}
	defer unlock()

	if err := h.Preflight(ctx); err != nil {
		return Result{}, err
	}
	// Hand edits are committed even with --no-sync so Apply always starts
	// from a clean tree and a failed Apply can be rolled back safely.
	if err := h.commitStrays(ctx); err != nil {
		return Result{}, err
	}
	if op.RequireFreshBase {
		if h.NoSync {
			return Result{}, &ExitError{Code: ExitGit, Msg: "operation requires a fresh remote base; --no-sync is not allowed"}
		}
		if err := h.requireFreshBase(ctx); err != nil {
			return Result{}, err
		}
	} else if !h.NoSync {
		if err := h.fetchAndRebase(ctx); err != nil {
			return Result{}, err
		}
	}

	var res Result
	subject := opSubject(op)
	nonce := newNonce()
	opCommit := "" // SHA of this run's operation commit when it is HEAD; "" when unknown
	needApply := true
	attempts := 0
	for {
		if needApply {
			sha, err := h.applyAndCommit(ctx, op, subject, nonce)
			if err != nil {
				return Result{}, err
			}
			switch {
			case sha != "":
				opCommit = sha
				res.SHA = sha
			case h.NoSync || h.aheadCount(ctx) == 0:
				h.clearJournal()
				return Result{}, nil // idempotent: nothing changed, nothing stranded
			default:
				// Nothing new, but earlier local commits (this run's operation
				// commit after a rebase, an interrupted run's commit, offline
				// work) are still unpushed: push them. Only a commit carrying
				// this run's nonce may later be discarded.
				opCommit = h.soleLocalCommitWithNonce(ctx, nonce)
				res.SHA = opCommit
			}
		}
		if h.NoSync {
			h.clearJournal()
			res.Message = "committed locally (--no-sync)"
			return res, nil
		}
		attempts++
		_, err := h.git(ctx, "push", "--quiet", "origin", "HEAD:"+h.Branch)
		if err == nil {
			res.Pushed = true
			h.clearJournal()
			h.writeTime(lastFetchFile, h.now())
			h.writeTime(lastAttemptFile, h.now())
			return res, nil
		}
		if !IsPushRejected(err) {
			h.clearJournal()
			res.Message = fmt.Sprintf("committed locally; push failed: %s; run bn sync", firstLine(err.Error()))
			return res, nil
		}
		if attempts >= maxPushAttempts {
			h.clearJournal()
			return res, &ExitError{Code: ExitGit, Msg: fmt.Sprintf("push rejected %d times; another writer is racing this hub; your change is committed locally as %s; run bn sync", maxPushAttempts, short(res.SHA))}
		}
		res.Retries++
		if _, err := h.git(ctx, "fetch", "--quiet", "origin"); err != nil {
			h.clearJournal()
			res.Message = fmt.Sprintf("committed locally; push rejected and fetch failed: %s; run bn sync", firstLine(err.Error()))
			return res, nil
		}
		if _, err := h.git(ctx, "rebase", "--quiet", "origin/"+h.Branch); err == nil {
			if h.aheadCount(ctx) == 0 {
				h.clearJournal()
				h.writeTime(lastFetchFile, h.now())
				return Result{Pushed: true, Retries: res.Retries, Message: "already applied by another writer"}, nil
			}
			// Re-derive the operation on the rebased tree; see the doc comment.
			opCommit = ""
			needApply = true
			continue
		}
		_, _ = h.git(ctx, "rebase", "--abort")
		// Only bn's own operation commit may be discarded and re-applied; a
		// hand-edit, recovered-partial, or offline commit is the user's.
		if opCommit != "" && h.aheadCount(ctx) == 1 && h.head(ctx) == opCommit {
			if _, err := h.git(ctx, "reset", "--hard", "--quiet", "origin/"+h.Branch); err != nil {
				h.clearJournal()
				return res, err
			}
			opCommit = ""
			needApply = true
			continue
		}
		h.clearJournal()
		return res, errConflict
	}
}

func (h *Hub) requireFreshBase(ctx context.Context) error {
	h.writeTime(lastAttemptFile, h.now())
	if _, err := h.git(ctx, "fetch", "--quiet", "origin"); err != nil {
		return &ExitError{Code: ExitGit, Msg: "operation requires a fresh remote base; fetch failed: " + firstLine(err.Error())}
	}
	if _, err := h.git(ctx, "rebase", "--quiet", "origin/"+h.Branch); err != nil {
		_, _ = h.git(ctx, "rebase", "--abort")
		return errConflict
	}
	h.writeTime(lastFetchFile, h.now())
	return nil
}

// opSubject is the commit subject of an operation.
func opSubject(op Operation) string {
	msg := "bn: " + strings.TrimSpace(op.Verb+" "+op.ID)
	if op.Summary != "" {
		msg += " — " + op.Summary
	}
	return msg
}

// aheadCount returns how many local commits are not on origin/<branch>.
func (h *Hub) aheadCount(ctx context.Context) int {
	out, err := h.git(ctx, "rev-list", "--count", "origin/"+h.Branch+"..HEAD")
	if err != nil {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return -1
	}
	return n
}

func (h *Hub) head(ctx context.Context) string {
	out, _ := h.git(ctx, "rev-parse", "HEAD")
	return strings.TrimSpace(out)
}

// soleLocalCommitWithNonce returns HEAD when it is the only local commit
// and carries this run's nonce trailer (the operation commit, possibly
// rebased); otherwise "". Hand-edit, recovered-partial, and other runs'
// commits never carry it, so no subject collision can misidentify them.
func (h *Hub) soleLocalCommitWithNonce(ctx context.Context, nonce string) string {
	if h.aheadCount(ctx) != 1 {
		return ""
	}
	out, err := h.git(ctx, "log", "-1", "--format=%(trailers:key="+nonceTrailer+",valueonly)")
	if err != nil || strings.TrimSpace(out) != nonce {
		return ""
	}
	return h.head(ctx)
}

// nonceTrailer is the commit-message trailer that ties an operation commit
// to the Mutate run that created it.
const nonceTrailer = "Bn-Run"

func newNonce() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

// commitStrays commits uncommitted changes as hand edits (or as a recovered
// partial operation when a journal is present) after deleting orphaned .tmp
// files, so the rebase that follows runs on a clean tree.
func (h *Hub) commitStrays(ctx context.Context) error {
	if err := h.removeTempFiles(); err != nil {
		return err
	}
	clean, err := h.isClean(ctx)
	if err != nil {
		return err
	}
	if clean {
		h.clearJournal()
		return nil
	}
	msg := "bn: hand edits"
	if j, ok := h.readJournal(); ok {
		subject := strings.TrimSpace(j.Verb + " " + j.ID)
		msg = "bn: recovered partial " + subject
		fmt.Fprintf(h.stderr(), "bn: warning: a previous %s was interrupted; committing its partial changes as %q\n", subject, msg)
	}
	if _, err := h.git(ctx, "add", "-A"); err != nil {
		return err
	}
	if err := h.commit(ctx, msg); err != nil {
		return err
	}
	h.clearJournal()
	return nil
}

func (h *Hub) fetchAndRebase(ctx context.Context) error {
	h.writeTime(lastAttemptFile, h.now())
	if _, err := h.git(ctx, "fetch", "--quiet", "origin"); err != nil {
		// Offline: work on the local tip; the push step reports the failure.
		fmt.Fprintf(h.stderr(), "bn: fetch failed, working offline: %s\n", firstLine(err.Error()))
		return nil
	}
	if _, err := h.git(ctx, "rebase", "--quiet", "origin/"+h.Branch); err != nil {
		_, _ = h.git(ctx, "rebase", "--abort")
		return errConflict
	}
	h.writeTime(lastFetchFile, h.now())
	return nil
}

// applyAndCommit journals, applies the operation, stages its paths, and
// commits. It returns "" when nothing was staged.
func (h *Hub) applyAndCommit(ctx context.Context, op Operation, subject, nonce string) (string, error) {
	if err := h.writeJournal(op); err != nil {
		return "", err
	}
	paths, err := op.Apply(h.Dir)
	if err != nil {
		// The tree was clean before Apply (commitStrays ran): drop whatever
		// it half-wrote so the failure leaves no trace.
		_, _ = h.git(ctx, "reset", "--hard", "--quiet", "HEAD")
		_, _ = h.git(ctx, "clean", "-fdq")
		h.clearJournal()
		return "", err
	}
	if len(paths) == 0 {
		return "", nil
	}
	args := append([]string{"add", "-A", "--"}, paths...)
	if _, err := h.git(ctx, args...); err != nil {
		return "", err
	}
	if _, err := h.git(ctx, "diff", "--cached", "--quiet"); err == nil {
		return "", nil // nothing staged
	}
	if err := h.commit(ctx, subject, nonceTrailer+": "+nonce); err != nil {
		return "", err
	}
	sha, err := h.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(sha), nil
}

func (h *Hub) commit(ctx context.Context, msg string, trailers ...string) error {
	name, email := h.identity(ctx)
	args := []string{"commit", "--quiet", "-m", msg}
	for _, t := range trailers {
		args = append(args, "-m", t)
	}
	args = append(args, "--author", name+" <"+email+">")
	_, err := h.git(ctx, args...)
	return err
}

// removeTempFiles deletes only named temporary files created by Beans. User
// files ending in .tmp are ordinary hub content and must survive.
func (h *Hub) removeTempFiles() error {
	return filepath.WalkDir(h.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".bn-write-") || strings.HasPrefix(d.Name(), ".bn-plan-") || d.Name() == "plan.md.tmp" {
			return os.Remove(p)
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// Sync and Status
// ---------------------------------------------------------------------------

// Sync pulls with rebase and pushes. It also commits stray hand edits first,
// like Mutate, so a dirty tree never blocks the rebase.
func (h *Hub) Sync(ctx context.Context) error {
	unlock, err := h.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	if err := h.Preflight(ctx); err != nil {
		return err
	}
	if err := h.commitStrays(ctx); err != nil {
		return err
	}
	h.writeTime(lastAttemptFile, h.now())
	if _, err := h.git(ctx, "fetch", "--quiet", "origin"); err != nil {
		return &ExitError{Code: ExitGit, Msg: "fetch failed: " + firstLine(err.Error())}
	}
	if _, err := h.git(ctx, "rebase", "--quiet", "origin/"+h.Branch); err != nil {
		_, _ = h.git(ctx, "rebase", "--abort")
		return errConflict
	}
	h.writeTime(lastFetchFile, h.now())
	if _, err := h.git(ctx, "push", "--quiet", "origin", "HEAD:"+h.Branch); err != nil {
		return &ExitError{Code: ExitGit, Msg: "push failed: " + firstLine(err.Error())}
	}
	return nil
}

// Status reads ahead/behind, dirty paths, and the last fetch time.
func (h *Hub) Status(ctx context.Context) (Status, error) {
	st := Status{Dir: h.Dir, Branch: h.Branch, LastFetch: h.LastFetch()}
	if out, err := h.git(ctx, "remote", "get-url", "origin"); err == nil {
		st.Remote = strings.TrimSpace(out)
	}
	if out, err := h.git(ctx, "symbolic-ref", "--short", "-q", "HEAD"); err == nil && strings.TrimSpace(out) != "" {
		st.Branch = strings.TrimSpace(out)
	}
	if out, err := h.git(ctx, "rev-list", "--left-right", "--count", "HEAD...origin/"+h.Branch); err == nil {
		fields := strings.Fields(out)
		if len(fields) == 2 {
			st.Ahead, _ = strconv.Atoi(fields[0])
			st.Behind, _ = strconv.Atoi(fields[1])
		}
	}
	out, err := h.git(ctx, "status", "--porcelain")
	if err != nil {
		return st, err
	}
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if l != "" {
			st.Dirty = append(st.Dirty, l)
		}
	}
	return st, nil
}

// ---------------------------------------------------------------------------
// File helpers for operations
// ---------------------------------------------------------------------------

// WriteFile writes data to path through a temporary file and a rename so a
// file is never left half-written. Parent directories are created.
func WriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".bn-write-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// WritePlanFile atomically replaces a manifest without staging an artifact
// inside the strict plan bundle. The temporary is in the bundle parent.
func WritePlanFile(path string, data []byte) error {
	parent := filepath.Dir(filepath.Dir(path))
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(parent, ".bn-plan-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// RecoverPlanTemp removes only the legacy temporary filename previously
// emitted by WriteFile for a plan manifest.
func RecoverPlanTemp(path string) error {
	err := os.Remove(path + ".tmp")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
