// Package vault resolves the hub and its projects and (from WP4) indexes the
// vault. remote.go normalizes git remote URLs so a project's [remotes] list can
// be matched against the origin of the repository bn is invoked from.
package vault

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// ValidateRemoteURL rejects control characters, unsupported schemes, missing
// hosts, and embedded userinfo in a git remote URL.
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

// RemoteHost returns the lowercase host of a hosted remote, ok=false for
// file and bare-path remotes.
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

// ErrNoRemote is returned by NormalizeRemoteURL when the input is empty,
// indicating a local-only repository with no configured remote.
var ErrNoRemote = errors.New("repository: no remote URL (local-only repo)")

// defaultPorts maps URL schemes to their default port strings. A port equal to
// the scheme's default is stripped from the canonical key.
var defaultPorts = map[string]string{
	"https": "443",
	"http":  "80",
	"ssh":   "22",
	"git":   "9418",
}

// NormalizeRemoteURL returns a canonical URL string suitable as a stable
// lookup key for a remote repository. The canonical form is:
//
//	https://{lowercase-host}[:{non-default-port}]/{path}
//
// where {path} has the trailing ".git" suffix stripped. The three common
// transport forms of the same hosted repository all collapse to one key:
//
//	git@github.com:alice/app.git        → https://github.com/alice/app
//	ssh://git@github.com/alice/app.git  → https://github.com/alice/app
//	https://github.com/alice/app.git    → https://github.com/alice/app
//
// file:// and absolute bare-path remotes are normalized to:
//
//	file://{clean-abs-path}             (trailing ".git" stripped)
//
// Path case is intentionally preserved — the host is lowercased but the path
// component is not, since path case-sensitivity is host-dependent. Callers
// that need case-insensitive collapse (e.g. GitHub) must lowercase the input
// path before calling.
//
// A relative bare path is an ambiguous canonical key and returns an error.
// An empty remote (local-only repo with no remote configured) returns
// ErrNoRemote; callers should supply a synthetic file:// key derived from
// the absolute git toplevel in that case.
// Windows-style drive paths (C:\...) are not supported and return an error.
func NormalizeRemoteURL(remote string) (string, error) {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return "", ErrNoRemote
	}
	if strings.ContainsAny(remote, "\x00\r\n") {
		return "", errors.New("repository: remote_url contains control characters")
	}

	if isSCPRemote(remote) {
		return normalizeSCP(remote)
	}

	u, err := url.Parse(remote)
	if err != nil {
		return "", fmt.Errorf("repository: NormalizeRemoteURL parse: %w", err)
	}

	switch strings.ToLower(u.Scheme) {
	case "https", "http", "ssh", "git":
		return normalizeHostedURL(u)
	case "file":
		return normalizeFileURL(u)
	case "":
		return normalizeBarePath(remote)
	default:
		return "", fmt.Errorf("repository: NormalizeRemoteURL: unsupported scheme %q", u.Scheme)
	}
}

// trimPathSuffix strips trailing slashes then the ".git" suffix from a path.
// The slash-first order matters: "repo.git/" has a trailing slash that must be
// removed before TrimSuffix can match ".git".
func trimPathSuffix(path string) string {
	return strings.TrimSuffix(strings.TrimRight(path, "/"), ".git")
}

func normalizeHostedURL(u *url.URL) (string, error) {
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", fmt.Errorf("repository: NormalizeRemoteURL: missing host in URL")
	}

	port := u.Port()
	if dp := defaultPorts[strings.ToLower(u.Scheme)]; dp != "" && port == dp {
		port = ""
	}

	hostField := host
	if port != "" {
		hostField = host + ":" + port
	}

	path := trimPathSuffix(u.Path)
	if path == "" {
		path = "/"
	}

	return "https://" + hostField + path, nil
}

func normalizeSCP(remote string) (string, error) {
	colon := strings.IndexByte(remote, ':')
	hostPart := remote[:colon]
	pathPart := remote[colon+1:]

	if at := strings.LastIndexByte(hostPart, '@'); at >= 0 {
		hostPart = hostPart[at+1:]
	}

	host := strings.ToLower(strings.TrimSpace(hostPart))
	if host == "" {
		return "", errors.New("repository: NormalizeRemoteURL: empty host in SCP remote")
	}

	// Trim leading/trailing slashes before stripping .git so that paths like
	// "alice/app.git/" correctly have the suffix removed.
	path := strings.Trim(trimPathSuffix(strings.TrimLeft(pathPart, "/")), "/")

	return "https://" + host + "/" + path, nil
}

func normalizeFileURL(u *url.URL) (string, error) {
	if h := u.Host; h != "" && !strings.EqualFold(h, "localhost") {
		return "", fmt.Errorf("repository: NormalizeRemoteURL: file:// URL must not specify a host, got %q; use ssh:// for network file remotes", h)
	}
	path := trimPathSuffix(u.Path)
	if path == "" {
		return "", errors.New("repository: NormalizeRemoteURL: empty path in file URL")
	}
	return "file://" + path, nil
}

func normalizeBarePath(remote string) (string, error) {
	if !filepath.IsAbs(remote) {
		return "", fmt.Errorf("repository: NormalizeRemoteURL: relative path %q is ambiguous as a canonical key; use an absolute path or file:// URL", remote)
	}
	path := strings.TrimSuffix(filepath.Clean(remote), ".git")
	return "file://" + path, nil
}

func isSCPRemote(remote string) bool {
	if strings.Contains(remote, "://") {
		return false
	}
	colon := strings.IndexByte(remote, ':')
	if colon <= 0 {
		return false
	}
	// A single-character "host" before the colon is a Windows drive letter
	// (e.g. C:\repo), not an SCP hostname.
	if colon == 1 {
		return false
	}
	slash := strings.IndexAny(remote, `/\`)
	return slash == -1 || colon < slash
}
