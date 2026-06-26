# Positive Notes

- `store/store.go:1545` centralizes non-empty `CreationCommit` validation in the store insert helper, so create/import-style callers share the same boundary rule.
- `store/store.go:1623` selects `ir.creation_commit` in the common `populateIssueRepos` query, which covers `GetIssue`, `ListIssues`, `ReadyIssues`, and other issue hydration paths that call it.
- `store/store_sqlite_contract_test.go:639` exercises returned create data, hydrated read data, invalid values, empty values, and rollback behavior in one focused contract test.

