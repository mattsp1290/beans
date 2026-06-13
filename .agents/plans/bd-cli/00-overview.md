# `bn` — a Postgres-backed tracker (+ optional memory) CLI — REVISED post-review

> Revised after two independent Opus reviews (`review-opus1.md`,
> `review-opus2.md`) and reconciled in `09-review-reconciliation.md`, which
> **governs** the detail files `01`–`08`. Read `09` for the binding corrections.

`bn` ("beans") is a [charmbracelet/fang](https://github.com/charmbracelet/fang)
+ Cobra CLI that stores issues, dependencies (and optionally memories) in
**Postgres** instead of bd's embedded Dolt. It is the **native authoring**
front-end for the Postgres `tracker.Tracker` adapter, sharing one Go `store`
package with it — so authoring and the orchestrator's read/close path never
drift.

## 0. The decision this plan now supports (changed by review)

**Default: defer, unless a concrete project that genuinely cannot/will not run
Dolt exists today.** Both reviews (and the prior postgres-tracker reviews) reach
the same place:

- The **only** thing `bn`+Postgres uniquely buys is a **no-Dolt** stack. The
  concurrency win is **not collected** (multi-orchestrator dispatch is still the
  unbuilt `Manager`+`run_attempts` race — a non-goal here); single-orchestrator
  bd/Dolt is already fine.
- **"Keep bd/Dolt for new projects too"** is zero-build and gives the full bd
  surface + memory + unchanged skills.
- **m2xx (Dolt SQL server)** keeps the whole bd ecosystem *and* gives multi-client
  concurrency with no new tracker product — the lower-risk concurrency answer.
- So `bn` is the **big-product branch the prior reviews deferred**, and
  "greenfield: new projects use bn" risks **self-manufacturing** the demand. Name
  the real trigger (a no-Dolt deployment) or default to defer.

This plan is therefore a **decision + build-spec for *if* that trigger appears** —
not a "build now."

## 1. Reframe: a native surface, NOT a bd-compat promise (changed by review)

The original draft promised drop-in **bd compatibility**. Review found that has
**no consumer**: the orchestrator reads issues **in-process via `store` →
`core.Issue`** (never bd's CLI/JSON/id format), and the skills are being rewritten
anyway. The **only** place bd's format matters is `bn import` of a real
`bd export` file — and even that is **best-effort** (bd may export dependency
*counts*, not edges; see `06`).

So `bn` defines its **own native contract**:
- a **bd-*shaped*** command/flag set (so the skill rewrite is a near-trivial binary
  swap), but **not** a binding promise to track bd's evolving behavior;
- the load-bearing contract is between **`bn` and its callers** (the rewritten
  skills + the in-process adapter), e.g. `create --silent` → bare id, id
  pass-through, `ready` = open + all-blockers-terminal — these are **`bn`'s**
  contracts, not "bd compat";
- `bn import` accepts bd-export JSONL on a **best-effort seed** basis; `bn↔bn`
  round-trip is lossless via an explicit edge field (`06`).

## 2. Why it exists (the synthesis — still valid)

It de-circularizes the postgres-tracker: native `bn create`/`bn dep add` author
issues directly into Postgres (no bd-in-the-loop), the orchestrator's in-process
Postgres adapter reads/closes the same store, `/big-change` & `/new-project`
produce a dispatchable backlog. **Honest scope:** that in-process adapter **is the
deferred postgres-tracker adapter** — the single largest deliverable here, not a
given (`02`, `08`, `09`).

## 3. Decisions captured (from clarifying questions)

1. Shared Go `store` over Postgres; orchestrator uses the in-process adapter;
   `bn` is the authoring surface (no orchestrator-shells-out-to-`bn`).
2. Cover issues + deps **(memory deferred out of v1 per review — `05`, `09`)**.
3. Binary **`bn`**; skills updated to call it via a **committed per-repo marker**,
   not a `${TRACKER:-bd}` env default (review — `07`, `09`).
4. **Greenfield:** bd/Dolt stay canonical for `beans`; `bn`+Postgres is
   for new projects that opt in. No migration.

## 4. v1 scope (cut down per review)

`store` + schema + migrator → the Postgres `tracker.Tracker` adapter + the
conformance **extraction** refactor → a **minimal native CLI**
(`init/create/dep/ready/list/show/update/close` with `--silent`/`--json`) →
**create-only** `import`. **Deferred from v1:** memory (`05`), `export`/round-trip
(`06`), the skill edits (`07`, out-of-repo). Aggregate effort is **not** "a day" —
it is store+adapter (week+) **plus** the CLI/import/conformance stack on top
(`08`, `09`).

## 5. Non-goals
- Not changing beans's tracker/memory (stays bd/Dolt).
- Not orchestrator-shells-out-to-`bn`.
- **Not** multi-orchestrator dispatch safety (the `Manager`+`run_attempts` race —
  unbuilt, carried from m2xx/postgres-tracker).
- **Not** a fix for idp #6 (the actual idp blocker).
- **Not** a binding bd-compat contract (§1).

## The series
`01` command surface · `02` architecture · `03` data model · `04` cli/fang ·
`05` memory (deferred) · `06` import (create-only; export deferred) ·
`07` skill integration · `08` validation/sequencing · **`09` review
reconciliation (binding corrections)**.
