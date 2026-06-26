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
with the auto-detect case appended after the marker:

| Priority | Source | Notes |
|---|---|---|
| 1 | `--project <prefix>` flag | Explicit always wins |
| 2 | `BN_PROJECT` env var | Per-shell override |
| 3 | `.bn` marker `project` field | Persisted across commands in a git repo |
| 4 | Cwd git auto-detect | Only when 1, 2, and 3 are all absent |
| 5 | Error: prefix required | Emitted at command execution, not at init |

Auto-detect (priority 4) fires **only** when priorities 1, 2, and 3 are all
absent. If `.bn` exists with a `project` field, auto-detect is skipped
entirely. This gives users explicit control via `bn init --prefix=<prefix>`
without fight with auto-detect on every invocation.

When auto-detect fires, the derived prefix comes from `AutoRegisterRepo`'s
return value (`Repo.Prefix`). A prefix explicitly set via `--project` or
`BN_PROJECT` always wins: if auto-detect also fires (e.g. on a repo-aware
command), the resolved auto-detect prefix is used **only for the repo
association** — the explicit prefix remains the command's operating prefix.
If the two disagree (explicit prefix ≠ auto-detected prefix), the explicit
prefix is used and the issue is created under that project with the
auto-registered repo's slug attached.

---

## Repo resolution chain

New; applies to commands that accept a repo context (e.g. `bn create --repo`):

| Priority | Source | Notes |
|---|---|---|
| 1 | `--repo <value>` flag | Explicit slug or remote URL |
| 2 | `.bn` marker `repo` field | Slug written by `bn init --repo` |
| 3 | Cwd git auto-detect | `git rev-parse --show-toplevel` → URL → `AutoRegisterRepo` |
| 4 | No repo context | `IssueRepoInput` is nil; issue created without a repo link |

Priority 1 supersedes all others: when `--repo` is provided, auto-detect and
the `.bn` marker repo field are both skipped. The `.bn` marker repo field
(priority 2) is checked before auto-detect (priority 3). Commands that require
a repo context error at priority 4 with: `"this command requires a repo; use
--repo <slug-or-url>"`.

### Creation-time commit snapshot

When `bn create` creates an issue with a repo link, it makes a best-effort
snapshot of the exact cwd `HEAD` commit and stores it on the issue's
`bn_issue_repos.creation_commit` field. This is immutable issue metadata: it
describes where the associated repo was when the issue was created. It is not a
field on the `bn_repos` registry row, and changing a repo's registration later
does not rewrite existing issue snapshots.

The snapshot is captured only after the repo-resolution chain selects the issue
repo, and only when the cwd git identity resolves to the same registered repo
row:

| Selected repo source | Capture rule |
|---|---|
| Cwd git auto-detect | Auto-detect registers or resolves the cwd repo, selects that same repo for the issue, then records `git rev-parse HEAD` from that cwd repo. |
| `.bn` marker `repo` field | The marker selects the repo. `bn create` still probes cwd git identity and records HEAD only if the cwd remote (or local-only `file://` identity) resolves to that marker repo row. |
| Explicit `--repo` | The flag selects the repo. `bn create` records cwd HEAD only if the cwd repo identity resolves to the same repo row selected by the flag. |
| No repo context | No `bn_issue_repos` row is created, so no commit snapshot exists. |

The comparison uses registered repo identity, not string equality on the raw
flag or marker value. Remote URLs are normalized through the repo registry, and
local-only repos use the same synthesized `file:///abs/git-toplevel` identity
described below.

Dirty state is intentionally not recorded. The snapshot is just the full
lowercase 40-character object ID returned by `git rev-parse HEAD`; it does not
include whether the worktree had staged or unstaged changes, untracked files, or
a generated diff.

If the cwd repo does not match the selected repo, `creation_commit` is left
empty and the issue link still points at the selected repo. If the prefix guard
rejects an auto-detected repo because the command is operating under a different
explicit project prefix, no repo link is attached and therefore no snapshot is
stored.

Snapshot capture is best-effort. It leaves `creation_commit` empty when cwd is
outside a git worktree, the cwd repo cannot be resolved in the registry, HEAD is
unborn or otherwise unavailable, git returns an error, or the resolved HEAD is
not a full lowercase 40-character hex object ID. These failures do not block
issue creation.

### Repo-aware commands and auto-detect opt-in

Auto-detect is not a global behavior — each command must explicitly opt in by
calling the resolution helper. The commands that opt in are limited to those
that benefit from "just work in my current repo" ergonomics (e.g. `bn create`,
`bn list`, `bn ready`). Commands that operate on specific issues by ID do not
opt in. The exact set is an implementation detail of beans-3u3; this record
defines the chain, not the set.

Whether `--repo` is a root persistent flag or per-command is left to beans-3u3
to decide based on ergonomics. If persistent, the value is always available;
if per-command, each opt-in command declares it.

---

## What `--repo` accepts

`--repo` accepts exactly two forms, distinguished by a CLI-level pre-check
**before** `NormalizeRemoteURL` is called:

### Discriminator (applied in order)

1. If the value contains `://` → **URL form** (Form 2).
2. If the value starts with `git@` → **URL form** (Form 2, SCP-syntax SSH).
3. If the value starts with `/`, `./`, `../`, `~`, or matches a Windows drive
   letter pattern (`C:\`) → **path form** → rejected (Form 3 below).
4. Otherwise → **slug form** (Form 1).

This pre-check is applied before `NormalizeRemoteURL` because the library's
`normalizeBarePath` accepts absolute paths and converts them to `file://` —
the CLI explicitly rejects them to keep `--repo` focused on slugs and explicit
remote URLs.

### Form 1: Registered slug

A value not matching Form 2 or Form 3 patterns — e.g.:

```
--repo myapp
--repo owner-myapp
--repo github-owner-myapp
```

Behavior: `GetRepoBySlug(slug, slug)` (under topology (a), prefix == slug, so
both arguments are the slug). `GetRepoBySlug` already exists at
`store/repo_store.go:424`. If the slug is not found: error at the CLI layer
(not the store layer) — NOT auto-register-by-name, because a bare slug carries
no URL from which to derive the canonical identity.

Error (emitted by `cmd/bn`, not `store`): `repo "myapp" not found; to register a repo provide a remote URL`

### Form 2: Remote URL

Any string containing `://` or starting with `git@`:

```
--repo https://github.com/alice/myapp
--repo git@github.com:alice/myapp
--repo ssh://git@github.com/alice/myapp
```

Note: `file://` URLs ARE accepted (they pass through `NormalizeRemoteURL`), but
bare local paths (`/home/alice/myapp`) are rejected (Form 3) even though the
library would accept them. If a user wants to pass a local path, they must use
the explicit `file:///home/alice/myapp` form.

Behavior: `AutoRegisterRepo(ctx, AutoRegisterInput{RemoteURL: value})`.
Idempotent: returns the existing row if already registered. Sets both the repo
and the prefix from the returned `Repo.Prefix`.

### Form 3 (rejected at CLI layer): Local filesystem path

Bare paths starting with `/`, `./`, `../`, `~`, or Windows drive letters are
rejected with:

`"--repo: path-style argument not supported; use a slug or a remote URL (for local repos, use file:///abs/path)"`

---

## Edge-case behaviors

### 1. Interaction between auto-detect and an existing `.bn` marker

`.bn` is authoritative when present:
- `.bn` with `project` set → prefix chain priority 3 fires, auto-detect (priority 4) never runs.
- `.bn` with `repo` set → repo chain priority 2 fires, auto-detect (priority 3) skipped.

To opt out of a persisted `.bn` marker and use auto-detect instead: delete the
`.bn` file entirely (not just remove the `project` field — the parser at
`app.go:352` requires `project` to be present and errors if the file exists but
the field is absent). Alternatively, use `--project` or `--repo` flags to
override per-invocation without touching the file.

### 2. Outside any git repo

`gitRoot()` returns `("", false, nil)`. Auto-detect short-circuits with no
result. Repo chain falls through to `.bn` marker (if found by the directory
walk in `activeProjectMarkerPath`) or no-repo (priority 4). Prefix chain falls
through normally.

No error is produced by the auto-detect path alone — it is silent when no git
context exists. The command errors downstream only if prefix is required and no
other source provided it.

### 3. Local-only repo — no remote configured

`gitRoot()` succeeds (returns the toplevel) but `RemoteURL()` returns
`("", false, nil)` (no `remote.origin` configured).

Because `AutoRegisterRepo` calls `NormalizeRemoteURL` first and that function
returns `ErrNoRemote` on an empty string, passing an empty URL is not valid.
The cwd auto-detect path MUST synthesize a `file://` URL from the git toplevel
instead:

```
file_url = "file://" + gitToplevl   // e.g. "file:///home/alice/my_project"
```

`AutoRegisterRepo` is then called with `RemoteURL: file_url`. `NormalizeRemoteURL`
accepts this and produces a canonical form. Idempotency is by the canonical
`file:///abs/path` URL (consistent with the "`file://` URL idempotency by path"
non-decision at the bottom of this record).

Example:
- Git toplevel: `/home/alice/my_project`
- Synthesized URL: `file:///home/alice/my_project`
- Candidate slug from URL parse: `my_project`
- Registration: creates prefix=`my_project`, slug=`my_project`
- On re-run from the same directory: `GetRepoByRemoteURL("file:///home/alice/my_project")` hits the idempotency fast-path and returns the existing row.

The synthesized `file://` URL is written to the `.bn` marker's `remote` field
so future invocations use the `.bn` marker (priority 2) and skip auto-detect.

Note: the file URL key is the **resolved absolute path of the git toplevel**,
not the cwd. This ensures two sessions in different subdirectories of the same
repo resolve to the same registration.

Creation-time commit snapshots for local-only repos use this same synthesized
`file://` identity. A local-only issue captures cwd HEAD only when the selected
repo row is the row registered for that exact git toplevel path.

### 4. Nested / submodule repos

`git rev-parse --show-toplevel` returns the **inner repo's** root (standard git
behavior for submodules — git considers the submodule's `.git` when resolving
`--show-toplevel`). This is the correct behavior: each submodule is an
independent git repo and is treated as an independent registered repo in `bn`.

No special handling is required. The outer repo and inner submodule generate two
separate `bn_repos` rows with distinct normalized URLs (or distinct `file://`
paths for local-only) and distinct prefixes.

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
| `--repo <path>` style argument | `--repo: path-style argument not supported; use a slug or a remote URL (for local repos, use file:///abs/path)` |
| All slug candidates exhausted | `AutoRegisterRepo: slug disambiguation exhausted all candidates` (wraps `ErrSlugExhausted`) |
| Prefix required and missing | `project prefix required: use --project, set BN_PROJECT, or run bn init --prefix=<project>` (existing message, unchanged) |
| Repo required and missing | `this command requires a repo; use --repo <slug-or-url>` |

---

## Implementation responsibilities

| Issue | Responsibility |
|---|---|
| beans-3u3 | Wire the full resolution chain in `cmd/bn/app.go`; add `--repo` flag; implement the Form 1/2/3 discriminator; synthesize `file://` URL for local-only repos |
| beans-75l | `GetRepoBySlug` CMD integration — store method exists at `repo_store.go:424`; beans-75l wires it to the CLI `--repo <slug>` path |
| beans-7kv | `bn repo add --path` (local-path registration, deferred) |
| beans-kf2 | `gitResolver` seam (complete — provides `Toplevel` + `RemoteURL`) |

---

## Non-decisions (explicitly deferred)

- **Multiple remotes**: `git config --get remote.origin.url` uses `origin` by
  convention. Support for `--remote-name` to select a different remote is
  deferred to a future issue.
- **`file://` URL idempotency by path**: for local repos (no remote and
  local-only), `NormalizeRemoteURL` on the synthesized `file:///abs/toplevel`
  URL produces a canonical form; idempotency is by that canonical URL, same as
  remote repos. The `file://` synthesizer lives in the cwd-auto-detect layer
  inside `cmd/bn` (not in the store or `gitResolver`).
- **Cross-prefix auto-detect**: auto-detect never reads issues from multiple
  prefixes in one command. Each invocation is scoped to a single resolved
  prefix.
- **`--require-repo` flag**: referenced as a possible future flag on commands
  that mandate a repo context; not implemented in beans-3u3 scope.
