# Suggestions

## Suggestions

- `cmd/bn/git_resolver_test.go:126`: The rebase fixture can assert that active HEAD equals the recorded `main` commit. That makes the test more precise and easier to reason about.
- `cmd/bn/git_resolver_test.go:82`: Consider splitting the broad common-state test into smaller tests if it grows further; the current form is acceptable but fixture coupling will make future failures harder to localize.
- `cmd/bn/git_resolver_test.go:219`: If older Git versions are in scope, replace `git init -b main` with `git init` plus branch rename. Current project tooling appears modern enough for `-b`.

