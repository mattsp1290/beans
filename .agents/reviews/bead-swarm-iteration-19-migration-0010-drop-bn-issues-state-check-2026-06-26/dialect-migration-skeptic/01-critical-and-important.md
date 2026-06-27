# Critical And Important Findings

## Critical

No Critical issues found.

## Important

### Cross-dialect ready-for-state writes are not proven after migration

- Severity: Important
- File: `store/store_integration_test.go:152`

The new proof is strong for SQLite’s rebuild path, but it does not demonstrate `ready_for_*` writes on Postgres/MySQL after migration 0010. Existing MySQL schema integration applies migrations but does not attempt these writes, and the cross-dialect store contract lacks an assertion that the newly valid hold states can be persisted on every dialect.

Suggested fix:

```go
t.Run("workflow_hold_states_after_migration", func(t *testing.T) {
    s := dialect.open(t)
    // create an issue and update through ready_for_review,
    // ready_for_validation, and ready_for_merge
})
```
