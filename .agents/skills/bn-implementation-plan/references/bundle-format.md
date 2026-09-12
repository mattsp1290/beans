# Native Beans bundle format

Use the scaffold from `bn plan init` as the authority for generated frontmatter. The installed CLI's validator is the final structural gate. In a Beans source checkout, consult `docs/format.md` and `plan/` when a schema detail is unclear.

## Files and identity

```text
bundle/
├── plan.md
└── sections/
    ├── 00-overview.md
    ├── 01-first-work-package.md
    └── 02-execution-handoff.md
```

Always include an overview, at least one work-package section, and an execution handoff. Scale the middle files to architectural or dependency boundaries. List every section exactly once in manifest frontmatter, in reading order:

```yaml
sections:
  - sections/00-overview.md
  - sections/01-first-work-package.md
  - sections/02-execution-handoff.md
```

Keep `id`, `aliases`, `slug`, `created`, and `updated` from the scaffold or retrieval. The slug is frozen; aliases include the plan ID. The CLI updates `updated` during publication and uses it to reject stale revisions. Edit lifecycle via `status` in the local manifest, not an invented `bn plan update` command.

Only `plan.md` and listed `sections/<name>.md` files are permitted. No nested section directories, symlinks, assets, review transcripts, or unrelated files. Files must be UTF-8 with LF line endings and a final newline, at most 512 KiB each and 2 MiB total. Use relative Markdown links within the bundle; source-code paths refer to the named source repository, not the hub directory.

## Manifest body

Preserve exactly one `## Summary` with these subsections in this order:

- `### Outcome`: meaningful prose describing the resulting behavior.
- `### Affected areas`: a list of affected components and interfaces.
- `### Execution order`: an ordered list of bounded work packages and gates.
- `### Risks`: a list of concrete risks and mitigations. A blocked plan requires an item starting `- BLOCKER:` with owner and unblock action.
- `### Change graph`: exactly one `bn-change-graph` YAML fence, with no surrounding prose inside that subsection.

A ready plan needs all of those meaningful sections, at least one graph node, and no `<!-- bn:todo -->` scaffold markers. Remove placeholders from the detailed sections too. The validator permits incomplete draft content; that does not make it ready.

Example graph syntax (replace with the actual change model):

```bn-change-graph
version: 1
nodes:
  - id: contract
    label: Define the data contract
    kind: interface
  - id: consumer
    label: Integrate the consumer
    kind: component
edges:
  - from: contract
    to: consumer
    kind: precedes
```

Node fields are `id`, `label`, `kind`, and optional `ref`. IDs match `^[a-z][a-z0-9-]{0,63}$`; labels are nonempty, single-line, and at most 120 characters. Kinds are `artifact`, `component`, `interface`, `data`, `workflow`, or `external`.

Edge fields are `from`, `to`, `kind`, and optional `label`. Both endpoints must exist. Kinds are `precedes`, `affects`, `enables`, `produces`, `replaces`, or `contains`. Do not use invented kinds such as `depends_on`. Use prerequisite-to-dependent direction for `precedes` and check sequencing for cycles yourself. Maximums are 200 nodes and 400 edges. Unknown fields, duplicate nodes or identical edges, YAML aliases, and custom tags are invalid.

`ref` can be generic metadata or a real Beans issue binding. Omit it when no real reference exists. Execution state derived from linked issues is separate from the authored plan lifecycle; graph sequencing alone does not schedule or block issues.

## Overview

Include the source repository and observed revision, inferred change type, requested outcome, measurable success criteria, scope and non-goals, repository evidence, design decisions and relevant alternatives, a clear change model, assumptions, risks, and unresolved decisions. State that implementation has not occurred. Include a document map and any external dependency/request map.

Under `## Application context`, include exactly one fenced JSON block tagged `implementation-plan`:

```implementation-plan
{
  "version": 1,
  "active_users": false,
  "backward_compatibility_required": false,
  "feature_flags": "not-applicable",
  "confirmed_at": "2026-09-12T01:35:16Z",
  "confirmation_digest": "<lowercase-sha256>"
}
```

Only those six keys are allowed. The two user answers are JSON booleans. `feature_flags` is one of `appropriate`, `not-appropriate`, `decide-per-pr`, or `not-applicable`; the last value is valid only when both booleans are false. `confirmed_at` is an RFC 3339 timestamp with a timezone. Compute `confirmation_digest` by removing that field, serializing the remaining object as UTF-8 canonical JSON with keys sorted and separators `,` and `:` (no insignificant whitespace), then taking lowercase SHA-256. Explicitly identify missing answers and their owner in prose, but never fabricate the block: a missing answer blocks `ready` status.

## Work packages

Each package specifies its goal, prerequisites, relevant source evidence, exact paths and symbols, intended behavior and invariants, error and lifecycle paths, compatibility surfaces, tests, observable acceptance criteria, dependencies, risks, and exclusions. Label nonexistent paths or symbols `new` or `proposed`, anchored to existing parents or insertion points. Use small diagrams or pseudocode only when they remove ambiguity.

## Execution handoff

Give dependency-ordered packages, concrete change surfaces, prerequisites and parallelization constraints, per-package verification commands, integration and regression gates, rollback requirements where relevant, and a final definition of done. Identify deferred work without presenting it as already filed or authorized. Keep this order consistent with the manifest and graph. Store execution progress in Beans issues, not a competing Markdown task tracker.

End the handoff with exactly one `bn-execution-map` YAML fence. Its version-1 shape is:

```bn-execution-map
version: 1
packages:
  - id: package-id
    node_id: graph-node-id
    source: sections/01-package.md
    source_digest: <lowercase-sha256>
    prerequisites: []
    paths: [repository/relative/path]
    validation: [exact command or discovery rule]
    acceptance: [observable result]
    exclusions: [bounded non-goal]
references:
  - node_id: context-only-node
    reason: architecture context only
```

Only the shown keys are allowed. Package and node IDs are unique and match the graph ID grammar. Every graph node appears exactly once in `packages` or `references`. A package source is one listed implementation-package section, never the overview or handoff. Normalize its UTF-8 bytes to LF, remove trailing spaces and tabs from each line, ensure one final newline, and hash those bytes for `source_digest`. Package prerequisites name package IDs, form a DAG, and match prerequisite-to-dependent graph `precedes` edges. Lists may be empty only where the package genuinely has no prerequisite; paths, validation, acceptance, and exclusions otherwise contain nonempty strings. Reference entries contain only `node_id` and a nonempty reason. An issue-resolving graph ref must be executable, never reference-only.

For semantic drift checks, canonicalize the validated application-context object without its digest, the execution map, and the change graph as sorted-key compact JSON, then combine those values with the normalized bytes of each package source in map order. Before canonicalizing the graph, exclude node `ref` values. Hash every component as its eight-byte big-endian length followed by its bytes using SHA-256. CLI-owned manifest `updated` and lifecycle `status` are excluded. Thus link, revision, and lifecycle-only changes are stable while graph semantics, executable prose, or mapping changes are not.
