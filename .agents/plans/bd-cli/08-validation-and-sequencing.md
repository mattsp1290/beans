# 08 — Validation, risks, sequencing

## Grounding gates (resolve BEFORE coding the relevant part)

1. **fang option names/signatures** (04) — confirm `fang.Execute` options
   (`WithVersion`, signal/notify, completions/man toggles) against the pinned
   version. Provisional in the plan.
2. **bd `--json` issue shape + `bd export` dependency edges** (03, 06) — capture a
   real `bd show --json` and a `bd export` line; confirm `status` vs `state`, the
   field set, and whether dep **edges** (not just counts) are exported. The
   import loader and `--json` parity depend on this.
3. **"ready"/terminal-state semantics** (03) — diff `bd ready`'s id set against
   the `store` SQL on a seeded fixture (open-only vs +in_progress; the configured
   terminal set, not hardcoded `closed`). Parity is load-bearing.

## Tests

- **`store` unit tests** (testcontainers Postgres, mirroring `internal/persistence`):
  create/read/filter, dep add + cycle rejection, `ready` = open+unblocked over the
  configured terminal set, close idempotency, show/update NotFound (rows-affected
  → not-found), import modes (create-only never regresses terminal state), memory
  upsert/search.
- **CLI golden tests** (`cmd/bn`): `create --silent` prints **only** the id
  (no ANSI, no extra lines) — the load-bearing skill contract; `--json` shape;
  exit codes + stderr classification; non-interactive (no TTY prompt).
- **tracker conformance reuse:** run the existing `tracker.Tracker` contract suite
  (`test/conformance/tracker/`) against the **Postgres adapter** over the shared
  `store`. Per the postgres-tracker review this needs an **extraction refactor**
  (the suite today bakes bd-argv/subprocess/stderr assertions into the contract
  tests); extract the backend-agnostic assertions into a factory-parameterized
  suite. Budget for it (it's not a constructor swap).
- **End-to-end (the loop):** `bn init`→`bn create`×N→`bn dep add`→ orchestrator
  (postgres adapter) dispatches the ready set→`Close`→`bn list` shows closed. A
  real new-project smoke.
- **Parity harness:** a fixture run through both `bd` and `bn`, asserting the same
  `--silent` ids round-trip and the same ready-set (modulo id text).

## Risks (carried + new)

- **Two writers on `state`** (orchestrator `Close` + `bn import`): mitigated by
  import defaulting to `create-only`/`merge`-never-regress (06). Do not ship a
  default `replace`.
- **Pool starvation:** `bn`/adapter use their **own** pgx pool, not the
  persistence pool (02).
- **fang stdout styling corrupting `--silent`/`--json`** (04) — guarded by golden
  test.
- **Search parity** for memory is approximate, by design (05) — documented.
- **Dispatch race** for multiple orchestrators is unsolved (Manager+run_attempts),
  same as m2xx/postgres-tracker — explicit non-goal (00).
- **Schema migrations / coexistence** with the orchestrator audit DB — separate
  schema + migrator (02).
- **Skill edits are out-of-repo** (dotfiles) — sequence last; `bn` works without
  them (07).

## Relationship to the other plans

- **postgres-tracker** (`.agents/plans/postgres-tracker/plan.md`): `bn` supplies
  the **native authoring** path that review found missing — so the postgres
  tracker stops being "circular." This plan + the postgres-tracker adapter are
  two halves of one system over a shared `store`.
- **m2xx / jsonl-bridge:** orthogonal (those keep bd/Dolt). `bn`+Postgres is the
  "drop Dolt for new projects" direction; bd/Dolt stay canonical for
  beans.
- **idp #6:** unrelated; still the actual idp blocker. `bn` does not touch it.

## Sequencing (proposed)

1. **`store` + schema + migrator** (the foundation; testcontainers-tested).
2. **Postgres `tracker.Tracker` adapter** over `store` + the
   factory/conformance-extraction (this is also postgres-tracker Phase-1; do it
   here, once).
3. **`bn` core** (`init/create/dep/ready/list/show/update/close`) over `store`,
   with the `--silent`/`--json` golden tests.
4. **`bn import/export`** (create-only default).
5. **`bn remember/memories`** (independently shippable; orchestrator-independent).
6. **Skill integration** (dotfiles; `TRACKER=bn` parameterization) — last, optional.

Steps 1–2 are the high-value, reusable core (they're also what an eventual
postgres-tracker needs). 3–4 deliver the authoring CLI. 5–6 are additive.

## Open questions (for review)

- Q1: `store` location/name — `internal/tracker/postgres` (shared with the
  adapter) vs a dedicated `internal/bn/store`? (Recommend the former.)
- Q2: is `bn export` dependency-edge fidelity worth full bd→bn round-trip in v1,
  or is `import` create-only-seed enough?
- Q3: memory search — FTS default + `ILIKE` fallback acceptable, or is exact bd
  parity required?
- Q4: should `bn` and the orchestrator share one Postgres instance (separate
  schema) or default to separate DBs?
- Q5: do we want a `bn`-owned migrator, or fold into the existing
  `internal/persistence` migrator pattern?
