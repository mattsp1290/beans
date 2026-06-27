# Suggestions

- [cmd/bn/app.go:55] Add a regression test proving invalid workflow config still fails during command execution after the help preload skips the load error. This is non-blocking because `ensureWorkflow` still returns the loader error on execution, but a focused test would lock in that subtle contract.
