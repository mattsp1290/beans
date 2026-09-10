# Memory Search Strategy Decision

Issue: `beans-c51`

This decision defines how `SearchMemories` should work after the store moves
from Postgres/pgx to GORM with PostgreSQL, MySQL, and SQLite backends.

## Current Behavior

`SearchMemories` is currently Postgres-specific:

| Surface | Location | Current behavior |
| --- | --- | --- |
| Generated FTS column | `schema/migrations/0002_bn_memories.sql:19` | `tsvector GENERATED ALWAYS AS (to_tsvector('english', body)) STORED` |
| FTS index | `schema/migrations/0002_bn_memories.sql:23` | GIN index on `tsv` |
| Query predicate | `store/store.go:1156` | `tsv @@ plainto_tsquery('english', query)` |
| Ranking | `store/store.go:1169` | `ORDER BY ts_rank(...) DESC, created_at DESC` |
| Tag filtering | `store/store.go:1165` | `tags @> <jsonb array>` means all requested tags are present |
| Empty query ordering | `store/store.go:1171` | `ORDER BY created_at DESC` |
| CLI help | `cmd/bn/cmd_memory.go:21`, `cmd/bn/cmd_memory.go:78` | User-facing text says Postgres/tsvector |

The migration must remove the hard dependency on `tsvector`, GIN, JSONB
containment, and Postgres FTS functions.

## Decision

Use a **dialect-specific memory search adapter** selected from `store.Config.Driver`.
Do not make a portable `LIKE` scan the default implementation.

Correctness contract shared by all adapters:

- Scope is identical across backends: when `MemoryFilter.All` is false, return
  memories where `prefix = filter.Prefix` or `prefix IS NULL`; when true, search
  all prefixes and global memories.
- `MemoryFilter.Type` is an exact match on `mtype`.
- `MemoryFilter.Tags` uses all-tags semantics: every requested tag must be
  present on the memory.
- `MemoryFilter.Limit <= 0` uses `defaultMemoryLimit`.
- Empty query returns recent memories ordered by `created_at DESC, id DESC`.
- Whitespace-only query strings are normalized to the empty-query path before
  adapter dispatch.
- Non-empty query returns only memories that match the backend's text-search
  query semantics.
- Ties after backend rank are ordered by `created_at DESC, id DESC`.

Ranking contract:

- Relevance ranking is **not a cross-dialect public contract**. PostgreSQL,
  MySQL, and SQLite tokenize, stem, score, and handle stop words differently.
- Tests may assert backend-specific ranking through dialect expectation hooks.
- Cross-dialect contract tests should assert inclusion/exclusion, scope, tags,
  type filters, limit handling, and deterministic tie-breaking, not identical
  relevance order.

## Adapter Strategies

| Driver | Search implementation | Ranking |
| --- | --- | --- |
| `postgres` | Keep Postgres FTS with `to_tsvector`, `plainto_tsquery`, and `ts_rank`, expressed through the Postgres adapter. | `ts_rank DESC, created_at DESC, id DESC` |
| `mysql` | Use InnoDB `FULLTEXT` on memory body and `MATCH(body) AGAINST (? IN NATURAL LANGUAGE MODE)`. | `MATCH ... AGAINST` score DESC, `created_at DESC, id DESC` |
| `sqlite` | Use SQLite FTS5 with an external-content virtual table for memory body text. | `bm25(...) ASC` because lower scores are better, then `created_at DESC, id DESC` |

The adapter owns query construction and any dialect-specific DDL names. The
store-facing API remains `SearchMemories(ctx, query string, f MemoryFilter)`.
The `id DESC` tie-break is an intentional tightening over the current Postgres
implementation, which only sorts equal-rank rows by `created_at DESC`.

MySQL FULLTEXT has server-level stopword and minimum-token behavior. Short terms
or common stopwords may be ignored even when the memory body visibly contains the
raw text. That behavior is backend-specific and should be covered by MySQL
expectation tests; cross-dialect fixtures should use unambiguous searchable terms.
Queries that reduce to no searchable MySQL terms should return an empty result
without falling back silently to `LIKE`.

## Fallback Policy

A portable `LIKE` search may exist only as an explicitly marked degraded mode,
for example `MemorySearchModeDegradedLike` selected from an explicit store
configuration or test hook. It is not the normal path for supported drivers. It
is acceptable for:

- emergency compatibility when FTS objects are missing and the caller chooses a
  degraded mode;
- development diagnostics;
- a temporary migration flag while dialect migrations are being built.

If `LIKE` fallback is used, it must:

- escape `%`, `_`, and the dialect escape character;
- use case-insensitive matching where the backend supports it consistently;
- preserve scope, type, tag, limit, and empty-query ordering semantics;
- return a warning or error path visible enough that operators know ranking is
  degraded.

Do not silently fall back from a broken FTS schema to `LIKE` in normal operation.
Missing FTS objects should fail as a migration/schema error.

## Tag Filtering

Tags filtering is not solved by the FTS adapter. `tags @> ...::jsonb` has no
portable equivalent, and all-tags semantics are part of the `SearchMemories`
correctness contract.

Preferred design: add a normalized `bn_memory_tags` table:

```sql
CREATE TABLE bn_memory_tags (
    memory_id <memory id type> NOT NULL REFERENCES bn_memories(id) ON DELETE CASCADE,
    tag       <tag key type>   NOT NULL,
    PRIMARY KEY (memory_id, tag)
);
CREATE INDEX bn_memory_tags_tag_idx ON bn_memory_tags (tag, memory_id);
```

`InsertMemory` should write `bn_memories` and normalized tag rows in the same
transaction. Tags remain case-sensitive, matching the current JSON array
semantics. Duplicate tags in input or stored JSON should collapse to one
`(memory_id, tag)` row because the primary key is `(memory_id, tag)`.

`SearchMemories` should filter requested tags through a subquery that returns
matching memory IDs, then join or `WHERE id IN (...)` in the outer memory search
query:

```sql
SELECT memory_id
FROM bn_memory_tags
WHERE tag IN (?)
GROUP BY memory_id
HAVING COUNT(DISTINCT tag) = ?
```

Do not select full `bn_memories` columns in the grouped tag query; MySQL
`ONLY_FULL_GROUP_BY` can reject that shape. The JSON `tags` column may remain as
the API round-trip storage for now, but search filtering should not depend on
JSON containment.

Migration/backfill requirement:

- The migration that introduces `bn_memory_tags` must backfill it from every
  existing `bn_memories.tags` value before search switches to the normalized
  table.
- Backfill must be idempotent and preserve current `encodedLabels` semantics:
  tags are case-sensitive, duplicate stored JSON values produce one tag row, and
  invalid or non-array payloads fail migration rather than silently dropping
  tags.
- Upgrade tests must insert memories through the old JSON-only shape, run the
  migration, and verify tag-filtered `SearchMemories` still finds the same rows.

This tag decision overlaps `beans-mhs`; if that bead chooses a different
implementation, this memory-search strategy must still preserve the all-tags
contract and adapter tests.

## Schema Requirements

Each dialect migration must create the base `bn_memories` table plus its search
objects:

- PostgreSQL: generated or maintained `tsvector` field and GIN index, or an
  expression index if the migration strategy chooses that instead.
- MySQL: `FULLTEXT` index on `body`; ensure table/column types and collation are
  compatible with InnoDB full-text search.
- SQLite: FTS5 virtual table with external-content sync. Use triggers or an
  explicit transaction helper so insert/update/delete of `bn_memories` and FTS
  rows stay consistent.
- SQLite: the FTS5 table must use `content='bn_memories'` and
  `content_rowid='id'`, or an equivalent design where FTS `rowid` is always equal
  to `bn_memories.id`. Triggers or the explicit transaction helper must write FTS
  rows with `rowid = new.id`.
- All dialects: `created_at` and `id` are available for deterministic tie-breaks.
- All dialects: tag index/table supports all-tags filtering.

SQLite FTS5 virtual tables and MySQL FULLTEXT indexes are raw dialect DDL; they
should not be hidden behind generic GORM `AutoMigrate`.

## Query Builder Requirements

Memory search should move out of ad hoc string concatenation in `SearchMemories`
and into a small adapter boundary, for example:

```go
type memorySearchAdapter interface {
	Search(ctx context.Context, db *gorm.DB, query string, f MemoryFilter) ([]Memory, error)
}
```

Adapter implementation rules:

- Use bound parameters for all user input.
- Never concatenate raw query terms into FTS expressions.
- Normalize empty/whitespace-only query strings to the empty-query path.
- Treat `SearchMemories` input as plain user text for every adapter. PostgreSQL
  keeps `plainto_tsquery`; MySQL natural-language mode may bind the plain text
  directly; SQLite must transform plain text into a valid FTS5 query by
  tokenizing and quoting/escaping terms, or by using a documented helper that
  returns a safe no-match query for input that cannot be represented.
- SQLite input containing quotes, boolean operators, `NEAR`, wildcard
  characters, punctuation, or unmatched syntax must not surface an FTS syntax
  error for ordinary CLI/store search. Inputs that reduce to no searchable terms
  return no matches unless the raw input was whitespace-only.
- Apply scope, type, tags, limit, and tie-break ordering in every adapter.
- Return `Memory` values with UTC `CreatedAt`, decoded `Tags`, and stable JSON
  output behavior.

## CLI Contract

Update `cmd/bn/cmd_memory.go` and `primeText` after implementation:

- `remember` should no longer say "Persist a memory to Postgres".
- `memories` help should say search uses the configured database backend.
- Help should avoid promising identical stemming or relevance ranking across
  databases.
- JSON output order follows the store result order and therefore only promises
  cross-dialect deterministic tie-breaking, not identical relevance order.

## Tests Required

Cross-dialect contract tests:

- Insert global and project-scoped memories; scoped search returns project plus
  global memories, while `All=true` returns all prefixes.
- Empty query orders by `created_at DESC, id DESC`.
- Type filter returns only exact matching `mtype` rows.
- Tags filter requires all requested tags and does not match partial tag sets.
- Tags filter works through the normalized tag table under a MySQL
  `ONLY_FULL_GROUP_BY` configuration.
- Limit truncates results after filters and ordering.
- Non-empty query includes matching rows and excludes clearly non-matching rows.
- Whitespace-only query uses the empty-query path.
- Results decode tags and normalize `CreatedAt` to UTC.
- Upgrade/backfill tests preserve tag-filtered search for memories that existed
  before `bn_memory_tags`.

Dialect expectation tests:

- PostgreSQL: verifies `plainto_tsquery` behavior that the Postgres adapter
  intentionally keeps, including a stable rank fixture with `ts_rank`.
- MySQL: verifies `MATCH ... AGAINST` returns expected matches and uses
  backend-specific rank ordering when scores differ. Include one searchable token,
  one short-token case, and one stopword/common-word case to document server
  default behavior.
- SQLite: verifies FTS5 `MATCH` and `bm25` ordering, plus FTS table sync after
  inserting memories. Include quotes, punctuation, `AND`/`OR`/`NEAR`, wildcard
  characters, and unmatched double quotes to prove plain-text normalization does
  not expose raw FTS5 query syntax. Verify rows returned from FTS have the same
  `id` and `body` as the base `bn_memories` row after insert and any future
  update/delete path.
- Fallback mode, if implemented, escapes wildcard characters and is not used
  silently when FTS objects are missing.

## Rejected Alternatives

### Portable `LIKE` as the Default

`LIKE` would be simpler and likely good enough for small datasets, but it would
drop stemming/tokenization and degrade the existing user experience on Postgres.
It is useful as a degraded mode, not the default for supported backends.

### One Cross-Dialect Ranking Contract

Trying to make PostgreSQL `ts_rank`, MySQL natural-language full-text scores,
and SQLite `bm25` produce identical ordering would make tests brittle and hide
backend differences behind arbitrary normalization. The stable public contract is
matching/filtering plus deterministic tie-breaks.

### Keep Tags in JSON Queries

JSON query APIs vary across Postgres, MySQL, and SQLite, especially for
all-elements array containment. A normalized tag table is more predictable,
indexable, and easier to test.
