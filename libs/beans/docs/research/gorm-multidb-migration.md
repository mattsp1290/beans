# Research: Migrating `store` to GORM with Postgres / MySQL / SQLite

Background research for a coding agent doing the migration. The current `store`
package (`store/store.go`) is hand-written `pgx/v5` and is **heavily
Postgres-specific**. This doc catalogs what translates cleanly, what does not,
and the decisions an implementer must make.

## Current Postgres-specific surface (what has to change)

Audited from `store/store.go` and `schema/`:

| Feature in use | Postgres form | Portable? |
| --- | --- | --- |
| Driver | `jackc/pgx/v5` + `pgxpool` | No — replaced by GORM dialectors |
| Placeholders | `$1, $2 …` | No — MySQL/SQLite use `?`. GORM generates these. |
| Upsert | `INSERT … ON CONFLICT … DO UPDATE` / `DO NOTHING` | Via `clause.OnConflict` |
| Array param | `WHERE id = ANY($1)` | No — rewrite as `IN (?)` (GORM expands slices) |
| JSON columns | `JSONB` (`labels`, `tags`, repo `metadata`) | Via `datatypes.JSON` |
| JSON containment | `tags @> $1::jsonb` | **No portable operator** — see below |
| Full-text search | `tsvector`, `plainto_tsquery`, `ts_rank` (memories) | **No** — biggest blocker, see below |
| Recursive cycle check | `WITH RECURSIVE … ` CTE | Supported by PG, MySQL 8+, SQLite 3.8.3+ but **dialect SQL differs**; consider doing graph walk in Go |
| `now()` | server-side timestamp | Use GORM `autoUpdateTime`/`autoCreateTime` or `CURRENT_TIMESTAMP` |
| Serializable tx | `pgx.TxOptions{IsoLevel: Serializable}` | `db.Transaction(fn, &sql.TxOptions{Isolation: sql.LevelSerializable})` — **SQLite has no real SERIALIZABLE**, it serializes writes globally |
| Error codes | `pgconn.PgError.Code == "23505"` | **No** — each driver has its own error type; need a dialect-aware `isDuplicate(err)` |

## GORM drivers and DSNs

- Postgres: `gorm.io/driver/postgres`, `postgres.Open(dsn)`
  DSN: `host=… user=… password=… dbname=… port=… sslmode=disable`
- MySQL: `gorm.io/driver/mysql`, `mysql.Open(dsn)`
  DSN: `user:pass@tcp(host:3306)/db?charset=utf8mb4&parseTime=True&loc=Local`
  (`parseTime=True` is **required** or `time.Time` scans fail.)
- SQLite: two options —
  - `gorm.io/driver/sqlite` (CGO; needs a C toolchain)
  - `github.com/glebarez/sqlite` (**pure-Go, no CGO**) — strongly preferred so
    `CGO_ENABLED=0` builds and cross-compilation keep working.
  DSN: `file:bn.db?...` or in-memory `file::memory:?cache=shared`.

Pattern: a `store.Config` gains a `Driver`/dialect field; `New` selects the
`gorm.Dialector` and opens with `gorm.Open(dialector, &gorm.Config{})`. Tune the
pool via `db.DB()` → `SetMaxOpenConns` / `SetMaxIdleConns`.

## The two hard problems

This section covers the FTS and tag-containment design space in detail. The
full migration inventory also needs the repo-admin bootstrap lock, schema
session locking, pool/config/error normalization, inline timestamp SQL, command
help text, and timezone behavior called out in the task graph.

### 1. Full-text search (`bn_memories` / `SearchMemories`)

This is the single feature with **no GORM abstraction**. Postgres uses a `tsv`
column with a GIN index, `plainto_tsquery`, and `ts_rank` ordering. Equivalents:

- **MySQL**: `FULLTEXT` index (InnoDB ok) + `MATCH(col) AGAINST(? IN NATURAL LANGUAGE MODE)`.
- **SQLite**: FTS5 virtual table + `MATCH`; ranking via `bm25()`. Virtual tables
  can't be `AutoMigrate`d — needs raw `CREATE VIRTUAL TABLE` and triggers to
  keep it in sync, or a contentless/external-content table.

Recommended approach for the plan: define a small **search strategy interface**
with one implementation per dialect, selected at `New` time. As a fallback for
correctness-over-relevance, a `LIKE '%term%'` scan is portable and fine for the
expected memory volume — decide whether ranked FTS is a hard requirement or a
nice-to-have before committing to per-dialect FTS DDL. This is the area most
likely to need its own design sub-task.

### 2. JSON containment for tag filtering (`SearchMemories` tags, `@>`)

`tags @> $1::jsonb` has no portable operator. `gorm.io/datatypes` offers
`datatypes.JSONQuery("tags").HasKey(...)` and array helpers that compile to
`JSON_EXTRACT` (MySQL/SQLite) vs JSONB ops (PG), but containment of an arbitrary
set is awkward. Options: (a) use `datatypes.JSONArrayQuery`, (b) normalize tags
into a child table (`bn_memory_tags`) and filter with a join/`GROUP BY HAVING
COUNT` — most portable and indexable, (c) portable `LIKE` fallback. Same
labels-as-JSON question applies to `bn_issues.labels`, though that column is only
read/written whole, not queried, so `datatypes.JSON` covers it directly.

## Schema / migration strategy

- The repo already has versioned SQL migrations in `schema/`. GORM offers
  `AutoMigrate`, but hand-written portable migrations give more control over
  per-dialect DDL (JSON vs JSONB, FTS objects, index types). **Decide:**
  AutoMigrate for the portable tables + dialect-specific raw SQL for the FTS
  objects, vs. fully hand-written per-dialect migrations.
- Type mapping gotchas: PG `JSONB` → MySQL `JSON` → SQLite `TEXT`/`JSON`;
  `text[]` does not exist outside PG (the code already stores arrays as JSON, so
  this is fine); identifier/timestamp defaults differ.
- GORM struct tags carry the column definitions: `gorm:"type:jsonb"` is
  PG-only — use `gorm:"serializer:json"` or `datatypes.JSON` for portability,
  and reserve dialect-specific `type:` overrides for migrations.

## Testcontainers integration testing

- Modules: `github.com/testcontainers/testcontainers-go/modules/postgres` and
  `.../modules/mysql`. Each spins a real container, applies migrations, returns a
  DSN; containers auto-stop/remove on cleanup.
- **SQLite needs no container** — run it in-process (file or `:memory:`). Keep
  the test matrix uniform by having the suite accept a `gorm.Dialector` and
  running the same store-contract tests against all three backends.
- Structure: one table-driven suite (`storeContractTest(t, openFn)`) executed
  for `{postgres-container, mysql-container, sqlite-memory}`. Gate the container
  backends behind a build tag or `testing.Short()` so unit runs stay fast and CI
  without Docker still passes on SQLite.
- Existing `store/store_integration_test.go` is the seam to generalize.

## Suggested decision points to resolve before/while planning

1. Is ranked full-text search a hard requirement on all three DBs, or is a
   portable `LIKE` fallback acceptable for MySQL/SQLite? (Drives the largest
   chunk of work.)
2. Tags filtering: `datatypes.JSON` query vs. normalized child table.
3. Recursive cycle detection: per-dialect recursive CTE vs. Go-side graph walk
   (the Go walk removes a whole class of dialect SQL and is the same code path
   for all three — likely preferable).
4. Migrations: AutoMigrate + raw FTS DDL vs. fully hand-written per dialect.
5. SQLite driver: pure-Go `glebarez/sqlite` (keeps `CGO_ENABLED=0`).

## Sources

- [GORM: Connecting to the Database](https://gorm.io/docs/connecting_to_the_database.html)
- [GORM: Create / Upsert (`clause.OnConflict`)](https://gorm.io/docs/create.html)
- [GORM: Dialect-specific data types](https://v1.gorm.io/docs/dialects.html)
- [GORM: Write Driver / dialect interface](https://gorm.io/docs/write_driver.html)
- [`gorm.io/datatypes` package docs](https://pkg.go.dev/gorm.io/datatypes)
- [Testcontainers for Go — Postgres module](https://golang.testcontainers.org/modules/postgres/)
- [Integration Test Postgres with testcontainers-go](https://kashifsoofi.github.io/integrationtest/postgres/go/integration-test-postgres-with-testcontainers-go/)
- [Integration Test MySQL with testcontainers-go](https://kashifsoofi.github.io/integrationtest/mysql/go/integration-test-mysql-with-testcontainers-go/)
- [SQLite FTS5](https://sqlite.org/fts5.html)
- [MySQL Full-Text Search Functions](https://dev.mysql.com/doc/en/fulltext-search.html)
- [PostgreSQL Full-Text Search intro](https://www.postgresql.org/docs/current/textsearch-intro.html)
- [glebarez/sqlite (pure-Go GORM SQLite driver)](https://github.com/glebarez/sqlite)
