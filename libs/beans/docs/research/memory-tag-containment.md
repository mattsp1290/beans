# Memory Tag Containment Decision

Issue: `beans-mhs`

This decision finalizes the replacement for `SearchMemories` tag filtering after
the store migration to PostgreSQL, MySQL, and SQLite.

## Current Behavior

Memory tags are currently stored as a JSON array in `bn_memories.tags` and
filtered with Postgres JSONB containment:

```sql
AND tags @> $N::jsonb
```

Relevant surfaces:

| Surface | Location | Current behavior |
| --- | --- | --- |
| Memory input | `store/store.go:1052` | `MemoryInput.Tags []string` |
| Memory row | `store/store.go:1060` | `Memory.Tags []string` |
| Insert encoding | `store/store.go:1089` | `encodedLabels(in.Tags)` writes JSON array |
| Tag filter | `store/store.go:1163` | `tags @> <jsonb array>` means all requested tags are present |
| Result decoding | `store/store.go:1189` | `decodeLabels(tagsBytes)` returns tags for API/JSON output |

`tags @>` has no portable equivalent across MySQL JSON, SQLite JSON/TEXT, and
Postgres JSONB for arbitrary all-elements array containment.

## Decision

Use a normalized `bn_memory_tags` table for all tag filtering. Do not use
`gorm.io/datatypes` JSON query helpers or a portable `LIKE` fallback for normal
tag filtering.

The JSON `bn_memories.tags` column may remain as API round-trip storage during
the migration, but `SearchMemories` must not use JSON containment for filtering
once the normalized table exists.

## Semantics

Preserve current all-tags behavior:

- `MemoryFilter.Tags == nil` or `len(Tags) == 0` applies no tag filter.
- A non-empty tag filter matches a memory only when every requested tag is
  present on that memory.
- Tags are case-sensitive strings.
- No stemming, tokenization, trimming, lowercasing, or Unicode normalization is
  applied by the store. CLI callers may pass tags as entered.
- Duplicate tags in `MemoryInput.Tags`, stored JSON, or `MemoryFilter.Tags`
  collapse to a single semantic tag.
- Empty tag strings are rejected by one shared normalization helper for writes,
  filters, and backfill. `SearchMemories` should return a validation error for
  empty filter tags instead of silently widening the query.
- Tags longer than the chosen maximum length are rejected and never truncated.
- The shared normalization helper returns a deterministic lexically sorted tag
  slice before encoding JSON or returning tag slices.

The returned `Memory.Tags` slice should keep the existing API shape. Prefer
decoding from `bn_memories.tags` until a later cleanup removes the JSON column;
if the normalized table becomes the source of truth for returned tags, sort tags
lexically so JSON output is deterministic.

## Schema

Add a normalized tag table in every dialect migration:

```sql
CREATE TABLE bn_memory_tags (
    memory_id <memory id type> NOT NULL REFERENCES bn_memories(id) ON DELETE CASCADE,
    tag       <tag key type>   NOT NULL,
    PRIMARY KEY (memory_id, tag)
);

CREATE INDEX bn_memory_tags_tag_memory_idx ON bn_memory_tags (tag, memory_id);
CREATE INDEX bn_memory_tags_memory_idx ON bn_memory_tags (memory_id);
```

Type requirements:

- `memory_id` must match the dialect-specific `bn_memories.id` type.
- `tag` must be a bounded string type for MySQL index compatibility; do not use
  unbounded MySQL `TEXT` in the primary key.
- Choose one max tag length in Go validation and migrations. A 255-character
  bound is sufficient unless product requirements say otherwise.
- `tag` comparisons and uniqueness must be byte-for-byte/case-sensitive in every
  dialect:
  - PostgreSQL may use ordinary `text` or bounded `varchar(N)` equality.
  - MySQL must use a binary/case-sensitive collation for the `tag` column, such
    as `VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`, or an
    equivalent explicitly case-sensitive collation.
  - SQLite should declare `tag TEXT NOT NULL COLLATE BINARY` or an equivalent
    bounded representation with binary comparison semantics.
- Preserve `ON DELETE CASCADE` so deleting a memory removes its tag rows.

The `(tag, memory_id)` index supports filtering by requested tags. The
`(memory_id)` index is useful for cleanup, verification, and possible future tag
source-of-truth reads.

## Write Path

`InsertMemory` should write `bn_memories` and `bn_memory_tags` in one
transaction:

1. Normalize, validate, dedupe, and sort tags once with the shared helper.
2. Encode `bn_memories.tags` from that normalized tag slice, not from the raw
   `MemoryInput.Tags` value, while the JSON column remains part of the API shape.
3. Insert one `(memory_id, tag)` row for each normalized tag.
4. Commit.

If tag-row insertion fails, the memory insert must roll back. The API must not
create a memory whose JSON tags and normalized tags disagree.
Returned `Memory.Tags` decoded from JSON must match the normalized tag rows
exactly, including duplicate removal, ordering, and empty-tag rejection.

Memories are currently append-only. If future code adds update/delete behavior,
updates must replace tag rows atomically with the base memory row; deletes rely
on cascade.

## Query Shape

Filter tags through a subquery that returns matching memory IDs:

```sql
SELECT memory_id
FROM bn_memory_tags
WHERE tag IN (?)
GROUP BY memory_id
HAVING COUNT(DISTINCT tag) = ?
```

The outer `SearchMemories` query should apply this as `WHERE id IN (...)` or by
joining the subquery result, then apply scope, type, full-text predicate, ranking,
tie-break ordering, and limit.

Do not select full `bn_memories` columns in the grouped tag query. MySQL
`ONLY_FULL_GROUP_BY` can reject grouped queries that select non-grouped memory
columns, and keeping tag matching in a subquery makes the FTS adapters simpler.
The tag subquery returns only the complete set of `memory_id` values matching all
normalized filter tags. Do not apply `ORDER BY` or `LIMIT` inside the tag
subquery. Apply scope, type, FTS predicate, ranking, tie-break ordering, and the
final `LIMIT` in the outer memory query after tag filtering.

Implementation notes:

- Deduplicate filter tags before calculating the `HAVING` count.
- Use bound parameters for tag values. With GORM, prefer slice expansion such as
  `Where("tag IN ?", tags)` over interpolating placeholder lists manually.
- For zero normalized filter tags, skip the tag subquery entirely.
- The same tag subquery should be used by PostgreSQL, MySQL, and SQLite adapters.

## Migration and Backfill

The migration introducing `bn_memory_tags` must backfill it from existing
`bn_memories.tags` before `SearchMemories` switches to the normalized table.

Backfill requirements:

- Idempotent: rerunning the migration or backfill helper must not duplicate rows
  or fail on already-inserted tag pairs.
- Case-sensitive: preserve exact stored tag strings.
- Duplicate-safe: duplicate values in a stored JSON array produce one tag row.
- Strict: invalid JSON, non-array JSON, or non-string array elements should fail
  migration rather than silently dropping or rewriting data.
- Empty tags fail migration with a controlled error, matching the write/filter
  validation policy.
- Tags longer than the chosen maximum length fail migration with a controlled
  error.

Per-dialect backfill can be implemented with raw SQL JSON array expansion or a
Go migration helper that scans existing memories and inserts tag rows in
batches. Prefer the Go helper if it reduces dialect-specific JSON parsing
surface and can run inside the migration transaction where the backend supports
transactional DDL/data migration.

Migration cutover invariant:

- The migration is not complete until every existing `bn_memories` row has
  normalized tag rows matching its normalized JSON tag set.
- Where transactional DDL is supported, create the table, backfill, validate, and
  record the migration in one transaction.
- Where DDL is not transactional, the backfill helper must be rerunnable and must
  perform a final validation query before the migration is recorded complete.
- Application code may switch `SearchMemories` to `bn_memory_tags` only at schema
  versions where that final validation has succeeded.
- A partially populated `bn_memory_tags` table from a failed attempt must be safe
  to repair by rerunning the migration/backfill.

## Rejected Alternatives

### `gorm.io/datatypes` JSON Queries

GORM JSON helpers can express some key/path checks, but all-elements array
containment is not uniform across Postgres, MySQL, and SQLite. Relying on these
helpers would leave the search adapter with backend-specific behavior and weaker
indexing.

### Portable `LIKE` Against JSON Text

`LIKE` against serialized JSON is not a semantic tag query. It can match
substrings, quoted JSON syntax, or escaped characters, and it cannot reliably
distinguish tag boundaries.

### Keep Postgres JSONB for Postgres Only

Using JSONB containment on Postgres and normalized tags elsewhere would split the
correctness path and tests. A single normalized strategy keeps all backends on
the same all-tags semantics.

## Tests Required

- `InsertMemory` writes matching JSON tags and normalized tag rows in one
  transaction.
- Tag filter with one tag matches memories containing that tag.
- Tag filter with multiple tags requires all requested tags.
- Duplicate filter tags do not change results.
- Duplicate stored/input tags create one normalized tag row.
- Case sensitivity is preserved, for example `Design` and `design` are distinct.
- MySQL tests run against a database whose default collation would otherwise be
  case-insensitive and verify the tag column still treats `Design` and `design`
  as distinct.
- Empty tag input is rejected consistently on write, filter, and backfill.
- Tags longer than the chosen maximum length are rejected on write, filter, and
  backfill.
- Scope, type, full-text search, tag filtering, ordering, and limit compose in
  one `SearchMemories` call.
- More tagged rows than the requested limit preserve the same final order as an
  equivalent untagged search restricted to matching memory IDs.
- MySQL contract tests run with `ONLY_FULL_GROUP_BY` enabled.
- Deleting a memory cascades normalized tag rows.
- Upgrade tests seed old JSON-only memories, run the migration/backfill, and
  verify tag-filtered search still returns those memories.
- Upgrade tests simulate a preexisting `bn_memory_tags` table with partial
  backfill, rerun migration/backfill, and verify every old JSON-only memory is
  searchable by tag.
- Backfill rejects invalid/non-array/non-string JSON tag payloads in a controlled
  migration test.
