## Action Items

### Critical

_None._

### Important

- [ ] [commit `faa3de830dab` message / `apps/bean-counter/scripts/deploy-production.sh:require_repo_root`] Correct the mutation-coverage claim. The loop has **nine** trusted paths, not six, and swapping `libs/beans`, `libs/beans/schema`, `libs/beans/schema/migrations` or `apps/bean-counter` out of it fails **zero** tests (measured; the other five entries each fail exactly one, and deleting the loop fails six). Record the correction in the merge commit, or add cases so the claim becomes true. The control itself is sound — the four entries are redundant with deeper ones because `require_in_repo` resolves physically — so this is a message defect, not a behavior defect.
- [ ] [`.gitignore` / `.agents/plans/monorepo-consolidation/03-go-module-restructure.md:149,225` / `00-overview.md:249`] Decide `go.work.sum`'s status and make the tree and the plan agree. It is currently neither tracked nor ignored, so a workspace-mode `go list -m all` leaves an untracked 3215-byte file that trips `require_clean_local_ref` (`apps/bean-counter/scripts/deploy-production.sh:536`) and aborts `--check` and live deploys. Preferred fix: add an anchored `/go.work.sum` to `.gitignore` (matching the `/bn` precedent already in that file) and retract the three plan statements that say it is tracked. `go work sync` does not generate it, so committing it is the higher-maintenance option.

### Suggestions

- [ ] [Beads issue `bean-counter-log`] Repoint or fold in: it still says "Clone bean-counter to infra-admin@10.0.0.106:~/git/bean-counter", contradicting `beans-nlc`, which exists to move that checkout to `$HOME/git/beans` and which already found the clone present on the host. Two open blockers on `bean-counter-m0p` currently describe opposite remote layouts.
- [ ] [`.agents/plans/monorepo-consolidation/IMPLEMENTATION-NOTES.md`, "Stale acceptance criteria"] Extend the existing `go.work.sum` retraction to cover `03-go-module-restructure.md:149`, its AC3 at `:225`, and the layout listing at `00-overview.md:249`; today only doc 01's AC2 is retracted.
- [ ] [`apps/bean-counter/scripts/deploy-production.sh`, `require_repo_root` loop comment] Note that the four ancestor entries are deliberately redundant — `require_in_repo` resolves directories with `cd … && pwd -P`, so the deepest entry already catches an out-of-tree symlink at any ancestor — so the next reader does not conclude each entry is independently load-bearing.
- [ ] [`setup-beads.sh`, `setup-multi-repo-beads.sh`, `apps/bean-counter/deploy/README.md`] State the `shellcheck` gate's scope. `shellcheck` over the deploy pair is clean; over all tracked `*.sh` it exits 1 with 104 info-level findings (identical counts on `main`, so not a branch regression). Either add `disable=` headers to the two setup scripts or say in one place that the gate covers the deploy pair only.
- [ ] [`.github/workflows/ci-workspace.yml`, final step] Add `fmt-check` to the fan-out loop (it checks 8 of the root Makefile's 11 targets). `fmt-check` is the one target that deliberately does not fan out, so it is the most likely to break silently when `libs/beans` grows one.

---

## Merge verdict: APPROVE

### What the merge commit should record

`main` is an ancestor of `faa3de8`, so a `--no-ff` merge commit is both possible and the
right vehicle. It should state:

1. **What landed.** A single-module repo becomes a monorepo: `libs/beans`
   (`github.com/mattsp1290/beans/libs/beans`) and `apps/bean-counter`
   (`github.com/mattsp1290/beans/apps/bean-counter`), the app depending on the library
   through `replace … => ../../libs/beans` with a tracked `go.work` for convenience; one
   Beads tracker at the root (170 issues, 17 open); three path-scoped workflows; 284 files
   changed, +22807/-422 over 129 commits, of which 114 are bean-counter's imported history.
2. **That the import is a graft, not a copy.** `bfe40cc` has 114 commits on its second
   parent, rooted at bean-counter's own `dd88be7`; two root commits are reachable from HEAD.
3. **The correction to `faa3de8`.** Its claim that "every path in the list is now
   individually mutation-covered: swapping any one of them for a harmless path fails exactly
   one case" is false — the loop has nine entries and four of them (`libs/beans`,
   `libs/beans/schema`, `libs/beans/schema/migrations`, `apps/bean-counter`) fail zero cases
   when swapped out, because `require_in_repo` resolves physically and deeper entries catch
   the same symlink. "Deleting the loop fails six" is correct. No behavior change follows;
   the record does. This is the same correct-forward handling `faa3de8` itself applied to
   `40d8e64`'s "from 7 to 11".
4. **What is not verified and why.** `docker build` (both images), the build-stage `go.work`
   probe, and `make ci-integration` were not runnable — the local Docker engine will not
   start. The `images` and `integration` jobs cover all four on every PR, and `beans-oba`
   tracks the local re-run.
5. **What blocks the first deploy.** `beans-ued` (embedded schema 11 vs prod 8 aborts the
   parity gate) and `beans-nlc` (the infra host is still checked out at
   `$HOME/git/bean-counter`). Merging this branch does not deploy anything, and both are
   deliberate open blockers on `bean-counter-m0p`.
6. **`go.work.sum`**, if action item 2 is not taken before merge — that it is untracked and
   un-ignored, and that `go list -m all` will dirty the worktree until that is settled.

### Would a squash lose anything? Yes — do not squash

A squash merge collapses all 129 commits into one, which destroys:

- **Success criterion 4** — `git log --follow -- libs/beans/store/store.go` would show one
  commit instead of 41 reaching `39d689c` (2026-06-13).
- **Success criterion 5** — `git log -- apps/bean-counter/internal/server/app.go` would show
  one commit instead of 5 reaching `433d900` (2026-06-14).
- **The entire point of the `git filter-repo` import.** 114 commits of bean-counter history
  and its root commit `dd88be7` disappear; the plan's history-migration work package
  produces nothing.

**Rebase-and-merge is also unsafe** here: it drops merge commits and replays the rest onto
`main` with new SHAs, which would linearize the graft, discard the second root commit, and
rewrite the authorship dates of 114 imported commits.

**Merge with `--no-ff`** (or a GitHub "Create a merge commit"). A plain fast-forward would
preserve the history correctly but leaves nowhere to record items 3–6 above, so the explicit
merge commit is worth the extra node.
