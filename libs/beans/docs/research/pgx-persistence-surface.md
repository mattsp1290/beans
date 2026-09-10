# pgx/Postgres Persistence Surface Catalog

Issue: `beans-ws8`

This catalog audits the current persistence layer before the GORM multi-database
migration. It is intentionally an implementation inventory: each row names the
current Postgres/pgx coupling, where it appears, and what the migration must
preserve or replace.

## Driver, Pool, and Migration Wiring

| Surface | Current location | Migration implication |
| --- | --- | --- |
| `pgxpool.Pool` is the store connection holder | `store/pool.go:9`, `store/pool.go:20`, `store/pool.go:30`, `store/pool.go:44`, `store/pool.go:79` | Replace with a GORM/`database/sql` holder that can open Postgres, MySQL, and SQLite. Preserve `Close` idempotence and post-close `ErrPoolClosed` behavior. |
| `puddle.ErrClosedPool` normalization | `store/pool.go:10`, `store/pool.go:90` | Replace with driver-neutral closed-DB handling; keep callers able to use `errors.Is(err, ErrPoolClosed)`. |
| `Config` is Postgres/pgx shaped | `store/config.go:13`, `store/config.go:36`, `store/config.go:42`, `store/config.go:45` | Add explicit dialect or DSN-scheme selection; convert `MaxConns`/`MinConns int32` to `database/sql` pool tuning; redesign DSN redaction for postgres, mysql, and sqlite DSNs. |
| `Store.New` passes a pgx pool into schema migration | `store/store.go:40`, `store/store.go:46` | `schema.Migrate` must accept the new DB abstraction or a dialect-aware migration runner input. |
| Goose is hardcoded to Postgres through pgx stdlib | `schema/schema.go:13`, `schema/schema.go:14`, `schema/schema.go:15`, `schema/schema.go:87`, `schema/schema.go:92`, `schema/schema.go:105` | Replace `*pgxpool.Pool`, `stdlib.OpenDBFromPool`, and `goose.DialectPostgres` with a dialect-aware strategy. |
| Postgres session advisory lock for migrations | `schema/schema.go:24`, `schema/schema.go:100` | Requires per-dialect locking or a different migration strategy; the current session locker is Postgres-only. |
| Direct persistence and integration-test dependencies | `go.mod:7`, `go.mod:8`, `go.mod:9`, `go.mod:11`, `go.mod:12` | Add GORM core/drivers before implementation. Do not prune pgx, puddle, or goose until imports are removed and migration verification is green. Keep or replace testcontainers core and the Postgres module as part of the cross-dialect integration plan. |

## pgx Row, Rows, and Tx Coupling

| Surface | Current location | Migration implication |
| --- | --- | --- |
| Store imports `pgx` and `pgconn` directly | `store/store.go:13`, `store/store.go:14`; `store/repo_store.go:14` | Store methods and helpers must stop exposing pgx-specific row/tx types. Error classifiers must become dialect-aware. |
| Methods use `pool.QueryRow`, `pool.Query`, `pool.Exec` throughout | Examples: `store/store.go:73`, `store/store.go:87`, `store/store.go:240`, `store/store.go:268`, `store/store.go:328`; `store/repo_store.go:151`, `store/repo_store.go:202`, `store/repo_store.go:436`, `store/repo_store.go:531` | Convert to GORM CRUD/query APIs or dialect-aware raw SQL where necessary. Preserve current sentinel error wrapping. |
| Explicit pgx transactions | `store/store.go:150`, `store/store.go:462`, `store/store.go:635`, `store/store.go:774`, `store/store.go:913`; `store/repo_store.go:104`, `store/repo_store.go:226`, `store/repo_store.go:306` | Convert to `db.Transaction` or `Begin`/`Commit` on the new holder; preserve transaction boundaries around create/update issue+repo writes, notes, deps, imports, repo registry changes, and bootstrap. |
| Serializable pgx transactions | `store/store.go:635`, `store/store.go:913` | `AddDep` and `ImportIssuesFull` currently rely on Postgres serializable behavior for cycle safety. The migration needs a portable graph/isolation decision and tests for concurrent edges/imports. |
| Helper interfaces expose `pgx.Row`/`pgx.Rows` | `store/store.go:1201`, `store/store.go:1214`, `store/store.go:1232`, `store/store.go:1252`, `store/store.go:1275`, `store/store.go:1298`, `store/store.go:1385`; `store/repo_store.go:555`, `store/repo_store.go:638` | These helpers are a major cross-cutting dependency. They should be rewritten with store-local model scanners or GORM model mapping rather than pgx row interfaces. |
| `insertIssueRepo` requires `pgx.Tx` | `store/store.go:1336` | Preserve atomicity between issue row writes, repo target replacement, and optional note insertion in `CreateIssue`/`UpdateIssue`; this helper cannot become an out-of-transaction side effect. |
| Shared scanners accept pgx types | `store/store.go:1455`, `store/store.go:1493`; `store/repo_store.go:676`, `store/repo_store.go:736` | Replace with GORM model conversion or `database/sql` row scanning only if raw SQL remains. |
| `pgx.ErrNoRows` sentinel checks | `store/store.go:274`, `store/store.go:523`, `store/store.go:1240`; `store/repo_store.go:446`, `store/repo_store.go:566` | Replace with `gorm.ErrRecordNotFound` or dialect-neutral no-row detection while preserving `ErrNotFound` wrapping. |

## Placeholder and Dynamic SQL Patterns

| Surface | Current location | Migration implication |
| --- | --- | --- |
| `$N` positional placeholders are pervasive | Examples: `store/store.go:74`, `store/store.go:241`, `store/store.go:310`, `store/store.go:522`, `store/store.go:571`, `store/repo_store.go:136`, `store/repo_store.go:342`, `store/repo_store.go:378` | MySQL/SQLite use `?`; prefer GORM generated SQL or GORM clauses. Raw SQL must be generated through dialect-aware placeholder handling. |
| Dynamic `argN` builders | `store/store.go:480`, `store/store.go:503`, `store/store.go:1136`; `store/repo_store.go:342`, `store/repo_store.go:520` | Replace with GORM `Updates`/query chaining or a dialect-aware SQL builder. |
| `RETURNING` is used after inserts/updates | `store/store.go:154`, `store/store.go:1103`; `store/repo_store.go:259`, `store/repo_store.go:378`, `store/repo_store.go:653` | Postgres-style `RETURNING` is not uniformly portable. Use GORM create/update followed by read-back where needed, or dialect-specific returning support behind tests. |
| `RowsAffected` drives behavior | `store/store.go:515`, `store/store.go:577`, `store/store.go:614`, `store/store.go:687`, `store/store.go:953`, `store/store.go:984`, `store/store.go:1034`; `store/repo_store.go:123`, `store/repo_store.go:189` | Preserve idempotency and not-found semantics when moving to GORM result rows affected. |

## Postgres SQL Semantics in `store/store.go`

| Surface | Current location | Migration implication |
| --- | --- | --- |
| `ON CONFLICT DO NOTHING` and `ON CONFLICT DO UPDATE` | `store/store.go:74`, `store/store.go:789`, `store/store.go:809`, `store/store.go:838`, `store/store.go:945`, `store/store.go:964`, `store/store.go:1028` | Convert to `clause.OnConflict` or dialect-aware upsert. Preserve import created/updated/skipped counts and terminal-state merge rules. |
| `= ANY($1)` array parameter | `store/store.go:230`, `store/store.go:240`, `store/store.go:814`, `store/store.go:969`, `store/store.go:1312`, `store/store.go:1399` | Replace with GORM `IN ?` or expanded predicates. Terminal state checks need portable `IN` semantics. |
| Recursive CTE cycle check | `store/store.go:1250`, `store/store.go:1258` | PostgreSQL, MySQL 8, and SQLite support recursive CTEs with differences. The task graph should decide between dialect-specific SQL and Go-side graph walk. |
| JSONB labels/tags encoded as bytes | `store/store.go:1582`, `store/store.go:1594` | Whole-column JSON labels can map to `datatypes.JSON` or serializer JSON. |
| JSONB tag containment | `store/store.go:1163`, `store/store.go:1165` | `tags @> ...::jsonb` has no portable equivalent. Decide normalized tag table, `datatypes` JSON query, or LIKE fallback. |
| Postgres full-text memory search | `store/store.go:1121`, `store/store.go:1156`, `store/store.go:1169` | Replace `tsv`, `plainto_tsquery`, and `ts_rank` with per-dialect FTS adapters or a documented portable fallback. |
| Inline `now()` timestamp updates | `store/store.go:504`, `store/store.go:538`, `store/store.go:571`, `store/store.go:797`, `store/store.go:820`, `store/store.go:975`; `store/repo_store.go:342` | Replace with GORM auto timestamps, `CURRENT_TIMESTAMP`, or Go-side UTC timestamps consistently. Tests should assert semantic ordering, not exact Postgres behavior. |
| `pgconn.PgError` error codes | `store/store.go:1636`, `store/store.go:1642`, `store/store.go:1650` | Duplicate key and serialization failure detection must be dialect-aware: Postgres SQLSTATE, MySQL numeric codes, SQLite constraint strings/types. |

## Repo Registry Store Surface

| Surface | Current location | Migration implication |
| --- | --- | --- |
| Bootstrap first-admin guard uses `pg_advisory_xact_lock(hashtext($1))` | `store/repo_store.go:103`, `store/repo_store.go:110` | Needs redesign for MySQL and SQLite. A constraint-backed bootstrap or dialect-specific lock must preserve exactly-one-first-admin under concurrency. |
| Repo/admin inserts use `ON CONFLICT DO NOTHING` | `store/repo_store.go:113`, `store/repo_store.go:135` | Convert to GORM conflict clauses or dialect-aware upsert. Preserve unauthorized vs no-op semantics. |
| Dynamic repo updates include `updated_at = now()` and `$N` placeholders | `store/repo_store.go:342`, `store/repo_store.go:344`, `store/repo_store.go:378` | Replace with GORM update maps or dialect-aware SQL; keep audit rows atomic with the update. |
| Repo metadata/audit are JSONB payloads | `store/repo_store.go:254`, `store/repo_store.go:369`, `store/repo_store.go:613`, `store/repo_store.go:696`; migrations `0004`/`0005` | Use portable JSON mapping and verify round trip of repo metadata and audit old/new values. |
| Alias replacement and repo audit depend on pgx.Tx | `store/repo_store.go:575`, `store/repo_store.go:613` | Keep transaction-scoped alias replacement and audit insertion when converting to GORM. |

## Schema DDL Surface

| Surface | Current location | Migration implication |
| --- | --- | --- |
| Goose migration annotations and versioning | `schema/migrations/0001_bn_init.sql:17`, `schema/migrations/0002_bn_memories.sql:10`, `schema/migrations/0003_bn_issue_state_check.sql:7`, `schema/migrations/0004_bn_repos.sql:10`, `schema/migrations/0005_bn_issue_repos.sql:7` | Decide whether to keep goose with per-dialect directories or replace with GORM AutoMigrate plus raw dialect DDL. |
| `TIMESTAMPTZ NOT NULL DEFAULT now()` | `schema/migrations/0001_bn_init.sql:28`, `0001:49`, `0001:50`, `0001:91`, `0002:20`, `0004:32`, `0004:33`, `0004:58`, `0004:74`, `0004:94`, `0005:18`, `0005:19` | Map timestamp defaults and scan behavior across Postgres/MySQL/SQLite; account for MySQL `parseTime` and SQLite precision. |
| `BIGSERIAL` | `schema/migrations/0001_bn_init.sql:87`, `0002_bn_memories.sql:14`, `0004_bn_repos.sql:86` | Replace with dialect-appropriate auto-increment primary keys. |
| `JSONB` | `schema/migrations/0001_bn_init.sql:46`, `0002_bn_memories.sql:18`, `0004_bn_repos.sql:31`, `0004:91`, `0004:92`, `0005_bn_issue_repos.sql:17` | Replace with Postgres JSONB, MySQL JSON, SQLite JSON/TEXT strategy. |
| `tsvector` generated column and GIN index | `schema/migrations/0002_bn_memories.sql:19`, `0002:23` | Requires per-dialect FTS DDL or removal in favor of fallback search. The current expression hardcodes English stemming through `to_tsvector('english', body)`. |
| `NOT VALID` constraint | `schema/migrations/0003_bn_issue_state_check.sql:10`, `0003:13` | Postgres-only; tests currently assert this and must be rewritten. |
| POSIX regex CHECK constraints | `schema/migrations/0004_bn_repos.sql:37`, `0004:43`, `0005_bn_issue_repos.sql:20` | MySQL/SQLite syntax differs; move validation to Go and/or dialect-specific constraints. |
| Mixed foreign-key delete actions | `schema/migrations/0001_bn_init.sql:39`, `0001:72`, `0001:73`, `0001:88`, `0002_bn_memories.sql:15`, `0004_bn_repos.sql:22`, `0004:55`, `0004:57`, `0004:72`, `0004:88`, `0005_bn_issue_repos.sql:11`, `0005:12` | Preserve each relationship's delete action: issue-owned rows cascade, repo aliases cascade, repo audit uses `ON DELETE SET NULL`, and `bn_issue_repos.repo_id` currently restricts repo deletion. SQLite requires foreign keys enabled. |

## CLI and User-Facing Postgres Assumptions

| Surface | Current location | Migration implication |
| --- | --- | --- |
| Root command and connection errors say Postgres | `cmd/bn/app.go:32`, `cmd/bn/app.go:64`, `cmd/bn/app.go:76` | Update help/errors once DSN/dialect selection changes. |
| Memory command help describes Postgres FTS | `cmd/bn/cmd_memory.go:21`, `cmd/bn/cmd_memory.go:78`, `cmd/bn/cmd_memory.go:81`, `cmd/bn/cmd_memory.go:162`, `cmd/bn/cmd_memory.go:166` | Revise help to describe dialect-dependent search or fallback behavior. |
| `cmd/bn/main.go` only wires the store indirectly | `cmd/bn/main.go:22`, `cmd/bn/main.go:30` | No direct pgx coupling; changes flow through `appState.initConnWithOptions`. |

## Test Coverage and Known Postgres-Coupled Assertions

| Surface | Current location | Migration implication |
| --- | --- | --- |
| Integration tests are Postgres container only | `store/store_integration_test.go:1`, `store/store_integration_test.go:11`, `store/store_integration_test.go:20` | Generalize to a contract suite with SQLite default/no-Docker coverage and Postgres/MySQL containers. |
| Tests mention pgx error surfaces | `store/store_integration_test.go:819` | Rewrite to assert sentinel errors and behavior instead of driver-specific strings. |
| Schema tests assert Postgres migration details | `schema/schema_test.go:53` | Replace SQL-text assertions with dialect-aware migration inventory/schema assertions. |
| Current Docker integration dependency | `go.mod:11`, `go.mod:12` | Add MySQL testcontainers module when implementing cross-dialect integration. |

## High-Risk Migration Decisions

1. **Cycle safety:** `AddDep` and `ImportIssuesFull` combine recursive graph checks
   with serializable Postgres transactions. Decide whether to keep per-dialect
   recursive SQL/isolation or move graph traversal into Go with a portable
   concurrency strategy.
2. **Memory search:** `SearchMemories` combines Postgres FTS ranking and JSONB tag
   containment. This likely needs a strategy interface or a deliberately weaker
   portable fallback.
3. **Repo first-admin bootstrap:** `pg_advisory_xact_lock(hashtext(prefix))` is
   Postgres-only and protects a security-sensitive exactly-one-admin invariant.
4. **Migration runner:** Current goose wiring depends on pgx stdlib, Postgres
   dialect, and Postgres session locks. The migration must pick per-dialect SQL
   directories or GORM model migration plus raw dialect-specific objects.
5. **Timestamp contract:** Inline `now()`, `TIMESTAMPTZ`, UTC conversions after
   scans, MySQL `parseTime`, and SQLite precision all need a single tested policy.
6. **Error normalization:** `ErrNotFound`, `ErrConflict`, `ErrDuplicateDep`,
   `ErrPoolClosed`, and serialization retry behavior must survive driver changes
   without exposing dialect-specific errors to CLI callers.
