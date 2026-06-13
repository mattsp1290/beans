# Big Change Planning with Beads

## Agent Instructions

You are an expert software architect creating a comprehensive task breakdown for a change to an existing codebase. This task graph will be executed by AI agents working in parallel, coordinated through MCP Agent Mail with file reservations to prevent conflicts.

<quality_expectations>
Create a thorough, production-ready task graph. Include all necessary analysis, preparation, implementation, testing, and documentation tasks. Go beyond the basics — consider edge cases, error handling, security considerations, backwards compatibility, and integration points. Each task should be specific enough for an agent to execute independently without ambiguity.
</quality_expectations>

<critical_constraint>
You must NOT implement any of the changes yourself. Your ONLY output is a bash shell script containing `bd create` and `bd dep add` commands. Do NOT use `bd add` — the correct command is `bd create`. Do not write code. Do not create files other than the shell script. Do not modify existing files. Read and analyze the codebase, then produce the script.
</critical_constraint>

## Change Information

### Change Type
MIGRATION

### Description
Migrate the `bn` tracker persistence layer from the hand-written `jackc/pgx/v5`
data access layer to GORM, and add support for three SQL backends — PostgreSQL,
MySQL, and SQLite — behind a single dialect-agnostic store API. The migration
replaces pgxpool, `$N` placeholders, Postgres-only SQL (`ON CONFLICT`,
`= ANY()`, JSONB operators, recursive CTEs, `tsvector` full-text search), and
the goose `DialectPostgres` migration runner with portable equivalents.
Completion is validated by a single store-contract integration suite that runs
against all three backends (Postgres + MySQL via testcontainers, SQLite
in-process).

### Links to Relevant Documentation
- `docs/research/gorm-multidb-migration.md` — in-repo research doc cataloging the
  Postgres-specific surface, the two hard problems (full-text search and JSON tag
  containment), driver/DSN choices, and the testcontainers strategy. **Read this
  first.**
- [GORM: Connecting to the Database](https://gorm.io/docs/connecting_to_the_database.html)
- [GORM: Create / Upsert (`clause.OnConflict`)](https://gorm.io/docs/create.html)
- [`gorm.io/datatypes` package](https://pkg.go.dev/gorm.io/datatypes)
- [Testcontainers for Go — Postgres module](https://golang.testcontainers.org/modules/postgres/)
- [glebarez/sqlite (pure-Go GORM SQLite driver)](https://github.com/glebarez/sqlite)
- [SQLite FTS5](https://sqlite.org/fts5.html) / [MySQL Full-Text Search](https://dev.mysql.com/doc/en/fulltext-search.html)

### Affected Areas
- **`store/`** (the entire DAL):
  - `store/store.go` — issue/dep/memory CRUD; all queries use `$N`, `ON CONFLICT`,
    `= ANY()`, JSONB `@>`, recursive CTE cycle check, `tsvector`/`plainto_tsquery`/
    `ts_rank`. Note the **shared pgx-typed scan/tx helpers** (`scanIssue(row pgx.Row)`
    at :1442, `collectIssues(rows pgx.Rows)` at :1480, the inline
    `interface{ QueryRow(...) pgx.Row }` constraints at :1187/:1200/:1218/:1238/:1261,
    and `insertIssueRepo(... tx pgx.Tx)` at :1322) — these cross the issues/deps/
    memories boundaries, so the file **cannot** be split into independently-mergeable
    per-concern units (see Sequencing constraints below).
  - `store/repo_store.go` — repo registry CRUD on `pgx`. **Contains a Postgres-only
    `pg_advisory_xact_lock(hashtext($1))` at :110** guarding the single-first-admin
    bootstrap (tested by `TestRepoAdminBootstrapAllowsOnlyOneConcurrentFirstAdmin`).
    MySQL has only non-transaction-scoped `GET_LOCK`; SQLite has no advisory locks —
    this concurrency guard must be redesigned (see hard problem 7).
  - `store/pool.go` — `pgxpool` wrapper with atomic swap-on-close; becomes a
    `*gorm.DB` holder.
  - `store/config.go` — `SecretDSN`, pgx-specific `MaxConns/MinConns int32`; needs
    a dialect/driver selector.
  - `store/errors.go` — sentinel errors + `pgconn.PgError` (`23505`) duplicate-key
    detection; needs dialect-aware error classification. Note the duplicate-key
    logic is **two** classifiers (`isPKConflict`/`isDupKeyConflict` at store.go:1624/
    :1628) plus `normalizePoolError` keyed on `puddle.ErrClosedPool` (pool.go:90) —
    all must be reworked for pq/mysql(`1062`)/sqlite(`UNIQUE constraint failed`)
    shapes, and the `puddle` pool-closed hook disappears with pgxpool.
- **`schema/`**:
  - `schema/schema.go` — goose provider hardcoded to `goose.DialectPostgres` with a
    `pg_advisory_lock` session locker; takes `*pgxpool.Pool`.
  - `schema/migrations/0001..0005_*.sql` — Postgres-only DDL (`BIGSERIAL`, `JSONB`,
    `TIMESTAMPTZ`, generated `tsvector` column, `GIN` index, `REFERENCES`,
    `ON DELETE CASCADE`, **POSIX regex `CHECK` constraints `~`/`!~`** at
    0004:37/0004:43/0005:20, and the `NOT VALID` constraint add at 0003:13 — all
    Postgres-only spellings).
- **`cmd/bn/`** — `app.go`/`main.go` build `store.Config` from `BN_DSN`; every
  `cmd_*.go` consumes `*store.Store`. Public store API changes ripple here.
  **User-facing Postgres strings that must change:** `app.go:32`/:64/:76 (the
  `BN_DSN=postgres://…` hint), and `cmd/bn/cmd_memory.go:21`/:78/:81/:163/:166
  (help text hardcoding "Postgres tsvector/plainto_tsquery" and "BN_DSN = Postgres
  connection string"). If search semantics go per-dialect, this help text is wrong.
- **No `tracker` package exists in this module** — store.go:22 and errors.go:9
  mention `tracker.Tracker`/`tracker.CategoryNotFound` in *stale doc comments only*.
  The sole real consumer of `*store.Store` is `cmd/bn/*`. Do **not** create beads
  targeting a "tracker adapter."
- **`store/store_integration_test.go`** — `//go:build integration`, single Postgres
  testcontainer; generalize into a per-dialect contract suite.
- **`go.mod` / `go.sum`** — add `gorm.io/gorm`, `gorm.io/driver/{postgres,mysql}`,
  `github.com/glebarez/sqlite`, `gorm.io/datatypes`, testcontainers mysql module;
  prune now-unused `pgx`/`puddle` if fully removed.

### Success Criteria
1. All three backends (PostgreSQL, MySQL, SQLite) pass the **same** store-contract
   integration suite — identical assertions, parameterized only by dialector.
2. Existing Postgres behavior is **semantically** preserved (idempotent close,
   never-regress-terminal import, cycle rejection, ready-issue semantics, memory
   search). Note this is *semantic* preservation, not "every current test passes
   byte-for-byte": several existing tests assert on Postgres-only artifacts and
   **must be rewritten** as part of the migration — `schema_test.go:53` asserts the
   migration SQL contains the Postgres-only string `"NOT VALID"`, and both
   `schema_test.go:50` and `store_integration_test.go:1027` assert error/SQL text
   contains the Postgres constraint name `"bn_issues_state_check"`. The migration
   must list and update these Postgres-coupled assertions rather than treat them as
   regressions.
3. CI is green **without Docker**: the SQLite backend runs in-process so the default
   `go test ./...` path needs no containers; the Postgres/MySQL container backends
   are gated (build tag / skip-when-Docker-unavailable) and run where Docker exists.

### Constraints
N/A — the project has no users; backwards compatibility is explicitly dropped.
Existing Postgres-only migrations and the `BN_DSN`/pgx config surface may be
replaced outright rather than preserved. No dual-write, no data migration, no
compatibility shim required.

---

## Your Task

Analyze this codebase change and create a comprehensive **Beads task graph** using the `bd` CLI. Beads provides dependency-aware, conflict-free task management for multi-agent execution.

Before creating the task graph, you MUST first analyze the affected areas of the codebase:

1. Check `docs/specs/` and `docs/adr/` for existing architectural decisions
   (NOTE: neither directory exists in this repo — rely on `docs/research/gorm-multidb-migration.md` instead).
2. Examine the directory/module structure of the affected areas listed above.
3. Identify key interfaces, APIs, and integration points that must be preserved
   (the public `store.Store` method set consumed by `cmd/bn/*`; there is no separate
   tracker adapter — ignore the stale `tracker.*` doc comments).
4. Note existing test patterns and coverage in the affected areas
   (`store/*_test.go`, `store/store_integration_test.go` with the `integration` build tag,
   `schema/schema_test.go`).
5. Assess risk areas where changes could break existing functionality
   (full-text memory search, JSON tag containment, recursive cycle detection,
   serializable-isolation guarantees that SQLite cannot honor).

Use your analysis to make each bead specific — reference actual file paths, module names, and patterns you observed.

Then generate a shell script that creates the complete task graph.

**IMPORTANT: Your ONLY deliverable is a bash shell script with `bd create` commands. Not an implementation plan. Not a design document. Not a code review. A runnable `.sh` script.**

---

## Output Format

Generate a shell script that creates the full task graph. The script should:

1. **Initialize Beads** (if not already initialized)
2. **Create all beads** with appropriate priorities
3. **Establish dependencies** between beads
4. **Add labels** for phase grouping

### Example Output

```bash
#!/bin/bash
# Project: beans
# Change: Migrate store to GORM with Postgres/MySQL/SQLite support
# Generated: 2026-06-13

set -e

# Initialize beads if needed
if [ ! -d ".beads" ]; then
    bd init
fi

echo "Creating change beads..."

# ========================================
# Phase 0: Blocking Design Decisions (MUST be parents of impl/migration/test work)
# ========================================
# These three resolve the open questions in docs/research/gorm-multidb-migration.md.
# EVERY impl/migration/test bead below depends on the decision that governs it.

DESIGN_FTS=$(bd create "DECISION: ranked per-dialect FTS vs portable LIKE for SearchMemories (bn_memories) — determines contract-suite ranking assertions and whether SQLite needs FTS5 virtual tables" -p 0 --label analysis --silent)
DESIGN_MIGRATE=$(bd create "DECISION: migration strategy — per-dialect embedded migration dirs vs GORM AutoMigrate + raw FTS DDL; goose does NOT transform DDL (see hard problem 5)" -p 0 --label analysis --silent)
DESIGN_DSN=$(bd create "DECISION: DSN/dialect selection + SecretDSN redesign — scheme-inferred vs BN_DRIVER env; redaction must hide creds in MySQL user:pass@ DSNs" -p 0 --label analysis --silent)

# ========================================
# Phase 1: Analysis & Preparation
# ========================================

ANALYZE_CURRENT=$(bd create "Analyze current pgx store surface in store/ — catalog every Postgres-specific construct (placeholders, ON CONFLICT, = ANY, JSONB @>, recursive CTE, tsvector) per method" -p 0 --label analysis --silent)

CHAR_TESTS=$(bd create "Add characterization tests capturing current store behavior (CRUD, deps, cycle rejection, ready semantics, import merge, memory search) before migrating" -p 0 --label prep --silent)
bd dep add $CHAR_TESTS $ANALYZE_CURRENT

# ========================================
# Phase 2: Core Implementation
# ========================================

# Connection/config layer — gated by the DSN/dialect decision.
GORM_CONN=$(bd create "Introduce GORM connection layer: replace store/pool.go pgxpool with a *gorm.DB holder; implement dialect selector + SecretDSN redesign in store/config.go (glebarez/sqlite for CGO-free SQLite)" -p 0 --label impl --silent)
bd dep add $GORM_CONN $CHAR_TESTS
bd dep add $GORM_CONN $DESIGN_DSN

# schema.Migrate signature change LEADS — it gates store.New, cmd/bn, and tests.
SCHEMA_MIGRATE=$(bd create "Rework schema.Migrate: drop *pgxpool.Pool + goose.DialectPostgres + pg session locker; take *gorm.DB/*sql.DB + dialect, locker dialect-conditional. Implement chosen migration DDL strategy." -p 0 --label migration --silent)
bd dep add $SCHEMA_MIGRATE $DESIGN_MIGRATE
bd dep add $SCHEMA_MIGRATE $GORM_CONN

# store.go converts as ONE unit — do NOT split into issues/deps/memories beads
# (shared pgx-typed scan/tx helpers: scanIssue/collectIssues/insertIssueRepo). The
# 750-LOC rule does NOT apply here; this is one large foundational bead.
STORE_CORE=$(bd create "Convert ALL of store/store.go in one pass: GORM data-access primitives (db holder, scan helpers, tx wrapper) + every method body; Go-side cycle walk replacing recursive CTE; dialect-aware error classifiers (isPKConflict/isDupKeyConflict + pool-closed path)" -p 0 --label impl --silent)
bd dep add $STORE_CORE $GORM_CONN
bd dep add $STORE_CORE $SCHEMA_MIGRATE

# repo_store.go — its own bead: ON CONFLICT, RETURNING, ErrNoRows→ErrNotFound,
# JSON metadata round-trip, dynamic argN SET-builder, and the advisory-lock redesign.
REPO_STORE=$(bd create "Convert store/repo_store.go: ON CONFLICT/RETURNING/pgx.ErrNoRows, encode/decodeJSONObject metadata, argN UPDATE builder; redesign pg_advisory_xact_lock admin-bootstrap guard portably (+ concurrency test)" -p 0 --label impl --silent)
bd dep add $REPO_STORE $STORE_CORE

# Memory FTS implementation — gated by the FTS decision.
FTS_IMPL=$(bd create "Implement SearchMemories per the FTS decision (per-dialect strategy or LIKE fallback) + JSON tag-containment approach replacing tags @> ::jsonb" -p 1 --label impl --silent)
bd dep add $FTS_IMPL $STORE_CORE
bd dep add $FTS_IMPL $DESIGN_FTS

# cmd/bn wiring — only the user-facing Postgres strings + Config construction.
CMD_WIRE=$(bd create "Update cmd/bn: app.go BN_DSN/dialect wiring + root Short, cmd_memory.go help text (drop hardcoded Postgres tsvector/plainto strings)" -p 1 --label impl --silent)
bd dep add $CMD_WIRE $GORM_CONN

# ========================================
# Phase 3: Testing & Validation
# ========================================

CONTRACT_SUITE=$(bd create "Build dialect-parameterized store-contract suite (storeContractTest(t, openFn)) vs Postgres+MySQL (testcontainers) and SQLite (in-process), with per-dialect expectation hooks for FTS ranking / isolation / timestamps" -p 0 --label testing --silent)
bd dep add $CONTRACT_SUITE $STORE_CORE
bd dep add $CONTRACT_SUITE $REPO_STORE
bd dep add $CONTRACT_SUITE $FTS_IMPL
bd dep add $CONTRACT_SUITE $DESIGN_FTS

# Rewrite Postgres-coupled tests (schema_test.go is contingent on the migration decision).
FIX_TESTS=$(bd create "Rewrite Postgres-coupled test assertions: schema_test.go (wholesale — version/table-shape asserts depend on migration strategy) and store_integration_test.go bn_issues_state_check/NOT VALID expectations" -p 1 --label testing --silent)
bd dep add $FIX_TESTS $SCHEMA_MIGRATE
bd dep add $FIX_TESTS $DESIGN_MIGRATE

# ========================================
# Phase 4: Cleanup & Documentation
# ========================================

CLEANUP=$(bd create "Remove now-unused deps from go.mod (pgx, puddle, and goose if migration strategy drops it); update docs/README for multi-DB DSN config" -p 3 --label cleanup --silent)
bd dep add $CLEANUP $CONTRACT_SUITE

echo ""
echo "Bead graph created! View with:"
echo "  bd ready              # List unblocked tasks"
```

---

## Bead Creation Guidelines

### Priority Levels
- `-p 0` = Critical (blocking other work, or high-risk changes needing early validation)
- `-p 1` = High (important implementation work)
- `-p 2` = Medium (standard work)
- `-p 3` = Low (cleanup, nice-to-haves)

### Labels (Phase Grouping)
Use `--label` to group beads by phase:
- `analysis` - Understanding current state
- `prep` - Preparation work (characterization tests, feature flags, scaffolding)
- `impl` - Core implementation
- `testing` - Test coverage
- `migration` - Data/code migration
- `docs` - Documentation updates
- `cleanup` - Post-rollout cleanup

### Dependency Rules
1. Never create cycles
2. Analysis tasks should complete before implementation begins
3. Characterization tests should exist before changing code
4. Use `bd dep add CHILD PARENT` (child depends on parent completing first)
5. Parallel work should share a common ancestor, not depend on each other

### Task Granularity
- Each bead should be completable in **under 750 lines of code changed**
- Tasks should be atomic enough for one agent to complete without coordination
- If a task requires multiple file areas, consider splitting by file area

---

## Change-Specific Considerations

### For Migrations
- Create rollback plan as an explicit task (here: the migration is one-way — no
  users — but still capture a "revert branch" fallback as a task).
- Plan data validation checkpoints (the store-contract suite IS the checkpoint).
- Consider dual-write period if applicable (NOT applicable — no users, no live data).
- Include monitoring/alerting tasks (NOT applicable for a CLI tool).

### Migration-specific hard problems (must each get dedicated beads)
1. **Full-text memory search** (`SearchMemories`, `bn_memories.tsv`): design a
   per-dialect search strategy (PG `tsvector`, MySQL `FULLTEXT MATCH…AGAINST`,
   SQLite FTS5 `bm25()`), or a portable `LIKE` fallback. Decide ranked-vs-portable
   first; this is the largest single unit and likely needs its own design bead.
2. **JSON tag containment** (`tags @> $1::jsonb`): choose `datatypes.JSONQuery` vs.
   a normalized `bn_memory_tags` child table vs. portable `LIKE`.
3. **Recursive cycle detection** (`hasCycle` CTE): per-dialect recursive CTE vs. a
   Go-side graph walk (preferred — one code path for all three dialects).
4. **Duplicate-key classification** (`pgconn.PgError 23505`): dialect-aware
   `isDuplicate(err)` covering pq/mysql/sqlite error shapes.
5. **Multi-dialect migrations**: goose's dialect setting only controls its
   *version-tracking table* — **it does NOT transform your DDL**. The same `.sql`
   is sent verbatim to every backend, so a single "portable" file is impossible
   here: `tsvector`, `GIN`, the generated column, and the regex `CHECK`s have no
   portable spelling. Realistic options are (a) **one embedded migration dir per
   dialect** (`migrations/{postgres,mysql,sqlite}/`, which means reworking the
   `//go:embed` + `migrationFilenameRE`/`ListMigrations` machinery in schema.go),
   or (b) GORM `AutoMigrate` for portable tables + raw dialect-specific DDL for the
   FTS objects (abandons the project's current versioned-migration policy). This is
   a **blocking design decision** (see below). Per-type rewrites within it:
   `BIGSERIAL`→`BIGINT AUTO_INCREMENT`/`INTEGER PRIMARY KEY AUTOINCREMENT`,
   `JSONB`→`JSON`/`TEXT`, `TIMESTAMPTZ`+`now()`→portable timestamp, generated
   `tsvector` + `GIN`→dialect FTS objects, POSIX regex `CHECK` (`~`/`!~`)→`REGEXP`
   (MySQL) / drop-and-validate-in-Go (SQLite; note `repo.ValidateTarget` already
   does app-layer validation, so the constraints may be droppable), `NOT VALID`→omit.
6. **Isolation semantics**: `AddDep` (store.go:635) and `ImportIssuesFull`
   (store.go:898) open `pgx.Serializable` transactions and rely on the recursive-CTE
   cycle check running inside them so concurrent edge inserts can't race into a
   cycle. SQLite serializes all writes globally (cycle-safety still holds, but via a
   different mechanism, and `sql.LevelSerializable` may not be honored by the
   driver). Make this a concrete bead with a **concurrent-AddDep race test**, and
   accept that this test needs **per-dialect expectations** — it is one of the places
   "identical assertions across all three backends" cannot hold.
7. **Admin-bootstrap advisory lock** (`repo_store.go:110`,
   `pg_advisory_xact_lock(hashtext($1))`): guarantees exactly-one-first-admin under
   concurrency (tested at store_integration_test.go:139). No portable equivalent —
   redesign to rely on the `WHERE NOT EXISTS … ON CONFLICT` insert itself (verify it
   already suffices) or a dialect-conditional lock. Needs its own bead + concurrency test.
8. **`now()` SQL literals inside UPDATEs** (not just column defaults): `updated_at =
   now()` appears inline in `UpdateIssue` (store.go:504/:538), `CloseIssue` (:571),
   both import upserts (:797/:820/:961), and repo_store.go:342. SQLite has no
   `now()`. GORM `autoUpdateTime` only covers model-based updates — these raw-SQL
   expressions must be converted (to `CURRENT_TIMESTAMP`, or to GORM model writes).
9. **Timestamp / timezone handling**: the store normalizes everything with `.UTC()`
   and orders by `created_at DESC`. MySQL **requires** `parseTime=True&loc=…` in the
   DSN or `time.Time` scans fail, and `DATETIME` is timezone-naive — a wrong `loc`
   silently shifts times and breaks ordering assertions. SQLite stores timestamps as
   text/numeric with differing sub-second precision. Verify round-trip + ordering in
   the contract suite for all three.
10. **Per-dialect config / DSN selection** (`config.go`, `pool.go`, `app.go`):
    `SecretDSN` is documented as wrapping a *Postgres* DSN and its redaction is keyed
    to one format; `Config.MaxConns/MinConns` are `int32` (pgxpool's type; GORM uses
    `db.DB().SetMaxOpenConns(int)`); `newPool` calls `pgxpool.ParseConfig` (pool.go:30)
    which can't parse MySQL/SQLite DSNs. The three DSN formats are incompatible
    (`host=… sslmode=…` vs `user:pass@tcp(…)/db?parseTime=True` vs `file:bn.db`).
    **Unresolved decision:** is the dialect inferred from the DSN scheme, or set via a
    new `BN_DRIVER` env var? Redaction must still hide credentials in a MySQL
    `user:pass@` DSN. This is a blocking design decision (below).

### Sequencing & decomposition constraints (read before building the graph)

The naive ordering "connection layer → queries → migrations → tests" with parallel
per-concern store splits will produce broken intermediate states. Three couplings
force a different shape:

- **`store.go` converts as ONE unit, not per-concern.** The pgx-typed shared helpers
  (`scanIssue`/`collectIssues`/`pool.conn()` and the `pgx.Tx`/`pgx.Row` helper
  signatures) are used by issues, deps, AND memories. The first agent that changes
  them to GORM types breaks compilation for every other store method. Create a single
  foundational bead "establish GORM data-access primitives (db holder, scan helpers,
  tx wrapper) and convert all store.go method bodies in one pass" rather than three
  parallel issues/deps/memories beads. The 750-LOC granularity guideline is
  **not achievable** for this file — call that out explicitly in the bead.
- **`schema.Migrate` is on the critical path of everything and must LEAD.** It is
  hardcoded to `stdlib.OpenDBFromPool(pool)` + `goose.DialectPostgres` +
  `NewPostgresSessionLocker` and takes a `*pgxpool.Pool` (schema.go:92/:100/:106).
  `store.New` calls it (store.go:46) and is consumed by `cmd/bn/app.go` and the
  integration test. Its signature change (to a `*gorm.DB`/`*sql.DB` + dialect, with a
  dialect-conditional or dropped session locker — SQLite is single-writer; goose ships
  only a Postgres session locker) must land **with or before** the connection layer,
  not after the query rewrites.
- **The contract suite needs per-dialect expectations, not byte-identical assertions.**
  FTS ranking (`ts_rank` vs `bm25()` vs `LIKE`), serializable-isolation behavior, and
  timestamp precision diverge by backend. Success criteria 1 ("same suite") and 3
  ("no Docker on SQLite") only co-hold if the divergent cases are parameterized by
  dialect. Design the suite as `storeContractTest(t, openFn)` with a small set of
  dialect-specific expectation hooks.

### Required blocking design-decision beads (must precede parallel impl)

These are currently *open questions* in the research doc but hard prerequisites — each
should be a `-p 0` decision bead that blocks the impl/migration/test beads downstream:

1. **Ranked FTS vs. portable `LIKE`** for `SearchMemories` — determines whether the
   contract suite asserts on relevance ordering at all, and whether SQLite needs FTS5
   virtual tables + sync triggers. Largest single driver of scope.
2. **Migration strategy** — per-dialect embedded migration dirs vs. `AutoMigrate` +
   raw FTS DDL (see hard problem 5). Blocks all migration and FTS DDL work.
3. **DSN / dialect selection + `SecretDSN` redesign** (see hard problem 10). Blocks the
   connection-layer and `cmd/bn` wiring beads.

---

## File Reservation Planning

For each major work area, note the file patterns that will need exclusive reservation:

```bash
# Reservation notes (add as bead descriptions)
# Connection/config: store/pool.go, store/config.go (foundational — block other impl on this)
# Core queries: store/store.go — convert as ONE unit; do NOT split by concern
#   (issues/deps/memories share scanIssue/collectIssues/tx helpers; see Sequencing constraints).
# Repo registry: store/repo_store.go (own bead — ON CONFLICT/RETURNING/ErrNoRows/JSON metadata/
#   argN SET-builder/advisory-lock redesign; NOT just the lock)
# Migrations: schema/schema.go, schema/migrations/** (coordinate; FTS DDL is dialect-specific)
# Wiring: cmd/bn/app.go, cmd/bn/main.go, cmd/bn/cmd_memory.go (Postgres help strings)
# Tests: store/store_integration_test.go, store/*_test.go, schema/schema_test.go
```

This helps agents claim appropriate file surfaces when they start work.

---

## Verification Steps

After generating the script:

1. **Run it**: `chmod +x setup-beads.sh && ./setup-beads.sh`
2. **Check ready work**: `bd ready` should show initial analysis/prep tasks

---

## Completeness Checklist

Ensure your task graph includes:

- [ ] Analysis of current implementation in affected areas
- [ ] Characterization tests for existing behavior
- [ ] Feature flag or gradual rollout mechanism (if applicable)
- [ ] Core implementation broken into small units
- [ ] Unit tests for new/changed code
- [ ] Integration tests for affected workflows
- [ ] Regression testing plan
- [ ] Documentation updates
- [ ] Migration scripts (if data changes)
- [ ] Rollback plan
- [ ] Cleanup tasks for post-rollout
- [ ] Clear dependency chains with no cycles
