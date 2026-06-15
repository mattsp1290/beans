package repo

import (
	"errors"
	"testing"
)

func TestValidateTargetAcceptsLocalAndTestRepos(t *testing.T) {
	t.Parallel()
	cases := []Target{
		{RemoteURL: "/tmp/repo.git", AuthRef: AuthRefTestNone},
		{RemoteURL: "file:///tmp/repo.git", AuthRef: AuthRefTestNone, CloneStrategy: CloneStrategyFreshClone},
		{RemoteURL: "git@github.com:punk1290/boxy.git", AuthRef: "ssh-key:github-default", WorktreeSubdir: "services/api"},
		{RemoteURL: "https://github.com/punk1290/boxy.git", AuthRef: "ssh-key:github-default"},
	}
	for _, tc := range cases {
		if err := ValidateTarget(tc); err != nil {
			t.Fatalf("ValidateTarget(%+v): %v", tc, err)
		}
	}
}

func TestValidateTargetRejectsUnsafeInputs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   Target
	}{
		{"empty remote", Target{AuthRef: AuthRefTestNone}},
		{"unsupported scheme", Target{RemoteURL: "ftp://example.com/repo.git", AuthRef: AuthRefTestNone}},
		{"https userinfo", Target{RemoteURL: "https://token@example.com/repo.git", AuthRef: AuthRefTestNone}},
		{"bad clone strategy", Target{RemoteURL: "/tmp/repo.git", AuthRef: AuthRefTestNone, CloneStrategy: "shared"}},
		{"bad auth scheme", Target{RemoteURL: "/tmp/repo.git", AuthRef: "token:github"}},
		{"bad auth name", Target{RemoteURL: "/tmp/repo.git", AuthRef: "ssh-key:../secret"}},
		{"absolute subdir", Target{RemoteURL: "/tmp/repo.git", AuthRef: AuthRefTestNone, WorktreeSubdir: "/etc"}},
		{"escaping subdir", Target{RemoteURL: "/tmp/repo.git", AuthRef: AuthRefTestNone, WorktreeSubdir: "../x"}},
		{"dot subdir", Target{RemoteURL: "/tmp/repo.git", AuthRef: AuthRefTestNone, WorktreeSubdir: "."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateTarget(tc.in); err == nil {
				t.Fatalf("ValidateTarget(%+v): want error", tc.in)
			}
		})
	}
}

func TestNormalizeRemoteURL(t *testing.T) {
	t.Parallel()

	// All three transport forms of the same hosted repo must collapse to one key.
	const githubAliceApp = "https://github.com/alice/app"
	tripleEquivalence := []struct {
		name string
		in   string
	}{
		{"scp", "git@github.com:alice/app.git"},
		{"ssh-url", "ssh://git@github.com/alice/app.git"},
		{"https-with-git", "https://github.com/alice/app.git"},
	}
	for _, tc := range tripleEquivalence {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeRemoteURL(tc.in)
			if err != nil {
				t.Fatalf("NormalizeRemoteURL(%q): unexpected error: %v", tc.in, err)
			}
			if got != githubAliceApp {
				t.Fatalf("NormalizeRemoteURL(%q) = %q, want %q", tc.in, got, githubAliceApp)
			}
		})
	}

	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
		errIs   error
	}{
		// No .git suffix — idempotent.
		{"https-no-git", "https://github.com/alice/app", "https://github.com/alice/app", false, nil},
		// Host case-folding.
		{"https-upper-host", "https://GitHub.COM/alice/app.git", "https://github.com/alice/app", false, nil},
		// SCP with no user@.
		{"scp-no-user", "github.com:alice/app.git", "https://github.com/alice/app", false, nil},
		// git:// protocol.
		{"git-proto", "git://github.com/alice/app.git", "https://github.com/alice/app", false, nil},
		// Non-standard SSH port is preserved.
		{"ssh-nonstandard-port", "ssh://git@git.corp.example.com:2222/alice/app.git", "https://git.corp.example.com:2222/alice/app", false, nil},
		// Default SSH port (22) is stripped.
		{"ssh-default-port", "ssh://git@github.com:22/alice/app.git", "https://github.com/alice/app", false, nil},
		// HTTP with non-standard port preserved.
		{"http-nonstandard-port", "http://git.corp.example.com:8080/alice/app.git", "https://git.corp.example.com:8080/alice/app", false, nil},
		// HTTPS default port stripped.
		{"https-default-port", "https://github.com:443/alice/app.git", "https://github.com/alice/app", false, nil},
		// file:// URL: .git suffix stripped.
		{"file-url", "file:///tmp/repo.git", "file:///tmp/repo", false, nil},
		// file:// URL: no .git suffix — idempotent.
		{"file-url-no-git", "file:///tmp/repo", "file:///tmp/repo", false, nil},
		// Absolute bare path.
		{"abs-bare-path", "/tmp/repo.git", "file:///tmp/repo", false, nil},
		// Absolute bare path without .git.
		{"abs-bare-path-no-git", "/tmp/repo", "file:///tmp/repo", false, nil},
		// Self-hosted with path depth > 2 (GitLab subgroup).
		{"deep-path", "https://gitlab.example.com/group/sub/repo.git", "https://gitlab.example.com/group/sub/repo", false, nil},
		// Empty → ErrNoRemote.
		{"empty", "", "", true, ErrNoRemote},
		// Whitespace only → ErrNoRemote.
		{"whitespace", "   ", "", true, ErrNoRemote},
		// Relative path → error.
		{"relative-path", "../other-repo", "", true, nil},
		// Unsupported scheme → error.
		{"ftp", "ftp://example.com/repo.git", "", true, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeRemoteURL(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NormalizeRemoteURL(%q): want error, got %q", tc.in, got)
				}
				if tc.errIs != nil && !errors.Is(err, tc.errIs) {
					t.Fatalf("NormalizeRemoteURL(%q): error = %v, want errors.Is %v", tc.in, err, tc.errIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeRemoteURL(%q): unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeRemoteURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestValidateRemoteAllowed(t *testing.T) {
	t.Parallel()
	if err := ValidateRemoteAllowed("git@github.com:punk1290/boxy.git", []string{"github.com"}); err != nil {
		t.Fatalf("ValidateRemoteAllowed ssh: %v", err)
	}
	if err := ValidateRemoteAllowed("https://github.com/punk1290/boxy.git", []string{"github.com"}); err != nil {
		t.Fatalf("ValidateRemoteAllowed https: %v", err)
	}
	if err := ValidateRemoteAllowed("/tmp/repo.git", []string{"github.com"}); err != nil {
		t.Fatalf("ValidateRemoteAllowed local path: %v", err)
	}
	if err := ValidateRemoteAllowed("git@gitlab.com:punk1290/boxy.git", []string{"github.com"}); err == nil {
		t.Fatal("ValidateRemoteAllowed disallowed host: want error")
	}
}
