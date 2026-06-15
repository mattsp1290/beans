# Decision Record: Repo Resolution Precedence + Edge-Case Behavior

**Status:** Decided  
**Date:** 2026-06-15  
**Closes:** beans-934  
**Blocks unblocked:** beans-3u3 (wire precedence in app.go)

---

## Decision

Two independent resolution chains are established. They compose, but neither
overrides the other:

1. **Prefix chain** — determines which project prefix `bn` operates in.
2. **Repo chain** — determines which `bn_repos` row to associate with a command.

Under topology (a) (prefix == slug), when auto-detect fires it sets both at
once via `AutoRegisterRepo`. In all other cases the two chains resolve
independently.

---

## Prefix resolution chain

Unchanged from the existing implementation (`resolveProjectPrefix`, app.go:172),
with the auto-detect case inserted between env and marker:

| Priority | Source | Notes |
|---|---|---|
| 1 | `--project <prefix>` flag | Explicit always wins |
| 2 | `BN_PROJECT` env var | Per-shell override |
| 3 | Cwd git auto-detect | Only when 1 and 2 are absent AND no `.bn` project field exists |
| 4 | `.bn` marker `project` field | Persisted across commands in a git repo |
| 5 | Error: prefix required | Emitted at command execution, not at init |

Auto-detect (priority 3) fires **only** when priorities 1, 2, and 4 are all
absent. If `.bn` exists with a `project` field, auto-detect is skipped entirely.
This gives users explicit control via `bn init --prefix=<prefix>` without
fight with auto-detect on every invocation.

---

## Repo resolution chain

New; applies to commands that accept a repo context (e.g. `bn create --repo`):

| Priority | Source | Notes |
|---|---|---|
| 1 | `--repo <value>` flag | Explicit slug or remote URL |
| 2 | Cwd git auto-detect | `git rev-parse --show-toplevel` → `git config --get remote.origin.url` → `AutoRegisterRepo` |
| 3 | `.bn` marker `repo` field | Slug of the repo written by `bn init --repo` |
| 4 | No repo context | `IssueRepoInput` is nil; command creates issue without a repo link |

Priorities 1 and 2 are mutually exclusive: when `--repo` is provided,
auto-detect is skipped. When neither `--repo` nor auto-detect applies,
the `.bn` marker repo field is used. Commands that require a repo (e.g.
`bn create --require-repo`) must error at priority 4 with a clear message.

### When auto-detect fires

Auto-detect fires on priority 2 only when ALL of the following are true:
- `--repo` flag is absent
- The command is "repo-aware" (the command opts in by calling the auto-detect path)
- Priority 3 (`.bn` repo field) is also absent

If auto-detect fires, its result is passed to `AutoRegisterRepo` (idempotent by
normalized URL) and the returned `Repo.Prefix` is used as the effective prefix
for the command. This is the only case where prefix resolution is deferred past
priority 4 of the prefix chain above.

---

## What `--repo` accepts

`--repo` accepts exactly two forms:

### Form 1: Registered slug

A slug matching `[a-z0-9][a-z0-9._-]*` with no `://` or `@` → slug lookup:

```
--repo myapp
--repo owner-myapp
--repo github-owner-myapp
```

Behavior: `GetRepoBySlug(prefix, slug)`. Under topology (a), `slug == prefix`,
so the lookup is `GetRepoBySlug(slug, slug)`. If the slug is not found:
`bn` returns an error — NOT auto-register-by-name, because a bare slug carries
no URL from which to derive the canonical identity.

Error: `repo "myapp" not found; to register a repo provide a remote URL`

### Form 2: Remote URL

Any string recognized as a URL by `NormalizeRemoteURL`: HTTPS URLs, SCP-syntax
SSH (`git@host:path`), SSH scheme (`ssh://`), local `file://` paths:

```
--repo https://github.com/alice/myapp
--repo git@github.com:alice/myapp
--repo file:///home/alice/myapp
```

Behavior: `AutoRegisterRepo(ctx, AutoRegisterInput{RemoteURL: value})`.
Idempotent: returns the existing row if already registered. Sets both the repo
and the prefix from the returned `Repo.Prefix`.

### Form 3 (rejected): Local filesystem path

Bare paths (`/home/alice/myapp`, `./myapp`, `../sibling`) are NOT accepted by
`--repo`. If a user wants to register a local repo at a specific path, they
run `bn repo add --path /path/to/repo` (separate command, future work).
Attempting a local path via `--repo` returns:
`"--repo: path-style argument not supported; use a slug or remote URL"`

---

## Edge-case behaviors

### 1. Interaction between auto-detect and an existing `.bn` marker

`.bn` is authoritative when present. Auto-detect is skipped if:
- `.bn` exists with `project` set → prefix chain priority 4 fires, auto-detect never runs
- `.bn` exists with `repo` set → repo chain priority 3 fires, auto-detect skipped

Example: a mono-repo with `project=monorepo` in `.bn`. `bn create "Fix bug"`
inside that repo uses `prefix=monorepo` and no repo link, regardless of what
`git config --get remote.origin.url` returns. To opt into auto-detect, remove
`project` from `.bn` or use `--project` to override to a specific prefix that
triggers lookup.

### 2. Outside any git repo

`gitRoot()` returns `("", false, nil)`. Auto-detect short-circuits with no
result. Repo chain falls through to `.bn` marker (if found by directory walk)
or no-repo. Prefix chain falls through normally.

No error is produced by the auto-detect path alone — it is silent when no git
context exists. The command may error downstream if prefix is required.

### 3. Local-only repo — no remote configured

`gitRoot()` succeeds (returns the toplevel) but `RemoteURL()` returns
`("", false, nil)` (no `remote.origin` configured).

Fallback slug: the **basename of the git toplevel directory**, normalized via
`normalizeSlugCandidate`. Example: `/home/alice/my_project` → slug candidate
`my_project`.

`AutoRegisterRepo` is called with an empty `RemoteURL`. The store stores it
with empty `remote_url` and treats idempotency by slug (not URL) for this case.
Subsequent calls from the same directory produce the same slug via the basename
fallback; calls from a different directory with the same basename collide and
trigger the slug-disambiguation algorithm normally.

The `.bn` marker is written after successful registration with the derived slug
so future invocations skip auto-detect.

### 4. Nested / submodule repos

`git rev-parse --show-toplevel` returns the **inner repo's** root (standard git
behavior for submodules). This is the correct behavior: each submodule is an
independent git repo and is treated as an independent registered repo in `bn`.

No special handling is required. The outer repo and inner submodule generate two
separate `bn_repos` rows with distinct normalized URLs and distinct prefixes.

### 5. Bare / detached repos

**Bare repos** (`git init --bare`): `git rev-parse --show-toplevel` exits
non-zero ("fatal: this operation must be run in a work tree"). `gitRoot()`
returns `("", false, nil)`. Auto-detect produces no result — same path as
"outside any git repo" (edge case 2). No crash.

**Detached HEAD**: does not affect `--show-toplevel`. Auto-detect works
normally. `git config --get remote.origin.url` still reads from `.git/config`,
which is unaffected by HEAD state. No special handling needed.

---

## Error messages

| Situation | Message |
|---|---|
| `--repo <slug>` not found | `repo "{slug}" not found; to register a repo provide a remote URL` |
| `--repo <path>` style argument | `--repo: path-style argument not supported; use a slug or remote URL` |
| All slug candidates exhausted | `AutoRegisterRepo: all slug candidates exhausted for URL "{url}"` (wraps `ErrSlugExhausted`) |
| Prefix required and missing | `project prefix required: use --project, set BN_PROJECT, or run bn init --prefix=<project>` (existing message, unchanged) |

---

## Implementation responsibilities

| Issue | Responsibility |
|---|---|
| beans-3u3 | Wire the full resolution chain in `cmd/bn/app.go`; add `--repo` flag to `newRootCmd` persistent flags |
| beans-75l | `GetRepoBySlug(prefix, slug)` — the form-1 `--repo` lookup path |
| beans-7kv | `bn repo add --path` (local-path registration, deferred) |
| beans-kf2 | `gitResolver` seam (complete — provides `Toplevel` + `RemoteURL`) |

---

## Non-decisions (explicitly deferred)

- **Multiple remotes**: `git config --get remote.origin.url` uses `origin` by
  convention. Support for `--remote-name` to select a different remote is
  deferred to a future issue.
- **`file://` URL idempotency by path**: for local repos with a `file://`
  remote, `NormalizeRemoteURL` produces a canonical `file:///abs/path` form;
  idempotency is by that canonical URL, same as remote repos.
- **Cross-prefix auto-detect**: auto-detect never reads issues from multiple
  prefixes in one command. Each invocation is scoped to a single resolved
  prefix.
