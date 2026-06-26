# Positive Notes

- `cmd/bn/app_test.go`: The added legacy `show` assertion checks the actual JSON wire shape, not only the Go helper value.
- `cmd/bn/cmd_export_test.go`: The export test verifies both non-empty `creation_commit` emission and empty-field omission in the command path.
- `cmd/bn/cmd_import_test.go`: The import tests cover older rows without `repo`, `remote_url` auto-registration, existing `repo.slug` resolution, and the unresolved slug hard-failure path already present in the file.
