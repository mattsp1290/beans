# Suggestions

## Non-Blocking

### Validate status before terminal reopen checks

- Path/line: `cmd/bn/cmd_update.go:44`
- Rationale: An invalid `--status` on a terminal issue currently looks like a reopen attempt because unknown statuses are non-terminal. The command can report `use --force to re-open` before it ever reaches the invalid-status error. Validating `status` before computing `wantsReopen` would make typo errors more direct and would avoid requiring `--force` just to discover the status was invalid.

### Add explicit ready/import root coverage for custom buckets

- Path/line: `cmd/bn/app_test.go:218`
- Rationale: The new root test covers update validation and list warning vocabulary. A small companion case for `ready` with a custom `active` bucket, plus import dry-run/live parsing of a custom status, would lock down the other command surfaces changed by this branch.

### Consider making help text show discoverability hints

- Path/line: `cmd/bn/cmd_update.go:141`
- Rationale: The dynamic status list cannot be known safely at command construction time, so the generic help text is understandable. It may still be worth mentioning `BN_CONFIG` or `bn prime`/workflow config docs so users can find the configured vocabulary when `--help` no longer lists the default statuses.
