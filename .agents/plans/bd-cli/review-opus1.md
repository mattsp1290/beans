# Review — `bn` (bd-compatible Postgres tracker + memory CLI)

Reviewer: Opus (review-opus1)
Scope: all nine plan files in `.agents/plans/bd-cli/` (`00`–`08`), checked
against repo source (`internal/tracker`, `internal/core/issue.go`,
`internal/runtime`, `test/conformance/tracker`, `internal/persistence/migrate.go`)
and the prior `.agents/plans/postgres-tracker/` plan + reviews.

## Verdict: **Approve with changes**

This is a well-grounded, internally-mostly-consistent design doc. It correctly
inherits the hard-won findings from the postgres-tracker review instead of
re-making the same mistakes — the conformance "extraction refactor, not a
constructor swap" honesty (`08`) matches postgres-tracker §2.2 verbatim, the
import `create-only` default directly answers the lost-update/re-dispatch bug
(§2.1), the terminal-set-is-config-driven blocker predicate is carried through
(`03`), and the separate-schema/separate-pool discipline is preserved (`02`).
Credit where due: the plan did its grounding homework.

The changes below are refinements and one honesty correction on scope. None
require a rewrite, but the scope/effort gap (#1) and the `import` FK ordering
gap (#4) are load-bearing and must be closed before implementation starts.

---

## What the plan gets right (verified)

- **`core.Issue` mapping is faithful.** `03`'s field list matches
  `internal/core/issue.go:58` exactly (ID, Identifier, Title, Description,
  Priority, State, BranchName, URL, Labels[], BlockedBy[], CreatedAt, UpdatedAt).
  The `state`↔`status` JSON-boundary note (`03`) is real: `core.Issue` serializes
  `State` as `"state"` (issue.go:71), but bd's JSON uses `status` (confirmed in the
  conformance fixtures, `conformance_test.go:38`). The plan flags it; good.
- **Priority boundary is correctly framed.** `core.Priority` (issue.go:16) is an
  internal enum where the adapter must map 0–4 at the parse boundary; `03` says
  `Priority(int→core.Priority)`. Consistent.
- **terminal-set predicate.** `WorkspaceConfig.TerminalStates` exists
  (`internal/core/workflow.go:82`), defaults to `["closed","done"]`
  (`internal/config/defaults.go:66`). `03`/`08` correctly say "do not hardcode
  `='closed'`." Confirmed.
- **activeStates citation is accurate.** `poller.go:314` is
  `ActiveStates: []core.IssueState{"open","in_progress"}` — exactly as `03` cites.

---

## Prioritized change list

### 1. (Critical) State the real scope honestly — this plan *builds the deferred postgres-tracker adapter*, it doesn't merely "de-circularize."

The synthesis in `00`/`08` frames `bn` as the thing that "removes the circular
ingestion." That is **true for the authoring half only**: `bn create` / `bn dep
add` write issues natively into Postgres, so you no longer run bd+Dolt to author.
That half stands on its own and needs no orchestrator integration.

But the *consumption* half — "the orchestrator reads/closes them in-process via
the Postgres `tracker.Tracker` adapter" (`00`, `02`) — **is essentially the
entire postgres-tracker adapter that was explicitly deferred** ("a harder defer
than m2xx", "only for a real no-bd deployment", postgres-tracker plan §0/§7).
The greenfield decision is what *creates* that no-bd deployment, so building it is
now justified — but the plan should say plainly that the committed surface is:

> deferred-PG-tracker-adapter + conformance-extraction + new fang CLI +
> memory FTS + import/export + skill edits.

Right now `00`/`02` read as if the in-process adapter already exists. It does not.
Add one paragraph to `02` making the adapter a *deliverable of this plan*, not a
given. (The user's greenfield decision is settled — don't relitigate it — but the
scope it implies must be visible.)

### 2. (Critical) Effort is undercounted — there is no aggregate estimate anywhere.

`08` inherits the postgres-tracker "≈ a week, not a day" figure for the conformance
extraction. But that week was for **steps 1–2 only** (adapter + harness
extraction + ingestion). This plan *stacks* steps 3–6 (the fang CLI, import/export,
memory FTS, skill edits) on top and gives **no aggregate number**. State explicitly
that "week+" is a **floor for steps 1–2**, and that 3–6 are additional. An
implementer reading `08` today would badly under-budget.

### 3. (Important) The `newTracker` factory signature break is named in `08` but unspecified — and absent from `02`.

`08` step 2 says "factory/conformance-extraction (also postgres-tracker Phase-1)."
That is a real mention — but it understates a breaking change. The production
factory is **typed concretely**:

```
internal/runtime/runtime.go:357   newTracker func(cfg beadsadapter.Config) tracker.Tracker
internal/runtime/runtime.go:415   f.newTracker = func(cfg beadsadapter.Config) tracker.Tracker { return beadsadapter.New(cfg) }
internal/runtime/runtime.go:620   r.Tracker = fac.newTracker(beadsadapter.Config{...})
```

To dispatch on `tracker.kind`, this signature must change from
`func(beadsadapter.Config) tracker.Tracker` to a kind-dispatching factory
(postgres-tracker §3 proposed `func(core.TrackerConfig) (tracker.Tracker, error)`),
which **touches every call site and every test that injects `newTracker`**, and
`TrackerConfig` (`internal/core/workflow.go`, already has `LogValue` redaction at
workflow.go:66) gains a DSN/pool selector. `02-architecture.md` — which owns the
in-process integration story — does not mention this break at all. Add it to `02`
as a named signature change, not just a one-liner in the sequencing file.

### 4. (Critical) `import` against the `bn_issue_deps` FK is not specified as two-pass — it will fail on forward edge references.

`03` declares `bn_issue_deps.blocked_by_id TEXT NOT NULL REFERENCES bn_issues(id)`.
`06` says "reconstruct `bn_issue_deps` from each line's edges" and "stream
line-by-line." But a JSONL line for issue A that is `blocked_by` B, where B's line
comes *later* in the file, will **violate the FK** if edges are inserted as each
line streams. The loader must be **two-pass** (all issues first, then all edges) or
defer constraint checking within the import transaction. `06` does not say which.
Specify two-pass (or `SET CONSTRAINTS ... DEFERRED`), and note it interacts with
the "stream don't load whole file" goal — you can stream issues but must buffer
edges.

### 5. (Important) Dependency / ready-set edge cases under the `03` schema — several are unaddressed.

The `ready` semantics (open + all blockers terminal) are correct in principle, but
the schema raises concrete gaps the plan should answer:

- **`ON DELETE CASCADE` on `bn_issue_deps` silently unblocks.** Deleting a blocker
  row cascades the edge away, so the child becomes ready with no record of why.
  Is that bd-parity, or a quiet re-dispatch path? At minimum document it.
- **Cross-prefix deps are structurally legal but ready-scoping is per-prefix.**
  `bn_issue_deps` keys on `id` with no prefix constraint, yet `ready`/`list` scope
  by `prefix` (`02`). A child in prefix X blocked by a parent in prefix Y — does the
  ready query evaluate the cross-prefix blocker, or does prefix-scoping hide it and
  mark the child ready prematurely? Specify (recommend: blocker evaluation ignores
  prefix; only the *result set* is prefix-scoped).
- **Self-deps and cycles.** `01` says "reject cycles" and `dep cycles` exists, but
  the FK allows `issue_id = blocked_by_id`. Add an explicit self-dep CHECK or
  reject-at-insert (the conformance suite test #4 proves bd drops self-refs —
  `conformance_test.go:236`).
- **Missing/non-existent blocker id.** The FK prevents inserting an edge to a
  non-existent issue, which is stricter than bd (bd export/import may carry an edge
  to an id outside the file). Combined with #4, decide: reject the import, or skip
  the dangling edge with a warning in the `--json` summary.

### 6. (Important) The non-regression guard is import-only — the CLI has the same lost-update hole.

`06`/`08` correctly default `import` to `create-only`/`merge`-never-regress so a
file can't resurrect an orchestrator-closed issue. But a human running
`bn update <id> --status=open` (or `bn` reopening via `update`) on an issue the
orchestrator already `Close`d has **no guard** and would re-dispatch — the exact
lost-update class the import default defends against, reintroduced through the CLI.
Add one line stating whether reopen-via-`update` is an intentional human override
(probably yes) or needs the same terminal-state guard. As written it's an
unaddressed asymmetry.

### 7. (Important) `--silent`/`--json` vs fang styling — the contract is right, but pin two more leaks beyond the golden test.

`04`/`08` correctly make `create --silent` → bare id the load-bearing contract and
guard it with a golden test. Two gaps the single golden test won't catch:

- **Color in pipes.** fang/lipgloss must emit no ANSI when stdout is not a TTY.
  The golden test runs under `exec` (no TTY) so it may pass while an *interactive*
  `bn create --silent | cat` still leaks color if fang's color decision keys on
  something other than stdout's TTY-ness. Add an assertion / force
  `NO_COLOR`/`CLICOLOR=0` on the machine-output paths.
- **Errors-to-stdout.** `04` says errors go to stderr, but fang's styled-error
  rendering is the most likely place to accidentally write to stdout. The golden
  test asserts the *success* path; add a negative test that a failing
  `create --silent` writes **nothing** to stdout (so `ID=$(...)` captures empty,
  not a styled error).

### 8. (Minor) `bn init` idempotency is unspecified.

`01`/`07` say `bn init --prefix` registers a project; `03` has `bn_projects` with
`prefix PRIMARY KEY`. A second `bn init` on an existing prefix must be a 0-exit
no-op (like the migrator's idempotency, `migrate.go:133`), not a PK-violation
error — agents/skills may re-run it. State it. Also `07` mentions "auto on first
create" as an alternative — pick one; two registration paths is drift surface.

### 9. (Minor) Migrator decision (Q5) should be made, not deferred.

`08` Q5 leaves "own migrator vs fold into `internal/persistence`" open. The
existing migrator (`migrate.go`) is clean, advisory-locked, and embed-FS based —
but it's bound to the orchestrator's `*Pool` and its own `schema_migrations` table.
Since `02` mandates a **separate schema/pool** for `bn`, reusing the *pattern* (a
parallel migrator over `bn`'s pool + its own bookkeeping table) is right; sharing
the *instance* is not. Recommend: copy the pattern, separate bookkeeping. Decide it
in `02` rather than leaving it as an open question into implementation.

### 10. (Minor) Confirm the grounding gates are genuinely blocking.

`08` lists three grounding gates (fang option names; bd `--json`/export edge
shape; `ready` parity). `06` already flags the real risk: **bd export may carry
only `dependency_count`, not edges** — in which case `bn import` of a *real* bd file
imports issues with no deps, silently. This is the difference between "seed from
bd" working and quietly losing the dependency graph. Elevate gate #2 from "verify
before finalizing" to "blocks the import/export milestone," and make `bn export`'s
edge field (the documented `bn` extension) the canonical round-trip format so
`bn→bn` is always lossless even if `bd→bn` isn't.

---

## Answers to the focus questions

1. **`--silent`/`--json` vs fang.** Adequately guaranteed in principle; golden
   test is necessary but not sufficient — see #7 (pipe-color + error-to-stdout).
2. **`ready`/dep parity in SQL.** Core semantics correct (config-driven terminal
   set). Edge cases under-specified — see #5 (cascade-unblock, cross-prefix,
   self-dep, dangling edge).
3. **Import state-ownership.** `create-only` default is the correct fix for the
   postgres-tracker §2.1 bug and is well-specified. `replace` is guarded. The hole
   is the *CLI* path, not import — see #6. FK ordering is the missing mechanic —
   see #4.
4. **Does it de-circularize?** Yes — the authoring half (`bn create`/`dep add`)
   removes the bd-in-the-loop authoring dependency by itself; no remaining bd/Dolt
   dependency in the new-project loop. The synthesis is **sound**. But it
   de-circularizes *by committing to build the deferred consumption adapter* — see
   #1. That's the honest framing the plan should adopt.
5. **Conformance reuse honesty.** Correct and honest — `08` reproduces the
   postgres-tracker §2.2 finding accurately. Verified against the suite: the
   contract tests *are* entangled with bd-argv (`conformance_test.go:35`
   `--db=/state`), subprocess-call-count (`:394` "want 4"), and bd-stderr→Category
   mappings (test #7) inside the same functions as the genuine interface
   assertions. An extraction refactor (factory-parameterized suite, argv/subprocess
   tests stay beads-only) is the right call; "week+, not a day" stands. The only
   miss is not aggregating that with steps 3–6 — see #2.
6. **Scope/effort realism.** Sequencing order is sound (foundation → adapter →
   CLI → import → memory → skills, with memory/skills correctly marked
   independently shippable). Underestimated: aggregate effort (#2), `newTracker`
   signature break (#3), import FK ordering (#4). Missing: `bn init` idempotency
   (#8), transaction/isolation note for the two-writer import path beyond
   "idempotent re-runs."
7. **Architecture (shared store).** Co-locating `cmd/bn` in beans while
   beans stays on bd/Dolt is a defensible call — the store *must* evolve
   with the adapter (one schema, one `ready` definition), and a split module forces
   a premature shared-library extraction (`02` argues this and is right). The
   coupling risk is acceptable for v1; the "build-but-don't-dogfood" awkwardness is
   real but the greenfield decision owns it. No change needed; `02`'s rationale is
   sound.
8. **Cross-file consistency.** Generally tight. The one genuine inconsistency: `02`
   omits the `newTracker` signature break that `08` step 2 implies (#3), and `07`
   offers two project-registration paths (`init` vs auto-on-create) that `01`/`08`
   don't reconcile (#8). The store-package name is left open (`08` Q1) but `02`/`08`
   both recommend the same answer (`internal/tracker/postgres`), so that's fine to
   resolve in-place.

---

## Bottom line

Approve with changes. Land #1–#4 before coding (scope honesty, effort floor,
the `newTracker` break in `02`, and the import FK two-pass). #5–#7 are
should-fix before the relevant milestone. #8–#10 are tidy-ups. The plan's
greatest strength is that it carries the postgres-tracker review's findings
forward faithfully; its greatest weakness is presenting the deferred
consumption adapter as a fait accompli rather than the largest deliverable it
actually is.
