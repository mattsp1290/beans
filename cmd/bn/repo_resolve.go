package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	store "github.com/mattsp1290/beans/store"
)

// repoArgForm is the result of classifying a --repo flag value.
type repoArgForm int

const (
	repoArgSlug repoArgForm = iota // e.g. "myapp", "owner-myapp"
	repoArgURL                     // e.g. "https://github.com/alice/myapp", "git@…"
	repoArgPath                    // e.g. "/home/alice/myapp", "./rel" — rejected at CLI
)

// classifyRepoArg classifies the raw --repo value into slug, URL, or path
// form. The classification must happen BEFORE NormalizeRemoteURL because the
// library's normalizeBarePath accepts absolute paths — the CLI explicitly
// rejects them to keep --repo focused on slugs and explicit remote URLs.
func classifyRepoArg(s string) repoArgForm {
	// URL: contains a scheme separator
	if strings.Contains(s, "://") {
		return repoArgURL
	}
	// URL: SCP-syntax SSH (git@host:path)
	if strings.HasPrefix(s, "git@") {
		return repoArgURL
	}
	// Path: absolute or relative with leading dot, tilde, or Windows drive letter
	if strings.HasPrefix(s, "/") ||
		strings.HasPrefix(s, "./") ||
		strings.HasPrefix(s, "../") ||
		strings.HasPrefix(s, "~") ||
		(len(s) >= 3 && s[1] == ':' && (s[2] == '/' || s[2] == '\\')) {
		return repoArgPath
	}
	return repoArgSlug
}

// resolveRepoContext resolves the repo context for repo-aware commands following
// the precedence: --repo flag > .bn marker repo field > cwd git auto-detect > nil.
//
// The caller receives nil when no repo context is available; that is valid for
// commands where a repo is optional.  This function is consumed by beans-75l
// (cmd_list.go) and beans-7kv (cmd_create.go) in subsequent iterations.
func (rs *appState) resolveRepoContext(ctx context.Context) (*store.Repo, error) {
	// Priority 1: --repo flag
	if rs.repoArg != "" {
		return rs.resolveRepoArg(ctx, rs.repoArg)
	}

	// Priority 2: .bn marker repo field.  Use rs.prefix (already resolved by
	// initConn) as the project scope to avoid treating the slug as both prefix
	// and slug — they are the same under topology (a), but using rs.prefix is
	// explicit and correct in the general case.
	cfg, err := readActiveProjectConfig("")
	if err == nil && cfg.Repo != "" {
		slug := cfg.Repo
		prefix := rs.prefix
		if prefix == "" {
			prefix = slug // topology-a fallback: prefix == slug
		}
		repo, err := rs.store.GetRepoBySlug(ctx, prefix, slug)
		if err != nil {
			return nil, fmt.Errorf("bn: .bn marker repo %q not found: %w", slug, err)
		}
		return &repo, nil
	}

	// Priority 3: cwd git auto-detect (use cached value from initConn if present)
	if rs.resolvedRepo != nil {
		return rs.resolvedRepo, nil
	}
	if err := rs.tryGitAutoDetect(ctx); err != nil {
		return nil, err
	}
	return rs.resolvedRepo, nil
}

// resolveRepoArg processes the --repo flag value, dispatching on its form.
func (rs *appState) resolveRepoArg(ctx context.Context, arg string) (*store.Repo, error) {
	switch classifyRepoArg(arg) {
	case repoArgPath:
		return nil, fmt.Errorf("--repo: path-style argument not supported; use a slug or a remote URL (for local repos, use file:///abs/path)")

	case repoArgURL:
		repo, err := rs.store.AutoRegisterRepo(ctx, store.AutoRegisterInput{
			RemoteURL: arg,
			Actor:     rs.actor,
		})
		if err != nil {
			return nil, fmt.Errorf("bn: --repo register: %w", err)
		}
		return &repo, nil

	default: // repoArgSlug — lookup only; NOT auto-register
		repo, err := rs.store.GetRepoBySlug(ctx, arg, arg)
		if err != nil {
			return nil, fmt.Errorf("repo %q not found; to register a repo provide a remote URL", arg)
		}
		return &repo, nil
	}
}

// warnIfCrossRepo writes a warning to w when rs.resolvedRepo is set and the
// issue's assigned repo differs from the current repo context.  The lookup is
// never rejected — cross-repo ID operations are always valid; this is advisory.
func warnIfCrossRepo(w io.Writer, rs *appState, iss store.Issue) {
	if rs.resolvedRepo == nil || iss.Repo == nil {
		return
	}
	if iss.Repo.Slug == rs.resolvedRepo.Slug {
		return
	}
	fmt.Fprintf(w, "warning: issue %s is assigned to repo %q (current context: repo %q)\n",
		iss.ID, iss.Repo.Slug, rs.resolvedRepo.Slug)
}

// tryGitAutoDetect attempts to discover the current git repo and auto-register
// it.  On success it sets rs.prefix (if currently empty) and rs.resolvedRepo.
// On any non-fatal condition (not in a git repo, no remote, etc.) it returns
// nil without modifying rs.  Real store errors are propagated.
func (rs *appState) tryGitAutoDetect(ctx context.Context) error {
	root, ok, err := rs.git.Toplevel("")
	if err != nil || !ok {
		return nil // silent: not in a git repo
	}

	rawURL, _, _ := rs.git.RemoteURL(root)

	var regURL string
	if rawURL != "" {
		regURL = rawURL
	} else {
		// Local-only repo (no remote.origin): synthesize a file:// URL from
		// the git toplevel so AutoRegisterRepo has a non-empty canonical key.
		// filepath.ToSlash converts Windows backslashes, and the leading slash
		// ensures the URL has an empty host ("file:///") rather than treating
		// a Windows drive letter as a host ("file://C:/...").
		slashRoot := filepath.ToSlash(root)
		if !strings.HasPrefix(slashRoot, "/") {
			slashRoot = "/" + slashRoot
		}
		regURL = "file://" + slashRoot
	}

	repo, err := rs.store.AutoRegisterRepo(ctx, store.AutoRegisterInput{
		RemoteURL: regURL,
		Actor:     rs.actor,
	})
	if err != nil {
		return nil // auto-detect is best-effort; real errors surface elsewhere
	}

	rs.resolvedRepo = &repo
	if rs.prefix == "" {
		rs.prefix = repo.Prefix
	}
	return nil
}
