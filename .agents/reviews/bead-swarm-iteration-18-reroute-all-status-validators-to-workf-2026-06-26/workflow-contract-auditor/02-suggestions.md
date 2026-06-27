# Suggestions

- `cmd/bn/cmd_update.go:68` - `--claim` still hardcodes `in_progress`. The store remains write-strict and will reject it if `in_progress` is not configured, so this is not a persistence invariant break, but the CLI should either validate that configured workflows include the claim target or introduce a configured claim/active target instead of relying on the legacy literal.

- `cmd/bn/app_test.go:220` - The new root-command vocabulary test covers update and list warning behavior under a custom workflow. Add a sibling test for `ready` with a custom active/terminal set so the CLI-level path at `cmd/bn/cmd_ready.go:27` is protected from future regression, not just the lower-level store tests.
