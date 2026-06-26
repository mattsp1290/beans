# Positive Notes

- store/store.go:1325 copies import repo inputs before normalizing them, avoiding mutation of caller-owned slices.
- store/store.go:1350 checks slug-only `creation_commit` imports against the target prefix, matching the requested "registered slug only" behavior.
- cmd/bn/cmd_import_test.go exercises both parser-level and store-level failure modes for imported creation commits.

