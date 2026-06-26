# Positive Notes

- `cmd/bn/repo_resolve.go:150` keeps the identity guard at the right boundary by resolving the cwd git identity back through the store and comparing `cwdRepo.ID` with the selected repo ID before reading and returning HEAD.
- `cmd/bn/repo_resolve.go:159` prevents invalid fake or future resolver output from reaching `CreateIssue`, preserving best-effort create behavior instead of surfacing a store validation error for `creation_commit`.
- `cmd/bn/cmd_create_test.go:344` proves the command path omits `creation_commit` from JSON when capture fails due to either repo identity mismatch or invalid git output, not just when the resolver helper is called directly.
- `cmd/bn/cmd_create_test.go:399` covers the prefix mismatch guard and verifies the rejected repo is omitted from JSON entirely, which protects the project/repo boundary that could otherwise mask capture mistakes.
- `cmd/bn/cmd_auto_detect_test.go:88` and `cmd/bn/cmd_auto_detect_test.go:132` explicitly assert that plain auto-detect does not read HEAD; this keeps creation_commit capture scoped to issue creation after repo selection.
- `cmd/bn/cmd_create.go:124` now writes JSON through `cmd.OutOrStdout()`, making command tests able to observe `create --json` output without redirecting global stdout.
