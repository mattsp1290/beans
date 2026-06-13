# 09 — Review reconciliation (binding corrections)

Two independent Opus reviews (`review-opus1.md` = Approve-with-changes;
`review-opus2.md` = Reconsider/defer). Both agreed the **architecture** (one
`store`, two thin surfaces) is sound and genuinely de-circularizes the
postgres-tracker; the disagreement is **altitude/timing**, not correctness.

**This file governs `00`–`08`.** Where it conflicts with a detail file, this wins.

## A. Verdict adopted
**Default: defer.** Build only if a concrete project that genuinely cannot/will
not run Dolt is named (`00 §0`). The unique value is *no-Dolt only*; the
concurrency win is uncollected (dispatch race unbuilt); m2xx keeps the ecosystem
and gives concurrency; "keep bd/Dolt for new projects" is zero-build. Do not let
"greenfield" self-manufacture the demand.

## B. Reframe: native surface, not bd-compat (highest-leverage change)
Drop the **bd-compat promise** (`01` is reframed by this): no consumer needs strict
bd parity — the orchestrator reads in-process via `store`→`core.Issue`; the skills
are rewritten anyway; only `bn import` touches bd's format, **best-effort**. Keep
`--silent` bare-id + id pass-through as **`bn`↔caller** contracts, not bd debt.
This removes the maintenance treadmill.

## C. Scope cut for v1
Ship: `store` + schema + migrator → Postgres `tracker.Tracker` adapter +
conformance **extraction** → minimal native CLI
(`init/create/dep/ready/list/show/update/close`, `--silent`/`--json`) →
**create-only** import. **Cut from v1:** memory (`05`) and `export`/round-trip
(`06`); skill edits (`07`) last and out-of-repo.

## D. Engineering corrections (apply before/at the relevant milestone)

1. **Scope honesty (critical):** the orchestrator's in-process Postgres adapter
   **is the deferred postgres-tracker adapter** — the single largest deliverable,
   not a fait accompli. `bn` de-circularizes via the *authoring* half; the
   *consumption* half is the big build. (`02` must say this; `08` sequencing step 2.)
2. **Aggregate effort (critical):** state a whole-stack estimate, not just the
   "week+" that covered store+adapter. CLI + import + conformance extraction sit on
   top. (`08`.)
3. **`newTracker` signature break (important):** `runtime.go:357` is typed to
   `beadsadapter.Config`; kind-dispatch (`beads` vs `postgres`) changes the
   signature to `func(core.TrackerConfig) (tracker.Tracker, error)` and touches
   every call site + injecting test. Name it in `02`, not only `08`.
4. **`import` FK two-pass (critical):** `bn_issue_deps.blocked_by_id` is a NOT NULL
   FK; streaming edge inserts fail on forward references. Loader must **insert all
   issues first, then all edges**, in one transaction. (`06`.)
5. **Dependency edge cases (important):** `ON DELETE CASCADE` silently unblocks an
   issue when a blocker is deleted (decide: cascade vs restrict); a `self-dep`
   CHECK; cross-prefix deps vs prefix-scoped `ready`; dangling/non-existent blocker
   ids. Specify these in `03`'s ready query.
6. **CLI lost-update hole (important):** `bn update --status=open` on an
   orchestrator-closed issue has **no** non-regression guard (import does). Decide
   the policy: forbid re-opening a terminal issue without `--force`, or document it
   as allowed. (`01`/`03`.)
7. **`--silent`/`--json` leaks (important):** the golden test must also assert
   **no color when piped** (`NO_COLOR`/non-TTY) and **errors go to stderr, never
   stdout** — not just "bare id." fang styles help/errors; ensure neither reaches
   the machine-output path. (`04`/`08`.)
8. **`bn init` idempotency + one registration path:** `01`/`07`/`08` imply both
   explicit `init` and auto-on-create. Pick: `init` is idempotent (register-if-
   absent) and `create` auto-registers the prefix — document one model. (`01`/`03`.)
9. **Migrator decision (resolve, don't defer Q5):** a `bn`-owned migrator under
   `internal/tracker/postgres` reusing the `internal/persistence/migrate.go`
   pattern; separate migration namespace from `0001_init.sql`. (`02`/`08`.)
10. **Two-writer transaction note:** import (create-only/merge-never-regress) +
    orchestrator `Close` both write `state`; wrap multi-row import in a txn and
    state the isolation expectation. (`06`.)

## E. Selection / skills (per review)
Replace the `${TRACKER:-bd}` env default with a **committed per-repo marker**
(e.g. a `.bn`/config file in the project) that selects `bn` vs `bd` — eliminating
the silent wrong-tracker default and the LLM-compliance risk of a prompt that
hardcodes `bd`. Provide a `bn`-analogue for the skills' `.beads`-dir init guard.
Skill edits are out-of-repo (dotfiles), sequenced last; `bn` stays usable without
them. (`07`.)

## F. Cleanups
- Fix `00`'s "drop-in/full replacement" overclaim → "skill-subset native surface,
  with explicit divergences." (Done in `00`.)
- Pick the store package name **`internal/tracker/postgres`** (shared by the
  adapter and `bn`); delete the alternative from `02`/`08`.
- `06` round-trip honesty: `bn→bn` is lossless via an explicit edge field; `bd→bn`
  is best-effort (bd may export counts, not edges) — gate the import milestone on
  capturing a real `bd export` line.

## G. What both reviews credited (keep)
The store-two-surfaces architecture; carrying the postgres-tracker findings
forward (create-only import never regresses terminal state; config-driven terminal
set; separate pgx pool); the conformance-extraction honesty; the `--silent`/fang
golden test (extended per D7). The synthesis is sound — the corrections are
altitude (defer), framing (native not bd-compat), scope (cut memory/export), and
the listed engineering specifics.
