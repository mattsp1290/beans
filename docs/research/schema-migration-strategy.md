# Schema Migration Strategy Decision

Issue: `beans-s96`

This decision defines how `schema/` should migrate from a single Postgres SQL
directory to PostgreSQL, MySQL, and SQLite support.

## Current State

`schema.Migrate` is Postgres-only:

| Surface | Location | Current behavior |
| --- | --- | --- |
| Embedded migrations | `schema/schema.go:19` | `//go:embed migrations/*.sql` |
| Migration listing | `schema/schema.go:35` | Reads one `schema/migrations` directory |
| Database handle | `schema/schema.go:87` | Accepts `*pgxpool.Pool` |
| Goose DB adapter | `schema/schema.go:92` | `stdlib.OpenDBFromPool(pool)` |
| Goose dialect | `schema/schema.go:106` | `goose.DialectPostgres` |
| Migration lock | `schema/schema.go:100` | Postgres session advisory locker |

The SQL files are also Postgres-only. Examples:

- `BIGSERIAL` in issue notes, memories, and repo audit tables.
- `JSONB` for labels, memory tags, repo metadata/audit, and issue-repo metadata.
- `TIMESTAMPTZ DEFAULT now()`.
- Generated `tsvector`, `to_tsvector`, GIN index, `plainto_tsquery` support.
- POSIX regex `CHECK` constraints.
- `NOT VALID` state-check migration.
- Postgres-specific goose comments and advisory-lock assumptions.

Goose does not transform SQL across dialects, so the existing migration files
cannot be shared verbatim.

## Decision

Use **per-dialect embedded goose migration directories**. Do not use GORM
`AutoMigrate` as the primary schema migration mechanism.

Target layout:

```text
schema/
  migrations/
    postgres/
      0001_bn_init.sql
      0002_bn_memories.sql
      ...
    mysql/
      0001_bn_init.sql
      0002_bn_memories.sql
      ...
    sqlite/
      0001_bn_init.sql
      0002_bn_memories.sql
      ...
```

The current `schema/migrations/*.sql` files should be treated as the source for
the first `postgres/` directory during the migration. MySQL and SQLite get
hand-written equivalent migrations with the same version numbers and semantic
schema objects.

GORM model definitions remain useful for runtime CRUD mapping and tests, but
they should not be the source of truth for DDL. The schema includes raw
dialect-specific objects that `AutoMigrate` cannot express safely, including
FTS, case-sensitive tag collations, migration guard tables, and backfill/cutover
validation.

## Migration API

`schema.Migrate` should accept the opened SQL/GORM handle and explicit driver:

```go
func Migrate(ctx context.Context, db *sql.DB, driver store.Driver) error
```

or an equivalent package-local driver type if importing `store` from `schema`
would create a cycle. The driver value must come from `store.Config.Driver`; do
not infer the dialect again from the DSN in `schema`.

Responsibilities:

- Select the embedded migration directory from the explicit driver.
- Select the goose dialect from the same driver.
- Select the lock strategy from the same driver.
- Run `provider.Up(ctx)`.
- Return errors that name the selected driver and migration directory.

`Store.New` should open the GORM/database-sql handle, ping it, and pass both the
handle and `Config.Driver` to `schema.Migrate`.

## Goose Dialects and Locks

Keep goose as the versioned migration runner if it supports all required target
dialects in this repo version. The runner should use:

| Driver | Goose dialect | Lock strategy |
| --- | --- | --- |
| `postgres` | `goose.DialectPostgres` | Existing Postgres session locker or goose-supported Postgres locking. |
| `mysql` | `goose.DialectMysql` | Goose-supported MySQL locking if available; otherwise a repo-local lock table or named-lock helper documented in code. |
| `sqlite` | `goose.DialectSQLite3` or current goose SQLite dialect constant | No process-global lock is required for single-process tests; production/file SQLite should rely on SQLite writer serialization or a simple repo-local lock table if goose supports none. |

Do not keep a Postgres advisory locker for non-Postgres drivers. If goose's lock
support is insufficient for MySQL or SQLite, implement a small migration lock
adapter rather than weakening the migration contract.

## Dialect DDL Requirements

The per-dialect migrations must preserve the semantic schema, not textual SQL.

| Feature | PostgreSQL | MySQL | SQLite |
| --- | --- | --- | --- |
| Auto IDs | `BIGSERIAL` or identity columns | `BIGINT AUTO_INCREMENT` | `INTEGER PRIMARY KEY AUTOINCREMENT` where rowid behavior is needed |
| JSON payloads | `JSONB` | `JSON` | `TEXT` with JSON validation in Go or SQLite JSON checks where available |
| Timestamps | `TIMESTAMPTZ` or app-side UTC fields | `DATETIME`/`TIMESTAMP` with UTC policy and `parseTime=true&loc=UTC` | UTC text/datetime convention with scan normalization |
| State validation | Postgres `CHECK` or app-side validation | MySQL-compatible `CHECK` only if enforced for supported versions; app-side validation remains required | SQLite `CHECK` where portable; app-side validation remains required |
| Regex path checks | Postgres POSIX regex | Move to Go validation or dialect-specific `REGEXP` only with known support | Move to Go validation |
| Memory FTS | `tsvector`/GIN or expression index | InnoDB `FULLTEXT` | FTS5 virtual table and sync triggers/helper |
| Memory tags | `bn_memory_tags` with case-sensitive comparison | `bn_memory_tags.tag` with binary/case-sensitive collation | `bn_memory_tags.tag COLLATE BINARY` |
| Dependency graph guard | `bn_dep_graph_guard` seed row | same semantic table | same semantic table |
| Repo admin bootstrap | `bn_project_admin_bootstraps` plus backfill | same semantic table with bounded keys | same semantic table |

Constraints that cannot be expressed consistently in DDL must move to Go
validation and contract tests. DDL constraints are allowed as extra defense, but
they cannot be the only enforcement when dialect behavior differs.

## Versioning Rules

- Use the same migration version numbers across all dialect directories.
- A version number must describe the same semantic step in every dialect.
- A dialect may have different SQL file bodies and dialect-specific object names,
  but `ListMigrations(driver)` should report the same version/name sequence for
  all supported drivers.
- New semantic tables from prior decisions must appear in the versioned plan:
  - `bn_project_admin_bootstraps`
  - `bn_dep_graph_guard`
  - `bn_memory_tags`
  - dialect FTS objects for `bn_memories`
- Data backfills and cutover validation must live in migrations, not ad hoc
  application startup code.

## Listing and Tests

Replace `ListMigrations()` with a dialect-aware listing API:

```go
func ListMigrations(driver Driver) ([]Migration, error)
```

Tests should stop asserting Postgres-only SQL snippets such as `NOT VALID`.
Instead they should assert:

- each driver has a non-empty migration set;
- migration versions are sorted and duplicate-free per driver;
- supported drivers have the same version/name sequence;
- each driver includes required semantic objects for issues, deps, memories,
  repo registry, issue repo routing, admin bootstrap, dependency guard, and
  memory tags;
- Postgres-specific SQL appears only in the Postgres directory;
- MySQL and SQLite directories do not contain `JSONB`, `TIMESTAMPTZ`,
  `BIGSERIAL`, `tsvector`, `GIN`, `NOT VALID`, or Postgres regex operators.

Integration tests should run migrations for SQLite by default and for Postgres
and MySQL under the integration build tag/container suite.

## Implementation Checklist

- Move existing SQL files to `schema/migrations/postgres/`.
- Add `schema/migrations/mysql/` and `schema/migrations/sqlite/`.
- Change the embed pattern to include nested dialect directories.
- Add driver-aware migration directory selection.
- Add driver-aware goose dialect selection.
- Replace `*pgxpool.Pool` and `stdlib.OpenDBFromPool` with a `*sql.DB` input.
- Replace the hardcoded Postgres session locker with driver-aware locking.
- Change `Store.New` to pass `Config.Driver` to `schema.Migrate`.
- Update schema tests to check semantic inventory by dialect.
- Keep pgx/goose dependencies until the store and schema runner are fully
  migrated; prune only after imports are gone.

## Rejected Alternatives

### GORM AutoMigrate as Source of Truth

`AutoMigrate` cannot express the required schema with enough precision:
Postgres FTS, MySQL FULLTEXT, SQLite FTS5 virtual tables/triggers, migration
backfills, case-sensitive tag collations, and dialect-specific lock/guard
objects all need explicit SQL or migration code.

### One Shared SQL Directory

The current files contain Postgres-only syntax. Trying to write one SQL file that
works across all three backends would either fail outright or force the schema to
the weakest common denominator while still not covering FTS and JSON differences.

### Drop Goose Immediately

Replacing goose entirely would add migration-runner risk at the same time as the
database migration. Keeping goose preserves version tracking and lets this effort
focus on dialect selection, SQL, and tests. Revisit the runner only if goose
cannot support the required dialects or lock behavior.
