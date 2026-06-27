# Positive Notes

- `schema/schema_test.go:506` verifies the legacy SQLite state CHECK rejects `ready_for_review` before v10.
- `schema/schema_test.go:532` verifies a `ready_for_review` update succeeds after v10.
- `schema/schema_test.go:535` verifies a new `ready_for_validation` row can be inserted after v10.
- `schema/schema_test.go:556` checks child rows in `bn_issue_repos`, `bn_issue_deps`, and `bn_issue_notes` survive the rebuild.
- `schema/schema_test.go:529` confirms `bn_issues_prefix_state_idx` is recreated.
