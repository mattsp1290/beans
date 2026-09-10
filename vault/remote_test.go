package vault

import (
	"errors"
	"testing"
)

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
		// Trailing slash before .git — must still strip .git (order matters).
		{"https-trailing-slash", "https://github.com/alice/app.git/", "https://github.com/alice/app", false, nil},
		// SCP with trailing slash.
		{"scp-trailing-slash", "git@github.com:alice/app.git/", "https://github.com/alice/app", false, nil},
		// file:// trailing slash.
		{"file-trailing-slash", "file:///tmp/repo.git/", "file:///tmp/repo", false, nil},
		// file:// with non-localhost host → error.
		{"file-url-with-host", "file://server/share/repo.git", "", true, nil},
		// file://localhost is allowed (local file URL convention).
		{"file-url-localhost", "file://localhost/tmp/repo.git", "file:///tmp/repo", false, nil},
		// Windows path → error (not SCP, not abs path on POSIX, no scheme).
		{"windows-path-backslash", `C:\repo\app.git`, "", true, nil},
		// Mixed-case path is preserved (path case is intentionally not folded).
		{"https-mixed-case-path", "https://github.com/Alice/App.git", "https://github.com/Alice/App", false, nil},
		// Userinfo in HTTPS is stripped (not part of canonical key).
		{"https-with-userinfo", "https://user:token@github.com/alice/app.git", "https://github.com/alice/app", false, nil},
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

func TestNormalizeRemoteURLIdempotent(t *testing.T) {
	t.Parallel()
	// Normalizing an already-normalized URL must produce the same result.
	inputs := []string{
		"git@github.com:alice/app.git",
		"ssh://git@github.com/alice/app.git",
		"https://github.com/alice/app.git",
		"https://github.com/alice/app.git/",
		"file:///tmp/repo.git",
		"/tmp/repo.git",
		"ssh://git@git.corp.example.com:2222/alice/app.git",
	}
	for _, in := range inputs {
		first, err := NormalizeRemoteURL(in)
		if err != nil {
			continue // error cases don't need idempotence check
		}
		second, err := NormalizeRemoteURL(first)
		if err != nil {
			t.Errorf("NormalizeRemoteURL(NormalizeRemoteURL(%q)) error: %v", in, err)
			continue
		}
		if first != second {
			t.Errorf("NormalizeRemoteURL not idempotent for %q: first=%q second=%q", in, first, second)
		}
	}
}
