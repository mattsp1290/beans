#!/bin/bash
# Project: beans
# Change: Migrate store persistence from pgx/Postgres-only SQL to GORM with PostgreSQL, MySQL, and SQLite support
# Generated: 2026-06-13

set -euo pipefail

if [ ! -d ".beads" ]; then
  bd init --non-interactive
fi

if bd ready >/dev/null 2>&1; then
  existing_count=$(bd list --json 2>/dev/null | jq 'length')
  if [ "${existing_count:-0}" -gt 0 ]; then
    echo "Refusing to create duplicate graph: existing beads found." >&2
    echo "Use 'bd ready' to inspect current work." >&2
    exit 1
  fi
fi

echo "Creating GORM multi-database migration bead graph..."

# ========================================
# Phase 0: Analysis and Blocking Decisions
# ========================================

ANALYZE_CURRENT=$(bd create 'Read docs/research/gorm-multidb-migration.md first, then analyze current pgx persistence surface in store/, schema/, cmd/bn/, and go.mod: catalog pgxpool, pgx.Row/Rows/Tx helpers, $N placeholders, ON CONFLICT, = ANY(), JSONB, tags @>, recursive CTE cycle checks, tsvector search, now() literals, pgconn/puddle error handling, and goose Postgres migration wiring. Reservation note: read-only analysis across docs/research/**, store/**, schema/**, cmd/bn/**.' -p 0 --label analysis --silent)

ANALYZE_TESTS=$(bd create 'Analyze current test coverage and Postgres-coupled assertions: store/*_test.go, store/store_integration_test.go integration build tag, schema/schema_test.go NOT VALID and bn_issues_state_check expectations, repo admin bootstrap concurrency test, memory search assertions, timestamp ordering, and Docker/testcontainers assumptions.' -p 0 --label analysis --silent)
bd dep add "$ANALYZE_TESTS" "$ANALYZE_CURRENT"

API_CONTRACT=$(bd create 'Define the public Store API contract consumed by cmd/bn/*: Preserve Store method set behavior for issues, dependencies, memories, repos, audit, Close idempotence, ErrNotFound/ErrConflict semantics, and stale tracker doc comments must not create a tracker adapter task.' -p 0 --label analysis --silent)
bd dep add "$API_CONTRACT" "$ANALYZE_CURRENT"

DESIGN_DSN=$(bd create 'DECISION: DSN and dialect selection for store.Config: choose explicit Driver/BN_DRIVER versus DSN-scheme inference; redesign SecretDSN redaction for postgres, mysql user:pass@tcp, and sqlite file DSNs; map MaxConns/MinConns int settings to database/sql pool tuning.' -p 0 --label analysis --silent)
bd dep add "$DESIGN_DSN" "$ANALYZE_CURRENT"

DESIGN_MIGRATIONS=$(bd create 'DECISION: migration strategy for schema/: choose per-dialect embedded migration directories versus GORM AutoMigrate plus raw dialect-specific FTS DDL; goose does not transform DDL, so existing BIGSERIAL, JSONB, TIMESTAMPTZ, tsvector, GIN, regex CHECK, and NOT VALID spellings cannot be shared verbatim.' -p 0 --label analysis --silent)
bd dep add "$DESIGN_MIGRATIONS" "$ANALYZE_CURRENT"

DESIGN_FTS=$(bd create 'DECISION: memory search strategy for SearchMemories: ranked per-dialect FTS using Postgres tsvector, MySQL FULLTEXT, SQLite FTS5 and bm25, or portable LIKE fallback; define whether relevance ordering is contractually asserted.' -p 0 --label analysis --silent)
bd dep add "$DESIGN_FTS" "$ANALYZE_CURRENT"

DESIGN_TAGS=$(bd create 'DECISION: JSON tag containment replacement for SearchMemories tags filtering: datatypes JSON query, normalized bn_memory_tags table, or portable fallback; define indexes, migration shape, and all-tags matching semantics.' -p 0 --label analysis --silent)
bd dep add "$DESIGN_TAGS" "$DESIGN_FTS"

DESIGN_CYCLE_ISOLATION=$(bd create 'DECISION: portable AddDep and ImportIssuesFull cycle-safety semantics: replace pgx Serializable plus recursive CTE with Go-side graph walk or dialect-specific transaction strategy; specify per-dialect isolation expectations for concurrent edge inserts.' -p 0 --label analysis --silent)
bd dep add "$DESIGN_CYCLE_ISOLATION" "$ANALYZE_CURRENT"

DESIGN_ADMIN_BOOTSTRAP_LOCK=$(bd create 'DECISION: portable repo admin first-admin bootstrap guard: replace pg_advisory_xact_lock(hashtext($1)) with constraint-backed insert logic or dialect-conditional locks; preserve exactly-one-first-admin behavior under concurrency.' -p 0 --label analysis --silent)
bd dep add "$DESIGN_ADMIN_BOOTSTRAP_LOCK" "$ANALYZE_CURRENT"

INLINE_NOW_PLAN=$(bd create 'Catalog every inline server timestamp SQL literal and define the replacement strategy: UpdateIssue, CloseIssue, ImportIssues, ImportIssuesFull, repo updates, and any raw updated_at = now() statements must become GORM auto timestamps, CURRENT_TIMESTAMP, or Go-side UTC timestamps across Postgres/MySQL/SQLite.' -p 0 --label analysis --silent)
bd dep add "$INLINE_NOW_PLAN" "$ANALYZE_CURRENT"

# ========================================
# Phase 1: Preparation and Characterization
# ========================================

CHAR_STORE=$(bd create 'Add characterization tests for existing store semantics before migration: EnsureProject, issue CRUD, update validation, CloseIssue, DeleteIssue, AddDep/RemoveDep/ListDeps, cycle rejection, ReadyIssues, ImportIssues, ImportIssuesFull never-regress-terminal behavior, and ErrNotFound/ErrConflict normalization. Reservation note: store/*_test.go and store/store_integration_test.go.' -p 0 --label prep --silent)
bd dep add "$CHAR_STORE" "$ANALYZE_TESTS"
bd dep add "$CHAR_STORE" "$API_CONTRACT"

CHAR_MEMORY=$(bd create 'Add characterization tests for memory behavior: InsertMemory, SearchMemories empty query ordering, full-text query behavior, tag containment filters, prefix/global visibility, created_at ordering, and metadata round trip. Reservation note: store/store_integration_test.go and any focused store memory test files.' -p 0 --label prep --silent)
bd dep add "$CHAR_MEMORY" "$ANALYZE_TESTS"
bd dep add "$CHAR_MEMORY" "$DESIGN_FTS"
bd dep add "$CHAR_MEMORY" "$DESIGN_TAGS"

CHAR_REPO=$(bd create 'Add characterization tests for repo registry behavior: CreateRepo, aliases, metadata JSON, audit rows, ListRepos disabled filtering, AuthorizeRepoAdmin, RemoveRepoAdmin, DisableRepo, duplicate-key behavior, and concurrent first-admin bootstrap. Reservation note: store/repo_store.go tests and store/store_integration_test.go.' -p 0 --label prep --silent)
bd dep add "$CHAR_REPO" "$ANALYZE_TESTS"
bd dep add "$CHAR_REPO" "$DESIGN_ADMIN_BOOTSTRAP_LOCK"

IMPORT_PARSER_TESTS=$(bd create 'Add focused bn import parser and CLI summary tests from docs/prompts/bn-import-plan.md: bd JSONL field mapping, dependency filtering, malformed/missing/invalid rows, blank/comment lines, dry-run JSON shape, command stdin behavior, and no project registration during dry-run. Reservation note: cmd/bn/cmd_import_test.go and cmd/bn/cmd_import.go.' -p 0 --label prep --silent)
bd dep add "$IMPORT_PARSER_TESTS" "$ANALYZE_TESTS"

IMPORT_STORE_CONTRACT=$(bd create 'Add ImportIssuesFull contract coverage from docs/prompts/bn-import-plan.md: create-only idempotency, merge terminal-state truth table, cross-prefix conflicts, duplicate IDs/edges, missing blockers, self-deps, cycle skips, and DB-effect-based created/updated/skipped/deps counts. Reservation note: store/store_integration_test.go and store/store.go.' -p 0 --label prep --silent)
bd dep add "$IMPORT_STORE_CONTRACT" "$CHAR_STORE"
bd dep add "$IMPORT_STORE_CONTRACT" "$DESIGN_CYCLE_ISOLATION"

# ========================================
# Phase 2: Foundational Implementation
# ========================================

MODELS=$(bd create 'Introduce GORM model definitions and shared type mapping for bn_projects, bn_issues, bn_deps, bn_memories, repo registry tables, aliases, admins, audit, and issue-repo links; use datatypes.JSON or serializer json without Postgres-only gorm type tags. Reservation note: new or existing files under store/ and schema/ as chosen by design.' -p 0 --label impl --silent)
bd dep add "$MODELS" "$CHAR_STORE"
bd dep add "$MODELS" "$CHAR_MEMORY"
bd dep add "$MODELS" "$CHAR_REPO"
bd dep add "$MODELS" "$DESIGN_TAGS"

DEPS_ADD=$(bd create 'Add required dependencies to go.mod/go.sum before GORM implementation work: gorm.io/gorm, gorm.io/driver/postgres, gorm.io/driver/mysql, github.com/glebarez/sqlite, gorm.io/datatypes, and github.com/testcontainers/testcontainers-go/modules/mysql. Do not prune pgx/puddle/goose yet.' -p 0 --label prep --silent)
bd dep add "$DEPS_ADD" "$DESIGN_DSN"
bd dep add "$DEPS_ADD" "$DESIGN_MIGRATIONS"
bd dep add "$DEPS_ADD" "$DESIGN_FTS"

GORM_CONN=$(bd create 'Replace store/pool.go pgxpool holder with a GORM/database-sql holder: open postgres, mysql, and pure-Go glebarez sqlite dialectors; ping with ConnectTimeout; preserve Close idempotence and ErrPoolClosed; remove puddle-specific normalize path. Reservation note: store/pool.go, store/config.go.' -p 0 --label impl --silent)
bd dep add "$GORM_CONN" "$MODELS"
bd dep add "$GORM_CONN" "$DEPS_ADD"
bd dep add "$GORM_CONN" "$DESIGN_DSN"

ERRORS=$(bd create 'Implement dialect-aware error classification in store/errors.go and call sites: duplicate primary-key/unique violations for postgres, mysql 1062, sqlite UNIQUE constraint failed; preserve ErrNotFound, ErrConflict, and pool-closed normalization behavior after pgx removal.' -p 0 --label impl --silent)
bd dep add "$ERRORS" "$GORM_CONN"

SCHEMA_CORE=$(bd create 'Rework schema.Migrate and migration listing for chosen multi-dialect strategy: replace *pgxpool.Pool, stdlib.OpenDBFromPool, goose.DialectPostgres, and Postgres session locker with *gorm.DB/*sql.DB plus dialect-aware provider/locking behavior. Reservation note: schema/schema.go and schema/migrations/**.' -p 0 --label migration --silent)
bd dep add "$SCHEMA_CORE" "$DEPS_ADD"
bd dep add "$SCHEMA_CORE" "$DESIGN_MIGRATIONS"

MIGRATION_DDL=$(bd create 'Create or rewrite schema DDL for PostgreSQL, MySQL, and SQLite: replace BIGSERIAL, JSONB, TIMESTAMPTZ, generated tsvector, GIN, POSIX regex CHECK, REFERENCES/ON DELETE details, NOT VALID, and server now() defaults with dialect-correct equivalents.' -p 0 --label migration --silent)
bd dep add "$MIGRATION_DDL" "$SCHEMA_CORE"
bd dep add "$MIGRATION_DDL" "$MODELS"
bd dep add "$MIGRATION_DDL" "$DESIGN_TAGS"
bd dep add "$MIGRATION_DDL" "$DESIGN_FTS"

# ========================================
# Phase 3: Store Implementation
# ========================================

STORE_CORE=$(bd create 'Convert all of store/store.go in one coordinated pass: replace pgx scan helpers, Row/Rows/Tx interfaces, pool.conn usage, $N placeholders, = ANY(), ON CONFLICT SQL, now() literals, recursive CTE cycle checks, import upsert state rules, and raw scans with GORM primitives while preserving Store API behavior. Reservation note: store/store.go exclusively; exceeds the 750 LOC guideline because helpers are shared across issues/deps/memories.' -p 0 --label impl --silent)
bd dep add "$STORE_CORE" "$MIGRATION_DDL"
bd dep add "$STORE_CORE" "$ERRORS"
bd dep add "$STORE_CORE" "$API_CONTRACT"
bd dep add "$STORE_CORE" "$DESIGN_CYCLE_ISOLATION"
bd dep add "$STORE_CORE" "$INLINE_NOW_PLAN"
bd dep add "$STORE_CORE" "$IMPORT_STORE_CONTRACT"

REPO_STORE=$(bd create 'Convert store/repo_store.go to GORM: replace pgx transactions, ON CONFLICT/RETURNING, pgx.ErrNoRows, dynamic $N update builder, JSONB metadata/audit scans, and pg_advisory_xact_lock first-admin bootstrap with portable GORM or dialect-aware SQL. Reservation note: store/repo_store.go exclusively.' -p 0 --label impl --silent)
bd dep add "$REPO_STORE" "$STORE_CORE"
bd dep add "$REPO_STORE" "$DESIGN_ADMIN_BOOTSTRAP_LOCK"
bd dep add "$REPO_STORE" "$INLINE_NOW_PLAN"

MEMORY_SEARCH=$(bd create 'Implement SearchMemories using the decided FTS and tag strategy: per-dialect search adapter or LIKE fallback, SQLite FTS5 virtual tables/triggers if needed, MySQL FULLTEXT DDL if needed, Postgres tsvector behavior if retained, and portable all-tags filtering.' -p 0 --label impl --silent)
bd dep add "$MEMORY_SEARCH" "$STORE_CORE"
bd dep add "$MEMORY_SEARCH" "$MIGRATION_DDL"
bd dep add "$MEMORY_SEARCH" "$DEPS_ADD"
bd dep add "$MEMORY_SEARCH" "$DESIGN_FTS"
bd dep add "$MEMORY_SEARCH" "$DESIGN_TAGS"

CMD_WIRING=$(bd create 'Update cmd/bn wiring for multi-database config: construct store.Config with the selected dialect mechanism, update BN_DSN help and errors in app.go/main.go, and remove hardcoded Postgres tsvector/plainto_tsquery and Postgres connection string text from cmd_memory.go. Reservation note: cmd/bn/app.go, cmd/bn/main.go, cmd/bn/cmd_memory.go.' -p 1 --label impl --silent)
bd dep add "$CMD_WIRING" "$GORM_CONN"
bd dep add "$CMD_WIRING" "$DESIGN_DSN"
bd dep add "$CMD_WIRING" "$DESIGN_FTS"

# ========================================
# Phase 4: Testing and Validation
# ========================================

CONTRACT_SQLITE=$(bd create 'Build default no-Docker SQLite store-contract suite: in-process sqlite openFn, migration setup, shared assertions for issue CRUD, deps, imports, repos, memories, timestamps, error normalization, and Close behavior; ensure go test ./... runs it without integration tag.' -p 0 --label testing --silent)
bd dep add "$CONTRACT_SQLITE" "$STORE_CORE"
bd dep add "$CONTRACT_SQLITE" "$REPO_STORE"
bd dep add "$CONTRACT_SQLITE" "$MEMORY_SEARCH"
bd dep add "$CONTRACT_SQLITE" "$IMPORT_STORE_CONTRACT"

CONTRACT_CONTAINERS=$(bd create 'Generalize store/store_integration_test.go into a dialect-parameterized contract suite for Postgres and MySQL testcontainers plus SQLite: one storeContractTest(t, openFn) with dialect expectation hooks for FTS ranking, isolation, timestamp precision, and constraint/error text.' -p 0 --label testing --silent)
bd dep add "$CONTRACT_CONTAINERS" "$CONTRACT_SQLITE"
bd dep add "$CONTRACT_CONTAINERS" "$DEPS_ADD"
bd dep add "$CONTRACT_CONTAINERS" "$DESIGN_FTS"
bd dep add "$CONTRACT_CONTAINERS" "$DESIGN_CYCLE_ISOLATION"
bd dep add "$CONTRACT_CONTAINERS" "$DESIGN_ADMIN_BOOTSTRAP_LOCK"

FIX_SCHEMA_TESTS=$(bd create 'Rewrite schema/schema_test.go for the chosen migration strategy: stop asserting Postgres-only NOT VALID and bn_issues_state_check SQL text; assert migration ordering, dialect-specific filesystem shape, and required table/index/constraint presence instead.' -p 1 --label testing --silent)
bd dep add "$FIX_SCHEMA_TESTS" "$MIGRATION_DDL"

FIX_STORE_TESTS=$(bd create 'Rewrite Postgres-coupled store tests: remove assumptions about pgx error strings, bn_issues_state_check names, tsvector ranking text, and exact Postgres constraint messages; assert portable sentinel errors and semantic behavior instead.' -p 1 --label testing --silent)
bd dep add "$FIX_STORE_TESTS" "$CONTRACT_SQLITE"

TIMESTAMP_TESTS=$(bd create 'Add cross-dialect timestamp and timezone tests: MySQL parseTime and loc handling, SQLite precision/ordering, UTC normalization in scanned Store results, created_at DESC stability, and updated_at changes after UpdateIssue, CloseIssue, imports, and repo updates.' -p 1 --label testing --silent)
bd dep add "$TIMESTAMP_TESTS" "$CONTRACT_SQLITE"
bd dep add "$TIMESTAMP_TESTS" "$INLINE_NOW_PLAN"

CONCURRENCY_TESTS=$(bd create 'Add concurrency regression tests for portable guarantees: concurrent AddDep cannot create cycles, concurrent first-admin bootstrap allows exactly one first admin, SQLite single-writer behavior is accepted through dialect expectation hooks, and Postgres/MySQL container tests exercise real isolation.' -p 1 --label testing --silent)
bd dep add "$CONCURRENCY_TESTS" "$CONTRACT_CONTAINERS"
bd dep add "$CONCURRENCY_TESTS" "$DESIGN_CYCLE_ISOLATION"
bd dep add "$CONCURRENCY_TESTS" "$DESIGN_ADMIN_BOOTSTRAP_LOCK"
bd dep add "$CONCURRENCY_TESTS" "$REPO_STORE"

VERIFY_LOCAL=$(bd create 'Run local verification and fix fallout: make test, make vet, make lint, make build, make ci where available; confirm go test ./... is green without Docker and no pgx imports remain unless intentionally retained.' -p 0 --label testing --silent)
bd dep add "$VERIFY_LOCAL" "$DEPS_ADD"
bd dep add "$VERIFY_LOCAL" "$CMD_WIRING"
bd dep add "$VERIFY_LOCAL" "$FIX_SCHEMA_TESTS"
bd dep add "$VERIFY_LOCAL" "$FIX_STORE_TESTS"
bd dep add "$VERIFY_LOCAL" "$TIMESTAMP_TESTS"

VERIFY_INTEGRATION=$(bd create 'Run Docker-backed integration verification: go test -tags=integration ./... with Postgres and MySQL testcontainers; document skip behavior or failure mode when Docker is unavailable.' -p 1 --label testing --silent)
bd dep add "$VERIFY_INTEGRATION" "$CONTRACT_CONTAINERS"
bd dep add "$VERIFY_INTEGRATION" "$CONCURRENCY_TESTS"
bd dep add "$VERIFY_INTEGRATION" "$VERIFY_LOCAL"

# ========================================
# Phase 5: Documentation, Rollback, Cleanup
# ========================================

DOCS=$(bd create 'Update README and any command docs for multi-database usage: BN_DSN and dialect configuration, Postgres/MySQL/SQLite DSN examples, Docker requirements for integration tests, SQLite default/no-Docker test path, and changed memory search semantics if LIKE or per-dialect ranking differs.' -p 2 --label docs --silent)
bd dep add "$DOCS" "$CMD_WIRING"
bd dep add "$DOCS" "$CONTRACT_SQLITE"

ROLLBACK=$(bd create 'Create rollback and validation notes for this one-way migration: identify revert branch strategy, files touched by the GORM migration, dependency rollback steps in go.mod/go.sum, and validation checkpoints using SQLite default tests plus Docker integration suite. No dual-write or data migration is required because backwards compatibility is explicitly dropped.' -p 2 --label docs --silent)
bd dep add "$ROLLBACK" "$VERIFY_LOCAL"

DEPS_CLEANUP=$(bd create 'Prune replaced dependencies from go.mod/go.sum after the GORM migration compiles: remove jackc/pgx, puddle, and goose only if no remaining imports require them; run go mod tidy and verify module graph is clean.' -p 3 --label cleanup --silent)
bd dep add "$DEPS_CLEANUP" "$VERIFY_INTEGRATION"

CLEANUP=$(bd create 'Cleanup after green verification: remove stale pgx/Postgres comments, tracker doc-comment references if misleading, unused helpers, unused migrations or adapters, dead imports, and obsolete Postgres-only help text; run gofmt/go mod tidy.' -p 3 --label cleanup --silent)
bd dep add "$CLEANUP" "$VERIFY_INTEGRATION"
bd dep add "$CLEANUP" "$DEPS_CLEANUP"
bd dep add "$CLEANUP" "$DOCS"
bd dep add "$CLEANUP" "$ROLLBACK"

FINAL_REVIEW=$(bd create 'Final review gate: inspect full diff for accidental unrelated changes, verify file reservations were respected, confirm no bd task depends on a non-existent tracker adapter, rerun make ci and integration suite if available, and record any residual dialect-specific limitations.' -p 0 --label testing --silent)
bd dep add "$FINAL_REVIEW" "$CLEANUP"

echo ""
echo "Bead graph created."
echo "Initial ready work:"
bd ready || true
