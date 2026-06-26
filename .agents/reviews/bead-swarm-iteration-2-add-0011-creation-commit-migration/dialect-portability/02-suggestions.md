# Suggestions

VERDICT: APPROVE

- `schema/schema_integration_test.go:57`: MySQL is the dialect with the most fragile syntax here because `TEXT` defaults require the parenthesized expression form. The current MySQL integration test applies migration 0011, which catches syntax failure, but it does not verify that an existing `bn_issue_repos` row receives `creation_commit = ''` or that information_schema reports the expected column/default. Suggested fix: add a MySQL upgrade assertion similar to the SQLite legacy-row test, seeding an issue/repo/link before finishing `Migrate`, then selecting `creation_commit` afterward.
- `schema/schema_test.go:455`: The legacy-row default/backfill behavior is covered only on SQLite. PostgreSQL and MySQL should behave the same for `ALTER TABLE ... ADD COLUMN ... NOT NULL DEFAULT`, but a parity test would protect that contract. Suggested fix: when real-engine test coverage is available, add PostgreSQL/MySQL counterparts or a small dialect-specific helper that validates existing rows after migration 0011.
