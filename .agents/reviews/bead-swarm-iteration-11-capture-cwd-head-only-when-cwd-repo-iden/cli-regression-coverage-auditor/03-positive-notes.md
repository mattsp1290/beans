# Positive Notes

- `cmd/bn/cmd_create_test.go`: The new tests check both positive and negative commit-capture cases instead of only asserting that a repo link exists.
- `cmd/bn/cmd_scope_test.go`: The existing auto-detect test now proves "by construction" capture for the plain cwd path.
- `cmd/bn/repo_resolve.go`: Best-effort behavior is intentionally quiet and does not turn git lookup failures into create failures.
