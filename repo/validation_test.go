package repo

import "testing"

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
