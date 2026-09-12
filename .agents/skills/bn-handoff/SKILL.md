---
name: bn-handoff
description: Create, discover, verify, attach, or archive session-continuation handoffs in the Beans hub using bn. Use when work must be handed to another session or machine through Beans; do not create legacy handoff files under ~/.agents/projects/.
---

# Beans Handoffs

Treat the Beans hub as the canonical store for session handoffs. Handoffs are immutable snapshots: create a new one when context changes, and archive stale snapshots explicitly. Never edit or commit hub files by hand.

## Establish the repository, CLI, and hub

1. Resolve the source repository with `git rev-parse --show-toplevel`, read its applicable instructions, and inspect its branch, exact `HEAD`, remote, and worktree state.
2. Run `bn prime`, `bn handoff --help`, and `bn status --json`. Use one binary and the same global flags throughout the operation. Prefer JSON for machine-readable reads and mutation results.
3. If `handoff` is absent, locate a suitable current `bn` binary without replacing the user's global install. In the Beans source repository, building the checked-out revision with `go build -o bin/bn ./cmd/bn` is appropriate when that revision contains the command. If the capability exists only on another local branch, an isolated temporary Git worktree and temporary binary may be used; do not switch or disturb a dirty working tree. If no suitable implementation is available, preserve the handoff draft and report that publication is blocked.
4. Determine the intended project from repository resolution and user context. Use `--project <name>` when auto-detection is absent or ambiguous. If the configured hub is not at the CLI default, discover it from existing configuration or `bn status` and consistently pass `--hub <path>`; do not guess a remote or initialize a new hub without user authorization.

If the source repository has no Beans project and the user requested a canonical handoff, register the project before creating it:

```bash
bn --hub <hub> --json project create <project> --link
```

Run this from the source repository so `--link` records its normalized remote. Verify that the result reports `pushed: true`. Do not create a duplicate project merely because it contains no issues; confirm with `bn project list --json` or `bn status --json` first.

## Prepare the snapshot

Use a Markdown source file outside the hub, preferably in a `mktemp -d` staging directory. An existing legacy handoff may be used as input for migration, but do not create a new permanent `$HOME/.agents/projects/*/handoffs/` file.

Write concise continuation context supported by observed state. Include what another session needs to resume safely:

- repository remote, branch, exact commit, and relevant dependency revisions;
- outcome and material implementation decisions;
- completed validation with exact results, including the actual host/platform;
- dirty or generated state that must be preserved or cleaned;
- remaining gates, blockers, and exact continuation commands;
- warnings about temporary edits, uncommitted artifacts, external mutations, or stale binaries.

Use an H1 title. Do not include credentials, tokens, private keys, or copied environment secrets. Avoid claiming a gate passed when it was inferred rather than run.

## Create and verify

Create the snapshot through `bn`; the CLI adds owned frontmatter, commits the hub, and normally pushes it:

```bash
bn --hub <hub> --project <project> --json handoff create --file <handoff.md>
```

Pass `--issue <issue-id>` only when the governing issue is known. Otherwise leave the handoff unattached; it can later be linked with `bn handoff attach <handoff-id> <issue-id>`.

Record the returned `id`, `path`, `commit`, and `pushed` fields. A zero exit code alone does not prove remote publication. If `pushed` is false, inspect `bn status --json`, run `bn sync` once, and verify again; do not blindly repeat `handoff create`, because the first attempt may already have committed locally.

Verify the stored snapshot and hub state:

```bash
bn --hub <hub> --project <project> --json handoff show <handoff-id>
bn --hub <hub> --project <project> handoff list
bn --hub <hub> --json status
```

Confirm the stored body matches the intended source, the project/path are correct, and the hub is clean and synchronized. Report the actual handoff ID, hub-relative path, commit, push state, and portable lookup command.

## Continue or retire context

- Discover handoffs with `bn handoff list`; use `--issue`, `--archived`, `--older-than`, `--sort`, or `--limit` only after checking current help.
- Read a snapshot with `bn handoff show <id> --json` or `--raw`. Handoffs do not appear in `bn ready`.
- Associate or remove issue context with `bn handoff attach` and `bn handoff detach`.
- Before bulk archival, run `bn handoff archive --older-than <age> --dry-run`. Archive or restore only when requested or clearly part of the handoff-maintenance task.
