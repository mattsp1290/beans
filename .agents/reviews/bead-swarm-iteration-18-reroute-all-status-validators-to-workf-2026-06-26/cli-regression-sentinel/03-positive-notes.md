# Positive Notes

- `cmd/bn/cmd_import.go:96` keeps dry-run parse-only behavior while still loading workflow config, so configured statuses are honored without requiring `BN_DSN`.
- `cmd/bn/cmd_import.go:147` correctly passes the loaded workflow into JSONL parsing and uses the same terminal set for live imports at `cmd/bn/cmd_import.go:167`.
- `cmd/bn/cmd_list.go:55` preserves list's lenient warning behavior while switching the known-status list to the configured workflow.
- `cmd/bn/cmd_ready.go:27` routes ready filtering through configured active and terminal buckets instead of the removed package global.
- `cmd/bn/app_test.go:218` adds an end-to-end root-command regression test proving configured status vocabulary is used for create/update/list in the normal CLI path.
