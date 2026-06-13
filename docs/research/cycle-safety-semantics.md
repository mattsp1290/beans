# Dependency Cycle-Safety Decision

Issue: `beans-5gy`

This decision replaces the current Postgres-specific cycle-safety implementation
for `AddDep` and `ImportIssuesFull` with a portable strategy for PostgreSQL,
MySQL, and SQLite.

## Current Behavior

`Store.AddDep` and `Store.ImportIssuesFull` currently combine two Postgres
features:

| Surface | Location | Purpose |
| --- | --- | --- |
| Serializable pgx transaction | `store/store.go:635`, `store/store.go:913` | Prevent concurrent edge inserts from passing stale cycle checks. |
| Recursive CTE cycle query | `store/store.go:1259` | Walk existing dependencies to decide whether adding `childID -> parentID` would make `childID` reachable from `parentID`. |
| Postgres serialization retry | `store/store.go:887`, `store/store.go:1653` | Retries `ImportIssuesFull` on SQLSTATE `40001`. |
| Primary key on dep edges | `schema/migrations/0001_bn_init.sql:73` | Rejects duplicate `(issue_id, blocked_by_id)` edges. |
| Self-edge check | `schema/migrations/0001_bn_init.sql:74` | Rejects one-node cycles at the database layer. |

The behavior to preserve:

- `AddDep` returns `ErrNotFound` when either issue ID does not exist.
- `AddDep` returns `ErrCycle` when the proposed edge creates a transitive cycle.
- `AddDep` returns `ErrDuplicateDep` for an existing edge.
- `ImportIssuesFull` inserts valid dependency edges after issue writes.
- `ImportIssuesFull` skips missing, duplicate, self, and cyclic dependency edges
  through the existing result counters rather than failing the whole import.
- Concurrent dependency writers must not create a cycle.

## Decision

Use a **single dependency-graph guard row plus Go-side graph traversal**.

Add a tiny guard table seeded with one row:

```sql
CREATE TABLE bn_dep_graph_guard (
    id SMALLINT NOT NULL PRIMARY KEY,
    updated_at <dialect timestamp> NOT NULL
);

INSERT INTO bn_dep_graph_guard (id, updated_at) VALUES (1, <now>);
```

Every operation that may insert dependency edges must lock this row in the same
transaction before it reads existing edges or inserts new ones.

Locking strategy:

- PostgreSQL/MySQL: select the guard row with a row-level update lock through
  GORM's locking clause or a centralized dialect-aware raw SQL helper.
- SQLite: acquire writer serialization before any dependency graph read. Use
  `BEGIN IMMEDIATE` through a dialect helper, or an explicitly documented
  store-level mutex/single-connection strategy if GORM cannot issue that cleanly.
  A default deferred transaction is not sufficient for `AddDep` or the
  dependency pass in `ImportIssuesFull`, because it can read the graph before
  obtaining the writer lock.

After the guard is held, load the dependency graph into Go and perform reachability
checks in memory. Do not rely on recursive CTE SQL for cycle detection in the
GORM implementation. Recursive CTE support exists across the target backends, but
syntax and optimizer behavior differ enough that it is not the right place to
carry a security/correctness invariant.

This intentionally serializes all dependency edge writes. That is more
conservative than the current Postgres-only implementation, but the dependency
graph is small operational metadata and correctness is more important than write
parallelism here.

## AddDep Flow

`AddDep(ctx, childID, parentID)` should run in one transaction:

1. Validate `childID != parentID` before hitting the database and return
   `ErrCycle` for self-dependencies.
2. Lock `bn_dep_graph_guard(id=1)`.
3. Fetch both issue rows and return `ErrNotFound` if either is missing.
4. Load either the full dependency edge set or a complete transitive closure
   starting at `parentID` by following `issue_id -> blocked_by_id` edges until
   exhaustion. Loading only direct edges, prefix-local edges, or edges adjacent
   to `childID` is not sufficient.
5. If `childID` is reachable from `parentID`, return `ErrCycle`.
6. Insert `(childID, parentID)` with duplicate-safe insert semantics.
7. If the insert affects zero rows or reports a duplicate constraint, return
   `ErrDuplicateDep`.
8. Commit.

The graph walk should treat edges as global issue-ID edges, matching the current
store signature that accepts issue IDs without a prefix argument. A later public
API decision may choose to make dependencies explicitly prefix-scoped, but this
cycle-safety migration should not silently change cross-prefix behavior.

## ImportIssuesFull Flow

`ImportIssuesFull` should keep its two-pass shape:

1. Normalize/dedupe inputs.
2. In one transaction, upsert or create issue rows and record which issue IDs
   were written.
3. Lock `bn_dep_graph_guard(id=1)` before processing dependency edges.
4. Load the dependency graph once into Go.
5. For each dependency edge from written inputs:
   - skip self edges and increment `DepsSkippedSelf`;
   - skip repeated edges within the input item and increment
     `DepsSkippedDuplicate`;
   - skip blockers that do not exist in the destination prefix and increment
     `DepsSkippedMissingBlocker`;
   - skip edges that would make `item.ID` reachable from `blockerID` and
     increment `DepsSkippedCycle`;
   - insert valid edges with duplicate-safe semantics;
   - if the insert reports an existing dependency edge, including an edge
     created by a prior import or by another transaction that committed before
     this guarded dependency pass, increment `DepsSkippedDuplicate` rather than
     returning an error;
   - update the in-memory graph immediately after a successful insert so later
     edges in the same import see earlier imported edges.
6. Commit.

Issue writes and dependency-edge processing must remain in the same database
transaction. The graph guard may be acquired after pass 1, but the transaction
must not commit between pass 1 issue writes and the guarded pass 2 dependency
writes. If a dialect-specific implementation cannot hold both phases in one
transaction, this decision must be revisited to define the changed visibility and
result-counter contract before implementation.

Create-only concurrent imports should not depend on SQL serialization retries for
normal duplicate races. Duplicate issue and edge outcomes should be represented
through `Created`, `Skipped`, `DepsAdded`, and `DepsSkippedDuplicate` counters.

## Isolation and Retry Contract

The target isolation contract is:

| Backend | Required behavior |
| --- | --- |
| PostgreSQL | `ReadCommitted` is sufficient for dependency writes once the graph guard row is locked. Retrying serialization failures is allowed but should not be required for cycle safety. |
| MySQL | `ReadCommitted` or the backend default is sufficient once the graph guard row is locked. Duplicate-key and lock-timeout/deadlock errors must be normalized in the store layer. |
| SQLite | Dependency-write transactions must acquire writer serialization before graph reads, for example with `BEGIN IMMEDIATE` or an equivalent store-level mechanism. Tests may accept SQLite's single-writer behavior, including busy/locked retry handling if the store adds it. |

Do not rely on `sql.LevelSerializable` as the primary correctness mechanism. It
is acceptable as an extra backend setting, but the graph guard row is the
portable invariant.

If a transaction fails because of a deadlock, lock timeout, busy SQLite writer,
or serialization error, retry the whole operation only when the error is known
to be safe to retry for that dialect. Centralize retry classification with the
other dialect-aware error normalization work.

## Graph Walk Contract

Use the same direction as the current `hasCycle` helper: for a proposed edge
`childID -> parentID`, adding the edge creates a cycle when `childID` is already
reachable from `parentID` by following `blocked_by_id` edges.

Implementation guidance:

- Represent adjacency as `map[string][]string` from issue ID to blocker IDs.
- Prefer loading the full dependency graph for the initial implementation. A
  partial loader is acceptable only if it loads complete transitive blocker paths
  for every candidate edge in the operation.
- `ImportIssuesFull` must use a graph snapshot that includes every existing edge
  that could appear on a blocker path for any candidate edge in the batch, plus
  successful edges inserted earlier in the same import.
- Use iterative DFS or BFS to avoid recursion depth surprises.
- Track visited nodes so malformed legacy graphs cannot loop forever.
- Existing malformed cycles are not repaired by this migration. If the loaded
  graph already contains a cycle, traversal must still terminate and use a
  deterministic policy: reject a proposed edge when its reachability walk can
  reach `childID`, returning `ErrCycle` for `AddDep` or incrementing
  `DepsSkippedCycle` for `ImportIssuesFull`; allow edges whose walk does not
  reach `childID` and does not depend on the malformed component.
- Add the proposed edge to the in-memory graph after each successful insert in
  `ImportIssuesFull`.
- Keep deterministic ordering in tests, but do not make traversal ordering part
  of the public API.

## Schema and Migration Requirements

- Add `bn_dep_graph_guard` to every dialect migration set and seed row `id=1`.
  The seed must be idempotent. A missing guard row at runtime should fail loudly
  as a migration/corruption error rather than allowing unguarded dependency
  writes.
- Use a bounded integer type for `id` in MySQL and SQLite; this table should not
  depend on Postgres-only `SMALLSERIAL` or sequence behavior.
- Keep the existing `bn_issue_deps` uniqueness and cascade semantics.
- Keep Go-side self-edge validation even if dialect-specific `CHECK` constraints
  are also present.
- Existing databases only need the guard table and seed row. No dependency-edge
  backfill is required.

## Implementation Checklist

- Add a store helper that opens a transaction and locks `bn_dep_graph_guard`.
- Replace `hasCycle` recursive CTE with a Go graph loader and reachability
  helper.
- Update `AddDep` to use the guard row and Go graph walk.
- Update `ImportIssuesFull` dependency pass to lock the guard row, reuse the
  graph snapshot, and mutate the in-memory graph after each inserted edge.
- Preserve the single-transaction import boundary across pass 1 issue writes and
  guarded pass 2 dependency writes.
- Define a deterministic lock order inside the shared helper: open transaction,
  acquire the dependency graph guard, then load graph state. `ImportIssuesFull`
  is the only exception because it must write/pass-count issues before acquiring
  the graph guard, but it still acquires the guard before reading dependency
  edges.
- Replace Postgres-only serialization retry with dialect-aware retry
  classification for retryable transaction failures.
- Normalize duplicate edge errors for Postgres, MySQL, and SQLite.
- Preserve `ErrCycle`, `ErrDuplicateDep`, and `ErrNotFound` wrapping semantics
  used by `cmd/bn/cmd_dep.go`.

## Tests Required

- `AddDep` rejects self-dependencies with `ErrCycle`.
- `AddDep` rejects transitive cycles with `ErrCycle`.
- `AddDep` rejects a long transitive cycle, such as existing `A -> B -> C -> D`
  followed by attempted `D -> A`.
- `AddDep` rejects duplicate edges with `ErrDuplicateDep`.
- `ImportIssuesFull` skips self, duplicate, missing-blocker, and cyclic edges
  through the existing result counters.
- `ImportIssuesFull` increments `DepsSkippedDuplicate` for existing dependency
  edges discovered at insert time, including edges created by another committed
  transaction before this operation's guarded dependency pass.
- Concurrent opposite edge inserts, such as `A -> B` and `B -> A`, cannot both
  succeed. Exactly one may succeed; the other must return `ErrCycle`,
  `ErrDuplicateDep`, or a documented retry/conflict mapping that leaves the
  stored graph acyclic.
- Mixed concurrent `AddDep` and `ImportIssuesFull` operations that would form a
  cycle if unserialized must share the same guard and leave the graph acyclic.
- SQLite concurrent opposite-edge tests must prove the graph read happens after
  writer serialization, not merely that the final insert is serialized.
- Concurrent imports that would form a cycle across batches leave the graph
  acyclic and account for skipped or failed edges according to the documented
  result/error contract.
- A malformed legacy cycle fixture terminates traversal and produces the same
  deterministic `ErrCycle`/`DepsSkippedCycle` behavior across SQLite and
  integration backends.
- Cross-prefix regression coverage preserves the current global issue-ID graph
  behavior until the public Store API contract explicitly changes it.
- Contract tests run against SQLite by default and PostgreSQL/MySQL containers
  when integration tests are enabled.

## Rejected Alternatives

### Per-Dialect Recursive CTEs

PostgreSQL, MySQL 8, and SQLite all support recursive CTEs, but their placeholder
syntax, casting, optimizer behavior, and recursion limits differ. Keeping this
logic in SQL would force every dependency invariant test to validate three query
implementations.

### Serializable Isolation Alone

Postgres serializable transactions currently protect the check-then-insert race,
but SQLite does not provide the same model and MySQL behavior depends on engine,
isolation level, and locking details. Serializable isolation can remain a retry
signal, but it should not be the only correctness mechanism.

### No Serialization Guard

A Go-side graph walk without a write serialization point has the same TOCTOU
problem as a SQL cycle check without a lock: two transactions can both observe an
acyclic graph and insert edges that form a cycle together.
