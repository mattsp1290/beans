package main

import (
	"context"
	"testing"
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
