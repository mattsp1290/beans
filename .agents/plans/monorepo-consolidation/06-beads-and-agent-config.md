# 06 — Beads tracker and agent configuration

Goal: one Beads tracker at the repository root holding both projects' issues, and one
set of root-level agent instruction files that describes the monorepo.

Prerequisite state: [02-history-migration.md](02-history-migration.md) complete.
`apps/bean-counter/.beads/`, `apps/bean-counter/.gitignore`, `apps/bean-counter/AGENTS.md`,
`apps/bean-counter/CLAUDE.md`, and `apps/bean-counter/.claude/settings.json` still exist
because their content has not been merged yet.

## Existing tracker state

| | beans | bean-counter |
| --- | --- | --- |
| Dolt database | `beans` | `bean_counter` |
| Mode | embedded | embedded |
| Project ID | `3adba443-af86-4b61-92b0-f8803dc0c86c` | `7349a2e4-55a6-4120-9f34-42abd457cc9c` |
| Dolt remote | `git+ssh://git@github.com/mattsp1290/beans.git` | `git+ssh://git@github.com/mattsp1290/bean-counter.git` |
| Issue prefix | `beans-` | `bean-counter-` |
| Total issues at planning time | 96 | 54 |
| Open at planning time | 0 | 7 |
| Blocked at planning time | 0 | 2 |

Both `.beads/config.yaml` files are the unmodified bd template with every setting
commented out. They are functionally identical, so the root file needs no merge.

The prefixes are disjoint, so `bd import` — which upserts by issue ID — cannot collide.
Imported issues keep their `bean-counter-` IDs. That is desirable: existing plans,
commit messages, and the deploy documents reference those IDs.

## Step 1 — re-measure

Counts move. Take the authoritative measurement immediately before the export, not from
this document. Run `bd` commands serially; never put two in one parallel batch.

```bash
cd "$HOME/git/bean-counter" && bd stats
cd "$HOME/git/bean-counter" && bd list --status=open
cd "$HOME/git/bean-counter" && bd dep tree > /tmp/bc-dep-tree-before.txt
cd "$HOME/git/beans"        && bd stats
```

Record the four `bd stats` numbers for bean-counter (total, open, blocked, closed) and
keep `/tmp/bc-dep-tree-before.txt`. Gate G2 compares against them.

Treat `bd` output as authoritative. Do not read `.beads/issues.jsonl` to determine
status; in embedded Dolt mode that file can lag the live database.

## Step 2 — export

```bash
cd "$HOME/git/bean-counter"
bd export "$HOME/git/beans/.agents/plans/monorepo-consolidation/bean-counter-issues.jsonl"
wc -l "$HOME/git/beans/.agents/plans/monorepo-consolidation/bean-counter-issues.jsonl"
```

The export is written into the plan directory and committed. It is the archive artifact
that makes the tracker migration reversible from git alone, and it is the input to
step 3. `bd export` round-trips both issues and memory records.

Confirm the exact `bd export` argument form before running it. If this bd build writes
to `.beads/issues.jsonl` by default and takes an output path differently, use the form
that build supports; the requirement is a JSONL file at the path above, not a particular
flag spelling.

## Step 3 — import

```bash
cd "$HOME/git/beans"
bd import .agents/plans/monorepo-consolidation/bean-counter-issues.jsonl --dry-run
```

Read the dry-run output. It must report creates, not updates — every `bean-counter-*` ID
is new to the `beans` database. Any reported update means an ID collision that this plan
did not anticipate; stop and investigate before proceeding.

```bash
cd "$HOME/git/beans"
bd import .agents/plans/monorepo-consolidation/bean-counter-issues.jsonl
```

## G2 — import verification gate

```bash
cd "$HOME/git/beans"
bd stats
bd list --status=open
bd show bean-counter-m0p
bd show bean-counter-mkg
bd blocked
bd dep tree > /tmp/monorepo-dep-tree-after.txt
```

All must hold:

1. Total issues equal the beans total plus the bean-counter total from step 1.
2. Open issues equal the beans open count plus the bean-counter open count.
3. Blocked issues equal the sum of the two blocked counts.
4. `bd show bean-counter-m0p` resolves and shows its original title, "First live
   production deploy of bean-counter".
5. Every dependency edge present in `/tmp/bc-dep-tree-before.txt` appears in
   `/tmp/monorepo-dep-tree-after.txt`. `bean-counter-mkg` is an epic with children, and
   two issues were blocked before the import, so edges exist to check.
6. `bd memories` returns at least the memories that existed in the beans database
   before the import, plus any that bean-counter's export carried.

If any check fails, the import is incomplete. Do not proceed to step 4. The bean-counter
Dolt database is still intact at this point and is the recovery source.

## Step 4 — retire the bean-counter tracker

Only after G2 passes.

```bash
cd "$HOME/git/beans"
git rm -r apps/bean-counter/.beads
rm -rf apps/bean-counter/.beads
git commit -m "Retire the bean-counter Beads tracker; issues live in the root tracker"

cd "$HOME/git/beans" && bd dolt push
```

The `bean_counter` Dolt database itself lives in `apps/bean-counter/.beads/embeddeddolt/`,
which is gitignored, so `rm -rf` is what actually removes it. The G1 backup in
[02-history-migration.md](02-history-migration.md) holds a copy.

The bean-counter Dolt `git+ssh` remote is retired with the directory. No action is needed
against `github.com/mattsp1290/bean-counter` itself; that repository is left untouched
pending gate G3.

## Root `.gitignore`

Start from the beans root `.gitignore` and apply four changes:

| Change | Reason |
| --- | --- |
| Delete the `go.work` and `go.work.sum` lines | The workspace is now tracked. Already required by [03-go-module-restructure.md](03-go-module-restructure.md) step 1. |
| Add `/bn` | The stray root binary described in [01-target-layout-and-module-graph.md](01-target-layout-and-module-graph.md) must not come back. The leading slash anchors it to the root so it cannot shadow a future `bn` directory or file inside a module. |
| Add `node_modules/` | bean-counter's frontend needs it. The bean-counter root `.gitignore` did not have it; `frontend/.gitignore` did. Adding it at the root is harmless and covers future application frontends. |
| Do **not** add `.agents/reviews/` | bean-counter ignored it; beans tracks it and has committed review files under it. Adopting bean-counter's rule would leave tracked files permanently modified-but-ignored. |

Everything else in the beans file — `bin/`, `*.test`, `*.out`, `coverage.*`, `.env`,
`.dolt/`, `*.db`, `.beads-credential-key`, the compiled-object patterns — already covers
both projects.

Then delete `apps/bean-counter/.gitignore`. Keep `apps/bean-counter/frontend/.gitignore`;
it is frontend-scoped and correct where it is.

Verify after the merge:

```bash
cd "$HOME/git/beans"
git status --porcelain          # must print nothing
git check-ignore -v go.work     # must exit non-zero: go.work is NOT ignored
git check-ignore -v bn          # must exit zero
```

## Root `AGENTS.md`

Both files exist and both already carry the bd-generated
`<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:ca08a54f -->` block. The hash is
identical in both, so that block is byte-identical and must appear exactly once in the
merged file. Duplicating it would break the bd tooling that rewrites the block by
marker.

Merged structure:

```text
# Agent Instructions

## Repository layout            (new: the monorepo map and where work belongs)

## Commands                     (new: root make targets, then per-module targets)

## Non-Interactive Shell Commands
    (beans and bean-counter say the same thing; keep the longer bean-counter
     version, which enumerates apt-get and brew as well)

## libs/beans                   (from beans AGENTS.md: the Workflow Configuration
                                 section on model.WorkflowConfig, BN_CONFIG, bn.toml,
                                 and the ready_for_* hold states)

## apps/bean-counter            (anything bean-counter-specific not already covered)

<!-- BEGIN BEADS INTEGRATION ... -->   (exactly one copy, unmodified)
...
<!-- END BEADS INTEGRATION -->
```

The beans "Workflow Configuration" section describes `model.WorkflowConfig`, the
`BN_CONFIG` environment variable, `bn.toml` / `bn.yaml` / `$XDG_CONFIG_HOME/bn/config.*`,
and the `ready_for_review` / `ready_for_validation` / `ready_for_merge` hold states. It
references `docs/bn.toml.example`, which moves to `libs/beans/docs/bn.toml.example`.
Update that path when moving the section.

Delete `apps/bean-counter/AGENTS.md` after the merge. A single root file is what agents
read; two files with overlapping bd sections invite drift.

## Root `CLAUDE.md`

Same treatment. The beans root `CLAUDE.md` is the base. Its "Build & Test",
"Architecture Overview", and "Conventions & Patterns" sections are currently empty
placeholders — fill them from this plan's document map and the merged `AGENTS.md`
commands rather than leaving the placeholder text.

Delete `apps/bean-counter/CLAUDE.md` after the merge.

## `.claude/settings.json`

`apps/bean-counter/.claude/settings.json` is tracked; the beans repository's `.claude/`
has no tracked `settings.json`. Read both, merge any permission entries and environment
settings that are still relevant into the root `.claude/settings.json`, then delete the
bean-counter copy.

Discard entries that name paths from the old layout. A permission allowing a command in
`frontend/` is stale once the directory is `apps/bean-counter/frontend/`.

## `setup-beads.sh`

The root file is the beans variant, and it stays. `apps/bean-counter/setup-beads.sh`
differs from it and is deleted in
[02-history-migration.md](02-history-migration.md) step 5. Before that deletion, diff the
two files. If the bean-counter variant contains behavior the root variant lacks, port it
into the root script in the same commit. Do not drop it silently.

`setup-multi-repo-beads.sh` is beans-only and is unchanged.

## Acceptance criteria

1. `bd stats` at the repository root satisfies G2 checks 1 through 3.
2. `bd show bean-counter-m0p` resolves at the repository root.
3. `bd ready` at the repository root returns bean-counter's ready issues.
4. `apps/bean-counter/.beads/` does not exist, tracked or untracked.
5. `.agents/plans/monorepo-consolidation/bean-counter-issues.jsonl` is committed.
6. Exactly one `AGENTS.md` and one `CLAUDE.md` exist in the repository, both at the root.
7. Exactly one `<!-- BEGIN BEADS INTEGRATION` marker exists in `AGENTS.md`.
8. `git check-ignore go.work` exits non-zero and `git check-ignore bn` exits zero.
9. `git status --porcelain` is empty.

## Risks

- **Import loses dependency edges.** G2 check 5 is the defense. The exported JSONL stays
  committed, so a re-import is always possible.
- **The bd integration block is duplicated in `AGENTS.md`.** bd rewrites that block by
  marker; two copies produce undefined behavior on the next `bd` update. Acceptance
  criterion 7 checks for it.
- **`bd export`'s output path flag differs in this bd build.** Step 2 says to confirm the
  form rather than assume it.

## Exclusions

- No issue is closed, reopened, retitled, or reprioritized by this work package. The
  import is a move, not a triage pass.
- The `github.com/mattsp1290/bean-counter` GitHub repository is not archived, made
  read-only, or deleted. That is gate G3 and needs user approval.
