## Action Items

### Critical
- None

### Important
- [ ] [cmd/bn/workflow.go:52] Make direct command paths derive workflow from the injected store, or otherwise ensure custom workflow is present when root initialization is bypassed.

### Suggestions
- [ ] [cmd/bn/cmd_update.go:44] Validate explicit `--status` before terminal reopen checks so typos produce the invalid-status error first.
- [ ] [cmd/bn/app_test.go:218] Add root-command coverage for custom ready active buckets and import dry-run/live parsing.
- [ ] [cmd/bn/cmd_update.go:141] Add a help-text hint for discovering configured workflow statuses.
