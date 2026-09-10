// Package gitops wraps the system git binary for the hub clone: workspace
// queries about the repository the user is working in, and (from WP3) the
// lock, fetch, commit, and push pipeline over the hub.
package gitops

import (
	"os/exec"
	"strings"
)

// Resolver is the injectable seam for git workspace queries about the code
// repository bn is invoked from. The real implementation shells out to git;
// tests inject a fake without cd-ing the test process into a real repo.
type Resolver interface {
	// Toplevel returns the absolute path of the git working tree root for the
	// given directory (empty string = cwd). Returns ("", false, nil) when the
	// directory is not inside a git repo OR when any git error occurs.
	Toplevel(dir string) (root string, ok bool, err error)

	// RemoteURL returns the value of remote.origin.url for the repo rooted at
	// root. Returns ("", false, nil) when remote.origin is unset OR when any
	// git error occurs (git not found, permission denied, etc.) — all failures
	// collapse to ok == false, err == nil, matching the Toplevel convention.
	RemoteURL(root string) (url string, ok bool, err error)

	// HeadCommit returns the full lowercase 40-character HEAD commit for the
	// repo rooted at root. Returns ("", false, nil) when HEAD cannot be
	// resolved or git returns any failure; callers must stay best-effort when
	// commit capture is unavailable.
	HeadCommit(root string) (sha string, ok bool, err error)
}

// compile-time interface satisfaction check
var _ Resolver = SystemGit{}

// SystemGit is the production Resolver: it shells out to the git on PATH.
type SystemGit struct{}

func (SystemGit) Toplevel(dir string) (string, bool, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return "", false, nil
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", false, nil
	}
	return root, true, nil
}

func (SystemGit) RemoteURL(root string) (string, bool, error) {
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

func (SystemGit) HeadCommit(root string) (string, bool, error) {
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

// Branch returns the checked-out branch name (git rev-parse --abbrev-ref
// HEAD). ok is false on a detached HEAD or any git failure.
func (SystemGit) Branch(root string) (string, bool, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	if root != "" {
		cmd.Dir = root
	}
	out, err := cmd.Output()
	if err != nil {
		return "", false, nil
	}
	name := strings.TrimSpace(string(out))
	if name == "" || name == "HEAD" {
		return "", false, nil
	}
	return name, true, nil
}
