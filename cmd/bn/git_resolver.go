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
