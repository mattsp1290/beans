# Hub file formats

Normative description of every file `bn` reads or writes. The `issue`
package implements it; its round-trip fixtures under
`issue/testdata/roundtrip/` are the executable form of this document.

## Issue file

Path: `projects/<project>/issues/<id>-<slug>.md`, or
`projects/<project>/archive/<YYYY>/<id>-<slug>.md` once archived. When the
slug is empty the filename is `<id>.md`.

```markdown
---
id: exampleA-a3f2
aliases: [exampleA-a3f2]
title: Migrate the live infra-host deploy
type: task
status: open
priority: 2
labels: [deploy, infra]
assignee: matt
parent: "[[exampleA-mkg1-deploy-bean-counter]]"
blocked_by:
  - "[[exampleA-ued1-schema-parity]]"
url: https://example.invalid/ticket/1
created: 2026-06-15T10:22:00Z
updated: 2026-09-10T08:01:00Z
---
Description paragraphs. Free markdown.

## Acceptance
- [ ] parity gate passes

## Log
- 2026-09-10T08:01:00Z matt (exampleA@a1b2c3d feature/x): status open → in_progress
- 2026-09-11T10:00:00Z claude: closed — parity gate passes
```

### Frontmatter

Keys `bn` owns: `id`, `aliases`, `title`, `type`, `status`, `priority`,
`labels`, `assignee`, `parent`, `blocked_by`, `url`, `created`, `updated`.
Required: `id`, `title`, `type`, `status`, `priority`, `created`, `updated`.
Optional keys are omitted when empty, except `aliases`, which always
contains at least the id. Every other key is user-owned and preserved
verbatim, in place.

- `priority` is an integer 0 (critical) to 4 (backlog). There is no unset value.
- Timestamps are RFC3339 in UTC with second precision and a `Z` suffix.
  `updated` is set on every `bn` mutation; hand edits need not touch it.
- Links (`parent`, `blocked_by`) are written as `"[[<target basename>]]"`,
  quoted so YAML does not read the brackets as a flow sequence. On read `bn`
  accepts `[[basename]]`, `[[basename|alias]]`, `[[basename#heading]]`, and a
  bare id. `blocked_by` accepts a single string or a sequence.
- `blocked_by` is written as a block sequence, one link per line, so a git
  diff is one line per edit. `labels` and `aliases` are written flow style.
- Files must use `\n` line endings. A file containing `\r\n` is rejected
  with an error naming the file.

### Body

- The description is the text between the frontmatter and the first line
  starting with `## ` (or the end of the file). `bn update --description`
  replaces exactly that span.
- `## Log` is the log section. `bn` creates it at the end of the file on the
  first append. When a hand-edited file has an H2 after `## Log`, the log
  section ends at that heading and the rest of the file is kept as is.
- Every other heading and its content is user-owned.

Log entries are list items:

```text
- <RFC3339 UTC> <actor>[ (<repo>@<short sha> <branch>)]: <event>
```

The actor, repo, and branch fields never contain whitespace: `bn` replaces
runs of whitespace with `-` when writing (so a git `user.name` such as
`Matt Spurlin` is logged as `Matt-Spurlin`), which keeps the line
unambiguous even when a branch name contains `)`. A line inside a fenced
code block never counts as a heading; when a fence is never closed, fences
are ignored so a real `## Log` is still found.

Events: `created`, `status <from> → <to>`, `note — <text>`, `closed — <reason>`,
`reopened (was <status>)`, `archived`, `field <name>: <old> → <new>` (title,
priority, type, assignee, parent), `field labels: + <label>` and
`field labels: - <label>`, `field description: updated` (the old and new text
are not repeated in the log), `blocked_by + <id>`, `blocked_by - <id>`.
Continuation lines of a multi-line event are indented by
two spaces. A list item that does not parse as a log line is kept verbatim.

### Ids and slugs

Ids are `<prefix>-<hash>`: the project's `prefix` and 4 characters from
`[a-z0-9]` drawn from `crypto/rand`. On collision with an existing id the
draw repeats; after 8 collisions the hash grows to 5 characters. The accepted
grammar for parsing (imports keep foreign ids) is
`^[a-z0-9][a-z0-9-]*-[a-z0-9]+(\.[0-9]+)*$`.

Slugs: lowercase the title, replace every run of characters outside
`[a-z0-9]` with `-`, trim `-`, cut at 60 characters without splitting a word
when possible. The slug is frozen at creation; `bn update --title` never
renames the file. A title with no `[a-z0-9]` characters gives an empty slug.

## Round-trip guarantee

`bn` never re-serializes a whole file. `issue.Parse` records the exact
frontmatter lines and the line span of each top-level key; `issue.Encode`
re-emits only the `bn`-owned keys whose values changed and splices them into
the original lines. The description and body are used as they stand, and new
log entries are appended after the last non-blank line of the log section.

Consequences:

- `Encode(Parse(x)) == x` byte for byte for every file `Parse` accepts,
  whatever YAML style it was written in (indentation, comments, blank lines,
  flow or block sequences, quoting).
- A mutation touches only the lines of the keys it changed. A key `bn`
  rewrites is emitted in `bn`'s style (two-space indent, quoted links); an
  inline `# comment` on a rewritten scalar is carried over. Comment lines
  between keys belong to the key that follows them and are never dropped.
- A new `bn`-owned key is inserted after the last `bn`-owned key, before any
  user-owned keys that follow.
- The log is append-only through `bn`; editing or removing entries is a hand
  edit.

The fallback the plan allowed (normalizing `bn`-owned scalars when
byte-identity proved impossible) was not needed.

## Memory file

Path: `memories/<key>.md` at hub level, or `projects/<project>/memories/`.

```markdown
---
key: bean-counter-prod-schema
type: project
tags: [deploy]
created: 2026-06-14T00:00:00Z
updated: 2026-09-10T00:00:00Z
---
body
```

`key` is required and matches `^[a-z0-9][a-z0-9-]*$` (at most 80
characters). `type` is one of `user`, `feedback`, `project`, `reference`, or
empty. The same splice rule applies: only changed keys are rewritten.

## Doc file

Any `.md` under `docs/` at hub or project level. Frontmatter is optional;
`bn` reads `title`, `aliases`, and `tags` when present and never rewrites a
doc. `bn doc new` copies `templates/doc.md` if present, else writes a title
line.

## Plan bundle

Project plans live in `projects/<project>/plans/<id>-<slug>/`. A bundle has a
required `plan.md` manifest and optional Markdown files below `sections/`.
The manifest owns `id`, `aliases`, `title`, `slug`, `status`, `created`,
`updated`, and an ordered `sections` list. Plan ids are
`<project-prefix>-plan-<random>`; the generated slug is frozen and aliases
include the id. Only listed, regular `sections/<name>.md` files are allowed.

`plan.md` contains exactly one `## Summary`, with `### Outcome`, `### Affected
areas`, `### Execution order`, `### Risks`, and `### Change graph` in that
order. Change graph is exactly one `bn-change-graph` YAML fence containing
version 1, nodes, and edges. Plans use `draft`, `blocked`, `ready`, and
`complete`; ready and complete plans require meaningful Summary content and
at least one graph node. Complete plans are immutable.

Use `bn plan init`, edit the local bundle, `bn plan validate`, and `bn plan
put` to publish. `bn plan get`, `list`, and `show` are read-only. The browser
only exposes plan browsing and graph inspection; it never mutates plans.

## Templates

`projects/<project>/templates/<type>.md`, then hub-level
`templates/<type>.md`, then the built-in default (embedded in the binary from
`issue/templates/`). A template is a body only, no frontmatter. Built-ins:

| type | body |
| --- | --- |
| task | `## Acceptance` with one empty checkbox |
| bug | `## Steps`, `## Expected`, `## Actual`, `## Acceptance` |
| feature | `## Motivation`, `## Acceptance` |
| epic | `## Goal`, `## Scope` |
| chore | empty |

## Configuration

See `docs/beans.toml.example` for the hub `beans.toml`, the project
`beans.toml`, and the per-user `~/.beans/config.toml`. Workflow precedence:
`BN_CONFIG` (explicit; missing file is an error) > project `[workflow]` >
hub `[workflow]` > built-in defaults, merged per key. Actor precedence:
`--actor` > `BN_ACTOR` > `~/.beans/config.toml` `actor` > `git config
user.name` > `USER`.
