# 04 — Task sequence (beads-ready)

File as beads issues after the plan is approved. Labels: `impl`, `testing`,
`docs`, `deploy`. Priorities: P0 critical … P3 backlog.

## Core upgrade

1. **[P1, impl] Bump beans to `e52dce57b52c` and tidy.**
   `go get github.com/mattsp1290/beans@e52dce57b52c && go mod tidy`. Confirm
   `go.mod`/`go.sum` show only the beans change and `go build ./...` passes.
2. **[P1, testing] Baseline gates on the bump.** `go test ./...`, integration,
   vet/lint/fmt, frontend. Confirms API compatibility before behavior edits.
   Depends on (1).

## Preserve API behavior (recommended approach)

3. **[P1, impl] Route `/deps` and `/graph` through blocking-only.** Rename the
   local `Store` interface method **and** call site from `ListDeps` to
   `ListBlockingDeps` in `internal/handlers/deps/deps.go` and
   `internal/handlers/graph/graph.go`. Handlers receive the raw
   `*beansstore.Store` via `adapter.Store()` (which already has the method), so
   **no `Adapter` wrapper and no `main.go` change**. Remove the now-unused
   `Adapter.ListDeps` if it has no caller (grep first). Depends on (1).
4. **[P1, testing] Fix unit-test fakeStores.** Rename `fakeStore.ListDeps` →
   `ListBlockingDeps` in `internal/handlers/deps/deps_test.go` and
   `graph_test.go` (else they fail to compile). Same change as (3). Depends on (3).
5. **[P1, testing] New behavior tests + test plumbing.** (a) Re-export
   `DepTypeBlocks`/`DepTypeParentChild` (+ `AddTypedDep` passthrough) in
   `internal/store/adapter.go`; (b) surface the store from `sqliteHandlersApp`
   so a membership edge can be seeded; (c) deps/graph blocking-only test using
   **two distinct pairs** (avoid the `(issue_id,blocked_by_id)` PK collision);
   (d) `blocked_by` blocks-only assertion; (e) ready-excludes-epics test
   (HTTP-only, `issue_type:"epic"`). Depends on (3),(4).

## Verify

6. **[P1, testing] Full gate set.** Re-run unit + integration (local; CI does
   not run integration) + frontend. Depends on (4),(5).
6b. **[P2, deploy] Post-merge deploy `--check`.** After the bump lands on `main`
   **and** the deploy-parity PR (`fix/deploy-parity-stdin`) is merged, run
   `scripts/deploy-production.sh --ref main --check` (operator, needs prod Docker
   access); expect `embedded_max=8 db_max=8` + `CHECK OK`. Cross-branch dep on
   the deploy-parity PR. Depends on (6).
7. **[P2, docs] Note the version + behavior in README/AGENTS** if either
   documents the beans pin or the `/deps` semantics. Depends on (4).

## Follow-ups (backlog, not part of this bump)

8. **[P3, impl] Surface epics / parent-child membership** in the API + UI
   (`dep_type` in DTO, `ListMembers`/`ListParents` drill-down). The deliberate
   feature behind `0008`.
9. **[P3, deploy] Re-run first live deploy** of bean-counter on the bumped
   version once it lands on `main` (ties into deploy epic `bean-counter-mkg`,
   issue `bean-counter-m0p`).

## Critical path

(1)→(2) proves compatibility; (1)→(3)→(4)→(5)→(6) lands the behavior-preserving
upgrade. (6b) is post-merge and gated on the deploy-parity PR. (8),(9) are
backlog.
