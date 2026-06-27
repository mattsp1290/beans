# Positive Notes

- `cmd/bn/workflow.go:36` removes the mutable package-global workflow cache and makes loaded workflow state app-scoped, avoiding cross-command/test contamination.

- `cmd/bn/cmd_update.go:40` now uses the app workflow for terminal reopen detection and explicit `--status` validation, so custom terminal states no longer depend on the built-in vocabulary.

- `cmd/bn/cmd_ready.go:27` passes configured terminal and active buckets to `ReadyIssues`, preserving hold behavior and custom dispatch states.

- `cmd/bn/cmd_list.go:55` preserves tolerant reads for unknown `--status` filters while making the warning vocabulary come from the configured workflow.

- `cmd/bn/cmd_import.go:147` and `cmd/bn/cmd_import.go:168` route import parsing and merge terminal preservation through the configured workflow instead of the built-in default.

- `cmd/bn/cmd_import_test.go:48` adds focused coverage proving custom import statuses are accepted and legacy default-only statuses are skipped under a custom workflow.
