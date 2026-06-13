# Review (opus2) — `bn`: a bd-compatible, Postgres-backed tracker + memory CLI

Second independent reviewer. I read all nine plan files (00–08) plus the grounding
sources: `internal/core/issue.go`, `internal/tracker/tracker.go`, the
`postgres-tracker` plan + both its reviews, and the `big-change`/`new-project`
`SKILL.md`s the plan proposes to rewire. My focus is architecture fit, product
coherence, scope realism, and whether the bn surface + the skill/orchestrator loop
actually hang together — not a line-by-line restatement of a first reviewer.

## Verdict: **Reconsider / defer** — with a clean, smaller "if pursued" scope

The synthesis is *internally* coherent and it does honestly fix the one defect the
postgres-tracker reviews flagged (circular ingestion): native authoring via `bn`
replaces "import from bd," so the orchestrator's in-process Postgres adapter reads
what `bn` wrote, one store, no bd in the loop. That part holds.

But the plan is exactly the thing both prior reviews named as the *only*
non-circular form of this idea and then told you to defer: **"a product, not an
adapter… a much larger commitment"** (postgres-tracker plan §2.1(i), §6, §7). `bn`
embraces building that product. The two prior reviews set a single precondition for
doing so — *a concrete no-bd deployment that does not exist in this project today*.
This plan satisfies the **letter** of that precondition (full native replacement)
but **self-manufactures** the demand: new projects use `bn` only because we built
`bn` and pointed the skills at it. There is no external forcing function in the repo.

So the defer/proceed pivot rests on one fact I cannot verify from the files: **is
there a concrete new-project-on-Postgres need right now, or is this speculative
infra?** Absent that, the evidence leans defer — a multi-week build (store +
adapter + conformance-suite extraction + CLI + import + memory + skill rewire) with
**zero current consumers** (beans stays on bd/Dolt by the greenfield
decision; the orchestrator reads in-process; no new project is named). That is the
same shape the two postgres-tracker reviews deferred, only larger.

If a concrete project *does* exist, proceed — but with the cut-down scope in §C, not
the full nine-file surface.

---

## A. The load-bearing finding: **bd-compat has no consumer**

The plan's spine is "drop-in compatible with bd" (00 Goals; 01 "bd-compat
contract"; 03 "ID scheme (bd-compat)"; 06 "bd-export-compatible"). Walk the actual
consumers of that compatibility and it evaporates:

1. **The orchestrator** reads `bn`'s data **in-process** via the shared `store`,
   returning `core.Issue` (02, 03 "Mapping to core.Issue"). It never sees `bn`'s
   CLI, its `--json`, its `status`-vs-`state` mapping, or its id text format. It
   needs `core.Issue` to be correct — **zero** bd-CLI-compat requirement. Confirmed
   against `internal/tracker/tracker.go`: the interface is `FetchCandidates/
   FetchByStates/FetchStatesByIDs/Close/...` over `core.Issue`; nothing bd-shaped.

2. **The skills are being rewritten anyway** (07). They will call `bn`, not `bd`.
   They need a surface that `bn` *and the rewritten skill* agree on — they do **not**
   need parity with `bd`. Any stable native surface satisfies them equally. The only
   thing the skill loop actually depends on is the `--silent` → bare-id contract and
   id-passthrough (`ID=$(… --silent)`; `dep add <child> <parent>`) — and that's a
   contract between `bn` and the *new* skill, not a debt to bd.

3. **`bn import` of a real `bd export` file** is the *only* consumer that needs true
   bd-compat — and 06 already concedes the format gate may be unwinnable: bd's
   export may carry only `dependency_count`, not dep **edges**, in which case "a real
   bd file imports issues without deps." So even the one consumer that wants
   bd-compat may not get a faithful round-trip.

**Conclusion:** "drop-in compatible with bd" is a **self-imposed constraint with
essentially no consumer.** It is precisely the treadmill focus-4 worries about: to
*stay* bd-compatible you must track bd's `ready` semantics, JSON shape, and id
format as bd evolves independently — for a benefit nobody collects. The durable
contract is **"the skill loop + the orchestrator agree,"** which is satisfied by any
clean native surface `bn` defines for itself.

**Recommendation:** drop the bd-compat *promise*. Keep the *useful* primitives that
happen to look like bd because they're sensible (`{prefix}-{hash}` ids, `--silent`
bare id, priority 0–4, the `dep add child parent` convention) — but frame them as
`bn`'s native contract, not "compatibility with bd." Make `bn import` a **best-effort
seed** of bd JSONL, explicitly *not* a parity guarantee or round-trip. This single
reframe deletes plan-04's "option-name drift" anxiety's cousin (compat drift), the
06 round-trip gate's stakes, and most of the 08 "parity harness" work.

This collapses focus questions 3 and 4 into one cut: **the right minimal set is the
native surface the skill loop needs; everything justified only by "bd also has it"
is cuttable.**

## B. The greenfield boundary is a real footgun (understated, not broken)

Two trackers now coexist: bd/Dolt for beans, `bn`/Postgres for new
projects (00 decision 4; 07). Agents and skills must pick correctly per repo. The
plan's mechanism is `TRACKER=${TRACKER:-bd}` parameterization "as a one-line,
reviewable change" (07). Grounding against the actual skills shows this is
**understated and unreliable**, not broken:

- The skill's deliverable is an **LLM-emitted bash script**. The prompt says the
  agent's "ONLY output is a bash shell script containing `bd create` and `bd dep
  add` commands" and gives **fully worked examples that hardcode `bd`** throughout
  (`big-change/SKILL.md` lines 296, 337, 362–418; `new-project` 291–352). Switching
  to `$TRACKER` means rewriting the prompt prose **and** every worked example **and**
  then trusting the model to emit `$TRACKER` while surrounding instruction text
  still says `bd`. That is not a one-line edit; it is a prompt rewrite with a model-
  compliance risk.
- The init guard is `if [ ! -d ".beads" ]; then bd init; fi` (big-change 362). The
  `.beads/` directory *is* the Dolt marker. There is no `bn` analogue specified, so
  the selection signal and the init guard are inconsistent.
- The default is `bd` (`${TRACKER:-bd}`), which **contradicts** "new projects use
  bn." Nothing in the plan names the actor that sets `TRACKER=bn` for a new-project
  run. An agent in a fresh Postgres repo that forgets the env var silently authors
  into… nothing (no `.beads`, `bd init` creates Dolt state in a repo meant for
  Postgres). The failure is silent and wrong-tracker, the classic footgun.

**Cleaner signal:** a per-repo marker the skill reads (a committed `.bn` /
`tracker: postgres` in a project config), so selection is a property of the repo,
not an ambient env var an agent must remember. Resolve the selection mechanism
*before* touching skills (it's the contract the skill rewrite implements).

## C. If pursued: the concrete cut-down scope

Useful under either verdict. Ship the minimum that makes the loop work; defer the
rest:

1. **`store` + schema + migrator** (testcontainers-tested). Keep.
2. **Postgres `tracker.Tracker` adapter** + the conformance-suite **extraction
   refactor**. Keep — this is the genuinely reusable core, and 08 is honest that it
   is "≈ a week, not a constructor swap" (carried correctly from postgres-tracker
   §2.2).
3. **Minimal native CLI**: `init/create/dep/ready/list/show/update/close` with the
   `--silent` bare-id golden test. Keep — this is the skill-loop surface.
4. **`import`**: create-only seed behind a flag. Keep, but **drop `export` and the
   round-trip/round-trip-fidelity promise** (06's format gate). Seed-only.
5. **Memory (`remember`/`memories`, 05)**: **cut from v1.** 05 itself says it is
   "independently shippable," "lowest-priority," and ships with an "accepted"
   search-parity divergence. The orchestrator never reads it (05 "Relationship to
   issues"). It is a whole FTS subsystem (tsvector + GIN + ILIKE fallback) justified
   only by "bd also has memory" — the §A treadmill in miniature. Defer cleanly.
6. **Skill integration**: last; and only after the selection mechanism (§B) is
   settled. Keep `bn` usable standalone so this stays optional.

Net: ~steps 1–4 minus export, memory deferred, skills last. That is a meaningfully
smaller, more defensible v1 than the nine-file surface.

## D. Architecture / coupling (sound, with one open seam)

- **store ⇄ adapter sharing is correct.** One `store`, two thin surfaces (CLI author,
  in-process adapter reader) is the right shape and is the concrete answer to the
  circular-ingestion finding. The in-process decision (no orchestrator-shells-out-to-
  `bn`) matches `tracker.Tracker` having no `Create` — authoring is genuinely
  out-of-band, and `bn` *is* that band. Coherent.
- **`bn` in the beans repo while beans doesn't use `bn`** is
  *acceptable* for v1 (02's rationale — they must co-evolve on one schema/`ready`
  definition; a split module forces a premature shared-library extraction with no
  second consumer). But it's a mild smell worth a one-line non-goal: beans
  *builds* a tool it does not *use*, and CI will compile/test a CLI the repo's own
  workflow never exercises. Fine, but name it.
- **Unresolved store location** (focus 7 / 08 Q1): 02 itself offers two names
  (`internal/tracker/postgres/store` vs `internal/track/store`) and 08 Q1 leaves it
  open. Pick `internal/tracker/postgres` (shared with the adapter) and delete the
  alternative from 02 — the ambiguity is the kind of thing that bites when two PRs
  land in different packages.

## E. What the plan already guards (credit, so the criticism lands)

To keep this review fair: the plan does handle several real hazards, and these are
*not* where the problems are —

- `--silent`/`--json` stdout must never get fang ANSI styling, pinned by a golden
  test (04, 08). Correct and load-bearing.
- Separate pgx pool, not the persistence pool, to avoid starving audit writes (02,
  carried from postgres-tracker review §3). Correct.
- `import` defaults to `create-only` and `merge` never regresses a terminal state,
  so import can't resurrect a closed issue (06, 08). This is the exact lost-update
  hazard that sank the postgres-tracker import plan, and it's handled.
- `ready`/blocked uses the **config-driven terminal set** (`WorkspaceConfig.
  TerminalStates`, includes `done`), not hardcoded `='closed'` (03). Correct, and
  matches the orchestrator's actual semantics.
- State vocabulary preserved verbatim on import (03, 06) so reconcile's terminal
  match survives. Correct.

These are handled; the verdict is **not** about correctness of the parts but about
whether the *whole* should be built now.

## F. Overclaims / inconsistencies (focus 7)

- **00 overstates compatibility.** It says "drop-in compatible" and "a full
  bd-surface replacement," while 01 enumerates real divergences (no `dolt push`, no
  `bd graph`, memory search ranks differently, `--db`→DSN). Honest framing:
  "compatible for the skill subset, with documented divergences." Given §A, the
  cleaner fix is to stop claiming bd-compat at all.
- **06 round-trip is asserted then undercut.** "`bn export` ≈ `bd export`" and "a
  `bd export` file imports into `bn`" are stated as goals, then the same file
  concedes bd may export only counts, not edges, breaking dep round-trip. Don't
  promise the round-trip; make it best-effort seed (per §A/§C).
- **08's sequencing is honest** about the conformance extraction being week-scale and
  about idp #6 being untouched — good. But it lists memory as step 5 "additive"
  rather than recommending the cut; §C says cut it.
- The non-goals (00) are well-drawn and carry the m2xx/postgres-tracker non-goals
  forward correctly (no cross-process dispatch guarantee; doesn't fix idp #6). No
  overclaim there.

## G. The honest comparison the plan should make but doesn't

Focus 1 asks for it directly. Against the two real alternatives:

- **"Just keep bd/Dolt for new projects too."** Zero build. New projects already get
  bd's full surface, memory, and the existing skills *unchanged*. The only thing
  `bn` buys over this is "Postgres instead of Dolt" and "native concurrency" — but
  the concurrency win is **not collected** (00 non-goals: multi-orchestrator dispatch
  is still the unbuilt `Manager`+`run_attempts` gap; single-orchestrator bd/Dolt is
  already fine). So for a single-orchestrator new project, `bn` reimplements ids,
  states, deps, ready-set, memory FTS, import, and a CLI to avoid Dolt — for a
  concurrency benefit nobody dispatches against yet.
- **m2xx (Dolt SQL server).** Keeps the entire bd ecosystem and gets multi-client
  concurrency with *no* new tracker product. The postgres-tracker reviews already
  judged m2xx the lower-risk concurrency answer.

The plan should state plainly: the *only* thing `bn`+Postgres uniquely enables is a
**no-Dolt** stack, and that value is real **only** when a project genuinely cannot
or will not run Dolt. Name that trigger; if it isn't present, "keep bd/Dolt" wins on
cost and m2xx wins on concurrency.

---

## Prioritized recommendations

1. **Default to defer** (consistent with both postgres-tracker reviews), unless a
   *concrete, named* new-project-on-Postgres need exists today. Make the plan state
   that trigger explicitly; don't let "greenfield" self-manufacture the demand.
2. **Drop the bd-compat promise** (§A). Define `bn`'s own native contract; keep the
   `--silent` bare-id + id-passthrough as a `bn`↔skill contract, not a bd debt. Make
   `import` a best-effort seed, not round-trip parity. This is the highest-leverage
   change and removes most of the maintenance treadmill.
3. **If built, ship the cut-down scope (§C):** store + adapter + extraction +
   minimal native CLI + create-only import. **Cut memory** and **cut export** from
   v1.
4. **Resolve tracker selection before touching skills (§B):** a committed per-repo
   marker, not `${TRACKER:-bd}` env reliance; fix the `.beads`-dir init guard
   analogue; eliminate the silent wrong-tracker default.
5. **Pick the store package name** (`internal/tracker/postgres`) and delete the
   alternative from 02 (§D).
6. **Fix the 00 overclaim** ("drop-in"/"full replacement" → "skill-subset surface,
   native, with divergences") and the 06 round-trip overclaim (§F).

The engineering inside the plan is competent and the risk-handling is genuinely
good (§E). The problem is altitude: this is the big-product branch the prior reviews
deferred, dressed as "an authoring CLI," justified by a greenfield boundary that
creates its own demand, carrying a bd-compat contract no consumer needs. Defer; or
if a real no-Dolt project exists, build the smaller native thing.
