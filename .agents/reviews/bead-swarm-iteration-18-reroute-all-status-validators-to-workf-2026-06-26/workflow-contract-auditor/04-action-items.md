## Action Items

### Critical
- None

### Important
- [ ] [store/store.go:740] Route close writes and idempotence checks through `WorkflowConfig.Terminal` instead of hardcoding `"closed"`.

### Suggestions
- [ ] [cmd/bn/cmd_update.go:68] Validate or configure the `--claim` target instead of hardcoding `in_progress`.
- [ ] [cmd/bn/app_test.go:220] Add CLI-level ready coverage for a custom active/terminal workflow.
