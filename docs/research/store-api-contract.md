# Store API Contract for CLI Consumers

Issue: `beans-nl6`

This document defines the public `store.Store` behavior that `cmd/bn/*` depends
on before the GORM/multi-database rewrite. The conversion should preserve these
contracts even when the implementation stops using pgx, Postgres placeholders,
and Postgres-only SQL.

## Real Consumers

The active production consumer is the bn CLI under `cmd/bn/*`. It constructs a
single `*store.Store` in `appState.initConnWithOptions`, assigns it to
`appState.store`, and closes it from `main.go` after command execution.

There is no `tracker` package in this module. Comments in `store/store.go` and
`store/errors.go` that mention `tracker.Tracker` or `tracker.CategoryNotFound`
are stale documentation only. Do not create a tracker-adapter task from those
comments, and do not preserve any imaginary tracker interface beyond the store
methods actually used by `cmd/bn/*`.

## Lifecycle and Configuration

| API | CLI consumers | Contract to preserve |
| --- | --- | --- |
| `store.New(ctx, Config)` | `cmd/bn/app.go` | Opens the configured store, runs migrations before returning, and returns a ready `*Store` or a wrapped connection/migration error. |
| `(*Store).Close()` | `cmd/bn/main.go`, tests | Idempotent; may be called on nil receiver; releases resources without returning an error. |
| Store methods after close | Any command path if reused accidentally | Return or wrap `ErrPoolClosed`/future closed-store sentinel so callers can use `errors.Is`. |
| `store.Config` | `cmd/bn/app.go` | Currently built from `BN_DSN` only. Future dialect selection may extend this, but command code should not gain driver-specific persistence logic. |

The CLI still contains Postgres-specific help and error text. That text should
change when dialect selection changes, but it is not a store API contract.

## Sentinel Error Contract

Callers branch with `errors.Is`, never string matching. The rewrite must keep
sentinel wrapping stable even when driver-specific errors change.

| Sentinel | Current producers | CLI behavior |
| --- | --- | --- |
| `ErrNotFound` | Missing issue, repo, repo alias/admin, dependency edge | CLI commands turn this into not-found messages for show/update/close/delete/deps/repo operations. |
| `ErrConflict` | Repo duplicate slug/alias and last-admin removal; future unique conflicts | `cmd/bn/cmd_repo.go` maps to `repo ...: conflict`. |
| `ErrUnauthorized` | Repo admin and repo registry mutations without admin rights | `cmd/bn/cmd_repo.go` maps to `repo ...: unauthorized`. |
| `ErrDisabled` | Creating/updating issue repo target against disabled repo | Currently bubbles through command errors; preserve `errors.Is` for future CLI handling. |
| `ErrDuplicateDep` | Adding an existing dependency edge | `cmd/bn/cmd_dep.go` reports duplicate dependency. |
| `ErrCycle` | Adding a dependency that would create a cycle | `cmd/bn/cmd_dep.go` reports cycle. |
| `ErrPoolClosed` | Store access after close | Tests and future contract harness should preserve closed-store detection. |

## Issue API Contract

| API | CLI consumers | Contract to preserve |
| --- | --- | --- |
| `EnsureProject(ctx, prefix)` | init/create/import/memory/repo add/admin add | Idempotently creates a project prefix. Existing project is success. |
| `ProjectExists(ctx, prefix)` | tests | Reports whether a project exists without side effects. |
| `CreateIssue(ctx, CreateIssueInput)` | `bn create` | Generates a stable `prefix-*` ID, defaults state to `open`, stores labels as a JSON-compatible slice, writes optional repo target atomically, and returns populated repo target when requested. Missing/disabled repo target returns `ErrNotFound`/`ErrDisabled`; validation failures roll back the issue row. |
| `GetIssue(ctx, id)` | show/update tests/CLI helpers | Returns issue plus `BlockedBy` and optional repo target. Missing ID wraps `ErrNotFound`. |
| `ListIssues(ctx, ListFilter)` | list/export/create rollback checks | Prefix-scoped listing with optional state filter; returned issues include blockers and repo targets. |
| `ReadyIssues(ctx, prefix, terminalStates, activeStates)` | ready command | Returns active issues whose blockers are all terminal under caller-provided state sets. Custom terminal states such as `done` must work. |
| `UpdateIssue(ctx, id, UpdateIssueInput)` | update command | Partial update; nil fields mean keep. `AppendNotes` appends, repo target replacement is atomic with issue updates, and invalid/missing/disabled repo targets roll back all changes. Missing issue wraps `ErrNotFound`. |
| `CloseIssue(ctx, id, actor, reason)` | close command | Idempotently transitions non-closed issues to `closed`, records a note when state changes, and returns `ErrNotFound` for missing IDs. |
| `DeleteIssue(ctx, id)` | delete command | Deletes issue and cascades owned rows/dependency edges. Missing ID wraps `ErrNotFound`. |
| `GetIssueStatesByIDs(ctx, ids)` | import path | Returns states for existing IDs only; missing IDs are absent from the map. |

## Dependency API Contract

| API | CLI consumers | Contract to preserve |
| --- | --- | --- |
| `AddDep(ctx, childID, parentID)` | `bn dep add`, import | Requires both issues exist, rejects cycles with `ErrCycle`, rejects duplicate edge with `ErrDuplicateDep`, and preserves the graph under concurrent/import paths. |
| `RemoveDep(ctx, childID, parentID)` | `bn dep remove` | Deletes an existing edge; missing edge wraps `ErrNotFound`. |
| `ListDeps(ctx, prefix)` | `bn dep list`, cycle detection command | Returns all dependency edges for a prefix as `DepEdge{IssueID, BlockedByID}`. |

## Import API Contract

| API | CLI consumers | Contract to preserve |
| --- | --- | --- |
| `ImportIssues(ctx, items, terminalStates)` | legacy import helper/tests | Create/update import that must not regress terminal issues back to non-terminal states. |
| `ImportIssuesFull(ctx, items, ImportOptions)` | `bn import` | Supports create-only and merge modes, deduplicates by ID with later fields winning in the input batch, preserves terminal-state rules, counts created/updated/skipped/dependency outcomes, skips cross-prefix conflicts, skips invalid dependency edges, and retries serialization/conflict races without leaking partial writes. |

`ImportResult` field names are CLI JSON/reporting contract. Do not rename or
change their meaning during the store rewrite.

## Memory API Contract

| API | CLI consumers | Contract to preserve |
| --- | --- | --- |
| `InsertMemory(ctx, MemoryInput)` | `bn remember` | Empty prefix stores global memory (`Prefix == nil` on return); non-empty prefix must reference an existing project. Returns ID, body, optional type, tags, and UTC `CreatedAt`. |
| `SearchMemories(ctx, query, MemoryFilter)` | `bn memories` | When `All=false`, searches `Prefix` plus global memories; when `All=true`, ignores prefix. Empty and whitespace-only queries use recent ordering. Type filter is exact. Tags require all requested tags. Limit <= 0 uses default. Results use UTC `CreatedAt`, decoded tags, and deterministic `created_at DESC, id DESC` tie-breaks for empty search and rank ties. |

Backend-specific FTS ranking is not a cross-dialect public contract. Inclusion,
exclusion, scope, filters, limits, field round trip, and deterministic
tie-breaking are the shared contract.

## Repo Registry API Contract

| API | CLI consumers | Contract to preserve |
| --- | --- | --- |
| `AddRepoAdmin(ctx, prefix, targetActor, actor, bootstrap)` | `bn repo admin add` | Bootstrap succeeds only for the first admin. Later bootstrap attempts return `ErrUnauthorized`. Non-bootstrap add requires `actor` already be an admin and is idempotent for existing target. |
| `ListRepoAdmins(ctx, prefix)` | `bn repo admin list` | Returns admins ordered by actor. |
| `RemoveRepoAdmin(ctx, prefix, targetActor, actor)` | `bn repo admin remove` | Requires `actor` to be admin, removes existing target, missing target wraps `ErrNotFound`, unauthorized actor returns `ErrUnauthorized`, and removing the final admin returns `ErrConflict` while preserving the admin. |
| `AuthorizeRepoAdmin(ctx, prefix, actor)` | repo mutation gates | Returns nil only for current admins; otherwise `ErrUnauthorized`. |
| `CreateRepo(ctx, CreateRepoInput)` | `bn repo add`, issue repo setup tests | Requires admin actor. Defaults display name to slug, normalizes default branch/clone strategy, validates target, writes aliases and audit atomically, returns metadata map and UTC timestamps. Duplicate slug or alias wraps `ErrConflict`; failed alias insert rolls back repo row. |
| `UpdateRepo(ctx, prefix, slug, UpdateRepoInput)` | `bn repo update` | Requires admin actor. Nil pointer fields mean keep; `Metadata == nil` means keep, non-nil replaces. Alias slice nil means keep; non-nil replaces. Writes audit atomically. |
| `DisableRepo(ctx, prefix, slug, actor)` | `bn repo remove` | Convenience wrapper over `UpdateRepo` that sets `Enabled=false`; preserves authorization and audit semantics. |
| `GetRepoBySlug(ctx, prefix, slug)` | repo show/doctor/update/remove | Missing repo wraps `ErrNotFound`; disabled repos are still returned so callers can decide. |
| `ResolveRepoAlias(ctx, prefix, alias)` | issue routing | Resolves aliases by prefix; missing alias wraps `ErrNotFound`. |
| `ListRepos(ctx, prefix, includeDisabled)` | repo list | Orders by slug; excludes disabled repos unless requested. |
| `InsertRepoAudit(ctx, RepoAuditInput)` | tests/future audit callers | Appends direct audit row, supports repo-scoped and project-scoped rows, stores JSON old/new values, returns UTC `CreatedAt`. |
| `ListRepoAudit(ctx, prefix, repoID, limit)` | tests/future audit callers | Orders by `created_at DESC, id DESC`; `repoID` filters when non-empty; limit <= 0 defaults to 50. |

## JSON and Time Contract

- Store-returned timestamps are normalized to UTC.
- JSON-like fields (`Issue.Labels`, memory `Tags`, repo `Metadata`, repo audit
  old/new values, issue repo metadata) must round trip through Go values without
  exposing database-specific JSON types to CLI code.
- Repo metadata/audit maps currently decode numbers through Go's JSON defaults;
  do not introduce driver-native JSON values into public structs.

## Rewrite Guidance

The GORM rewrite may change private helpers, model structs, query builders,
transactions, and dialect adapters. It should not change `cmd/bn/*` command
code except where the command text itself is intentionally made dialect-neutral.

Do not preserve pgx-specific private helper shapes such as `pgx.Row`, `pgx.Tx`,
or `$N` placeholder builders. Preserve the public `Store` method set and the
sentinel/behavior contracts above.
