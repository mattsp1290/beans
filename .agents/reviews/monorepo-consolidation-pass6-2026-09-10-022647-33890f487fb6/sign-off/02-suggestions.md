# Suggestions

None of these block the merge. Three of the four are consequences of round 5's
`go.work.sum` fix that were not swept up with it, and all are cheap.

## S1 — `33890f4`'s "60/60" should read "62/62"

**Where:** commit message of `33890f4`, the paragraph beginning "A correction to
the previous commit message."

The four redundant containment entries survive removal at **62/62**, not 60/60.
The suite grew from 60 to 62 tests in this very commit, and the figure was
carried over from the pre-fix measurement. The finding it supports is correct —
I reproduced it exactly (see `01`) — so this is arithmetic, not an over-claim.

It cannot be fixed in place without rewriting a published commit. The right
remedy is to state the correct number in the merge commit body, which the
proposed text in `03-positive-notes.md` does. Worth doing, because the whole
point of that paragraph is that a future reader can re-derive the claim, and
they cannot re-derive 60/60.

## S2 — `ci-workspace.yml`'s comment now describes a check it no longer performs

**Where:** `.github/workflows/ci-workspace.yml:39-42`

```yaml
# git diff only sees tracked files, and `go work sync` can create a
# go.work.sum where none was tracked, so the porcelain check is what
# catches that case.
- name: Workspace is in sync
  run: |
    go work sync
    git diff --exit-code
    test -z "$(git status --porcelain)" || { git status --porcelain; exit 1; }
```

Since round 5 added an anchored `/go.work.sum` to `.gitignore`, `git status
--porcelain` can no longer see a `go.work.sum` at all — I confirmed this
directly (`git check-ignore -v go.work.sum` → `.gitignore:33`). So the porcelain
check specifically does **not** catch the case the comment says it catches.

This is not a correctness problem. Ignoring the file is the deliberate decision,
`go work sync` provably generates none for this workspace, and the step retains
real value for every *other* untracked artefact a sync might leave. But the
comment asserts a guarantee that the tree no longer provides, and stale comments
about safety gates are exactly what this branch has spent five rounds
eliminating. Reword to say the porcelain check covers untracked artefacts
generally, and that `go.work.sum` is deliberately ignored — cross-referencing
`IMPLEMENTATION-NOTES.md`'s "`go.work.sum` is ignored, not tracked" section.

## S3 — Two workflows still trigger on a path that can never be committed

**Where:** `.github/workflows/ci-libs-beans.yml:10,17` and
`.github/workflows/ci-apps-bean-counter.yml:12,23`

Both list `'go.work.sum'` among their `paths:` filters. That file is now
gitignored and will never appear in a diff, so those four entries are dead. They
are harmless — a filter that never matches simply never fires — but they imply a
tracked file that a reader will not find, and they were plainly written when the
plan still called for tracking it. Drop them, or leave them with a one-line
comment saying why they are vestigial.

## S4 — Three broken relative links inside committed review records

**Where:**
- `.agents/reviews/monorepo-consolidation-2026-09-10-005745-40d8e6475e51/build-and-deploy-integrity/02-suggestions.md` → `../.agents/plans/deploy/` and `../../../.agents/plans/bean-counter-deploy/`
- `.agents/reviews/monorepo-consolidation-pass3-2026-09-10-013439-5bd9e07513ec/regression-auditor/01-critical-and-important.md` → `../.agents/plans/deploy/`

`.agents/reviews/` is tracked (244 files), so these ship with the repo. All 107
relative links in the 89 *delivered* markdown files resolve — the delivered
documentation is clean, which is what criterion-level link health means here.
These three are inside point-in-time review artefacts added by `e3875a8`.

The other 22 "broken links" my checker reported are false positives: they are
the mandated `[File:line]` action-item markers from earlier bead-swarm reviews,
which markdown happens to parse as links. Those need no action ever.

Lowest priority on this list. Fixing point-in-time records is arguably wrong;
the reason to mention it at all is that if anyone ever adds a repo-wide link
checker, it will need to exclude `.agents/reviews/` or it will fail on day one.
