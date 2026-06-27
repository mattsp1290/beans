# Positive Notes

- [AGENTS.md:16] The new workflow configuration section gives agents the critical operational rule: `ready_for_*` states are valid hold states, not ready work and not blocker-satisfying.
- [cmd/bn/cmd_list.go:89] `list --status` help now names the configured statuses, which improves discoverability for deployments with custom vocabularies.
- [cmd/bn/cmd_ready.go:63] The table header and rows use the same computed width, keeping scan-friendly output for long statuses.
- [cmd/bn/app_test.go:396] The table test uses a status longer than the built-in defaults, which covers the custom-status UX rather than only the `ready_for_*` case.
