# 05 — Open questions & reviewer reconciliation

## Open questions

1. **`/deps` and `/graph` edge semantics (the only real decision).**
   `ListDeps` now returns blocking *and* parent-child edges, and prod has epic
   data. Options:
   - **(Recommended) Blocking-only** — route both handlers through
     `ListBlockingDeps`; API response unchanged; epics/membership are a later
     feature. Smallest, behavior-preserving.
   - **Expose membership** — add `dep_type` to the `Dependency` DTO and render
     parent-child edges now. Larger; changes the API contract and UI.
   Resolve before step 3/4.

2. **Pin to prod's pseudo-version vs wait for a tagged release.** Recommended:
   pin the exact commit prod runs (`e52dce57b52c`) now, to get bean-counter ==
   prod and unblock the deploy. Re-pin to a tag (e.g. a future `v0.1.2`) when one
   is published. Acceptable to depend on a pseudo-version? (local-symphony
   already does.)

3. **Where does this land relative to the open deploy fix?** The deploy script
   fix lives on `fix/deploy-parity-stdin` (not yet on `main`). Should the beans
   bump be a separate branch/PR (recommended) or combined? Keeping them separate
   keeps each reviewable.

## Risk register

| # | Risk | Severity | Mitigation |
|---|------|----------|-----------|
| 1 | `/deps` & `/graph` silently start showing parent-child edges | Medium | Q1 → `ListBlockingDeps`; new test asserts exclusion |
| 2 | Pseudo-version pin churns `go.sum` / pulls unexpected deps | Low | require set verified identical to v0.1.1; `git diff go.sum` review |
| 3 | Embedded migrations run against a non-prod DB | Low | only fresh local/test DBs migrate; prod already at 8 (no-op) |
| 4 | Hidden source break from the bump | Low | model identical, go.mod identical, signatures additive; `go build ./...` is the gate |

## Grounding notes (verified against source)

- Target = beans commit `e52dce5` = `~/git/beans` HEAD = prod pin
  `v0.1.2-0.20260615002029-e52dce57b52c`. Only tags: `v0.1.0`, `v0.1.1`.
- `model` package byte-identical v0.1.1→target (`diff -rq`). `go.mod` `go`
  directive and `require` set unchanged.
- `store`: `DepEdge` +`DepType`; `AddDep` signature preserved (wraps
  `AddTypedDep`); new `ListBlockingDeps`/`ListMembers`/`ListParents`/
  `AddTypedDep`/`ValidateDepType`; `ListDeps` returns all kinds; `ReadyIssues`
  excludes `issue_type='epic'` and gates on `dep_type='blocks'`.
- bean-counter dep/graph/ready usage: `internal/handlers/deps/deps.go:37`
  (`ListDeps`), `:59` (`AddDep`); `internal/handlers/graph/graph.go:36`
  (`ListDeps`); `internal/handlers/ready/ready.go:30` (`ReadyIssues`);
  `internal/store/adapter.go:131-139`. DTO: `internal/api/dto/deps.go`.
- Migration `0008_bn_dep_type.sql` adds `dep_type TEXT NOT NULL DEFAULT
  'blocks'` + index; PK unchanged.

## Reviewer reconciliation log

Two independent Opus reviewers (A: Go correctness; B: testability/operability)
assessed this plan; both verified findings against source. All accepted and
folded into revision 2; none rejected. Stricter severity taken on disagreement.

| # | Finding | Reviewer(s) | Severity | Decision | Where applied |
|---|---------|-------------|----------|----------|---------------|
| 1 | `Adapter.ListBlockingDeps` wrapper is dead code — handlers get the raw `*beansstore.Store` via `adapter.Store()` (already has the method); real change is the handler interface rename, no `main.go` change | A, B | BLOCKER | Accept | `02` Step 3, `04` task 3 |
| 2 | Renaming the handler interface breaks unit-test `fakeStore`s (`deps_test.go:38`, `graph_test.go:33` implement `ListDeps`) — "tests pass unchanged" is false | A, B | MAJOR | Accept | `01` Test impact, `02` Step 4, `04` task 4 |
| 3 | Step 5 same-pair blocking+parent-child collides on PK `(issue_id,blocked_by_id)` → `ErrDuplicateDep`; use two distinct pairs | B | MAJOR | Accept | `02` Step 5.3 |
| 4 | Step 5 needs raw-store seam: `AddTypedDep`/`DepType*` not re-exported by `appstore`, and `sqliteHandlersApp` doesn't surface the store | A, B | MAJOR | Accept | `02` Step 5.1–5.2, `04` task 5 |
| 5 | Third behavioral change missed: `populateBlockedBy` now blocks-only → `blocked_by` excludes parent-child | A | MINOR | Accept | `00`, `01` (behavior 3 + table), `02` Step 5.4 |
| 6 | Integration tests are a local/manual gate — CI does not run `-tags=integration` | B | MINOR | Accept | `03` pre-merge gates |
| 7 | Deploy `--check` is post-merge (ref must reach `origin/main`), needs prod Docker access, blocked on the deploy-parity PR | B | MINOR | Accept | `03` step 4, `04` task 6b |
| 8 | ready-excludes-epics test is HTTP-only (`issue_type:"epic"` is valid) — no raw-store plumbing for that half | B | NIT | Accept | `02` Step 5.5 |

Confirmed accurate by both (no action): `model` byte-identical; `go.mod` `go`
directive + `require` set unchanged; `AddDep`/`New`/`Config`/`ListFilter`/
`CreateIssueInput`/`UpdateIssueInput`/`ReadyIssues` arity + error sentinels
unchanged; `RemoveDep` unchanged and safe; pseudo-version `@e52dce57b52c`
resolves to `v0.1.2-0.20260615002029-e52dce57b52c`; migration `0008` `Up()` is a
no-op at schema 8 (no schema-rollback concern).
