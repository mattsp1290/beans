package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	store "github.com/mattsp1290/beans/store"
)

func TestClassifyRepoArg(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  repoArgForm
	}{
		// URL forms
		{"https://github.com/alice/myapp", repoArgURL},
		{"http://github.com/alice/myapp", repoArgURL},
		{"ssh://git@github.com/alice/myapp", repoArgURL},
		{"file:///home/alice/myapp", repoArgURL},
		{"git@github.com:alice/myapp", repoArgURL},
		// Path forms (rejected)
		{"/home/alice/myapp", repoArgPath},
		{"./myapp", repoArgPath},
		{"../sibling", repoArgPath},
		{"~/myapp", repoArgPath},
		{"C:/repos/myapp", repoArgPath},
		{"C:\\repos\\myapp", repoArgPath},
		// Slug forms
		{"myapp", repoArgSlug},
		{"owner-myapp", repoArgSlug},
		{"github-owner-myapp", repoArgSlug},
		{"my.app", repoArgSlug},
		{"my_app", repoArgSlug},
	}

	for _, tc := range cases {
		got := classifyRepoArg(tc.input)
		if got != tc.want {
			t.Errorf("classifyRepoArg(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestResolveRepoArgRejectsPathForm(t *testing.T) {
	t.Parallel()

	rs := &appState{git: &fakeGitResolver{}}
	_, err := rs.resolveRepoArg(context.TODO(), "/home/alice/myapp")
	if err == nil {
		t.Fatal("resolveRepoArg path: want error, got nil")
	}
	if got := err.Error(); got == "" {
		t.Fatal("resolveRepoArg path: error message is empty")
	}
}

func TestResolveRepoContextExplicitRepoBeatsMarkerAndGit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	t.Chdir(dir)

	s, explicitRepo := newTestStore(t, "", "https://github.com/acme/explicit")
	if explicitRepo == nil {
		t.Fatal("newTestStore did not register explicit repo")
	}
	if explicitRepo.Prefix != explicitRepo.Slug {
		t.Fatalf("test requires topology-a prefix==slug, got prefix=%q slug=%q", explicitRepo.Prefix, explicitRepo.Slug)
	}
	markerRepo, err := s.AutoRegisterRepo(ctx, store.AutoRegisterInput{
		RemoteURL: "https://github.com/acme/marker",
		Actor:     "test",
	})
	if err != nil {
		t.Fatalf("AutoRegisterRepo marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, activeProjectMarker), []byte("project="+markerRepo.Prefix+"\nrepo="+markerRepo.Slug+"\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	callCount := 0
	rs := &appState{
		store:   s,
		actor:   "test",
		prefix:  markerRepo.Prefix,
		repoArg: explicitRepo.Slug,
		git: &countingFakeGitResolver{fakeGitResolver: &fakeGitResolver{
			toplevel:  "/home/alice/gitrepo",
			remoteURL: "https://github.com/acme/gitrepo",
		}, calls: &callCount},
	}

	got, err := rs.resolveRepoContext(ctx)
	if err != nil {
		t.Fatalf("resolveRepoContext: %v", err)
	}
	if got == nil || got.Slug != explicitRepo.Slug {
		t.Fatalf("resolveRepoContext = %+v, want explicit repo %q", got, explicitRepo.Slug)
	}
	if callCount != 0 {
		t.Fatalf("git auto-detect ran %d times; explicit --repo should short-circuit marker and git", callCount)
	}
}

func TestResolveRepoContextMarkerBeatsGitAutoDetect(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	t.Chdir(dir)

	s, markerRepo := newTestStore(t, "", "https://github.com/acme/marker")
	if markerRepo == nil {
		t.Fatal("newTestStore did not register marker repo")
	}
	if err := os.WriteFile(filepath.Join(dir, activeProjectMarker), []byte("project="+markerRepo.Prefix+"\nrepo="+markerRepo.Slug+"\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	callCount := 0
	rs := &appState{
		store:  s,
		actor:  "test",
		prefix: markerRepo.Prefix,
		git: &countingFakeGitResolver{fakeGitResolver: &fakeGitResolver{
			toplevel:  "/home/alice/gitrepo",
			remoteURL: "https://github.com/acme/gitrepo",
		}, calls: &callCount},
	}

	got, err := rs.resolveRepoContext(ctx)
	if err != nil {
		t.Fatalf("resolveRepoContext: %v", err)
	}
	if got == nil || got.Slug != markerRepo.Slug {
		t.Fatalf("resolveRepoContext = %+v, want marker repo %q", got, markerRepo.Slug)
	}
	if callCount != 0 {
		t.Fatalf("git auto-detect ran %d times; .bn marker repo should short-circuit git", callCount)
	}
}

func TestTryGitAutoDetectNoGitRepo(t *testing.T) {
	t.Parallel()

	rs := &appState{
		git: &fakeGitResolver{
			toplevel: "", // not in a git repo
		},
	}
	if err := rs.tryGitAutoDetect(context.TODO()); err != nil {
		t.Fatalf("tryGitAutoDetect: want nil error outside git repo, got %v", err)
	}
	if rs.resolvedRepo != nil {
		t.Fatalf("tryGitAutoDetect: resolvedRepo should be nil outside git repo")
	}
	if rs.prefix != "" {
		t.Fatalf("tryGitAutoDetect: prefix should stay empty outside git repo, got %q", rs.prefix)
	}
}

func TestCwdCreationCommitForRepoRequiresMatchingIdentityAndValidHead(t *testing.T) {
	ctx := context.Background()
	const head = "4567890abcdef1234567890abcdef12345678901"

	for _, tc := range []struct {
		name        string
		selectedURL string
		gitRoot     string
		gitRemote   string
		gitHead     string
		want        string
	}{
		{
			name:        "matching remote origin captures head",
			selectedURL: "https://github.com/acme/remote-match",
			gitRoot:     "/home/alice/remote-match",
			gitRemote:   "git@github.com:acme/remote-match.git",
			gitHead:     head,
			want:        head,
		},
		{
			name:        "local-only synthesized file identity captures head",
			selectedURL: "file:///home/alice/local-match",
			gitRoot:     "/home/alice/local-match",
			gitHead:     head,
			want:        head,
		},
		{
			name:        "remote mismatch leaves empty",
			selectedURL: "https://github.com/acme/selected",
			gitRoot:     "/home/alice/other",
			gitRemote:   "https://github.com/acme/other",
			gitHead:     head,
			want:        "",
		},
		{
			name:        "invalid git output ignored",
			selectedURL: "https://github.com/acme/invalid-head",
			gitRoot:     "/home/alice/invalid-head",
			gitRemote:   "https://github.com/acme/invalid-head",
			gitHead:     "HEAD",
			want:        "",
		},
		{
			name:        "uppercase object ID ignored",
			selectedURL: "https://github.com/acme/uppercase-head",
			gitRoot:     "/home/alice/uppercase-head",
			gitRemote:   "https://github.com/acme/uppercase-head",
			gitHead:     "ABCDEF1234567890ABCDEF1234567890ABCDEF12",
			want:        "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, selected := newTestStore(t, "", tc.selectedURL)
			if selected == nil {
				t.Fatal("newTestStore did not register selected repo")
			}
			rs := &appState{
				store: s,
				actor: "test",
				git: &fakeGitResolver{
					toplevel:   tc.gitRoot,
					remoteURL:  tc.gitRemote,
					headCommit: tc.gitHead,
				},
			}

			if got := rs.cwdCreationCommitForRepo(ctx, selected); got != tc.want {
				t.Fatalf("cwdCreationCommitForRepo() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestFakeGitResolverRecordsCallArgs verifies that fakeGitResolver records the
// dir/root arguments so callers can assert correct wiring (e.g. RemoteURL is
// called with the root that Toplevel returned, not cwd).
func TestFakeGitResolverRecordsCallArgs(t *testing.T) {
	t.Parallel()

	fake := &fakeGitResolver{
		toplevel:  "/repo/root",
		remoteURL: "",
	}

	root, ok, err := fake.Toplevel("")
	if err != nil || !ok || root != "/repo/root" {
		t.Fatalf("Toplevel: got (%q, %v, %v), want (/repo/root, true, nil)", root, ok, err)
	}
	if fake.lastToplevelDir != "" {
		t.Errorf("lastToplevelDir = %q, want empty (cwd)", fake.lastToplevelDir)
	}

	_, _, _ = fake.RemoteURL(root)
	if fake.lastRemoteURLRoot != "/repo/root" {
		t.Errorf("lastRemoteURLRoot = %q, want /repo/root", fake.lastRemoteURLRoot)
	}
}
