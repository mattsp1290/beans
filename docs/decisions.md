# Decisions

Dated records of the choices behind bn. Each entry says what was decided,
what was rejected, and why.

## 2026-09-10: Hub-only topology

Every project's tracker lives under `~/.beans/hub/projects/<name>/`, one git
repository shared by every machine and agent. Rejected: a local `.beans/`
inside each code repository (two mental models, Obsidian ignores dotfolders,
claims made in one worktree are invisible in another). Rejected: a dedicated
tracker branch inside the code repository (hides the files from the default
branch on GitHub; verified in a scratch repository during planning).

## 2026-09-10: One commit per mutation

Every mutating command takes a lock, commits stray hand edits, pulls with
rebase, applies the operation, commits through the real index with `git add
<paths>`, and pushes. On rejection it fetches, rebases, and re-derives the
operation on the new tip, up to three times. Only the commit this run
created (identified by a `Bn-Run` nonce trailer, never by its subject) can
be discarded and re-applied; hand-edit, recovered-partial, and offline
commits are always carried forward by the rebase, and a conflict there is the
user's to resolve with `bn sync`. Rejected: never committing (abandons
multi-machine use). Rejected: scratch-index plumbing commits (leave the file
both staged and unstaged).

## 2026-09-10: Markdown plus YAML frontmatter, spliced not re-serialized

Issues, memories, and docs are markdown with YAML frontmatter so Obsidian
renders them and long text reads well. yaml.v3 normalizes indentation and
blank lines when it re-serializes a document, so the codec does not: it
records the original frontmatter lines and each key's span and splices only
the bn-owned keys whose values changed. `Encode(Parse(x)) == x` holds byte
for byte for every valid file. Rejected: TOML or YAML per issue (no Obsidian
rendering, poor for long text). The planned fallback (normalize bn-owned
scalars) was not needed.

## 2026-09-10: Hash ids, frozen slugs

Ids are `<prefix>-<4 chars>` from `crypto/rand`, unique across the hub;
the slug in the filename is frozen at creation and `bn update --title` never
renames the file. Rejected: sequential Jira-style ids (collide across
machines and agents).

## 2026-09-10: One module, one binary, embedded UI

`libs/beans` and `apps/bean-counter` became one module at the root with the
Svelte app embedded through `go:embed`. This reverses the earlier
bean-counter decision that the Go binary must not serve frontend assets:
that decision assumed a separately deployed UI behind nginx; with `bn serve`
on a laptop and the hub on GitHub there is no second process to justify.
Rejected: keeping `libs/` and `apps/` (no second consumer existed).

## 2026-09-10: Shell out to the system git

`gitops` runs the user's `git` binary so credential helpers, ssh agents, and
rebase semantics are exactly the user's. Every command carries `-c
user.name`/`-c user.email` (the actor; the hub clone's email or
`<actor>@bn.local`) because a rebase re-creates commits and a fresh machine
or CI runner has no global identity. Rejected: go-git.

## 2026-09-10: Docs are read-only in the UI

`bn serve` renders docs and resolves wikilinks and backlinks; editing docs
happens in an editor or Obsidian. Issues are writable through the API.

## 2026-09-10: No derived index cache

`vault.Load` over 5,000 generated issues takes about 335 ms on the planning
machine, so the SQLite cache the plan allowed stays deferred until a real
hub is measured slow.

## 2026-09-10: Import-time actor normalization

bd exports carry display names (`Matt Spurlin`) and emails as actors; bn
actors are short whitespace-free tokens, so `bn import bd` writes
`issue.Slug` of the display name (`matt-spurlin`) and reports every mapping
in its dry run.

## 2026-09-10: Log-line fields are whitespace-free tokens

Every log line bn writes replaces runs of whitespace in the actor, repo, and
branch fields with `-` (a git `user.name` of `Matt Spurlin` is logged as
`Matt-Spurlin`), so the line grammar stays unambiguous even when a branch
name contains `)`. This applies to every command, not only the import.

## 2026-09-10: Test doubles are not part of the public API

`gitops.FakeResolver` and `gitops.RecordingRunner` live in `_test.go` files
only and are never exported from `gitops`. `vault`, `issue`, `markdown`, and
`gitops` are public at the module root so a future consumer (the
eino-agent-extensions request) can import them, but no stability commitment
exists until one appears, and test doubles are excluded from that surface
regardless.

## 2026-09-10: Fetch throttle keyed on the last attempt

Reads fetch at most once per throttle window, measured from the last fetch
attempt rather than the last success, so an offline machine does not pay a
network timeout on every read; `bn status` still reports the last successful
fetch.

## 2026-09-10: Handoffs are their own note kind

Session handoffs capture continuation context rather than schedulable work.
They live in project `handoffs/` directories with stable IDs and an optional
issue attachment, rather than becoming issue types or untyped docs. This keeps
them out of readiness, blockers, and issue archival while retaining search,
backlinks, and the normal git-backed write pipeline.
