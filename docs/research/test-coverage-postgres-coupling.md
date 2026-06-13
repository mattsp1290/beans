# Test Coverage and Postgres Coupling Analysis

Issue: `beans-jnd`

This audit maps the current tests that protect the store and schema behavior
before the GORM/multi-database migration. It focuses on tests that are either
Postgres-coupled today or missing for behavior that must survive the migration.

## Current Test Layout

| Area | Files | Current role |
| --- | --- | --- |
| Store behavior | `store/store_integration_test.go` | One large Postgres/testcontainers integration suite covering migrations, projects, repos, issues, dependencies, ready queries, imports, and some error sentinels. |
| Schema inventory | `schema/schema_test.go` | Embedded migration listing and Postgres migration text checks. |
| CLI parsing/formatting | `cmd/bn/*_test.go` | Unit tests for config marker parsing, command registration, import/export formatting, JSON output, and dry-run behavior. These mostly avoid the store. |

There are no non-integration store tests. All store coverage currently requires
Docker and a Postgres container through `testcontainers`.

## Store Coverage Inventory

| Behavior | Current coverage | Gaps before migration |
| --- | --- | --- |
| Migrations | `TestMigrateIdempotent`, `TestMigrateBootstrapsLegacyGooseVersionTable` | Covers Postgres fresh and legacy version-table startup. No SQLite default/no-Docker migration test and no MySQL migration test yet. |
| Projects | `TestEnsureProjectIdempotent` | Covers idempotent creation and existence. Missing invalid prefix and post-close behavior. |
| Repo admins | `TestRepoAdminBootstrapAndAuthorization`, `TestRepoAdminBootstrapAllowsOnlyOneConcurrentFirstAdmin` | Good coverage for authorization and Postgres advisory-lock bootstrap semantics. Needs a dialect-neutral contract for the exactly-one-first-admin race. |
| Repo registry | `TestRepoRegistryRoundTrip`, `TestRepoRegistryValidatesRepoTargets` | Covers create, aliases, metadata JSON, listing disabled repos, update, disable, and audit row count. Missing focused duplicate slug/alias conflict assertions, `RemoveRepoAdmin`, direct `InsertRepoAudit`, audit limit/order, and disabled repo lookup behavior. |
| Issue create/read/list | `TestCreateAndGetIssue`, `TestCreateIssueWithRepo`, `TestListIssues` | Covers basic fields, repo target population, rollback on bad repo target, and state filtering. Missing labels round trip assertions, branch/url fields, priority/type normalization edge cases, and ordering expectations. |
| Issue update/close/delete | `TestUpdateIssue`, `TestUpdateIssueRejectsInvalidState`, `TestUpdateIssueNotFound`, `TestCloseIdempotent`, `TestDeleteIssue`, `TestUpdateIssueRepoTarget` | Covers partial update, missing issue, invalid DB state, close idempotence, cascade delete, and repo retarget rollback. Missing `CloseIssue` missing-id semantics, note/audit side effects, URL/branch/label update behavior, and updated-at monotonicity. |
| Dependencies | `TestDepAddRemoveCycleCheck`, `TestListDeps`, import dependency tests | Covers add/remove, duplicate edge, simple cycle, self-edge error, populated blockers, and prefix list. Missing cross-prefix dependency rejection, missing child/parent distinction, longer cycle shapes, and concurrent `AddDep` cycle races. |
| Ready issues | `TestReadyIssues`, `TestReadyIssues_CustomTerminal` | Covers chained blockers and custom terminal state sets. Missing active-state filtering beyond `open`, multiple blockers with mixed terminal/active states, and ordering. |
| Imports | `TestImportIssues`, `TestImportIssuesFullCreateOnlyCountsAreIdempotent`, `TestImportIssuesFullMergeStateTruthTable`, `TestImportIssuesFullConcurrentCreateOnlyRetriesSerialization`, `TestImportIssuesFullSkipsCrossPrefixConflicts`, `TestImportIssuesFullSkipsInvalidDependencyEdges` | Good coverage for terminal-state protection, create-only counts, merge truth table, serialization retry, cross-prefix conflicts, duplicate/missing/self/cycle deps. Missing repo target import behavior, metadata/labels branch/url round trips, and explicit rollback assertions on partial import failure. |
| Memories | None in `store/store_integration_test.go` | High-priority gap. `InsertMemory` and `SearchMemories` have no characterization coverage for global vs prefix visibility, empty-query ordering, Postgres FTS ranking, tag containment, metadata JSON, default limit, explicit limit, or created-at ordering. |
| Store lifecycle/errors | `TestCloseIdempotent` covers issue close, not store close | Missing `Store.Close` idempotence and post-close `ErrPoolClosed` behavior. Missing broad `ErrConflict` normalization tests for duplicate repos/aliases and missing-driver-independent error assertions. |

## Postgres-Coupled Assertions

These tests or helpers assert behavior through Postgres-specific implementation
details and should be rewritten or isolated before multi-dialect contract tests
become authoritative.

| Coupling | Location | Replacement direction |
| --- | --- | --- |
| Schema test asserts Postgres-only `NOT VALID` and constraint name `bn_issues_state_check` | `schema/schema_test.go` | Move to Postgres-only migration tests. Cross-dialect schema tests should assert semantic state validation exists or is handled by Go validation. |
| Invalid issue state test requires error text containing `bn_issues_state_check` | `store/store_integration_test.go` | Assert a store-level validation/sentinel behavior instead of a Postgres constraint name once validation moves out of Postgres-only DDL. |
| Repo first-admin concurrency relies on `pg_advisory_xact_lock(hashtext($1))` | `store/repo_store.go`, covered by `TestRepoAdminBootstrapAllowsOnlyOneConcurrentFirstAdmin` | Keep the behavior test, but make the implementation dialect-neutral or run a dialect-specific lock strategy behind one contract. |
| Import concurrency test relies on Postgres serializable retry behavior | `TestImportIssuesFullConcurrentCreateOnlyRetriesSerialization` | Preserve the concurrent import contract, but make retries/error normalization portable across MySQL/SQLite. |
| All store tests use Postgres containers | `store/store_integration_test.go` | Extract a reusable store contract suite that can run against SQLite by default and Postgres/MySQL under integration tags. |
| Legacy goose bootstrap fixture uses Postgres `now()` and identity syntax | `TestMigrateBootstrapsLegacyGooseVersionTable` | Keep as a Postgres-specific upgrade test. Add per-dialect fresh migration tests as dialect DDL lands. |

## Docker and Testcontainers Assumptions

`store/store_integration_test.go` starts a fresh `postgres:16-alpine` container
per test helper call. The helper skips when Docker is unavailable by matching
testcontainers error strings. This is workable for integration verification but
cannot be the only store contract gate during a multi-dialect migration.

Recommended split:

1. A default `go test ./...` suite that includes store contract tests against
   SQLite once SQLite migrations exist.
2. `go test -tags=integration ./...` for container-backed Postgres and MySQL.
3. Shared contract helpers that run the same store behavior cases across every
   configured dialect, with dialect-specific tests only for upgrade/DDL details.

## Characterization Test Priorities

Before rewriting store internals, add focused tests in this order:

1. Memory behavior: insert round trip, empty query ordering by `created_at`,
   full-text query behavior, prefix/global visibility, tag containment, metadata
   JSON round trip, default and explicit limits.
2. Store API errors and lifecycle: `Store.Close` idempotence, post-close
   `ErrPoolClosed`, duplicate repo/alias `ErrConflict`, missing repo/admin/issue
   `ErrNotFound`, duplicate dependency `ErrDuplicateDep`.
3. Repo registry details: duplicate slug and alias conflicts, `RemoveRepoAdmin`,
   `InsertRepoAudit`, `ListRepoAudit` limit/order, disabled repo lookup/listing.
4. Issue and dependency edge cases: labels/branch/url round trips, close missing
   id, update all mutable fields, cross-prefix dependency rejection, longer cycle
   shapes, concurrent dependency cycle attempts.
5. Import expansion: repo target import behavior, metadata/labels round trips,
   rollback on invalid dependency or repo target, and never-regress-terminal
   behavior across create-only and merge modes.

## Migration Guidance

Do not treat the existing Postgres integration suite as the future contract
suite. It is valuable characterization coverage, but it mixes public store
semantics with Postgres SQL details. The next test beads should extract reusable
store behavior helpers while keeping Postgres-specific assertions in clearly
named Postgres integration tests.
