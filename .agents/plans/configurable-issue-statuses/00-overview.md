# Configurable Issue Statuses Plan

## Problem

The `bn`/beans tracker hardcodes its status vocabulary in four independent
places that must be kept in lockstep by hand:

- The DB `CHECK` constraint — embedded inline in
  `schema/migrations/sqlite/0001_bn_init.sql:22` and added separately for
  Postgres/MySQL in `schema/migrations/*/0003_bn_issue_state_check.sql`.
- The store-layer validator `isValidIssueState` in `store/store.go:1765`.
- The CLI-layer validator `allowedStates`/`isAllowedState` in
  `cmd/bn/cmd_update.go:24`.
- The "ready" classification defaults `defaultTerminalStates` /
  `defaultActiveStates` in `cmd/bn/cmd_ready.go:12`.

Two things follow from that. First, adding a status today is a five-file,
three-dialect change with no single source of truth. Second, every deployment
is locked to the same five states (`open`, `in_progress`, `blocked`, `closed`,
`done`) — operators running different workflows cannot tailor the set.

We want to do both at once: **add three new statuses** and **make the status set
deployment-configurable** via a TOML/YAML file, so the next status change is a
config edit rather than a code change.

## Goals

1. Add three new lifecycle statuses:
   - `ready_for_review`
   - `ready_for_validation`
   - `ready_for_merge`
2. Make the full status vocabulary — plus its active/terminal classification and
   the default status for new issues — loadable from a deployment config file
   (TOML or YAML), with built-in defaults and env override.
3. Collapse the four hardcoded status sources into **one** runtime
   `WorkflowConfig`, consulted by the store and CLI layers.
4. Preserve backward compatibility: existing databases, existing `bd`
   import/export JSON, and existing `open/in_progress/blocked/closed/done` data
   keep working with zero operator action.

## Non-Goals

- **No status *transition* state machine in v1.** The three new statuses imply a
  natural pipeline (`in_progress → ready_for_review → ready_for_validation →
  ready_for_merge → closed`), but enforcing legal transitions is deferred. The
  config schema reserves room for it (see `02-config-system.md`); enforcement is
  a later phase.
- **No new dependency-readiness semantics.** Issues in the new "hold" states are
  neither dispatchable (`bn ready`) nor terminal, so they still block their
  dependents until they reach a terminal state. That is intended.
- **No rename of the `state`/`status` duality.** The store column stays `state`;
  the `bd`-compatible JSON field stays `status` (`cmd/bn/app.go:425`). Untouched.
- **No per-issue custom workflows.** Config is process/deployment-wide, not
  per-project or per-repo (a possible later extension, noted in rollout).

## The Three New Statuses

| Status                 | Bucket | Meaning                                              |
|------------------------|--------|------------------------------------------------------|
| `ready_for_review`     | hold   | Work done, awaiting code review                      |
| `ready_for_validation` | hold   | Review passed, awaiting QA/validation                |
| `ready_for_merge`      | hold   | Validated, awaiting merge to main                    |

"Hold" = the existing model's third bucket: "everything else — held without
cleanup" (`model/issue.go:38-44`). Not active (won't show in `bn ready`), not
terminal (won't satisfy blockers, won't trigger workspace cleanup). Naming
follows the established `snake_case` convention of `in_progress`.

## Core Decision

**Move the status vocabulary out of the database and into a single
config-derived `WorkflowConfig`, validated in the application layer.**

The DB `CHECK` constraint is the thing that makes the vocabulary un-configurable
(a config-defined status the DB rejects is useless). So we drop the constraint
and make the app layer the sole authority on what is a valid status. This is
consistent with the existing design intent already written into the code:

> "We model IssueState as a typed string rather than an enum so trackers with
> custom state names (Linear, JIRA workflows) flow through unchanged. The
> orchestrator classifies each state via WorkflowConfig at runtime."
> — `model/issue.go:45-47`

The plan operationalizes that already-documented `WorkflowConfig` concept.

### Safety invariant: read-tolerant, write-strict

Because operators can edit the config, a deployed config might omit a status
that already exists in the DB. The system must therefore be:

- **Write-strict** — reject any *write* (`create`, `update --status`, `import`)
  whose target status is not in the active config.
- **Read-tolerant** — never crash on *reading* a row whose status is not in the
  config. Display it verbatim; classify an unknown status as "hold" (neither
  active nor terminal) so it is conservatively excluded from dispatch and never
  falsely counted as done.

This invariant is the backbone of the migration story and is restated in
`02-config-system.md` and `06-rollout-and-testing.md`.

## Plan Files

- [01-status-model.md](01-status-model.md): the new statuses, their lifecycle
  semantics, and how classification buckets work.
- [02-config-system.md](02-config-system.md): config file format (TOML + YAML),
  load precedence, library choice, the `WorkflowConfig` type, and validation.
- [03-data-model-and-migrations.md](03-data-model-and-migrations.md): DB
  constraint removal per dialect (incl. the SQLite table-rebuild wrinkle) and
  the new migration.
- [04-code-changes.md](04-code-changes.md): the exact code seams to change, by
  `file:line`, to route every status decision through `WorkflowConfig`.
- [05-cli-ux.md](05-cli-ux.md): CLI behavior, help text, table-width fix, and
  optional convenience.
- [06-rollout-and-testing.md](06-rollout-and-testing.md): implementation
  sequence, beads issues to file, test plan, and acceptance criteria.
