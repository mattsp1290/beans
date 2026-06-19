package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	store "github.com/mattsp1290/beans/store"
)

// TestTryGitAutoDetectSurfacesRegisterError verifies that when AutoRegisterRepo
// fails inside a real git repo (here: the store is closed), tryGitAutoDetect
// stays best-effort (returns nil, leaves resolvedRepo unset) but writes a
// diagnostic to rs.stderr so the failure does not surface only as a downstream
// "prefix required" error.
func TestTryGitAutoDetectSurfacesRegisterError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s, _ := newTestStore(t, "", "")
	s.Close() // force AutoRegisterRepo to fail

	var stderr bytes.Buffer
	rs := &appState{
		store:  s,
		actor:  "test",
		stderr: &stderr,
		git: &fakeGitResolver{
			toplevel:  "/home/alice/myapp",
			remoteURL: "https://github.com/alice/myapp",
		},
	}

	if err := rs.tryGitAutoDetect(ctx); err != nil {
		t.Fatalf("tryGitAutoDetect should stay best-effort, got err: %v", err)
	}
	if rs.resolvedRepo != nil {
		t.Fatal("resolvedRepo should be nil when registration failed")
	}
	if got := stderr.String(); !strings.Contains(got, "could not auto-register repo") {
		t.Fatalf("expected diagnostic on stderr, got %q", got)
	}
}

// TestTryGitAutoDetectRegistersNewRepo verifies that tryGitAutoDetect calls
// AutoRegisterRepo with the git remote URL, sets rs.resolvedRepo, and sets
// rs.prefix (when previously empty) to the registered repo's prefix.
func TestTryGitAutoDetectRegistersNewRepo(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s, _ := newTestStore(t, "", "")

	rs := &appState{
		store: s,
		actor: "test",
		git: &fakeGitResolver{
			toplevel:  "/home/alice/myapp",
			remoteURL: "https://github.com/alice/myapp",
		},
	}

	if err := rs.tryGitAutoDetect(ctx); err != nil {
		t.Fatalf("tryGitAutoDetect: %v", err)
	}
	if rs.resolvedRepo == nil {
		t.Fatal("resolvedRepo is nil after tryGitAutoDetect with remote URL")
	}
	if rs.prefix == "" {
		t.Fatal("prefix is empty after tryGitAutoDetect with remote URL")
	}
	// Under topology (a): prefix == slug.
	if rs.prefix != rs.resolvedRepo.Slug {
		t.Fatalf("prefix %q != slug %q — topology-a invariant violated", rs.prefix, rs.resolvedRepo.Slug)
	}

	// Repo must actually exist in the store.
	got, err := s.GetRepoByRemoteURL(ctx, "https://github.com/alice/myapp")
	if err != nil {
		t.Fatalf("GetRepoByRemoteURL after auto-detect: %v", err)
	}
	if got.Slug != rs.resolvedRepo.Slug {
		t.Fatalf("store slug %q != resolvedRepo slug %q", got.Slug, rs.resolvedRepo.Slug)
	}
}

// TestTryGitAutoDetectLocalOnlyRepo verifies that a git repo with no remote
// gets a synthetic file:// URL and is successfully registered.
func TestTryGitAutoDetectLocalOnlyRepo(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s, _ := newTestStore(t, "", "")

	rs := &appState{
		store: s,
		actor: "test",
		git: &fakeGitResolver{
			toplevel:  "/home/alice/local-only",
			remoteURL: "", // no remote
		},
	}

	if err := rs.tryGitAutoDetect(ctx); err != nil {
		t.Fatalf("tryGitAutoDetect local-only: %v", err)
	}
	if rs.resolvedRepo == nil {
		t.Fatal("resolvedRepo is nil for local-only repo — file:// synthesis must register")
	}
	// The registered URL should be discoverable under the synthesized file:// key.
	got, err := s.GetRepoByRemoteURL(ctx, "file:///home/alice/local-only")
	if err != nil {
		t.Fatalf("GetRepoByRemoteURL file:///home/alice/local-only: %v", err)
	}
	if got.Slug != rs.resolvedRepo.Slug {
		t.Fatalf("store slug %q != resolvedRepo slug %q", got.Slug, rs.resolvedRepo.Slug)
	}
}

// TestTryGitAutoDetectDoesNotOverwritePrefix verifies that when rs.prefix is
// already set (e.g. from --project flag), auto-detect keeps it.
func TestTryGitAutoDetectDoesNotOverwritePrefix(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s, _ := newTestStore(t, "", "")

	const existingPrefix = "already-set"
	rs := &appState{
		store:  s,
		actor:  "test",
		prefix: existingPrefix,
		git: &fakeGitResolver{
			toplevel:  "/home/alice/myapp",
			remoteURL: "https://github.com/alice/myapp",
		},
	}

	if err := rs.tryGitAutoDetect(ctx); err != nil {
		t.Fatalf("tryGitAutoDetect: %v", err)
	}
	if rs.prefix != existingPrefix {
		t.Fatalf("prefix overwritten: got %q, want %q", rs.prefix, existingPrefix)
	}
	// resolvedRepo should still be set by auto-detect even though prefix was kept.
	if rs.resolvedRepo == nil {
		t.Fatal("resolvedRepo should be set even when prefix is preserved")
	}
}

// TestResolveRepoContextCachesResult verifies that resolveRepoContext skips
// tryGitAutoDetect when rs.resolvedRepo is already set (avoid double register).
func TestResolveRepoContextCachesResult(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s, existingRepo := newTestStore(t, "", "https://github.com/alice/cached")
	if existingRepo == nil {
		t.Fatal("newTestStore did not return a repo")
	}
	if err := s.EnsureProject(ctx, existingRepo.Prefix); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	callCount := 0
	countingGit := &countingFakeGitResolver{fakeGitResolver: &fakeGitResolver{
		toplevel:  "/home/alice/other",
		remoteURL: "https://github.com/alice/other",
	}, calls: &callCount}

	rs := &appState{
		store:        s,
		actor:        "test",
		prefix:       existingRepo.Prefix,
		resolvedRepo: existingRepo, // pre-populated cache
		git:          countingGit,
	}

	repo, err := rs.resolveRepoContext(ctx)
	if err != nil {
		t.Fatalf("resolveRepoContext: %v", err)
	}
	if repo.Slug != existingRepo.Slug {
		t.Fatalf("resolveRepoContext returned %q, want cached %q", repo.Slug, existingRepo.Slug)
	}
	if callCount > 0 {
		t.Fatalf("Toplevel was called %d times; expected 0 (cached result should skip auto-detect)", callCount)
	}
}

// TestSCPAndHTTPSSameRepo verifies that the same physical repo registered with
// an HTTPS remote URL resolves to the same slug when queried by its SCP form,
// exercising NormalizeRemoteURL on the read side.
func TestSCPAndHTTPSSameRepo(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	// Register via HTTPS.
	s, httpsRepo := newTestStore(t, "", "https://github.com/alice/myapp")
	if httpsRepo == nil {
		t.Fatal("newTestStore did not register repo")
	}

	// Look up via SCP form — store must normalize and find the same row.
	scpRepo, err := s.GetRepoByRemoteURL(ctx, "git@github.com:alice/myapp")
	if err != nil {
		t.Fatalf("GetRepoByRemoteURL SCP form: %v — NormalizeRemoteURL must equate HTTPS and SCP", err)
	}
	if scpRepo.Slug != httpsRepo.Slug {
		t.Fatalf("SCP lookup returned slug %q, HTTPS registered slug %q — expected same repo", scpRepo.Slug, httpsRepo.Slug)
	}
}

// TestResolveListFilterAllRepos verifies that allRepos=true yields AllRepos:true.
func TestResolveListFilterAllRepos(t *testing.T) {
	t.Parallel()

	s, _ := newTestStore(t, "myapp", "")
	rs := &appState{store: s, prefix: "myapp", git: &fakeGitResolver{}}

	f, err := rs.resolveListFilter(context.Background(), true)
	if err != nil {
		t.Fatalf("resolveListFilter allRepos: %v", err)
	}
	if !f.AllRepos {
		t.Fatalf("AllRepos = false, want true")
	}
	if f.Prefix != "" {
		t.Fatalf("Prefix = %q, want empty when AllRepos=true", f.Prefix)
	}
}

// TestResolveListFilterDefaultPrefix verifies that with no allRepos and no
// repoArg, the filter scopes to rs.prefix.
func TestResolveListFilterDefaultPrefix(t *testing.T) {
	t.Parallel()

	s, _ := newTestStore(t, "myapp", "")
	rs := &appState{store: s, prefix: "myapp", git: &fakeGitResolver{}}

	f, err := rs.resolveListFilter(context.Background(), false)
	if err != nil {
		t.Fatalf("resolveListFilter default: %v", err)
	}
	if f.AllRepos {
		t.Fatal("AllRepos = true, want false for default scoping")
	}
	if f.Prefix != "myapp" {
		t.Fatalf("Prefix = %q, want myapp", f.Prefix)
	}
}

// TestResolveListFilterRepoArgSlug verifies that a slug repoArg resolves to
// the correct prefix via GetRepoBySlug.
func TestResolveListFilterRepoArgSlug(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s, repo := newTestStore(t, "", "https://github.com/alice/backend")
	if repo == nil {
		t.Fatal("newTestStore did not register repo")
	}
	if err := s.EnsureProject(ctx, repo.Prefix); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	rs := &appState{store: s, prefix: repo.Prefix, repoArg: repo.Slug, git: &fakeGitResolver{}}

	f, err := rs.resolveListFilter(ctx, false)
	if err != nil {
		t.Fatalf("resolveListFilter slug: %v", err)
	}
	if f.Prefix != repo.Slug {
		t.Fatalf("Prefix = %q, want %q", f.Prefix, repo.Slug)
	}
}

// TestResolveListFilterRepoArgURL verifies that a URL repoArg resolves to the
// registered repo's slug via GetRepoByRemoteURL.
func TestResolveListFilterRepoArgURL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const url = "https://github.com/alice/frontend"
	s, repo := newTestStore(t, "", url)
	if repo == nil {
		t.Fatal("newTestStore did not register repo")
	}

	rs := &appState{store: s, prefix: repo.Prefix, repoArg: url, git: &fakeGitResolver{}}

	f, err := rs.resolveListFilter(ctx, false)
	if err != nil {
		t.Fatalf("resolveListFilter URL: %v", err)
	}
	if f.Prefix != repo.Slug {
		t.Fatalf("Prefix = %q, want %q", f.Prefix, repo.Slug)
	}
}

// TestResolveListFilterRejectsPathArg verifies that a path-style repoArg returns
// an error with a file:/// hint.
func TestResolveListFilterRejectsPathArg(t *testing.T) {
	t.Parallel()

	s, _ := newTestStore(t, "myapp", "")
	rs := &appState{store: s, prefix: "myapp", repoArg: "/home/alice/myapp", git: &fakeGitResolver{}}

	_, err := rs.resolveListFilter(context.Background(), false)
	if err == nil {
		t.Fatal("resolveListFilter path: want error, got nil")
	}
	if !strings.Contains(err.Error(), "file:///") {
		t.Fatalf("expected file:/// hint in error, got %q", err.Error())
	}
}

// TestResolveListFilterMutualExclusion verifies that combining allRepos=true
// with a non-empty repoArg returns an error.
func TestResolveListFilterMutualExclusion(t *testing.T) {
	t.Parallel()

	s, _ := newTestStore(t, "myapp", "")
	rs := &appState{store: s, prefix: "myapp", repoArg: "myapp", git: &fakeGitResolver{}}

	_, err := rs.resolveListFilter(context.Background(), true)
	if err == nil {
		t.Fatal("resolveListFilter allRepos+repoArg: want error, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected 'mutually exclusive' in error, got %q", err.Error())
	}
}

// TestResolveListFilterMissingPrefix verifies that the default path with no
// prefix set returns an error from requirePrefix.
func TestResolveListFilterMissingPrefix(t *testing.T) {
	t.Parallel()

	s, _ := newTestStore(t, "", "")
	rs := &appState{store: s, prefix: "", git: &fakeGitResolver{}} // no prefix

	_, err := rs.resolveListFilter(context.Background(), false)
	if err == nil {
		t.Fatal("resolveListFilter no prefix: want error, got nil")
	}
}

// TestResolveListFilterUnknownSlug verifies that a non-existent slug repoArg
// returns an error rather than silently producing an empty list.
func TestResolveListFilterUnknownSlug(t *testing.T) {
	t.Parallel()

	s, _ := newTestStore(t, "myapp", "")
	rs := &appState{store: s, prefix: "myapp", repoArg: "no-such-repo", git: &fakeGitResolver{}}

	_, err := rs.resolveListFilter(context.Background(), false)
	if err == nil {
		t.Fatal("resolveListFilter unknown slug: want error, got nil")
	}
}

// countingFakeGitResolver wraps fakeGitResolver and counts Toplevel calls.
type countingFakeGitResolver struct {
	*fakeGitResolver
	calls *int
}

func (c *countingFakeGitResolver) Toplevel(dir string) (string, bool, error) {
	*c.calls++
	return c.fakeGitResolver.Toplevel(dir)
}

// Compile-time interface check for countingFakeGitResolver.
var _ gitResolver = (*countingFakeGitResolver)(nil)

// TestAutoRegisterRepoIsIdempotent verifies that calling AutoRegisterRepo twice
// with the same URL returns the same slug without error (idempotency invariant).
func TestAutoRegisterRepoIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s, _ := newTestStore(t, "", "")

	reg := func() store.Repo {
		repo, err := s.AutoRegisterRepo(ctx, store.AutoRegisterInput{
			RemoteURL: "https://github.com/alice/myapp",
			Actor:     "test",
		})
		if err != nil {
			t.Fatalf("AutoRegisterRepo: %v", err)
		}
		return repo
	}

	r1 := reg()
	r2 := reg()

	if r1.Slug != r2.Slug {
		t.Fatalf("idempotency violated: first slug %q, second %q", r1.Slug, r2.Slug)
	}
}
