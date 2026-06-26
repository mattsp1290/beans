# 04 — Code Changes

Every hardcoded status decision is rerouted through `WorkflowConfig`. This is the
exhaustive seam list, each with its current `file:line` (from recon) and the
required change. The unifying rule is the **read-tolerant / write-strict**
invariant from `00-overview.md`.

## New files

| File | Purpose |
|------|---------|
| `model/workflow.go` | `WorkflowConfig` type, helpers, `DefaultWorkflowConfig()`. |
| `cmd/bn/config.go` (or extend `app.go`) | `loadWorkflowConfig()` — file discovery, TOML/YAML decode, validation. |

## Store layer

### `store/store.go:1765` — `isValidIssueState`

Current:

```go
func isValidIssueState(state model.IssueState) bool {
    switch state {
    case "open", "in_progress", "blocked", "closed", "done":
        return true
    default:
        return false
    }
}
```

Change: replace the hardcoded switch with a lookup into the config the store
holds. Either (a) give `Store` a `workflow model.WorkflowConfig` field set at
construction and call `s.workflow.IsValid(state)`, or (b) keep a package-level
default and let the validation site pass the config. Prefer (a) — one
authoritative copy on the store. Used at the write path
`store/store.go:551-552` (`CreateIssue`/update), so this enforces **write-strict**.

### `store/store.go:449` — `ReadyIssues`

Already parameterized: it takes `terminalStates`/`activeStates` arguments. **No
signature change needed.** The caller (`cmd_ready.go`) must pass the config's
slices instead of package globals. The unknown-status read-tolerance falls out
naturally: a row whose state is not in `activeStates` simply isn't selected, and
is not in `terminalStates` so it can't satisfy a blocker — i.e. unknown ≡ hold.

### Create path — default status

Where `CreateIssue` assembles the row, set `State` from
`WorkflowConfig.Default` when the caller didn't specify one (see
`03-data-model-and-migrations.md` "Default status on insert"). Grep the store
for the insert assembly; wire the default there.

## CLI layer

### `cmd/bn/cmd_update.go:24-30` — `allowedStates` / `isAllowedState`

Delete the package-level `allowedStates` map and `isAllowedState`. Replace the
call site at `cmd_update.go:93-94`:

```go
if !rs.workflow.IsValid(model.IssueState(status)) {
    return fmt.Errorf("invalid status %q (allowed: %s)",
        status, strings.Join(rs.workflow.StatusNames(), ", "))
}
```

The error now lists the *configured* vocabulary (add a `StatusNames()` helper),
so the message stays truthful after a config change. **Write-strict.**

### `cmd/bn/cmd_update.go:14-19` — `cliTerminalStates`

Delete this duplicated terminal-set map. Replace its uses (the re-open guard at
`cmd_update.go:59-75`) with `rs.workflow.IsTerminal(cur.State)`. This removes the
second source of terminal truth.

### `cmd/bn/cmd_ready.go:12-17` — `defaultTerminalStates` / `defaultActiveStates`

Delete both package globals. In `newReadyCmd`'s `RunE`, pass the config slices:

```go
issues, err := rs.store.ReadyIssues(cmd.Context(), f,
    rs.workflow.Terminal, rs.workflow.Active)
```

### `cmd/bn/cmd_ready.go:62-74` — `printIssueTable` column width

The `STATUS` column is `%-12s` (`cmd_ready.go:68,72`). `ready_for_validation` is
20 chars and will break alignment. Widen to `%-22s` (longest default is 20; 22
gives padding), or compute the width from the configured vocabulary's longest
name. Apply the same width to the header rule on `cmd_ready.go:69-70`. Audit any
other table printer that renders status with a fixed width.

### `cmd/bn/cmd_list.go:53-58` — status filter warning

Current warns with a hardcoded "known" list:

```go
fmt.Fprintf(..., "warning: unknown status %q (known: open, in_progress, blocked, closed)\n", status)
```

Replace the membership check with `rs.workflow.IsValid(...)` and interpolate
`rs.workflow.StatusNames()` into the message. Note this is currently a *warning*
that still applies the filter — preserve that lenient behavior for `list`
(filtering by a typo'd status simply returns nothing), or upgrade to an error;
recommend keeping it a warning since list is read-only. Also update the flag help
at `cmd_list.go:87` and `cmd_update.go:158` (both hardcode the status list in the
flag description).

### `cmd/bn/cmd_import.go:259-262` — import validation

`isAllowedState(raw.Status)` gates imports (skips + warns on unknown). Repoint to
`rs.workflow.IsValid(...)`. With the three new statuses in the default config,
`bd` exports containing them now import instead of being silently dropped.
**Write-strict** — an import row with a status outside the configured vocabulary
is still skipped+counted, exactly as today.

## Wiring `workflow` into `appState`

`appState` is constructed in `cmd/bn/app.go` near `storeConfigFromEnv()`
(`app.go:144`). Add:

```go
type appState struct {
    // ...existing...
    workflow model.WorkflowConfig
}
```

Resolve once at startup:

```go
wf, err := loadWorkflowConfig()   // defaults → file → env; validates; fail-fast
if err != nil {
    return fmt.Errorf("workflow config: %w", err)
}
rs.workflow = wf
// pass wf into the store constructor so isValidIssueState sees it
```

Then thread `wf` into the `Store` constructor so the store-layer validator and
any default-status insert use the same config instance. One load, one copy,
referenced everywhere.

## JSON output — no change

`toIssueJSON` (`cmd/bn/app.go:473`, `Status: string(iss.State)`) already passes
the state through as an opaque string. New statuses serialize correctly with zero
change. Same for export (`cmd/bn/cmd_export.go:111`).

## Summary: what gets deleted vs. added

**Deleted (sources of duplicated truth):**
- `allowedStates` map + `isAllowedState` — `cmd_update.go:24`
- `cliTerminalStates` map — `cmd_update.go:14`
- `defaultTerminalStates` / `defaultActiveStates` — `cmd_ready.go:12-17`
- hardcoded switch body in `isValidIssueState` — `store.go:1765`
- the SQL `CHECK` constraints — three migrations (see `03`)

**Added (single source of truth):**
- `model.WorkflowConfig` + `DefaultWorkflowConfig()` — `model/workflow.go`
- `loadWorkflowConfig()` — `cmd/bn/config.go`
- `appState.workflow` + store wiring
- migration `0010_*` × 3 dialects
