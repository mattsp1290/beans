# Review: native Postgres tracker adapter plan

Reviewer: Claude (Opus), design-plan review
Target: `.agents/plans/postgres-tracker/plan.md`
Verdict: **Approve-with-changes as a comparison/decision artifact; Needs-rework
before implementation.** The plan is honest, self-aware, and correctly
recommends deferring behind idp #6. As a *decision* doc it stands. As an
*implementation* plan it has two must-resolve gaps (conformance reuse §5.1,
ingestion state write-back §4.4) and several under-specified parity/error items
that the "~a day for the reader/closer" estimate hides.

Every claim below is grounded in the repo files read for this review.

---

## 1. Verdict rationale (answering the focus questions)

### Focus 1+4 — "ready"/dependency parity in SQL

Partly achievable, but the plan's "match bd exactly" framing both over- and
under-states the problem.

**Correction the plan should adopt (good news):** ordering is NOT a parity risk.
The orchestrator re-sorts every candidate set itself —
`internal/orchestrator/poll.go:533` calls `sortCandidatesInPlace(issues)` right
after `FetchCandidates` (poll.go:522), applying the spec §9.1 "priority asc,
null last, CreatedAt tiebreak" order (poll.go:702-718, exercised by
`poll_test.go:126+`). beads' `bd ready` ordering is therefore *also* discarded
today. So the SQL query does **not** need an `ORDER BY` to match bd — parity is
about the candidate **set**, not its order. Open question Q4's "priority
ordering" sub-concern is a non-issue; the plan should say so and drop it.

**Real parity gaps the plan hand-waves:**

- **`<> 'closed'` is too narrow for the blocker-satisfied predicate.** The §4.2
  query treats any blocker whose `state <> 'closed'` as still-blocking. But the
  terminal set is plural: the conformance suite's FetchByStates test
  (`conformance_test.go:146-200`) uses both `closed` AND `done` as terminal
  states, and `WorkspaceConfig.TerminalStates` (`workflow.go:82`) is an
  operator-configured *list*. If bd treats a `done` blocker as unblocking, then
  `<> 'closed'` keeps a satisfied dependency live and the pg adapter dispatches a
  strictly smaller set than bd. The predicate must compare against bd's full
  terminal/closed set, not a single literal — and that set is config-driven, so
  the query needs the terminal states passed in, not hardcoded.

- **Does `bd ready` include `in_progress`, or only `open`?** Conformance test #1
  (`conformance_test.go:30-57`) feeds `bd ready` a fixture containing an
  `in_progress` row and asserts it survives the `activeStates` filter — i.e. the
  *test* assumes `bd ready` can return in_progress. The plan's
  `WHERE state = ANY($activeStates)` will unconditionally return unblocked
  in_progress issues. If real `bd ready` only emits `open`, the two backends
  dispatch different sets. This is unverifiable from the repo (bd is an external
  binary); the plan must call it out as requiring a *behavioral* comparison
  against a live `bd ready`, which §5.2 only gestures at.

- **`bd ready` may apply filters the SQL won't.** `FetchCandidates`
  (`beads.go:209-226`) shells `bd ready` then runs `convertAndFilter`
  client-side. bd's `ready` is its own opinionated query (it may exclude certain
  issue types, deferred/blocked states, or apply its own dep semantics). The
  plan's Q4 lists this as open; it should be *resolved* (run `bd ready` against a
  seeded fixture and diff the ID set) before the SQL is treated as equivalent,
  not deferred to "verify in a test."

### Focus 2 — the ingestion crux (§4.4): yes, this guts v1 value as written

The plan is candid that there is no `Create` in the interface (confirmed:
`tracker.go:18-85` is read + mutate-existing only; `beads.go` and the runtime
both create issues out-of-band). But its recommended v1 path — (a) bd-export →
import — has two consequences the plan does not surface, and they are the heart
of the task's "is this a contradiction that guts v1" question:

1. **It guts the headline "no Dolt at all" (§0).** If issues are authored in bd
   and exported into Postgres, you are *still running bd + embedded Dolt* to
   author and to produce `issues.jsonl`. You have moved the orchestrator's
   **read** path onto a Postgres mirror, not eliminated Dolt. The comparison
   table row "Postgres tracker … infra added: none … bd kept? no" is then
   misleading for the recommended v1: bd is still present as the authoring tool;
   only the *runtime read/close* path is Postgres. The honest table entry is
   "bd kept for authoring; Postgres for runtime read/close" — which is a much
   smaller win than "drops bd."

2. **State write-back hazard — the concrete correctness bug.** Option (a) is
   one-way bd→pg, but the orchestrator writes `Close` *into Postgres*
   (§4.2, the v1 hot path). A periodic re-sync from bd would overwrite that close
   with bd's stale `open`, **re-opening completed issues**, unless either (i)
   closes are written back to bd, or (ii) the loader does a state-merge that
   never regresses a pg-terminal issue. The plan's §4.4(a) ("one-way or periodic
   sync") does not say which side owns state after dispatch starts. This is the
   specific reason (a) is incoherent as a standalone v1: an import-only loader
   plus an in-place `Close` is a lost-update waiting to happen on the second
   sync.

   The genuinely coherent shapes are: **bd-as-source-of-truth + pg-as-read-cache
   with closes written back to bd** (but then why not keep beads as the tracker
   and get m2xx's concurrency instead?), OR **pg-as-source-of-truth with a native
   authoring path (b)** — which the plan scopes out. So the task's framing is
   correct: the Postgres tracker only delivers its stated value *with* (b), and
   (b) is exactly what's deferred. The plan should state this directly rather
   than recommending (a) as "the honest minimal scope."

### Focus 3 — does it deliver the multi-process win?

The comparison table is *mostly* honest but slightly over-claims. The plan
correctly carries the m2xx non-goal forward: a shared tracker DB removes the
storage **lock**, not the dispatch **race** (dispatch dedup lives in the
in-process `orchestrator.Manager` + `run_attempts` `UNIQUE(issue_id, attempt)`
with a *non-unique* unfinished index — confirmed against the m2xx plan §3 and
the task's grounded facts). Good.

What it buys vs alternatives, stated precisely:

- **vs status quo (embedded Dolt):** removes `runMu`, the embedded exclusive
  lock, the `CallTimeout` head-of-line-blocking, and the cross-uid bind-mount
  problem — *for the runtime tracker only*. Real, but see the §4.4 caveat: if
  authoring stays on bd/Dolt, embedded Dolt has not actually left the
  deployment.
- **vs m2xx:** m2xx keeps the *entire* bd feature set (deps, memories, jsonl,
  `bd dolt push`) at the cost of operating a Dolt server. Postgres-tracker reuses
  an instance you already run, at the cost of dropping bd-as-runtime-tracker and
  re-implementing ready/deps in SQL. The table captures this.
- **The "multi-client, native, no infra" cell is the over-claim** for the
  recommended v1, per §4.4 above — "no infra added" is only true if you also
  drop bd authoring, which (a) does not.

### Focus 5 — schema/DB coupling (§4.5)

The plan's "share the instance, separate schema/migration namespace"
recommendation is the right default and the §6 honesty about availability
coupling (one outage, both down) is correct. One thing to add: **lock/contention
is a real, not theoretical, concern if the tracker shares the persistence pool.**
The persistence layer holds the orchestrator's hot write path (run_attempts,
events). A tracker that borrows the same `*persistence.Pool` competes for
pooled connections on every poll tick (`FetchCandidates`) and reconcile pass
(`FetchByStates`, `FetchStatesByIDs`). Recommend: **separate schema AND a
separate pgx pool** (own pool, same instance) so a tracker query storm cannot
starve audit writes of connections. The plan currently leaves "reuse the pool or
take its own" (§3 last bullet, §4.5) undecided — decide for a separate pool.

### Focus 6 — scope honesty / effort

The "~a day for reader/closer, ingestion is the bulk" estimate is **too
optimistic**, primarily because of the conformance claim below (§2). The plan IS
honest that this is a strategic fork (dropping bd-as-runtime-tracker), not a
drop-in — §0, §6, §7 all say so clearly. That framing is its strongest feature.
But "reader/closer ~a day" omits: the net-new pgx→Category error mapping
(§3 below), the rows-affected NotFound handling (§3), the schema+migration
namespace, and the harness *extraction* (not parameterization). Call the
adapter+harness work a week, not a day.

---

## 2. Strongest correction: the conformance-reuse claim (§5.1) is wrong

§5.1 says "parameterize `test/conformance/tracker/` off the beads-specific
constructor and run the **same interface contract suite** against the pg
adapter." Having read the suite, this is not a constructor swap — it is a
**refactor**, because the suite is not contract-clean. The contract assertions
are entangled with bd-CLI mechanics inside the *same* test functions:

- The harness itself is a **`/bin/sh` fake-bd stub** (`helpers_test.go:128-202`,
  `installFakeBD`) that scripts argv→stdout responses. There is no backend seam;
  `newAdapter` (`helpers_test.go:242-250`) hardcodes `beads.New(beads.Config{...})`.
- Test #1 asserts `matchAll: ["ready", "--db=/state", "--actor=symphony-conformance"]`
  — bd argv plumbing, meaningless for pg.
- Test #3 asserts the `bd list --status=<state> --all` fan-out shape.
- Test #6 asserts **"4 unique subprocess calls"** (`conformance_test.go:394`) —
  a bd-CLI fan-out artifact; a pg adapter does one query.
- Test #7 maps **bd stderr strings** (`"no such issue"`, `"permission denied"`,
  `"invalid status"`) to categories — entirely bd-specific.
- Test #8/#9 assert `--reason=` / `--append-notes=` argv presence/absence.

Genuinely backend-agnostic *contract* assertions are interleaved in those same
functions: filter-by-activeStates (#1), `BlockedBy` dedup/sort/self-ref-drop
(#4), label normalization (#5), FetchStatesByIDs absence handling (#6), Close
idempotency / NotFound (#7/#8). To run these against pg you must **extract** the
contract-level assertions into a backend-agnostic suite parameterized over a
`tracker.Tracker` factory, leaving the argv/subprocess-count/stderr assertions in
a beads-only file. That is real work and the plan's "parameterize the
constructor" wording badly understates it.

Contrast with m2xx (`.agents/plans/dolt-server-backend/plan.md` §6.3): m2xx is
still **bd-over-a-server**, so its argv-level conformance tests stay meaningful
and a constructor/DataDir swap is closer to sufficient. This plan inherits a
harder refactor *because* it abandons the bd CLI. The plan should say so and
re-estimate accordingly.

---

## 3. Under-specified items the plan treats as footnotes but are real work

- **NotFound-on-Close is a contract the §4.2 design silently breaks.**
  Conformance test #7 ("NotFound on Close", `conformance_test.go:423-439`) pins
  `Close(ghost)` → `CategoryNotFound`. The plan's
  `UPDATE tracker_issues SET state='closed' WHERE id=$1` affects **0 rows** for a
  missing id and, under the plan's "idempotent no-op" framing, would return
  `nil` — silently succeeding on a ghost id. The adapter must check
  rows-affected and distinguish "0 rows because already closed" (idempotent
  success) from "0 rows because id absent" (NotFound) — which needs a prior
  existence check or `RETURNING`. Not mentioned; it changes the Close
  implementation.

- **pgx→`tracker.Category` mapping is net-new and unspecified.** Test #7 pins the
  full category contract (NotFound / AuthFailed / Validation / Timeout /
  UnknownPayload / APIRequest / Unsupported). beads has a whole `errors.go` doing
  this for bd stderr; the pg adapter needs its own mapping (connection refused →
  CategoryAPIRequest; ctx deadline → CategoryTimeout; unique/constraint →
  Validation?; etc.). m2xx devotes a whole section to this (§4.5); this plan has
  no equivalent. It should — it is adapter work, not a footnote.

- **State-string parity for FetchByStates / reconcile (focus 4).** Reconcile
  drives workspace cleanup off `FetchByStates(terminalStates)`
  (`tracker.go:23-26`). `core.IssueState` is a free typed string passed through
  verbatim (`issue.go:36-48`, `parse.go` `toCore` does `core.IssueState(b.Status)`
  with no normalization). So the **exact** state strings the orchestrator is
  configured with (via `WorkspaceConfig.TerminalStates` / `activeStates`) must be
  the exact strings stored in `tracker_issues.state`. The ingestion loader (4.4a)
  must preserve bd's state vocabulary byte-for-byte; a mapping/normalization step
  there would silently break reconcile's terminal-state match. Add this as a
  loader invariant.

- **Audit-trail / notes parity (focus 7).** beads routes `Close` reason →
  `bd close --reason`, `Comment` → `bd update --append-notes` (beads.go:350-431),
  surfaced in `bd show`. The plan's optional `tracker_issue_notes` covers this,
  but it is marked "optional" — for `Close` it should be **required** if you want
  the agent's "why" preserved (the §4.2 design already appends a note row, so
  reconcile the "optional" wording with the design). `Transition` semantics are
  fine as a plain UPDATE. `LinkPR` → keep beads' posture (`CategoryUnsupported` +
  `ErrUnsupported`, `beads.go:438-444`); the plan says "either a column or
  unsupported" — pick **unsupported** for v1 to match the conformance pin
  (test #7 LinkPR subtest) and avoid a half-built PR-linkage feature.

- **`RateLimit()` zero-snapshot contract (focus 7).** Correctly handled — return
  zero `RateLimitSnapshot` like beads (`beads.go:346-348`); `IsUnknown()` is
  tolerated by the observability pass. No change needed; the plan has it right.

- **Factory generalization (§4.5).** Confirmed the seam: `newTracker` is typed
  `func(cfg beadsadapter.Config) tracker.Tracker` (`runtime.go:357`, default at
  :414-418) and the sole call site builds `beadsadapter.Config{DataDir: ...}`
  (`runtime.go:620-622`). Generalizing this means **changing the factory's
  signature** (it currently takes a beads-specific Config), not just adding a
  branch — the signature would become something like
  `func(cfg core.TrackerConfig) (tracker.Tracker, error)` so it can dispatch on
  `Kind` and return an error for unknown kinds. The plan should name the
  signature change explicitly; "generalize to dispatch on Kind" understates that
  the seam's *type* changes and every test that injects `newTracker` is touched.
  Also: `TrackerConfig` (`workflow.go:48-61`) currently has only `Kind`,
  `APIKey`, `DataDir` — it needs new fields for the pg DSN/pool selection
  (mirroring m2xx's `server_*` additions), with the same `LogValue` redaction
  discipline if a DSN carries a password.

---

## 4. Prioritized concrete change list

**Must-resolve before any implementation (block "pursue now"):**

1. **Fix the ingestion story (§4.4).** State explicitly who owns issue state
   after dispatch. Option (a) one-way bd→pg + in-place `Close` re-opens closed
   issues on the next sync — either (i) write closes back to bd (then justify
   why not just use beads+m2xx), or (ii) make the loader a non-regressing merge
   that never resurrects a pg-terminal issue, or (iii) accept that v1 needs the
   native authoring path (b) and stop calling (a) "sufficient." Resolve Q1
   here — this is the contradiction that otherwise guts v1 value.

2. **Rewrite §5.1.** Replace "parameterize the constructor" with "extract the
   backend-agnostic contract assertions from the bd-CLI mechanics into a shared
   suite parameterized over a `tracker.Tracker` factory; the argv /
   subprocess-count / stderr-pattern tests stay beads-only." Re-estimate effort
   (adapter+harness ≈ a week, not a day).

3. **Fix the blocker predicate (§4.2).** Parameterize the "satisfied blocker"
   set off the configured terminal states; do not hardcode `<> 'closed'`. A
   `done` blocker must unblock if bd treats it that way.

**Should-resolve before implementation:**

4. **Add a pgx→`tracker.Category` error-mapping section** mirroring m2xx §4.5,
   including the rows-affected NotFound handling on `Close` (test #7 pins it).

5. **Resolve the `bd ready` semantics question (Q4) empirically** — diff
   `bd ready`'s ID set against the proposed SQL on a seeded fixture *before*
   declaring equivalence, covering: which states `ready` emits (open only vs
   +in_progress), and any issue-type/deferred/blocked filters bd applies.

6. **Make the loader's state-vocabulary preservation an explicit invariant**
   (no normalization of `state` strings) so reconcile's terminal-state match
   survives.

7. **Decide the pool question:** separate schema **and** a separate pgx pool on
   the shared instance (don't borrow the persistence pool — connection-starvation
   risk against audit writes).

8. **Name the `newTracker` signature change** (`beadsadapter.Config` →
   `core.TrackerConfig`-driven, returns an error) and the `TrackerConfig` field
   additions (DSN/pool selector, with `LogValue` redaction).

**Corrections to fold in (cheap, improve honesty):**

9. **Drop the priority-ordering parity worry** (Q4 sub-item): the orchestrator
   re-sorts at `poll.go:533`, so the SQL needs no `ORDER BY` to match bd.

10. **Fix the comparison table** for the recommended v1: "infra added: none" and
    "bd kept? no" are only true with native authoring (b); under (a) bd+Dolt
    stay for authoring and only the runtime read/close path moves to Postgres.

11. **Make `Close` notes / `tracker_issue_notes` required (not optional)** for
    the reason audit, and **pin `LinkPR` as `CategoryUnsupported`** for v1.

---

## 5. Bottom line

The plan is a *good decision document*: it is honest about the fork, carries the
m2xx dispatch-race non-goal forward correctly, and recommends deferring behind
idp #6 — which is the right call. Approve it in that role.

It is **not yet an implementation plan.** The conformance claim (§5.1) and the
ingestion state-ownership gap (§4.4) are the two that must be resolved before
anyone writes code; on current wording both the "~a day" estimate and the
"reuse the conformance suite" promise are unsound, and the import-only ingestion
path has a lost-update bug against the in-place `Close`. Resolve 1–3, specify
4–8, fold in 9–11, and this becomes a buildable plan — at which point the
strategic question (Q5: does the project actually want to drop bd as the runtime
tracker, given CLAUDE.md's bd-centric workflow?) is the real gate, not the
engineering.
