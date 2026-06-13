# Timestamp SQL Literal Catalog and Replacement Strategy

Issue: `beans-f24`

This note catalogs every current server-side timestamp SQL literal and defines
the replacement strategy for the GORM/Postgres/MySQL/SQLite migration.

## Decision

Use **application-side UTC timestamps for mutable store writes** and keep
database defaults only for immutable append-only rows where no update timestamp
exists. In practice:

- For `created_at`/`updated_at` model fields on mutable rows, set timestamps in
  Go with a single `clockNowUTC()` value per logical operation.
- For GORM models, prefer `autoCreateTime`/`autoUpdateTime` only if the tests
  prove it uses UTC consistently for all dialectors; otherwise set fields
  explicitly before `Create`/`Updates`.
- For raw SQL that remains, pass `time.Now().UTC()` as a bound parameter instead
  of spelling `now()` or dialect-specific functions in update statements.
- For schema defaults, use dialect-specific migration DDL:
  - PostgreSQL: `TIMESTAMPTZ` with `DEFAULT now()` is acceptable.
  - MySQL: `DATETIME(6)` or `TIMESTAMP(6)` with an explicit UTC connection policy;
    DSNs must include `parseTime=True`.
  - SQLite: store UTC timestamps in a tested format, preferably through Go-side
    values rather than relying on `CURRENT_TIMESTAMP` precision.

This keeps timestamp semantics independent of database server timezone, SQLite
string precision, and MySQL session timezone.

## Store Update Sites

| Current SQL | Location | Current behavior | Replacement strategy |
| --- | --- | --- | --- |
| `updated_at = now()` in dynamic issue updates | `store/store.go:504` | Any field update bumps `bn_issues.updated_at` inside the `UpdateIssue` transaction. | Generate one `now := clockNowUTC()` at the start of the transaction when fields change and set `updated_at` to that value through GORM/update parameters. |
| `UPDATE bn_issues SET updated_at = now()` after repo retargeting | `store/store.go:538` | Repo target replacement bumps issue `updated_at` even when no issue fields changed. | Reuse the same transaction timestamp policy; if both fields and repo target change, use one timestamp for the whole logical update. |
| `CloseIssue` state update | `store/store.go:571` | Closing a non-closed issue sets `state = 'closed'` and bumps `updated_at`; repeated close does not bump. | Preserve idempotence by updating `updated_at` only in the conditional state-change update. Use a bound UTC timestamp. |
| Deprecated `ImportIssues` terminal-state upsert | `store/store.go:797` | Merge updates non-state fields and bumps `updated_at` on conflict. | If retained during migration, pass a UTC timestamp into the conflict update clause or replace with `ImportIssuesFull` behavior. |
| Deprecated `ImportIssues` active-state upsert | `store/store.go:820` | Merge updates state only when existing state is not terminal and bumps `updated_at`. | Same as above; preserve never-regress-terminal semantics while passing a UTC timestamp. |
| `ImportIssuesFull` merge upsert | `store/store.go:975` | Merge updates non-state fields, preserves existing terminal states, and bumps `updated_at` on conflict. | Use GORM `OnConflict` with an explicit `updated_at` assignment to a bound UTC timestamp. Create-only imports must not bump existing rows. |
| Repo update dynamic SQL | `store/repo_store.go:342` | `UpdateRepo` always bumps `bn_repos.updated_at` and sets `updated_by`, then writes audit in the same transaction. | Set `UpdatedAt: nowUTC` and `UpdatedBy` in a GORM update map inside the transaction; use the same timestamp policy for alias replacement/audit ordering tests. |

## Insert and Scan Sites

| Current source | Location | Current behavior | Replacement strategy |
| --- | --- | --- | --- |
| Issue insert `RETURNING created_at, updated_at` | `store/store.go:154`, `store/store.go:186` | Database defaults populate both timestamps, then Go normalizes scans to UTC. | Set both fields to the same `nowUTC` in Go on create, including issue+repo transactional creates. |
| Memory insert `RETURNING id, created_at` | `store/store.go:1103` | Database default populates immutable `created_at`. | Either keep a dialect default for append-only memories or set `CreatedAt` explicitly in Go; tests should only require UTC and descending order. |
| Repo create `RETURNING created_at, updated_at` | `store/repo_store.go:259` | Database defaults populate repo timestamps. | Set both fields to one `nowUTC` in Go and preserve atomic create+aliases+audit transaction. |
| Repo audit insert returning `created_at` | `store/repo_store.go:653` | Database default populates immutable audit timestamp. | Prefer explicit `CreatedAt: nowUTC` when writing audit so audit ordering is independent of dialect precision. |
| Scan normalization | `store/store.go:216`, `store/store.go:1117`, `store/store.go:1191`, `store/store.go:1486`, `store/store.go:1525`, `store/repo_store.go:505`, `store/repo_store.go:549`, `store/repo_store.go:691` | Scanned timestamps are converted with `.UTC()`. | Keep UTC normalization at API boundaries even when writes are app-side UTC; add contract tests for UTC location and ordering stability. |

## Schema Defaults

| Table/column | Current DDL | Strategy |
| --- | --- | --- |
| `bn_projects.created_at` | `schema/migrations/0001_bn_init.sql:28` | Append-only row. A dialect default is acceptable, but Go-side UTC is also fine if project creation moves through GORM models. |
| `bn_issues.created_at`, `bn_issues.updated_at` | `schema/migrations/0001_bn_init.sql:49`, `0001:50` | Mutable row. Prefer application-side UTC for both fields; keep database defaults only as a safety net in dialect-specific migrations. |
| `bn_issue_notes.created_at` | `schema/migrations/0001_bn_init.sql:91` | Append-only note. Explicit Go UTC is preferred when writing notes; otherwise dialect default is acceptable with UTC scan tests. |
| `bn_memories.created_at` | `schema/migrations/0002_bn_memories.sql:20` | Append-only memory. Explicit Go UTC or dialect default are both acceptable if contract tests cover ordering and UTC normalization. |
| `bn_repos.created_at`, `bn_repos.updated_at` | `schema/migrations/0004_bn_repos.sql:32`, `0004:33` | Mutable row. Use application-side UTC for both fields. |
| `bn_repo_aliases.created_at` | `schema/migrations/0004_bn_repos.sql:58` | Append-only alias row within replacement transaction. Explicit Go UTC is preferred for deterministic tests. |
| `bn_project_admins.created_at` | `schema/migrations/0004_bn_repos.sql:74` | Append-only admin row. Dialect default is acceptable, but explicit Go UTC avoids MySQL/SQLite precision drift. |
| `bn_repo_audit.created_at` | `schema/migrations/0004_bn_repos.sql:94` | Immutable audit row. Explicit Go UTC is preferred so audit ordering is stable with `ORDER BY created_at DESC, id DESC`. |
| `bn_issue_repos.created_at`, `bn_issue_repos.updated_at` | `schema/migrations/0005_bn_issue_repos.sql:18`, `0005:19` | Mutable link row. Use application-side UTC when creating/replacing issue repo targets. |

## Test Contract

Add cross-dialect tests that assert:

- Created rows return non-zero UTC timestamps.
- `created_at` and `updated_at` are equal or ordered on create according to the
  chosen model policy.
- `UpdateIssue`, repo retargeting, `CloseIssue`, `ImportIssuesFull` merge, and
  `UpdateRepo` advance `updated_at`; idempotent `CloseIssue` and create-only
  import skips do not.
- Memory and repo audit ordering remains deterministic; use secondary ordering
  (`id DESC`) where precision can tie.
- MySQL DSNs include `parseTime=True`; SQLite tests document accepted precision.

## Migration Notes

- Introduce one helper or interface for time (`clockNowUTC`) before converting
  call sites. That makes tests deterministic and prevents multiple timestamps
  within a single logical transaction.
- Do not rely on GORM's default zero-value omission for timestamp fields. Set the
  fields explicitly or verify generated SQL includes them.
- Avoid `CURRENT_TIMESTAMP` in mutable update SQL unless a dialect-specific raw
  statement remains unavoidable.
