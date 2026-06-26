# Positive Notes

- `cmd/bn/git_resolver.go:57`: The real resolver keeps command failures non-fatal by returning `("", false, nil)`, matching the existing `Toplevel` and `RemoteURL` best-effort contract.
- `cmd/bn/git_resolver.go:66`: Output is trimmed before validation, so normal `git rev-parse HEAD` newline output is accepted without accepting malformed surrounding content.
- `cmd/bn/git_resolver.go:73`: The validator is intentionally strict about lowercase full-length object IDs, matching the store-side `creation_commit` contract.
- `cmd/bn/git_resolver_test.go:55`: The fake resolver supports `HeadCommit` without shelling out and records the root argument for future wiring tests.

