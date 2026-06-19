# 06 — Rollout & Testing

## Implementation sequence

Ordered so the tree builds and tests pass after each step (no big-bang).

1. **`model.WorkflowConfig`** — type, helpers (`IsValid/IsActive/IsTerminal/
   IsHold`, `StatusNames`), `DefaultWorkflowConfig()` with the new 8-status
   default. Pure, unit-testable, no callers yet. (`model/workflow.go`)
2. **Config loader** — `loadWorkflowConfig()`: file discovery + precedence
   (`02-config-system.md`), TOML/YAML decode, fail-fast validation. Add the
   chosen encoder dep(s) to `go.mod` *after maintainer sign-off on the library
   choice*. (`cmd/bn/config.go`)
3. **Wire `appState.workflow`** and pass into the `Store` constructor. Still
   uses the default config (no file yet) — behavior identical to today **plus**
   the three new statuses are now valid. (`cmd/bn/app.go`)
4. **Reroute validators** to the config: `isValidIssueState`
   (`store.go:1765`), `isAllowedState`/`allowedStates` (`cmd_update.go`),
   `cliTerminalStates` (`cmd_update.go`), `defaultTerminal/ActiveStates`
   (`cmd_ready.go`), `cmd_list.go` warning, `cmd_import.go`. Delete the dead
   globals. (`04-code-changes.md`)
5. **Migration `0010_*` × 3 dialects** to drop the CHECK. Do SQLite last and
   carefully (table rebuild against the *post-`0009`* schema). (`03`)
6. **CLI polish** — table column width, flag help text, example `bn.toml`,
   README/AGENTS docs. (`05`)
7. **Optional aliases** — only if requested; separate beads.

Steps 1–4 are mergeable as one logical change (new statuses become usable via
default config). Step 5 is what unblocks DBs that already have the constraint.
Step 6 is polish. Keep them as distinct commits for reviewability.

## Beads issues to file

```bash
EPIC=$(bd create "Configurable issue statuses + ready_for_* states" \
  -d "Add ready_for_review/validation/merge; make status vocabulary config-driven via TOML/YAML. Plan: .agents/plans/configurable-issue-statuses/" \
  -t epic -p 1 --silent)

M=$(bd create "model.WorkflowConfig + DefaultWorkflowConfig" \
  -d "New model/workflow.go: typed vocabulary, active/terminal/hold helpers, 8-status default incl ready_for_*. Unit tested." \
  -t feature -p 1 -l impl --silent)

C=$(bd create "loadWorkflowConfig: TOML/YAML loader + precedence + validation" \
  -d "cmd/bn/config.go. Defaults->file(BN_CONFIG/cwd/.bn marker/XDG)->env. Fail-fast validation. Library choice pending maintainer sign-off." \
  -t feature -p 1 -l impl --silent)

W=$(bd create "Thread workflow config into appState and Store" \
  -d "appState.workflow; pass into Store constructor; default-status-on-insert from config." \
  -t feature -p 1 -l impl --silent)

R=$(bd create "Reroute all status validators to WorkflowConfig" \
  -d "Replace isValidIssueState(store.go:1765), allowedStates/isAllowedState + cliTerminalStates(cmd_update.go), defaultTerminal/ActiveStates(cmd_ready.go), cmd_list warning, cmd_import. Delete dead globals." \
  -t feature -p 1 -l impl --silent)

DB=$(bd create "Migration 0010: drop bn_issues state CHECK (pg/mysql/sqlite)" \
  -d "DROP CONSTRAINT for pg/mysql; SQLite table rebuild against post-0009 schema (highest risk). Symmetric down migrations restore legacy 5-value CHECK." \
  -t feature -p 1 -l impl --silent)

UX=$(bd create "CLI polish: table width, flag help, example bn.toml, docs" \
  -d "Widen STATUS column %-12s->%-22s; de-hardcode flag/usage strings; ship example config; README/AGENTS." \
  -t task -p 2 -l docs --silent)

bd dep add $C $M       # loader builds on the type
bd dep add $W $C       # wiring needs the loader
bd dep add $R $W       # rerouting needs config on appState/store
bd dep add $DB $R      # drop DB guard only once app-layer guard is in place
bd dep add $UX $R
for x in $M $C $W $R $DB $UX; do bd dep add $x $EPIC; done
```

> Order matters for `$DB`: **only drop the DB constraint after the app-layer
> validator is the source of truth** (`$R`), so there is never a window where
> nothing guards writes.

## Test plan

### Unit — `model.WorkflowConfig`
- `DefaultWorkflowConfig()` contains all 8 statuses; `active={open}`,
  `terminal={closed,done}`; the three `ready_for_*` are hold (not active, not
  terminal).
- `IsValid/IsActive/IsTerminal/IsHold` for known, unknown, and edge values.

### Unit — config loader
- TOML and YAML files decode to identical configs.
- Precedence: `BN_CONFIG` beats cwd beats marker beats XDG.
- Partial file inherits defaults key-by-key.
- Validation rejects: empty `statuses`, `default ∉ statuses`,
  `active/terminal ∉ statuses`, `active ∩ terminal ≠ ∅`. Each gives a distinct
  fail-fast error.

### Store — extend existing tests
- `isValidIssueState` accepts `ready_for_*` under default config; rejects junk.
- `TestReadyIssues` (`store_integration_test.go:2274`) — add a case asserting an
  issue in `ready_for_review` is **excluded** from ready and **still blocks** a
  dependent (does not satisfy the blocker).
- `TestReadyIssues_CustomTerminal` (`:2344`) — already exercises custom terminal
  sets; add a custom config where `ready_for_merge` is configured terminal and
  assert blocker-satisfaction flips. This is the key configurability proof.
- SQLite contract tests (`store_sqlite_contract_test.go`) — run the full
  migration set including `0010` and assert a `ready_for_*` write succeeds
  (proves the CHECK is gone) and that the rebuilt table preserved all rows +
  columns.

### Migration tests
- `schema/schema_test.go` already validates embedded migrations parse
  (`TestListMigrationsParsesEmbedded`, `:34`) and are non-empty (`:132`). The new
  `0010` files are covered automatically; confirm they appear and parse for all
  three dialects.
- Add an up→down→up round-trip test for `0010` on SQLite (rebuild is the risky
  one) asserting row/column/index preservation.
- MySQL/Postgres migration tests run via testcontainers — confirm `0010` applies
  cleanly and a `ready_for_*` insert succeeds post-migration.

### CLI / integration
- `bn update <id> --status=ready_for_review` succeeds; `--status=bogus` errors
  with a message listing the configured vocabulary.
- `bn list --status=ready_for_merge` filters correctly.
- `bn ready` omits issues in the new statuses.
- `bn import` of a `bd` export containing `ready_for_*` now imports (not skipped).
- A deployment with a custom `bn.toml` (e.g. drop `done`, add a custom status)
  changes validation accordingly — and a row with a now-unknown status still
  **reads** without crashing (read-tolerant) but cannot be **written**.

## Quality gates (per project + global rules)

- `go build ./...` (all targets) and `go vet ./...`.
- `golangci-lint run` (`.golangci.yml` present).
- `go test ./...` including the testcontainers-backed store/migration suites.
- Run a real `bn` against a scratch SQLite DB end-to-end (create → move through
  all three new statuses → close) per the verification rule ("it builds" ≠ "it
  works").

## Acceptance criteria

1. `ready_for_review`, `ready_for_validation`, `ready_for_merge` are settable,
   listable, filterable, and importable on all three dialects.
2. The status vocabulary, default, and active/terminal buckets are driven by a
   TOML **or** YAML config file with documented precedence; absent config
   reproduces today's behavior **plus** the three new statuses.
3. There is exactly **one** runtime source of status truth
   (`WorkflowConfig`); the four legacy hardcoded sources and the DB CHECK are
   gone.
4. Read-tolerant / write-strict invariant holds: unknown statuses display but
   cannot be written.
5. All quality gates pass; the change is reviewed via `/review` before merge
   (multi-file change touching schema + serialization boundary — review is
   mandatory per workflow rules).

## Open questions for the maintainer

1. **Config library**: two-encoder dispatch (BurntSushi/toml + yaml.v3) vs.
   koanf v2? (`02-config-system.md` recommends the former for v1.)
2. **Drop vs. expand the CHECK**: this plan drops it for full configurability.
   Acceptable to lose the DB-level guard in favor of app-layer validation?
3. **Config scope**: process-wide (this plan) vs. per-project/per-repo (the repo
   registry from the multi-repo plan could carry a workflow override later)?
4. **Default status**: keep `open` as the new-issue default, or make it
   `config.default` everywhere including the DB column default?
