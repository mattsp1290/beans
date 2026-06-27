# Positive Notes

- `schema/schema_test.go:469` migrates SQLite only through v9 before seeding rows, so the test exercises the actual v10 migration transition.
- `schema/schema_test.go:506` explicitly proves the pre-v10 CHECK still rejects a new hold state.
- `schema/schema_test.go:510` applies v10 through the goose provider rather than bypassing the embedded migration runner.
- `schema/schema_test.go:529` and `schema/schema_test.go:530` verify the index is recreated and the state CHECK is removed.
- `schema/schema_test.go:532` and `schema/schema_test.go:535` prove post-v10 `ready_for_*` persistence at the raw SQL layer for SQLite.
