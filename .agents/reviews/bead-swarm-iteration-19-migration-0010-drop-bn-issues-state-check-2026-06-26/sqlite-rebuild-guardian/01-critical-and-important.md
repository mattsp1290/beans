# Critical And Important Findings

## Critical

No Critical issues found.

## Important

### SQLite rebuild test does not prove live FK behavior or retained non-state constraints

- Severity: Important
- File: `schema/schema_test.go:514`

The new test checks column names, index presence, state-check removal, and child-row survival, but it does not prove the rebuilt `bn_issues` table retained live foreign-key behavior or the non-state schema constraints/defaults restated by `schema/migrations/sqlite/0010_bn_issue_state_drop_check.sql:24`. This leaves part of the SQLite rebuild contract unproven, especially because the migration disables and reenables `PRAGMA foreign_keys` around a parent table drop.

Suggested fix:

```go
assertSQLiteForeignKeyCheckClean(t, sqlDB)
assertSQLiteColumnProperties(t, sqlDB, "bn_issues", map[string]sqliteColumnProperties{
    "id": {typ: "TEXT", pk: true},
    "prefix": {typ: "TEXT", notNull: true},
    "state": {typ: "TEXT", notNull: true, defaultValue: "'open'"},
})
// Also assert missing parent inserts fail, priority/json CHECKs fail, and
// deleting a migrated issue cascades to dependent rows.
```
