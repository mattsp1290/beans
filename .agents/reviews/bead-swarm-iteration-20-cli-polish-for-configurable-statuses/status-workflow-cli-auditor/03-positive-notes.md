# Positive Notes

- [cmd/bn/app.go:52] The comment clearly documents why workflow config is preloaded before command execution and preserves the distinction between help rendering and startup validation.
- [cmd/bn/cmd_update.go:141] `update --status` help now uses `rs.workflowConfig().StatusNames()`, so configured deployments see their actual vocabulary instead of stale legacy states.
- [cmd/bn/cmd_ready.go:72] `issueStatusColumnWidth` keeps the default `ready_for_validation` width while expanding for longer custom statuses.
- [cmd/bn/app_test.go:306] The new help-vocabulary test checks both inclusion of configured statuses and exclusion of default-only statuses.
