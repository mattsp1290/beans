# 05 — CLI UX

## Setting the new statuses

No new command surface is required — the existing `--status` flag carries the new
values once validation is config-driven:

```bash
bn update beans-abc123 --status=ready_for_review
bn update beans-abc123 --status=ready_for_validation
bn update beans-abc123 --status=ready_for_merge
```

Moving *into* a hold status from `in_progress` needs no `--force` (hold states are
non-terminal, so the re-open guard at `cmd_update.go:59-75` does not trip).
Moving *out of* `closed`/`done` back into a hold status is a re-open and still
requires `--force` — unchanged, correct behavior.

## Filtering

```bash
bn list --status=ready_for_merge        # issues awaiting merge
bn ready                                # excludes all three (they are not active)
```

`bn ready` intentionally does **not** show issues in the new statuses — they are
in flight, not available work (see `01-status-model.md`).

## Help text (must be de-hardcoded)

Three flag descriptions currently enumerate the legacy five states and will lie
after a config change:

- `cmd/bn/cmd_update.go:158` — `"set state (open, in_progress, blocked, closed, done)"`
- `cmd/bn/cmd_list.go:87` — `"filter by state (open, in_progress, closed, …)"`
- the warning string at `cmd/bn/cmd_list.go:55`

Update each to derive the list from `rs.workflow.StatusNames()` at runtime (build
the usage string when the command is constructed), or use a generic phrasing like
`"set state (see configured workflow statuses)"`. Prefer the dynamic list so
`bn update --help` reflects the actual deployment.

## Table rendering

`printIssueTable` (`cmd/bn/cmd_ready.go:62`) uses a `%-12s` STATUS column.
`ready_for_validation` (20 chars) overruns it. Fix per `04-code-changes.md`:
widen to `%-22s` (header rule too), or size to the longest configured status.
Apply to every fixed-width status printer found in the audit.

JSON output (`-j/--json`) is unaffected — statuses pass through as strings.

## Optional convenience (not v1-blocking)

If the maintainer wants ergonomic shortcuts, these are cheap follow-ons (file as
separate beads, do not block the core change):

- `bn review <id>` → `--status=ready_for_review`, `bn validate <id>`,
  `bn mergeable <id>` as thin aliases over `update`.
- A `bn status` (no-arg) subcommand that prints the configured vocabulary and
  bucket classification — useful for operators to see what their config produced.

Recommend shipping the core change first and gauging whether aliases are wanted;
they add command surface that must itself stay config-aware.

## Discoverability

Document the config file in `README.md` / `AGENTS.md` (a `[workflow]` section
example) and note the search precedence (`02-config-system.md`). Ship an example
`bn.toml` in the repo (e.g. `docs/` or repo root, git-tracked) so operators have
a copy-paste starting point.
