package main

import (
	"os/exec"
	"strings"
)

// gitResolver is the injectable seam for git workspace queries.  The real
// implementation shells out to git; tests inject a fake without cd-ing the
// test process into a real repo.
type gitResolver interface {
	// Toplevel returns the absolute path of the git working tree root for the
	// given directory (empty string = cwd).  Returns ("", false, nil) when the
	// directory is not inside a git repo OR when any git error occurs (same
	// convention as the underlying gitRoot helper).
	Toplevel(dir string) (root string, ok bool, err error)

	// RemoteURL returns the value of remote.origin.url for the repo rooted at
	// root.  Returns ("", false, nil) when remote.origin is unset OR when any
	// git error occurs (git not found, permission denied, etc.) — all failures
	// collapse to ok == false, err == nil, matching the Toplevel convention.
	RemoteURL(root string) (url string, ok bool, err error)

	// HeadCommit returns the full lowercase 40-character HEAD commit for the
	// repo rooted at root.  Returns ("", false, nil) when HEAD cannot be
	// resolved or git returns any failure; create behavior must remain
	// best-effort when commit capture is unavailable.
	HeadCommit(root string) (sha string, ok bool, err error)
}

// compile-time interface satisfaction check
var _ gitResolver = realGitResolver{}

// realGitResolver is the production implementation: shells out to git.
type realGitResolver struct{}

func (realGitResolver) Toplevel(dir string) (string, bool, error) {
	return gitRoot(dir)
}

func (realGitResolver) RemoteURL(root string) (string, bool, error) {
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	if root != "" {
		cmd.Dir = root
	}
	out, err := cmd.Output()
	if err != nil {
		return "", false, nil
	}
	url := strings.TrimSpace(string(out))
	if url == "" {
		return "", false, nil
	}
	return url, true, nil
}

func (realGitResolver) HeadCommit(root string) (string, bool, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	if root != "" {
		cmd.Dir = root
	}
	out, err := cmd.Output()
	if err != nil {
		return "", false, nil
	}
	sha := strings.TrimSpace(string(out))
	if !isFullLowercaseHexCommit(sha) {
		return "", false, nil
	}
	return sha, true, nil
}

func isFullLowercaseHexCommit(sha string) bool {
	if len(sha) != 40 {
		return false
	}
	for _, ch := range sha {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}
