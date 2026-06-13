# Plan: native Postgres tracker adapter — REVISED post-review (decision document)

Status: REVISED after two independent Opus reviews (`review-opus1.md`,
`review-opus2.md`). Both: **Reconsider / defer — a *harder* defer than m2xx.**
This revision adopts that and corrects the circular value claim + a real
correctness bug.
Author: Claude (with Matt)
Relationship: third option vs embedded-Dolt (status quo) and the Dolt SQL-server
backend (`.agents/plans/dolt-server-backend/plan.md`, `beans-m2xx`).

## 0. Bottom line (the decision this document supports)

**Recommendation: do not build this for this project now. It is dominated by
m2xx and, as specified, partly circular.** A native Postgres `tracker.Tracker`
adapter is technically clean, but:

- The interface has **no `Create`** (it is read + mutate-existing), so a Postgres
  tracker needs an **issue-ingestion** story. The only low-cost one (import from
  bd) **keeps bd + embedded Dolt running to author issues** — so it does NOT
  "delete Dolt," it adds a Postgres schema **and** a sync pipeline on top. That v1
  is **dominated by m2xx** (one store, removes the lock, keeps the whole bd
  ecosystem).
- The non-circular version — Postgres as the **sole** issue authority — means
  **fully replacing bd** with a native authoring + dependency-management surface.
  That is **a tracker product, not "an adapter,"** and it fights the project's
  `CLAUDE.md`-mandated bd-centric workflow (bd for all tracking, `bd remember`
  memories, `bd dolt push`).
- It does **not** fix idp #6, and it does **not** make multiple orchestrators
  dispatch-safe (same `Manager` + `run_attempts` coordination gap as m2xx).

So: **m2xx is the lower-risk concurrency answer that preserves the ecosystem.**
The Postgres tracker only earns its place in a **concrete no-bd deployment**
(e.g. issues fed from Jira/GitHub) that does **not** exist in this project today.
Revisit only if such a deployment is real.

## 1. The fork, honestly (corrected comparison)

`tracker.Tracker` is an interface; `beads` (bd→Dolt) is the only impl; the
orchestrator already runs Postgres for its own state (`internal/persistence`).

| Option | Concurrency | Infra added | bd kept? | No-Dolt? |
|---|---|---|---|---|
| Embedded Dolt (status quo) | single-proc (`runMu`) | none | yes | no |
| Dolt SQL server (m2xx) | multi-client | a Dolt (MySQL-wire) server | yes (+ all features) | no |
| **PG tracker — v1 import-from-bd** | multi-client | **Postgres schema + a bd→pg sync** | **yes (still authoring in bd)** | **NO — bd+Dolt remain** |
| **PG tracker — full bd replacement** | multi-client | Postgres schema + **a whole authoring/dep surface** | **no** | yes |

The original draft's "infra: none / bd: no / no Dolt: yes" was true **only** for
the full-replacement column — not the recommended v1. Corrected per review.

## 2. Two findings that block "pursue now" (must-resolve)

### 2.1 Ingestion + state ownership = a lost-update / re-dispatch bug
The draft recommended **(a) one-way `bd export → loader → tracker_issues`** for
v1, while `Close` does an **in-place `UPDATE tracker_issues SET state='closed'`**
(§4.2). These conflict: the **orchestrator writes state into Postgres**, but the
**next sync upserts from `bd export`** which still shows the issue **open** →
the loader **resurrects a closed issue** → the orchestrator **re-dispatches
completed work**. Two writers, one `state` field, no ownership rule.

Only three coherent resolutions:
- (i) **Postgres is the sole authority** (full bd drop, native authoring) — the
  non-circular version; large scope ("a tracker, not an adapter").
- (ii) **`Close` writes back to bd**, Postgres is a read-only mirror — at which
  point you are running bd anyway, so **just use beads + m2xx**.
- (iii) A **non-regressing merge** loader that never resurrects a pg-terminal
  issue — adds sync complexity and still runs bd.
The plan must pick (i) (and accept it is a product) or concede the value is
small (ii). **(a) as written is broken** and must not be implemented.

### 2.2 The conformance suite is NOT reusable by a constructor swap
The draft's §5.1 ("parameterize the constructor") is wrong. The tracker
conformance suite (`test/conformance/tracker/`) is a **`/bin/sh` fake-bd stub**
with **bd-argv assertions, subprocess-call-count checks** (test #6 asserts "4
unique calls"), and **bd-stderr→`tracker.Category` mappings** entangled in the
**same** test functions as the genuine interface-contract assertions. Reusing it
requires an **extraction refactor** — pull the backend-agnostic contract
assertions into a shared suite parameterized over a `tracker.Tracker` factory;
the argv/subprocess/stderr tests stay beads-only. This makes the realistic effort
**≈ a week (adapter + harness extraction + ingestion), not "~a day."**

## 3. Engineering gaps to specify (should-resolve, if ever pursued)

- **Blocker predicate must use the configured terminal set**, not hardcoded
  `<> 'closed'`. `WorkspaceConfig.TerminalStates` is config-driven and includes
  e.g. `done`; a `done` blocker must unblock if bd treats it terminal.
- **`bd ready` semantics parity — verify empirically** before claiming
  equivalence: diff `bd ready`'s ID set against the proposed SQL on a seeded
  fixture (which states `ready` emits — open only vs `+in_progress`; any
  issue-type / deferred / blocked filters bd applies).
- **`Close` on a missing id** affects 0 rows and would **silently succeed** — but
  conformance test #7 pins `NotFound`. Add rows-affected → NotFound handling.
- **pgx → `tracker.Category` error mapping** is net-new and unspecified (beads has
  a whole `errors.go`; m2xx's plan has a §; this had none). Add it.
- **`newTracker` factory signature changes**, not just a branch: it is typed to
  `beadsadapter.Config` (`runtime.go:357`) and must become
  `func(core.TrackerConfig) (tracker.Tracker, error)` dispatching on `Kind` and
  erroring on unknown kinds — touching every test that injects `newTracker`.
  `TrackerConfig` (`workflow.go:48-61`, today `Kind/APIKey/DataDir`) gains a
  DSN/pool selector with `LogValue` **redaction** if a DSN carries a password.
- **Pool:** a **separate schema AND a separate pgx pool** on the shared instance
  — do not borrow the persistence pool (connection starvation against audit
  writes). Or a dedicated DB (decouples availability).
- **Loader must preserve the state vocabulary verbatim** (no normalization) so
  reconcile's terminal-state match survives.
- **`Close` reason audit** (`tracker_issue_notes` or a column) is **required**,
  not optional (parity with bd `--reason`); **`LinkPR` = `CategoryUnsupported`**
  for v1 (match beads).

## 4. Corrections folded in (cheap, honesty)
- **Dropped the priority-ordering parity worry** — the orchestrator **re-sorts**
  every candidate set itself (`poll.go:533`), so the SQL needs no `ORDER BY` to
  match bd. Parity is about the candidate **set**, not order.
- **Corrected the §1 comparison table** (the v1 column keeps bd + Dolt).
- **Re-scoped the effort claim** (week+, not a day) and the "an adapter" framing
  (it is a chunk of an issue tracker once factory + conformance extraction +
  parity + error mapping + migrations + coexistence + ingestion are counted).

## 5. What the plan still gets right (kept)
- The **no-`Create`** framing (creation is out-of-band in both backends).
- The **dispatch-race non-goal** carried from m2xx (a shared tracker DB removes
  the storage lock, not the cross-process dispatch race in `Manager` +
  `run_attempts`).
- **Doesn't fix idp #6**; SPOF/coupling honesty; `RateLimit`→zero-snapshot and
  `Close`-idempotency parity with beads.

## 6. If pursued anyway — the minimal, reversible step
Only if a concrete no-bd deployment appears. Land **only the additive seam**:
- generalize `newTracker` to dispatch on `tracker.kind` (returns error),
- the **extracted, factory-parameterized conformance suite**,
- a skeletal pg adapter behind `kind: postgres`, default unchanged.
Do **NOT** ship the bd-import loader (§2.1) — it has the lost-update bug and it
is the part that makes the option circular. Decide the **state-authority model
first** (full bd replacement vs mirror); everything downstream depends on it.

## 7. Decision
**Defer (harder than m2xx).** For this bd-centric project, m2xx is the
lower-risk way to get concurrency while keeping the ecosystem; status quo is fine
for the single-orchestrator case. Pursue the Postgres tracker **only** as a
**full bd replacement** for a **real no-bd deployment** — and recognize that is a
tracker product, a much larger commitment than the other two options. None of the
three fixes idp #6, which remains the actual priority.

## Appendix: review reconciliation
Adopted: corrected the comparison table (v1 keeps bd+Dolt; not "no infra/no bd");
named the lost-update/re-dispatch bug in import+in-place-Close and required a
state-authority decision; rewrote the conformance claim as an extraction refactor
(week+, not a day); parameterized the blocker predicate off configured terminal
states; required empirical `bd ready` parity; added pgx→Category + Close-NotFound;
named the `newTracker` signature + `TrackerConfig` changes with LogValue
redaction; separate schema+pool; required Close-notes + `LinkPR` unsupported;
dropped the priority-ordering worry (orchestrator re-sorts at poll.go:533);
re-scoped effort; set the verdict to defer-harder-than-m2xx. No findings rejected
— both reviews were well-grounded and agreed. Full detail in `review-opus1.md`
(11-item list) and `review-opus2.md`.
