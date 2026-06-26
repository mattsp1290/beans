# 01 — Status Model

## Today's model

`IssueState` is a typed string, not an enum (`model/issue.go:48`):

```go
type IssueState string
```

The model already documents three classification buckets
(`model/issue.go:36-47`):

- **active** — eligible for dispatch (e.g. `open`).
- **terminal** — workspace-cleanup target (e.g. `closed`, `done`).
- **everything else** — held without cleanup.

The default vocabulary is the five values `open`, `in_progress`, `blocked`,
`closed`, `done`, with classification currently split across:

- active = `{open}` — `cmd/bn/cmd_ready.go:17`
- terminal = `{closed, done}` — `cmd/bn/cmd_ready.go:14` and (duplicated)
  `cmd/bn/cmd_update.go:14` (`cliTerminalStates`)

## New default vocabulary

After this change, the **built-in default** config (used when no config file is
present) becomes, in lifecycle order:

```
open
in_progress
ready_for_review
ready_for_validation
ready_for_merge
blocked
closed
done
```

Classification of the built-in default:

| Bucket   | Members                                                              |
|----------|---------------------------------------------------------------------|
| active   | `open`                                                               |
| terminal | `closed`, `done`                                                     |
| hold     | `in_progress`, `blocked`, `ready_for_review`, `ready_for_validation`, `ready_for_merge` |

`hold` is implicit: any status in the vocabulary that is neither `active` nor
`terminal`. We do not store it explicitly.

## Lifecycle semantics of the three new statuses

The intended flow:

```
open ──claim──▶ in_progress ──▶ ready_for_review ──▶ ready_for_validation ──▶ ready_for_merge ──▶ closed
```

Because the three new statuses are **hold** states:

1. **`bn ready` excludes them.** `ReadyIssues` filters `state IN (activeStates)`
   (`store/store.go:455-465`); with active = `{open}` these never appear. An
   issue parked in `ready_for_review` is correctly *not* offered as new work.
2. **They do not satisfy blockers.** A dependent is ready only when its blockers
   are in a terminal state (`store/store.go:488-495`). An issue in
   `ready_for_merge` still blocks its dependents until it is `closed`/`done`.
   This is deliberate: "ready to merge" is not "merged."
3. **They are not terminal**, so re-opening guards (`cmd/bn/cmd_update.go:59-75`)
   do not treat them as closed, and moving *into* them from `in_progress` is a
   normal non-terminal status write (no `--force` needed).

## Transitions (deferred, but designed-for)

v1 does **not** enforce transition legality — any valid status can move to any
valid status, exactly as today (`--status=<state>` accepts any allowed value,
`cmd/bn/cmd_update.go:93-96`). The natural ordering above is documentation, not
enforcement.

The config schema in `02-config-system.md` reserves an optional `transitions`
section so a later phase can enforce it without another format change. When
absent (the v1 default), transitions are unrestricted. This keeps v1 scope tight
while not painting us into a corner.

## Why "hold," not "active" or "terminal"

- Making them **active** would surface half-finished work in `bn ready` and
  invite double-dispatch.
- Making them **terminal** would (a) satisfy blockers prematurely and (b) signal
  the orchestrator to clean up the workspace before the work is actually merged.

"Hold" is the only bucket that means "in flight, do not pick up, do not consider
done" — exactly the semantics of a review/validation/merge queue.
