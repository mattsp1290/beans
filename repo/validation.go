package repo

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	CloneStrategyMirrorCache = "mirror-cache"
	CloneStrategyFreshClone  = "fresh-clone"
	AuthRefTestNone          = "test:none"
)

var authRefNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type Target struct {
	RemoteURL      string
	DefaultBranch  string
	WorktreeSubdir string
	CloneStrategy  string
	AuthRef        string
}

func ValidateTarget(t Target) error {
	if err := ValidateRemoteURL(t.RemoteURL); err != nil {
		return err
	}
	if err := ValidateDefaultBranch(t.DefaultBranch); err != nil {
		return err
	}
	if err := ValidateWorktreeSubdir(t.WorktreeSubdir); err != nil {
		return err
	}
	if err := ValidateCloneStrategy(t.CloneStrategy); err != nil {
		return err
	}
	if err := ValidateAuthRef(t.AuthRef); err != nil {
		return err
	}
	return nil
}

func NormalizeCloneStrategy(strategy string) string {
	strategy = strings.TrimSpace(strategy)
	if strategy == "" {
		return CloneStrategyMirrorCache
	}
	return strategy
}

func NormalizeDefaultBranch(branch string) string {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "main"
	}
	return branch
}

func ValidateRemoteURL(remote string) error {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return errors.New("repository: remote_url is required")
	}
	if strings.ContainsAny(remote, "\x00\r\n") {
		return errors.New("repository: remote_url contains control characters")
	}
	if isSCPRemote(remote) {
		return nil
	}
	u, err := url.Parse(remote)
	if err != nil {
		return fmt.Errorf("repository: remote_url parse: %w", err)
	}
	if u.Scheme == "" {
		return nil
	}
	switch strings.ToLower(u.Scheme) {
	case "file":
		if u.User != nil {
			return errors.New("repository: file remote_url must not include userinfo")
		}
		return nil
	case "ssh", "git", "http", "https":
		if u.Host == "" {
			return fmt.Errorf("repository: %s remote_url requires a host", u.Scheme)
		}
		if (u.Scheme == "http" || u.Scheme == "https") && u.User != nil {
			return errors.New("repository: http(s) remote_url must not include userinfo")
		}
		return nil
	default:
		return fmt.Errorf("repository: unsupported remote_url scheme %q", u.Scheme)
	}
}

func ValidateRemoteAllowed(remote string, allowedHosts []string) error {
	if len(allowedHosts) == 0 {
		return nil
	}
	host, ok, err := RemoteHost(remote)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	host = strings.ToLower(host)
	for _, allowed := range allowedHosts {
		if host == strings.ToLower(strings.TrimSpace(allowed)) {
			return nil
		}
	}
	return fmt.Errorf("repository: remote host %q is not allowed", host)
}

func RemoteHost(remote string) (host string, ok bool, err error) {
	remote = strings.TrimSpace(remote)
	if err := ValidateRemoteURL(remote); err != nil {
		return "", false, err
	}
	if isSCPRemote(remote) {
		left := remote[:strings.IndexByte(remote, ':')]
		if at := strings.LastIndexByte(left, '@'); at >= 0 {
			left = left[at+1:]
		}
		return strings.ToLower(left), true, nil
	}
	u, err := url.Parse(remote)
	if err != nil {
		return "", false, fmt.Errorf("repository: remote_url parse: %w", err)
	}
	if u.Scheme == "" || strings.ToLower(u.Scheme) == "file" {
		return "", false, nil
	}
	return strings.ToLower(u.Hostname()), true, nil
}

func ValidateDefaultBranch(branch string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return nil
	}
	if strings.ContainsAny(branch, "\x00\r\n") {
		return errors.New("repository: default_branch contains control characters")
	}
	if strings.HasPrefix(branch, "-") {
		return errors.New("repository: default_branch must not start with '-'")
	}
	return nil
}

func ValidateWorktreeSubdir(subdir string) error {
	subdir = strings.TrimSpace(subdir)
	if subdir == "" {
		return nil
	}
	if strings.ContainsAny(subdir, "\x00\r\n") {
		return errors.New("repository: worktree_subdir contains control characters")
	}
	if filepath.IsAbs(subdir) {
		return errors.New("repository: worktree_subdir must be relative")
	}
	clean := filepath.Clean(subdir)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return errors.New("repository: worktree_subdir must stay inside the checkout")
	}
	return nil
}

func ValidateCloneStrategy(strategy string) error {
	switch NormalizeCloneStrategy(strategy) {
	case CloneStrategyMirrorCache, CloneStrategyFreshClone:
		return nil
	default:
		return fmt.Errorf("repository: unsupported clone_strategy %q", strategy)
	}
}

func ValidateAuthRef(authRef string) error {
	authRef = strings.TrimSpace(authRef)
	if authRef == "" {
		return errors.New("repository: auth_ref is required")
	}
	if authRef == AuthRefTestNone {
		return nil
	}
	const prefix = "ssh-key:"
	if !strings.HasPrefix(authRef, prefix) {
		return fmt.Errorf("repository: unsupported auth_ref %q", authRef)
	}
	name := strings.TrimPrefix(authRef, prefix)
	if name == "" || name == "." || name == ".." || !authRefNameRE.MatchString(name) {
		return fmt.Errorf("repository: invalid auth_ref %q", authRef)
	}
	return nil
}

func isSCPRemote(remote string) bool {
	if strings.Contains(remote, "://") {
		return false
	}
	colon := strings.IndexByte(remote, ':')
	if colon <= 0 {
		return false
	}
	slash := strings.IndexAny(remote, `/\`)
	return slash == -1 || colon < slash
}
