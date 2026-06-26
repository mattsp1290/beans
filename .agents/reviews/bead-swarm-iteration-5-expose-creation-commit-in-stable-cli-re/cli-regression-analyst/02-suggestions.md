# Suggestions

- `cmd/bn/app.go:515`: non-blocking consistency follow-up. `writeJSONTo(cmd.OutOrStdout(), ...)` improves command testability for `list`, `ready`, and `show`, but older JSON paths still use `writeJSON(os.Stdout)`. Consider migrating the remaining JSON commands later for consistent writer semantics.
