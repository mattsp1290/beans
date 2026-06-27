# Suggestions

- [cmd/bn/cmd_update.go:140] Consider clarifying the `--claim` help text because it still names `in_progress` directly while other status-facing help is configurable. This is non-blocking because `--claim` still intentionally maps to the existing claim transition, but the wording could mention that the target status must be allowed by the configured workflow.
