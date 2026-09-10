# 02 — Implementation

Ordered, each step independently verifiable. Steps 1–2 are the core upgrade;
steps 3–4 preserve API behavior; step 5 adds coverage.

## Step 1 — Bump the dependency

```bash
go get github.com/mattsp1290/beans@e52dce57b52c
go mod tidy
```

Expected `go.mod`:
`github.com/mattsp1290/beans v0.1.2-0.20260615002029-e52dce57b52c` (the same
pseudo-version production runs). `go mod tidy` should add no new requires (the
target's require set matches v0.1.1).

Verify nothing else moved unexpectedly:
```bash
git diff go.mod go.sum         # only the beans line + its go.sum hashes
go build ./...                 # compiles with no source changes
```

## Step 2 — Confirm clean compile + existing tests (baseline)

```bash
go test ./...                  # incl. e2e (sqlite); should pass unchanged
make vet lint fmt-check
```

If green, the upgrade is already API-compatible. Steps 3–4 address the one
behavioral change.

## Step 3 — Preserve blocking-only semantics for `/deps` and `/graph`

beans' `ListDeps` now returns parent-child edges too, and the **production
tracker has epic data**. To keep "dependency graph = blocking edges," route both
read paths through `ListBlockingDeps`.

**Grounded seam (corrected after review):** `cmd/bean-counter/main.go:66-74`
passes `adapter.Store()` — the **raw `*beansstore.Store`** — to `deps.Register`
and `graph.Register`. The raw store already exposes
`ListBlockingDeps(ctx, prefix)` (`store/store.go:712` in the target). So the
change is purely at the handler interface, and **no `Adapter` wrapper and no
`main.go` change are needed**:

1. In `internal/handlers/deps/deps.go` and `internal/handlers/graph/graph.go`,
   rename the local `Store` interface method **and** its call site from
   `ListDeps(context.Context, string)` to `ListBlockingDeps(context.Context,
   string)` (same signature: `([]appstore.DepEdge, error)`).
2. **Do NOT add `Adapter.ListBlockingDeps`** — nothing on the call path would
   use it (handlers bind to the raw store). Also **remove the now-unused
   `Adapter.ListDeps`** (`internal/store/adapter.go:137`) if it has no remaining
   caller — grep first (`grep -rn 'adapter.*ListDeps\|\.ListDeps(' internal cmd`).
3. Leave `AddDep` (blocking) and `ReadyIssues` as-is.

> Decision point (see `05` Q1): the alternative is to expose `dep_type` in the
> `Dependency` DTO and render membership edges. That is a **feature**, not part
> of this bump; recommended as a follow-up.

## Step 4 — Update the unit-test fakeStores (required by Step 3)

Renaming the handler `Store` interface method **breaks compilation** of the
existing unit tests, whose `fakeStore` implements `ListDeps`:
`internal/handlers/deps/deps_test.go:38` and
`internal/handlers/graph/graph_test.go:33`. In the **same** change as Step 3:

- rename `func (s *fakeStore) ListDeps(...)` → `ListBlockingDeps(...)` in both
  files; existing assertions (`TestListDependencies`, graph tests) still hold.

The DTO stays stable: no change to `internal/api/dto/deps.go` / `graph.go` under
the recommended approach (blocking-only edges, existing fields). The new
`DepEdge.DepType` field is simply not mapped. (If Q1 flips to "expose
membership," add `dep_type` here and to the DTO test.)

## Step 5 — Add coverage for the new behavior

Two independent behaviors to lock in. Note the harness constraints found in
review:

- The sqlite test harness creates deps **only over HTTP** (`POST
  /issues/:id/deps` → always a `blocks` edge via `AddDep`). There is **no HTTP
  route** to create a `parent-child` edge.
- `AddTypedDep`, `DepTypeBlocks`, `DepTypeParentChild`, `ListBlockingDeps` are
  **not** re-exported by `appstore` today.
- `sqliteHandlersApp` (`internal/handlers/sqlite_test.go:169-210`) returns only
  `(*fiber.App, closeFunc)` — it does **not** surface the adapter/raw store to
  the test body.

So the deps/graph membership test needs a small amount of plumbing:

1. **Re-export for tests** (and future use): add to `internal/store/adapter.go`
   `const DepTypeBlocks = beansstore.DepTypeBlocks`,
   `const DepTypeParentChild = beansstore.DepTypeParentChild`, and an
   `AddTypedDep` passthrough on `*Adapter` (or expose the raw store from the
   harness). Prefer re-exports so the test imports only `appstore`.
2. **Surface the store** from `sqliteHandlersApp` (return the `*Adapter` or raw
   `*Store` alongside the app) so the test can seed a membership edge directly.
3. **deps/graph blocking-only test** — use **two distinct ordered pairs** (the
   PK is `(issue_id, blocked_by_id)` excluding `dep_type`, so reusing one pair
   returns `ErrDuplicateDep`): create issues A,B,C,D; add a `blocks` edge A→B
   (via HTTP `POST /deps`) and a `parent-child` edge C→D (via the seeded
   `AddTypedDep`). Assert `GET /api/v1/deps` and `GET /api/v1/graph` return
   **only** the A→B blocking edge.
4. **`blocked_by` blocks-only test** — assert C's `GET /api/v1/issues/:id`
   `blocked_by` array does **not** include D (see Step 4b note: `populateBlockedBy`
   is now blocks-only).
5. **ready-excludes-epics test (HTTP-only, no plumbing)** — `POST /issues` with
   `"issue_type":"epic"` (a valid type, `internal/api/validate/validate.go:33`)
   and no blockers, plus a normal ready issue; assert `GET /api/v1/ready` omits
   the epic.

These exercise the new beans surface, so they double as proof the upgrade took.

## Step 6 — Re-run the full gate set

```bash
go test ./...
go test -tags=integration ./...        # postgres + mysql via testcontainers
make vet lint fmt-check
( cd frontend && npm ci && npm run check && npm test && npm run build )
```

Frontend is unaffected (no API shape change under the recommended approach), but
run it to keep "done" honest.

## Step 7 — Re-validate the deploy parity gate

```bash
scripts/deploy-production.sh --ref <sha-on-origin-main> --check
```

Expect `embedded_max=8 db_max=8` (was `7`/`8`) and `CHECK OK`. This closes the
deploy compatibility finding.

## Out of scope (follow-ups)

- Exposing epics / parent-child membership in the UI and DTO (`05` Q1).
- Any use of `ListMembers` / `ListParents` (epic drill-down views).
