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
	// directory is not inside a git repo.
	Toplevel(dir string) (root string, ok bool, err error)

	// RemoteURL returns the value of remote.origin.url for the repo rooted at
	// root.  Returns ("", false, nil) when no remote.origin is configured.
	RemoteURL(root string) (url string, ok bool, err error)
}

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

// fakeGitResolver is a test double that returns predetermined values without
// touching the filesystem or spawning processes.
type fakeGitResolver struct {
	toplevel  string
	remoteURL string
	// err values are returned for every call when non-nil
	toplevelErr  error
	remoteURLErr error
}

func (f *fakeGitResolver) Toplevel(_ string) (string, bool, error) {
	if f.toplevelErr != nil {
		return "", false, f.toplevelErr
	}
	if f.toplevel == "" {
		return "", false, nil
	}
	return f.toplevel, true, nil
}

func (f *fakeGitResolver) RemoteURL(_ string) (string, bool, error) {
	if f.remoteURLErr != nil {
		return "", false, f.remoteURLErr
	}
	if f.remoteURL == "" {
		return "", false, nil
	}
	return f.remoteURL, true, nil
}
