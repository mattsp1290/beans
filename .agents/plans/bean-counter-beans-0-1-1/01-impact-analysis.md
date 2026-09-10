# 01 — Impact analysis (grounded)

Diff basis: `beans@v0.1.1` vs `beans@v0.1.2-0.20260615002029-e52dce57b52c`
(commit `e52dce5`), plus bean-counter's actual consumption of the beans surface.

## What changed in beans

### Schema
- New migration `0008_bn_dep_type.sql`:
  `ALTER TABLE bn_issue_deps ADD COLUMN dep_type TEXT NOT NULL DEFAULT 'blocks';`
  plus index `bn_issue_deps_parent_idx (blocked_by_id, dep_type)`. The PK stays
  `(issue_id, blocked_by_id)` — at most one edge per ordered pair, any kind.
- `bn_schema_versions` max → 8.

### `store` package (additive + 2 behavior shifts)
- `DepEdge` gains `DepType string` (additive field).
- New consts: `DepTypeBlocks = "blocks"`, `DepTypeParentChild = "parent-child"`.
- New funcs: `AddTypedDep`, `ListBlockingDeps`, `ListMembers`, `ListParents`,
  `ValidateDepType`.
- `AddDep(ctx, child, parent)` **unchanged signature**; now delegates to
  `AddTypedDep(..., DepTypeBlocks)`.
- **Behavior:** three shifts, all toward "blocking-only":
  1. `ListDeps` returns *all* edge kinds (was blocking-only by construction).
  2. `ReadyIssues` excludes `issue_type='epic'` and gates only on
     `dep_type='blocks'`.
  3. `populateBlockedBy` now filters `dep_type='blocks'` (target store.go:1240,
     1268), so `Issue.BlockedBy` — exposed as `blocked_by` in
     `GET /api/v1/issues` and `/issues/:id` (`internal/api/dto/issues.go:18`) —
     no longer includes parent-child edges. Correctness improvement; no code
     change, but add a test assertion (see `02` Step 5.4).
- `CreateIssueInput` / `UpdateIssueInput` / `ListFilter` / `New` / `Config` /
  error sentinels: **unchanged**.

### `model` package
- **Identical.** No change to `Issue`, `Priority`, `IssueState`, `RepoTarget`.

### `go.mod`
- **Unchanged** `go` directive and `require` set. No new transitive deps.

## bean-counter consumption — file by file

| File | Uses | Compiles? | Behavior after bump | Action |
|------|------|-----------|---------------------|--------|
| `internal/store/adapter.go` | re-exports `DepEdge`, store/model types; `ListDeps`, `ReadyIssues` | ✅ (additive field, identical model) | `ListDeps` returns all kinds; `ReadyIssues` excludes epics | Add a `ListBlockingDeps` adapter method; keep `ReadyIssues` as-is |
| `internal/handlers/deps/deps.go` | `Store.ListDeps`, `Store.AddDep` | ✅ | `/api/v1/deps` would include parent-child edges | Rename handler iface+call to `ListBlockingDeps`; update `fakeStore` in `deps_test.go` |
| `internal/handlers/graph/graph.go` | `Store.ListDeps`, `Store.ListIssues` | ✅ | `/api/v1/graph` would include membership edges | Rename handler iface+call to `ListBlockingDeps`; update `fakeStore` in `graph_test.go` |
| `internal/handlers/ready/ready.go` | `Source.ReadyIssues` | ✅ | epics excluded, blocking-only readiness | No change (improvement) |
| `internal/handlers/issues/issues.go` | `Store.ListIssues` | ✅ | epics still listed; `blocked_by` now blocks-only | No change (improvement) |
| `internal/api/dto/deps.go` | maps `DepEdge`→`Dependency{IssueID,BlockedByID}` | ✅ | drops new `DepType` silently | Optionally add `dep_type` (only if exposing membership) |
| `internal/api/dto/graph.go` | `GraphResponseFromStore(issues, []DepEdge)` | ✅ | as above | As above |

**Net:** zero compile-breaking changes. The only decision is whether `/deps`
and `/graph` keep showing blocking edges only (recommended, behavior-preserving)
or start surfacing parent-child membership.

## Test impact

bean-counter's *integration/e2e/sqlite* tests create **blocking** deps only, so
with blocking-only data `ListDeps` and `ListBlockingDeps` return identical rows
and those tests pass unchanged.

**Exception (caught in review):** the per-handler **unit** tests mock the `Store`
interface by method name — `fakeStore.ListDeps` at
`internal/handlers/deps/deps_test.go:38` and
`internal/handlers/graph/graph_test.go:33`. Renaming the interface method to
`ListBlockingDeps` (Step 3) **breaks their compilation** until the fakes are
renamed too (Step 4). So "existing tests pass unchanged" is false for these two
files. New behavior (parent-child filtering, `blocked_by` blocks-only, epic
exclusion) needs **new** coverage — see `02` Step 5.

## Migration / runtime safety

- bean-counter does **not** own beans migrations (`adapter.go`). Bumping the lib
  raises bean-counter's embedded set to 0008; on connect, `beansstore.New` runs
  goose `Up()`. Against **prod (already at 8)** this is a no-op. Against a fresh
  local/test DB it applies 0001–0008.
- After the bump, the deploy parity gate sees `embedded=8, db=8` → exact match,
  removing the runtime-compat risk flagged during `--check`.
