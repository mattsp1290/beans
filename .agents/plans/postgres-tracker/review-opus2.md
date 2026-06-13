# Review (independent, second reviewer): native Postgres tracker adapter

Reviewer: Claude (Opus), second independent pass — architecture / strategic fit.
Plan reviewed: `.agents/plans/postgres-tracker/plan.md`
Comparators read in full: `dolt-server-backend/plan.md` (m2xx),
`jsonl-bridge/plan.md`, `internal/tracker/tracker.go`, `internal/core/issue.go`,
`internal/tracker/beads/doc.go`, `docs/tracker-adapters.md`, `CLAUDE.md`.

## Verdict: Reconsider / defer — a *harder* defer than m2xx

This is well-written, honest about most of its own costs, and grounded in the
real interface. But the strategic call is clearer than the plan lets itself
conclude: **the recommended v1 (option 4.4a, bd-import) is dominated by m2xx**,
and the only piece worth landing speculatively is the additive factory/conformance
seam — explicitly **not** the bd-import loader, which carries a latent
state-ownership defect (below). This is not "defer, same as m2xx." It is a
*stronger* defer: m2xx is the lower-risk concurrency answer **because it
preserves the ecosystem with one store**; this drops bd-as-runtime-tracker, adds
a sync boundary, splits the source of truth, and buys a smaller infra win. I am
not mirroring the m2xx reviews — I'm placing this option *below* the option they
deferred.

The verdict follows from the plan's **own** admissions (§1 "inherits no issue-
management tooling", §6 "loses the bd ecosystem", the §2 non-goals, Q1/Q5), not
from a decree against it.

---

## 1. The value proposition is circular as specified (lead finding)

The §0 framing table claims this option keeps **bd? "no"** and cross-uid/lock
pain **"gone (no shared FS, no Dolt)"**, infra added **"none"**. That row is
**false for the system as v1 actually ships it.**

The plan's own recommended v1 ingestion (§4.4a, restated in §7 and the §5.4 exit
criterion) is `bd export → loader → tracker_issues`, and §4.4a itself says this
"keeps bd as the authoring tool, Postgres as the orchestrator's read replica."
So in the shipped v1:

- **bd is still present** (authoring, deps, `bd dep`, `bd remember`, the
  `CLAUDE.md` workflow). The "no" in the table is true only for the runtime
  *read path*, not the system.
- **Dolt is still present** — bd's only backend is embedded Dolt (confirmed in
  the jsonl-bridge plan §1: "Dolt is the only live backend"). `bd export` runs
  against Dolt. So "no Dolt at all" and "cross-uid/lock pain gone" are **not**
  achieved; they're merely moved off the orchestrator's read tick. The export
  step still touches the embedded-Dolt lock the whole project is trying to escape.
- **Infra added is not "none"** — it's "Postgres tracker schema **plus** a
  bd→Postgres sync pipeline," running *alongside* the bd/Dolt you still operate.

Net: option (a) **maximizes cost** (two stores + a sync job + the orchestrator's
direct `Close` writes) while **minimizing payoff** (bd and Dolt both remain).
Compare m2xx, which removes the embedded lock with **one** store and keeps the
**entire** bd feature set. As specified, the plan's "recommended for v1" is the
weakest form of the idea.

The Postgres tracker only stops being circular if you **fully drop bd** — native
authoring + dependency management + memory in Postgres (option 4.4b, and really
4.4c). The plan is honest that (b) is "larger scope" and Q1 flags the risk
directly: "(b) actually required (which changes the scope from 'an adapter' to 'a
tracker')." That is the real shape of the non-circular version, and it is a
**product, not an adapter.** The plan should state outright that the only
non-circular form of this idea is the big one, and v1-as-recommended doesn't
deliver the headline.

## 2. Latent architecture defect in option (a): state-ownership / re-import regression

This is a correctness flaw, not just strategy, and it blocks approval of 4.4a
**as written** (it does not block landing the seam — see §6).

- §4.2 wires the orchestrator's `Close` to a **direct** `UPDATE tracker_issues
  SET state='closed'`. So Postgres becomes a *writer* of issue state.
- §4.4a keeps **bd** as the authoring store and describes the loader as
  "one-way or periodic sync" that "upserts into `tracker_issues`/`_deps`."
- The plan **never specifies what a re-import does to an issue the orchestrator
  has already closed in Postgres.** bd doesn't know about that close (the close
  went to Postgres, not back to bd). The next `bd export` still shows the issue
  open; an upsert loader then **overwrites `tracker_issues.state` back to open**
  → the orchestrator **re-dispatches a closed issue.** State regression /
  resurrection.

This is the classic two-writers-one-field hazard of a mirror with a back-writing
consumer. The plan's §6 risk list covers "ready parity must match bd" and "loses
bd ecosystem," but **not** this bidirectional-ownership conflict. m2xx has **no
such hazard** because there is exactly one store — the orchestrator's `bd close`
mutates the same Dolt the operator reads. The mirror architecture *creates* a
conflict that the single-store architecture doesn't have.

To ship 4.4a safely you'd need a real reconciliation policy (loader never
downgrades a Postgres-terminal state; or the close path also writes back to bd —
which re-introduces the bd/Dolt write the design was trying to delete; or upsert
is insert-only-for-new-ids). Each of these is more design than "a day for the
reader/closer." This must be named and resolved before 4.4a is approvable.

## 3. Fit with the project's bd-centric identity — poor, by the plan's own non-goals

`CLAUDE.md` makes bd the canonical tracker **and** memory system: "Use `bd` for
ALL task tracking — do NOT use TodoWrite… Use `bd remember`… do NOT use
MEMORY.md," and the mandatory session-close workflow is literally `bd dolt push;
git push`. The global beads rules reinforce a whole `bd create/dep/ready/close`
discipline.

Against that backdrop:

- A Postgres runtime tracker that **mirrors** bd (4.4a) is **redundant** — you're
  running the canonical store *and* a read replica of it, with a sync seam and
  the §2 regression hazard between them.
- A Postgres runtime tracker that **replaces** bd (4.4b/c) **fights the entire
  documented workflow** — no `bd ready`, no `bd dep` graphs, no `bd remember`,
  no `bd dolt push`. That's not an adapter swap; it's a different operating model
  for the whole repo.

The plan tries to thread this with the §2 non-goal "Not a replacement for bd in
the DEV workflow… the orchestrator's runtime tracker is a *separate* use." That
separation is conceptually real, but in *this* repo the dev backlog and the
orchestrator's runtime issues are the **same beads project** (`beans-*`).
There is no second, non-bd issue source here. So the "separate use" the non-goal
leans on is hypothetical: **the only place a Postgres tracker earns its keep is a
deployment that has no bd at all** — e.g. a customer whose issues arrive from
Jira/GitHub/a queue (the plan's own 4.4b/c). That deployment **does not exist in
this project today**, which makes this, here and now, a solution looking for a
problem.

## 4. The worker-topology premise doesn't favor this over m2xx

The user's motivation (per the task framing) is "a shared DB on the Linux machine
queried by workers on the network." Two facts defuse it as a *discriminator*:

- The current worker model is **in-process goroutines, single orchestrator
  process** (inproc dispatcher). There are no network workers yet.
- The plan correctly concedes (§2 non-goal, §6) that a shared tracker DB
  **removes the storage lock, not the dispatch race** — dispatch dedup lives in
  the in-process `orchestrator.Manager` + `run_attempts`, not the tracker. m2xx
  makes the *identical* concession (m2xx §3 non-goal, verbatim parallel).

So **both** options expose a network DB, and **both** require the same unbuilt
cross-process reservation (advisory lock / partial-unique on in-flight
`run_attempts`) before multi-orchestrator dispatch is safe. "Natively
multi-process" is true of **storage**, not **dispatch**, for both. The Postgres
tracker's *only* real edge over m2xx is **"reuse the existing Postgres, no new
infra"** (no Dolt server sidecar). That edge is **real but small** — and it is
**not decisive** when set against (a) dropping bd in a bd-centric repo, (b)
adding a sync boundary with the §2 regression hazard, and (c) m2xx preserving the
full ecosystem. A small infra saving does not outweigh a source-of-truth split.

## 5. "An adapter" is under-claimed effort; the plan is partly honest about it

The plan is commendably honest in §6 ("ingestion is the real work… a
reader/closer is ~a day; a credible issue-lifecycle story is the bulk") and in
§3 (factory generalization, conformance parameterization). But the full v1
surface is larger than "an adapter," and the cost line should enumerate it:

1. Generalize the beads-hardcoded `newTracker` factory to dispatch on `Kind`
   (§3 / runtime.go:356-415) and validate unknown kinds.
2. Parameterize the conformance harness off the beads-specific `Config{DataDir}`
   (the m2xx plan flags the exact pin: `helpers_test.go:245`
   `Config{DataDir:"/state"}`).
3. **Exact `bd ready` parity in SQL** — open + all-blockers-closed, *plus* any
   priority ordering / label / type / deferred-state subtleties (the plan's own
   Q4 admits it doesn't yet know these). A semantics mismatch silently changes
   what the orchestrator dispatches. This is a hard gate the plan rightly calls
   out (§5.2, §6) but cannot yet cost.
4. Notes/audit parity (`Close` reason, `Comment`) — a `tracker_issue_notes`
   table and its semantics vs `bd --append-notes`.
5. Error-category mapping into `*tracker.Error` (the beads adapter has a whole
   stderr→Category table in `docs/tracker-adapters.md`; the pg adapter needs its
   own pgx-error→Category mapping, including the retryable/non-retryable split).
6. Schema migrations + the ownership decision (§4.1, Q3) — separate namespace vs
   the `internal/persistence` migrator.
7. Coexistence with beads as default (additive, but real wiring + config
   validation).
8. **The ingestion story** with the §2 reconciliation problem solved.

That's "re-build a meaningful slice of an issue tracker's runtime + a sync
pipeline," not "write an adapter." The §6 framing gestures at this; the **cost
table in §7 should make it explicit** so the fork is chosen with eyes open.

## 6. Scope / non-goals honesty — mostly good, two corrections

Honest and correct: the "no Create in the interface" framing (§1), the
dispatch-race non-goal (§2), "doesn't fix idp #6" (§2/§6/§7), the SPOF/availability
coupling (§6), and Q1's admission that (b) changes "adapter" into "tracker." These
are the right caveats and they're stated plainly. Credit where due.

Two things to correct:

- **Over-claim:** the §0 table's "bd? no / no Dolt at all / infra: none" row for
  the v1-recommended path. As shown in §1, with 4.4a bd and Dolt both remain and
  a sync pipeline is added. Fix the table to describe the *fully-dropped-bd*
  world (4.4b/c), and add a separate row for "v1 as recommended (4.4a mirror)"
  that tells the truth: bd kept, Dolt kept (for authoring/export), Postgres +
  sync added.
- **Under-claim:** §6's risk list omits the §2 state-ownership / re-import
  regression. Add it as a first-class risk; it's the sharpest correctness issue
  in the plan.

## 7. Sequencing

Same conclusion as m2xx and jsonl-bridge reached, and the plan already half-says
it (§7: "none of these fix idp #6… an architecture decision to make *after* #6"):
**this does not touch idp #6 and should not be on the active roadmap now.** It is
strictly an "only if a concrete non-bd deployment appears" item — *more* so than
m2xx, because m2xx at least preserves the ecosystem and is a drop-in concurrency
fix, whereas this is a source-of-truth change with a migration/ownership story.

---

## Prioritized recommendations

1. **Defer the option, harder than m2xx.** Record the call explicitly: for the
   concurrency problem in *this* (bd-centric) project, **m2xx is the lower-risk
   answer** — one store, full ecosystem, removes the lock. The Postgres tracker
   becomes the right call **only** in a world that doesn't exist here yet: a
   concrete deployment with **no bd**, issues fed from an external system. In
   that world it is "build a tracker" (4.4b/c), not "an adapter."

2. **If you want speculative insurance, land ONLY the additive seam** — generalize
   the `newTracker` factory to dispatch on `Kind` + parameterize the conformance
   harness — behind `tracker.kind: postgres`, default unchanged. This is the
   cheap, reversible, low-risk part and it's genuinely useful (it also unblocks
   m2xx's harness parameterization and any future non-bd adapter). **Do NOT land
   the bd-import loader (4.4a)** — that's the piece carrying the §2 regression
   hazard and the circular cost, and it has no consumer.

3. **Before 4.4a could ever be approved, resolve the state-ownership conflict**
   (§2): define a reconciliation policy where a periodic re-import can never
   downgrade a Postgres-terminal state (loader is non-downgrading / insert-only
   for new ids), or abandon the mirror model entirely in favor of native
   authoring. Add this as a §6 risk now regardless.

4. **Fix the §0 table** to stop claiming "no Dolt / no bd / no infra" for the
   v1-recommended mirror path; split it into "fully-drop-bd (the real but large
   version)" vs "v1 mirror (bd+Dolt kept, +Postgres +sync)."

5. **Expand the §7 cost line** into the eight-item surface in §5 above so "an
   adapter" stops undercounting the work (factory, conformance, ready-parity,
   notes, error mapping, migrations, coexistence, ingestion-with-reconciliation).

6. **Answer Q5 in the plan, not as an open question:** given `CLAUDE.md`'s
   bd-centric workflow, dropping bd-as-runtime-tracker is not a direction this
   project wants today. State it.
