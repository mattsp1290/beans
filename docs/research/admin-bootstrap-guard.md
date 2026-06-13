# Repo Admin Bootstrap Guard Decision

Issue: `beans-3xc`

This decision replaces the current Postgres-only first-admin guard in
`store/repo_store.go` with a portable design for PostgreSQL, MySQL, and SQLite.
The security invariant is:

> For a project prefix, exactly one bootstrap operation may create the initial
> repo admin, even under concurrent calls.

## Current Behavior

`Store.AddRepoAdmin(..., bootstrap=true)` currently:

1. Begins a pgx transaction.
2. Runs `SELECT pg_advisory_xact_lock(hashtext($1))` keyed by project prefix.
3. Inserts `(prefix, targetActor)` only when no `bn_project_admins` row exists
   for that prefix.
4. Returns `ErrUnauthorized` when another admin already exists or a concurrent
   bootstrap wins first.

Relevant surfaces:

| Surface | Location | Portability issue |
| --- | --- | --- |
| Transaction advisory lock | `store/repo_store.go:104`, `store/repo_store.go:110` | `pg_advisory_xact_lock(hashtext(...))` is Postgres-only. |
| `INSERT ... SELECT ... WHERE NOT EXISTS` | `store/repo_store.go:113` | Without the advisory lock, concurrent inserts for different actors can both observe an empty admin set. |
| `ON CONFLICT DO NOTHING` | `store/repo_store.go:119`, `store/repo_store.go:136` | Replace with GORM `clause.OnConflict` or dialect-aware raw SQL. |
| Admin schema | `schema/migrations/0004_bn_repos.sql:71` | `PRIMARY KEY(prefix, actor)` prevents duplicate actors, but it does not enforce one bootstrap winner per prefix. |

## Decision

Use a **constraint-backed bootstrap claim table**. Do not introduce
dialect-specific advisory locks for this path.

Add a table with one row per project prefix:

```sql
CREATE TABLE bn_project_admin_bootstraps (
    prefix     <prefix key type> NOT NULL PRIMARY KEY REFERENCES bn_projects(prefix) ON DELETE CASCADE,
    actor      <actor key type>  NOT NULL,
    created_at <dialect timestamp> NOT NULL
);
```

`<prefix key type>` and `<actor key type>` must match the dialect-specific key
types chosen for `bn_projects.prefix` and `bn_project_admins.actor`. Do not use
unbounded MySQL `TEXT` as a primary key. Use a bounded string type that preserves
exact-prefix uniqueness, such as `VARCHAR(<project-prefix-limit>)`, and keep the
same limit in Go validation and every dialect migration.

Bootstrap should run in one transaction:

1. Validate `targetActor` is non-empty.
2. Insert the bootstrap claim row `(prefix, targetActor, nowUTC)` with
   insert-ignore/`ON CONFLICT DO NOTHING` semantics.
3. If the claim insert affects zero rows, return `ErrUnauthorized`.
4. Insert `(prefix, targetActor, nowUTC)` into `bn_project_admins`.
5. Commit.

The unique/primary-key constraint on `bn_project_admin_bootstraps.prefix` is the
concurrency guard. It is portable across Postgres, MySQL, and SQLite and does
not depend on transaction advisory locks, `SELECT ... FOR UPDATE` syntax, or
serializable isolation.

The bootstrap claim is historical and should not be deleted when admins change.
This intentionally makes bootstrap a once-per-project operation rather than a
recovery path after all admins are removed.

The `actor` in `bn_project_admin_bootstraps` is historical metadata. It should
not foreign-key to `bn_project_admins(prefix, actor)`, because the original
bootstrap actor may later be removed while the one-time bootstrap claim must
remain.

## Last-Admin Removal Rule

Because bootstrap becomes one-time, `RemoveRepoAdmin` must reject removal of the
last admin for a project. Otherwise a project could become permanently
unadministrable.

`RemoveRepoAdmin` must serialize removals for a prefix before it checks whether
the target is the final admin. Use the per-prefix bootstrap claim row as the
portable guard object, and centralize the dialect differences in one store
helper:

- Postgres/MySQL: select the `bn_project_admin_bootstraps` row for the prefix
  with a row-level update lock, using GORM's locking clause or dialect-aware raw
  SQL.
- SQLite: use a write transaction mode that serializes writers before the admin
  count is read.
- If the guard row is missing, treat the database as corrupt or not migrated and
  return a wrapped conflict/internal error rather than allowing removal.

`RemoveRepoAdmin` should then run in the guarded transaction:

1. Lock the bootstrap claim row for `prefix`.
2. Re-authorize `actor` inside the transaction.
3. Count or query admins for the prefix while the guard is held.
4. If `targetActor` is the last admin, return a sentinel error.
5. Delete `(prefix, targetActor)`.
6. If no row was deleted, return `ErrNotFound`.
7. Commit.

Prefer a new sentinel such as `ErrLastAdmin` so CLI callers can show a clear
message. If avoiding a new sentinel during the first GORM conversion, wrap
`ErrConflict` with context and add a follow-up issue to split the sentinel.

## Error Semantics

| Case | Result |
| --- | --- |
| Empty `targetActor` | Current validation error remains. |
| First bootstrap wins | `nil`; `targetActor` appears in `ListRepoAdmins`. |
| Concurrent bootstrap loses | `ErrUnauthorized`. |
| Bootstrap after a claim already exists | `ErrUnauthorized`, even if admins were later changed. |
| Non-bootstrap add by admin | Idempotent success when target already exists. |
| Non-bootstrap add by non-admin | `ErrUnauthorized`. |
| Remove missing admin | `ErrNotFound`. |
| Remove last admin | `ErrLastAdmin` or conflict-wrapped equivalent. |

Duplicate-key detection must be dialect-aware:

- Postgres: SQLSTATE `23505`.
- MySQL: duplicate entry error `1062`.
- SQLite: unique constraint failure from the selected SQLite driver.

GORM implementations can avoid most driver inspection by using
`clause.OnConflict{DoNothing: true}` and checking `RowsAffected`. Where raw SQL
is still used, normalize duplicate constraint errors in one helper rather than
checking error strings inline in `AddRepoAdmin`.

## Schema and Migration Requirements

Add `bn_project_admin_bootstraps` to the repo migration set for every dialect.
The schema must preserve:

- One bootstrap claim per project prefix.
- Cascade delete when a project prefix is deleted.
- Non-empty actor validation in Go; dialect-specific `CHECK` syntax is optional
  and should not be the only enforcement.
- UTC timestamp policy from `docs/research/timestamp-sql-strategy.md`.
- Bounded key-column types for MySQL-compatible primary and foreign keys.

The existing `bn_project_admins` primary key remains `(prefix, actor)`.

Existing databases require a backfill in the same migration that creates the
claim table. For every prefix with at least one row in `bn_project_admins`,
insert exactly one bootstrap claim before the new store code is released. Choose
a deterministic historical actor, such as the actor from the earliest
`created_at` row with `actor` as a tie-breaker, and set `created_at` to that
row's timestamp. After backfill, `AddRepoAdmin(..., bootstrap=true)` must return
`ErrUnauthorized` for every prefix that already had admins before migration.

## Implementation Checklist

- Add a store error sentinel or documented conflict mapping for last-admin
  removal.
- Add a repo-admin bootstrap model/table for GORM or a per-dialect migration
  entry if the migration strategy keeps SQL files.
- Change `AddRepoAdmin(..., bootstrap=true)` to insert the claim row before the
  admin row in the same transaction and map `RowsAffected == 0` to
  `ErrUnauthorized`.
- Remove `pg_advisory_xact_lock(hashtext($1))`.
- Replace `ON CONFLICT DO NOTHING` with GORM `clause.OnConflict{DoNothing:true}`
  or dialect-aware SQL.
- Add the existing-admin backfill to migrations and an upgraded-database
  regression test.
- Change `RemoveRepoAdmin` to lock the per-prefix bootstrap claim row before
  re-authorizing, counting admins, and rejecting deletion of the final admin.
- Keep `ListRepoAdmins` ordering by actor.
- Keep `AuthorizeRepoAdmin` as the authorization gate for non-bootstrap
  mutations.
- Update CLI error handling in `cmd/bn/cmd_repo.go` if a new `ErrLastAdmin`
  sentinel is added.

## Tests Required

- Concurrent bootstrap calls for the same prefix with different actors produce
  exactly one successful call and exactly one admin row.
- The losing concurrent bootstrap returns or wraps `ErrUnauthorized`.
- Bootstrap after a prior bootstrap claim returns `ErrUnauthorized`.
- Upgraded databases with existing `bn_project_admins` rows receive one
  backfilled bootstrap claim per prefix, and post-upgrade bootstrap returns
  `ErrUnauthorized`.
- Removing the last remaining admin fails with the chosen sentinel or conflict
  mapping.
- Removing one admin from a project with two admins succeeds and leaves the
  other admin authorized.
- Concurrent removals from a two-admin project, such as Alice removing Bob while
  Bob removes Alice, leave at least one admin. Exactly one removal may succeed;
  the losing call should return `ErrLastAdmin`, `ErrNotFound`, or another
  documented conflict mapping that preserves one remaining admin.
- Non-bootstrap add remains idempotent for an existing target actor.
- CLI tests cover the user-facing message for last-admin removal once the new
  sentinel or conflict mapping exists.
- Contract tests run against SQLite by default and Postgres/MySQL containers
  when integration tests are enabled.

## Rejected Alternatives

### Dialect-Specific Locks

Postgres advisory locks, MySQL named locks, and SQLite write transactions can
all serialize bootstrap attempts, but they spread a security-sensitive invariant
across three lock implementations. They also complicate tests because lock
scope, timeout behavior, and connection affinity differ by backend.

### Plain `INSERT ... WHERE NOT EXISTS`

This is unsafe without a lock or unique prefix-level constraint. The existing
`PRIMARY KEY(prefix, actor)` allows two concurrent first-admin inserts when the
actors differ.

### Prefix-Only Primary Key on `bn_project_admins`

Making `bn_project_admins.prefix` unique would enforce one admin total, not one
bootstrap winner. The product behavior requires multiple admins after bootstrap.
