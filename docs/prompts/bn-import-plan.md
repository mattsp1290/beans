# bn import implementation plan

## Goal

Make `bn import` a reliable bd-export-compatible JSONL importer that can seed or merge issues into the Postgres-backed bn store without corrupting existing tracker state.

The current repository already has a working import path in `cmd/bn/cmd_import.go` and `store/store.go`. This plan hardens that path and fills verification gaps before treating it as done.

## Current state

- `bn import [file]` accepts a positional file, `--file/-f`, or stdin.
- Import input is parsed as JSONL using bd-compatible fields: `id`, `title`, `description`, `status`, `priority`, `issue_type`, `labels`, `branch_name`, `url`, and `dependencies[]`.
- The CLI supports `--mode=create-only` and `--mode=merge`, plus `--dry-run`.
- The CLI auto-registers the target project prefix before importing.
- Store import has a two-pass flow: write issues first, then insert dependency edges.
- Store import skips dependency edges whose blockers are not in the batch or already present in the DB.
- Merge mode updates non-state fields and avoids regressing existing terminal states.

## Requirements

1. Preserve bd JSONL compatibility.
   - One JSON object per line.
   - Use `status` from bd as bn issue state.
   - Use `issue_type`, not CLI display field `type`.
   - Keep priority 0-indexed, matching bd and the `bn_issues.priority` column.
   - Import only dependency edges where `dependencies[].type == "blocks"` and `dependencies[].issue_id == raw.id`.

2. Preserve local tracker state.
   - `create-only` must never mutate existing issue rows.
   - `merge` may update non-terminal rows.
   - `merge` must not reopen or otherwise regress an existing terminal issue when incoming state is active.
   - `merge` must import terminal incoming states onto active existing issues.
   - Existing terminal rows may still receive non-state field updates.
   - State merge truth table:
     - existing active + incoming active: set state to incoming state.
     - existing active + incoming terminal: set state to incoming terminal state.
     - existing terminal + incoming active: keep existing terminal state.
     - existing terminal + incoming terminal: set state to incoming terminal state.

3. Be idempotent and report accurate counts.
   - Re-running the same create-only import should report existing rows as skipped.
   - Dependency edge counts should reflect edges actually inserted, not rows attempted with `ON CONFLICT DO NOTHING`.
   - Issue created/updated/skipped counts must be based on DB results, not stale prefetch assumptions.
   - Duplicate IDs and duplicate dependency edges in one input batch must not inflate counts.

4. Handle malformed or partial input predictably.
   - Skip malformed JSON lines and count parse warnings.
   - Skip rows missing required fields.
   - Skip rows with invalid priority or unsupported state.
   - Use empty slices for nil `labels` and `dependencies` where round-trip output expects arrays.
   - Keep current lenient behavior unless a line-level validation error would otherwise abort the whole import.

5. Keep prefix and dependency semantics explicit.
   - Import into the destination prefix column from `--project`, `BN_PROJECT`, or `.bn`.
   - Preserve source issue IDs exactly, even when their ID text carries a different source prefix.
   - Because `bn_issues.id` is currently a global primary key, an imported ID that already exists under a different prefix must be treated as a cross-prefix conflict and must not mutate the existing row.
   - Cross-prefix conflicts should be reported in the import summary.
   - Only add dependency edges for issues that were written in the current import operation.
   - Skip dependency edges with missing blockers and include them in summary output.
   - Skip self-dependencies and dependency edges that would create a cycle, and include both in summary output.

6. Keep the CLI contract ergonomic.
   - `bn import file.jsonl` works.
   - `bn import -f file.jsonl` works.
   - `cat file.jsonl | bn import` works.
   - `bn import --dry-run` parses and reports counts without any DB writes, including no project auto-registration.
   - `bn import --json` reports machine-readable summary counts, including dry-run and warning metadata.

7. Preserve dependency graph invariants.
   - Import must not bypass the no-self-dependency and no-cycle behavior enforced by normal dependency creation.
   - Invalid dependency edges should be skipped and counted rather than aborting the entire import when the issue rows are otherwise valid.
   - Blocker validation should happen inside the import transaction against the same identity scope used for writes.

## Implementation steps

1. Add focused parser tests in `cmd/bn/cmd_import_test.go`.
   - Valid full bd row maps all fields into `store.ImportInput`.
   - Dependency filtering ignores reverse/non-`blocks` edges.
   - Invalid JSON, missing id/title, invalid priority, and invalid state increment warnings and skip rows.
   - Blank lines and comments are ignored without warnings.

2. Add store integration coverage for `ImportIssuesFull`.
   - Create-only first run creates issues and deps.
   - Create-only second run skips existing issues and reports zero created edges for existing deps.
   - Merge mode updates non-state fields.
   - Merge mode does not regress a terminal existing state.
   - Merge mode imports terminal incoming state onto an active existing issue.
   - Missing blocker edges are skipped and counted.
   - Existing dep edges are not counted as newly added.
   - Cross-prefix ID conflicts are skipped/counted and do not mutate the original issue.
   - Duplicate IDs and duplicate dep edges in one batch do not inflate counts.
   - Self-dependencies and longer cycles are skipped/counted and do not abort the whole import.

3. Fix store import accounting.
   - Use command tags or explicit row checks so `Created`, `Updated`, `Skipped`, and `DepsAdded` match actual DB writes.
   - Avoid incrementing `DepsAdded` when `ON CONFLICT DO NOTHING` inserts no row.
   - Avoid overcounting creates if a concurrent or previously unseen row causes `ON CONFLICT DO NOTHING`.
   - Add summary fields for cross-prefix conflicts, skipped self-deps, skipped cycle deps, and skipped validation/warning lines where surfaced by CLI.

4. Add CLI-level tests where practical without a live DB.
   - Cover mode parsing helper if extracted.
   - Cover summary JSON shape through a small formatter helper.
   - Cover dry-run summary shape and ensure dry-run does not require/perform project registration in the command flow where practical.
   - Do not introduce broad mocking unless the command structure already supports it cleanly.

5. Run verification.
   - `go test ./cmd/bn`
   - `make test`
   - `make ci`
   - `go test -tags=integration ./...` only if Docker is available; otherwise report it was not run.

## Non-goals

- Do not import bd-only fields that bn has no storage for, such as `owner` and `close_reason`.
- Do not rewrite source IDs to match the destination prefix.
- Do not implement repo routing import unless a bd export source has a documented compatible shape for it.
- Do not change existing allowed state vocabulary beyond `open`, `in_progress`, `blocked`, `closed`, and `done`.
- Do not migrate the schema to make issue identity `(prefix, id)` scoped in this change; instead, detect and report cross-prefix ID conflicts.

## Subagent review outcomes

Two independent reviews found that the initial plan was not ready to implement. The revised contract above incorporates their blocking findings:

- Dry-run must not call `EnsureProject` or perform any DB writes.
- Global issue IDs mean cross-prefix ID collisions must be detected and reported rather than skipped ambiguously or merged into another project.
- Merge state behavior needs the explicit active/terminal truth table above.
- JSON summaries need stable metadata for dry-run and skipped-line counts.
- Import must not bypass dependency cycle protection.
- Row counts must come from actual DB effects, not pre-transaction assumptions.

## Review questions for subagents

1. Does this plan miss any bd compatibility detail visible from the current code?
2. Are the proposed count/idempotency semantics correct for create-only and merge?
3. Are there transaction or concurrency hazards in the proposed store fixes?
4. Is the test plan strong enough to prove `bn import` is ready?
