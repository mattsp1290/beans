# 02 — History migration

Goal: bring the full commit history of `github.com/mattsp1290/bean-counter` into the
beans repository so that `git log -- apps/bean-counter/<path>` returns pre-migration
commits at that path.

Prerequisite state: both working trees clean, both Dolt databases pushed, backups taken
(gate G1 below).

## G1 — pre-migration gate

Run every step. Do not proceed past a failure.

```bash
# 1. Both worktrees clean. The beans root binary must be gone first.
rm -f "$HOME/git/beans/bn"
cd "$HOME/git/beans"        && git status --porcelain   # must print nothing
cd "$HOME/git/bean-counter" && git status --porcelain   # must print nothing

# 2. Both git remotes up to date.
cd "$HOME/git/beans"        && git pull --rebase && git push && git status -sb
cd "$HOME/git/bean-counter" && git pull --rebase && git push && git status -sb

# 3. Both Dolt databases pushed. Run these serially, never in parallel.
cd "$HOME/git/beans"        && bd dolt push
cd "$HOME/git/bean-counter" && bd dolt push

# 4. Full filesystem backup of both repositories, including .beads and .git.
BK="$HOME/backups/monorepo-consolidation-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$BK"
cp -rf "$HOME/git/beans"        "$BK/beans"
cp -rf "$HOME/git/bean-counter" "$BK/bean-counter"
echo "$BK"
```

Record the backup directory path. Every later work package can be reverted by restoring
from it.

`bd dolt push` writes to the Dolt `git+ssh` remote, which is a different transport from
`git push`. Both are required. Per the repository's Beads conventions, run `bd` commands
serially and retry a bounded number of times on an embedded-Dolt exclusive-lock error
rather than working around it.

## Step 1 — install `git-filter-repo`

`git-filter-repo` is not installed on this machine. `git subtree` is.

```bash
brew install git-filter-repo
command -v git-filter-repo   # must print a path
```

If installation is not possible, use the fallback in the last section of this file and
record in the migration commit message that history paths were not rewritten.

## Step 2 — rewrite a scratch clone

Never run `git filter-repo` inside `$HOME/git/bean-counter`. filter-repo removes the
`origin` remote and rewrites every object in place. Work on a throwaway clone.

```bash
SCRATCH="$(mktemp -d)/bean-counter-rewrite"
git clone "$HOME/git/bean-counter" "$SCRATCH"
cd "$SCRATCH"
git log --oneline | wc -l          # record N_before

git filter-repo --to-subdirectory-filter apps/bean-counter --force

git log --oneline | wc -l          # must equal N_before
git ls-files | grep -cv '^apps/bean-counter/'   # must print 0
```

`--to-subdirectory-filter` moves every path in every commit under
`apps/bean-counter/`. Commit count, authorship, and dates are preserved; commit SHAs
change. SHA changes are acceptable because the user confirmed no external consumers.

The rewrite also relocates `.beads/`, `.github/`, `.agents/`, `LICENSE`, and the root
dotfiles into `apps/bean-counter/`. That is expected. WP3 removes or hoists them per
[01-target-layout-and-module-graph.md](01-target-layout-and-module-graph.md).

`bean-counter:.beads/` contains only tracked config (`config.yaml`, `metadata.json`,
`README.md`, `.gitignore`); the Dolt database itself is gitignored and is not carried by
the rewrite. The tracker data is migrated separately in WP8 through a JSONL export.

## Step 3 — merge into the beans repository

```bash
cd "$HOME/git/beans"
git checkout -b monorepo-consolidation

git remote add bean-counter-rewrite "$SCRATCH"
git fetch bean-counter-rewrite
git merge --allow-unrelated-histories \
  -m "Import bean-counter into apps/bean-counter with history" \
  bean-counter-rewrite/main
git remote remove bean-counter-rewrite
```

The merge cannot conflict at this point: the beans tree has no `apps/` directory and the
rewritten tree has nothing outside `apps/bean-counter/`. The beans side has not moved
yet — that happens in WP3, after this merge, so the two trees are disjoint here.

Verify immediately:

```bash
git log --oneline -- apps/bean-counter/internal/server/app.go | tail -3
# must show commits authored in the bean-counter repository, e.g. the deploy work

git log --oneline | wc -l
# must be >= (beans commit count + N_before)
```

## Step 4 — move the beans side

This is a separate commit on the same branch, after the merge, so that the merge stays
reviewable on its own.

```bash
cd "$HOME/git/beans"
mkdir -p libs/beans

git mv go.mod go.sum deps.go .golangci.yml Makefile README.md libs/beans/
git mv cmd model repo schema store version docs libs/beans/

git commit -m "Move beans library and bn CLI under libs/beans"
```

Rename detection is what preserves history here, and it works because the file contents
are unchanged in this commit. Do not combine this commit with the import-path rewrite in
[03-go-module-restructure.md](03-go-module-restructure.md); editing contents in the same
commit as a rename weakens rename detection and makes `git log --follow` unreliable.

Verify:

```bash
git log --follow --oneline -- libs/beans/store/store.go | tail -3
# must show pre-migration commits

git show --stat HEAD | head -20
# every line must be a rename (R###), not an add/delete pair
```

If `git show --stat` reports adds and deletes instead of renames, the commit combined
content changes with moves. Reset and redo the move alone.

## Step 5 — remove and hoist imported files

Still on `monorepo-consolidation`, in its own commit:

```bash
cd "$HOME/git/beans"

# Compare before deleting; port any unique logic into the root script.
diff apps/bean-counter/setup-beads.sh setup-beads.sh || true

mkdir -p .agents/plans
git mv apps/bean-counter/.agents/plans/beans-0-1-1 .agents/plans/bean-counter-beans-0-1-1
git mv apps/bean-counter/.agents/plans/deploy      .agents/plans/bean-counter-deploy

git rm -r --cached apps/bean-counter/.github
rm -rf apps/bean-counter/.github
git rm apps/bean-counter/LICENSE apps/bean-counter/setup-beads.sh

git commit -m "Hoist bean-counter plans and drop duplicated root-level files"
```

`apps/bean-counter/.beads/`, `.claude/settings.json`, `.gitignore`, `AGENTS.md`, and
`CLAUDE.md` are intentionally left in place at this step. They are removed in WP8 and
WP9 after their content is merged into the root equivalents. Deleting them here would
lose content that has not been merged yet.

## Fallback — `git subtree`

Use this only if `git-filter-repo` cannot be installed.

```bash
cd "$HOME/git/beans"
git checkout -b monorepo-consolidation
git subtree add --prefix=apps/bean-counter "$HOME/git/bean-counter" main
```

Trade-off, which must be recorded in the commit message if this path is taken: the
imported commits keep their original root-relative paths. `git log -- apps/bean-counter/...`
returns only post-migration commits. `git log --follow -- apps/bean-counter/<one-file>`
still traverses the rename for a single file. Success criterion 5 in
[00-overview.md](00-overview.md) relaxes accordingly, and the criterion's `git log`
invocation gains `--follow` and targets one file.

Steps 4 and 5 are unchanged by this fallback.

## Acceptance criteria

1. `git log --oneline -- apps/bean-counter/internal/server/app.go` returns at least one
   commit authored before the merge commit. Under the fallback, the equivalent
   `--follow` form does.
2. `git log --follow --oneline -- libs/beans/store/store.go` returns at least one commit
   authored before the move commit.
3. `git show --stat` on the move commit reports renames only.
4. The repository has no remote named `bean-counter-rewrite`.
5. `$HOME/git/bean-counter` is byte-identical to its state at G1. filter-repo was never
   run there.
6. The backup directory recorded at G1 exists and contains both repositories.

## Exclusions

- This work package does not edit any file's contents. Every content change belongs to
  a later work package.
- This work package does not touch either Dolt database beyond the G1 push.
- This work package does not delete or archive the bean-counter GitHub repository. That
  is gate G3 and requires user approval.
