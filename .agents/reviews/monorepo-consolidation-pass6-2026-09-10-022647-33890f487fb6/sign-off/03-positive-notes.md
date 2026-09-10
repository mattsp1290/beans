# Positive notes

## `IMPLEMENTATION-NOTES.md` is still accurate

I checked it clause by clause against the tree rather than reading it for
plausibility. It holds, including the new section.

**The new `go.work.sum` section** makes four factual claims and all four
reproduce exactly:

| Claim | Measured |
|---|---|
| "`go work sync` generates no `go.work.sum` for this workspace" | Deleted the file, ran `go work sync` → not regenerated. **Exact.** |
| "`go list -m all` writes a 3.2 KB `go.work.sum`" | 3215 bytes, byte-identical to the file already present (`cmp` clean). **Exact.** |
| "it then appeared in `git status --porcelain`" | Reproduced the mechanism: with the file present and the ignore rule the only thing standing between it and `git status`, `git check-ignore -v` names `.gitignore:33:/go.work.sum` as the sole reason status stays empty. **Exact.** |
| "The root `.gitignore` now carries an anchored `/go.work.sum`. `go.work` itself stays tracked." | `git ls-files --error-unmatch`: `go.work` tracked, `go.work.sum` not. **Exact.** |

The section is also honest about *why* the branch missed it — "`--dry-run` does
not call that gate, which is why no check on this branch caught it" — which is
the kind of admission that makes a notes file worth reading.

**The rest of the file** also holds. Spot-checks: the deploy dry-run reports
`embedded_max: 11`, matching the "deploy parity gate inverts" section and
`beans-ued`; the corrected criterion-9 invocation
(`./apps/bean-counter/scripts/deploy-production.sh --ref main --dry-run`) is the
one that works, and the plan's original spelling does not exist; criterion 7's
"both hold, but the numbers moved" is right (170 total, 17 open against
thresholds of 150 and 7); `.dockerignore` is indeed a tracked depth-1 file; and
`libs/beans` module path, `apps/bean-counter` module path and the
`replace … => ../../libs/beans` directive are all exactly as described.

The one thing the file does **not** yet record is that its own `go.work.sum` fix
left `ci-workspace.yml`'s comment stale and four workflow path-filter entries
dead (suggestions S2 and S3). That is a gap in coverage, not an inaccuracy.

## The tracker describes real remaining work, and nothing was dropped

Run serially: `bd stats`, `bd show beans-ued`, `bd ready`, `bd show
bean-counter-m0p`. 170 total, 17 open, 1 in progress, 3 blocked, 14 ready.

Every gap `IMPLEMENTATION-NOTES.md` records has a corresponding open, ready
issue — this is the check that matters, because the failure mode for a branch
with this many known-unfinished edges is quietly dropping one:

| Notes section | Issue | State |
|---|---|---|
| "The infra host is running bean-counter in production" — remote move not performed | `beans-nlc` P0 | open, ready |
| "The deploy parity gate inverts" — embedded 11 vs prod 8 | `beans-ued` P0 | open, ready |
| "Not verified" — Docker image builds | `beans-oba` P1 | open, ready |
| "Gate G3 was not executed" — old repo untouched | `beans-ad3` P1 | open, ready |

`beans-ued` is not a placeholder. Its description carries the evidence chain —
the pinned pseudo-version `v0.1.2-0.20260615002029-e52dce57b52c`, `git ls-tree`
confirming it embeds through `0008`, the three new migrations by name, an
explicit correction of a *stale memory* that claimed v0.1.1/0007, the specific
hazard in `0010` (it drops the `bn_issues_state_check` CHECK constraint), two
named remediation options and the instruction "Do not weaken the gate." That is
a handoff a stranger could act on.

The dependency graph is coherent too: `bean-counter-m0p` (first live deploy)
depends on all four of `bean-counter-am5`, `bean-counter-log`, `beans-nlc` and
`beans-ued`, so the deploy cannot be started by accident while the parity
problem is open. Two further follow-ups (`beans-8do` allowedStates
reconciliation, `beans-cjy` `.golangci.yml` unification) are real deferred work,
correctly prioritised P2/P3.

## What this branch got right

- **The gates defend themselves.** After this round, reverting either the round-2
  `pwd -P` fix or the round-5 tracked-symlink check makes the suite fail. A gate
  whose reversion is silent is not a gate, and both now fail loudly.
- **Rejecting a class instead of enumerating instances.** Replacing the
  uncompletable per-path list with "no tracked symlink may exist under the
  deployed trees" is the correct response to a reviewer demonstrating that the
  list could not be finished by hand. The per-path list was kept anyway, because
  it still catches *untracked* local symlinks — subsuming rather than swapping.
- **The `go.work.sum` bug is the good kind of find.** It was invisible to every
  check on the branch, harmless to CI, and would have surfaced as a developer
  unable to deploy for a reason no build step explains. Catching it in review
  rather than in an incident is the whole return on six passes.
- **Retraction as a habit.** Three over-claims from earlier rounds are now
  explicitly withdrawn in later commit messages, and `IMPLEMENTATION-NOTES.md`
  carries a "Stale acceptance criteria" section that corrects the plan against
  itself. The plan documents were left as written and corrected in one place
  rather than quietly edited — which is why this review could audit them at all.
- **The history import is the real thing.** 114 commits brought in under merge
  `bfe40cc`, with bean-counter's own root commit `dd88be7` preserved as a second
  root, and `store.go` still traceable through `--follow` to 2026-06-13.

---

# Proposed merge-commit body

Use with `git merge --no-ff monorepo-consolidation`. Subject line first, then
the body.

```
Merge monorepo-consolidation: beans + bean-counter into one repository

Splits the single Go module into libs/beans
(github.com/mattsp1290/beans/libs/beans) and apps/bean-counter
(github.com/mattsp1290/beans/apps/bean-counter), with the app depending on the
library through `replace ... => ../../libs/beans` rather than a published
version. One Beads tracker at the root, three path-scoped GitHub workflows, and
a root Makefile that only fans out — each module keeps its own rules, lint
policy and golangci-lint version.

DO NOT SQUASH OR REBASE THIS MERGE.

bean-counter's full 114-commit history was imported under merge bfe40cc, which
brings bean-counter's own root commit dd88be7 in as a second root of this
repository. Two of the migration's ten success criteria are statements about
that history: `git log --follow -- libs/beans/store/store.go` must reach
pre-restructure commits (it reaches 39d689c, 2026-06-13), and
`git log -- apps/bean-counter/internal/server/app.go` must reach commits
authored in the bean-counter repository before the import, at the new path (it
reaches 433d900, 2026-06-14). A squash collapses all 130 commits into one and a
rebase linearises away the second root; either one destroys both criteria
permanently, and neither is recoverable from this repository afterwards. That
is why this is a --no-ff merge.

WHAT WAS NOT VERIFIED

The Docker engine on the machine where this was reviewed would not start, so
three things are unrun rather than passing:

  - success criterion 6, that `docker build -f apps/bean-counter/Dockerfile .`
    from the repository root produces a runnable image;
  - the equivalent build of the UI image;
  - `make ci-integration`, since both modules' integration suites use
    testcontainers and need a daemon.

Tracked as beans-oba. A CI job builds both images on merge and asserts the API
build stage resolves the library without go.work, so the first real signal
arrives with this merge. Everything not requiring a daemon was executed: the
workspace build and vet, both modules under GOWORK=off, root `make ci`
(golangci-lint reports 0 issues in both modules), gofmt, shellcheck, the deploy
script's 62-test unit suite, `go work sync` idempotence, `go list -m all`
leaving the tree clean, all three workflows parsing, the root Makefile fan-out
across all eight targets, the deploy dry-run, and both history criteria. Nine of
the ten success criteria are met; criterion 6 is the only one Docker blocks.

WHAT BLOCKS A DEPLOY

This merge does not make bean-counter deployable, and the deploy script will
correctly refuse to run until two things are resolved:

  - beans-ued (P0). Consolidating onto libs/beans HEAD raises the embedded
    Postgres migration maximum from 0008 to 0011, while production at 10.0.0.106
    is at 0008. The version-parity gate aborts when embedded > database, which
    is right: those migrations would alter a Postgres that bean-counter does not
    own — it belongs to local-symphony — and 0010 drops the
    bn_issues_state_check CHECK constraint. Resolve it by migrating the shared
    database with local-symphony's owner after reviewing 0010 and 0011, or by
    deploying a commit whose libs/beans embeds no more than the database has.
    Do not weaken the gate.
  - beans-nlc (P0). The remote checkout on the infra host is still
    $HOME/git/bean-counter; the script now defaults --repo-dir to $HOME/git/beans.
    bean-counter-api-1 and bean-counter-ui-1 have been up and healthy for about
    two months on ports 8081 and 8088, so this is a live production migration
    needing a rollback path and explicit approval, not the no-op the plan
    assumed.

Both block bean-counter-m0p, the first live deploy.

Gate G3 was deliberately not executed: github.com/mattsp1290/bean-counter is
untouched — not archived, not read-only, not deleted — and $HOME/git/bean-counter
still has its origin remote and an intact bean_counter Dolt database, which is
the recovery source if this merge has to be undone. Tracked as beans-ad3. Do not
archive the old repository until beans-nlc is done.

CORRECTION TO 33890f4

Its message says four redundant containment entries "survive removal at 60/60".
The finding is correct and reproduces, but the count is stale: the same commit
added two tests, so the suite is 62 and those four entries survive at 62/62. The
five individually-covered entries each fail exactly one case when removed, and
deleting the whole require_in_repo loop fails six.

Reviewed over six passes; records under .agents/reviews/monorepo-consolidation-*.
Where the implementation departed from the plan, .agents/plans/monorepo-consolidation/IMPLEMENTATION-NOTES.md
is authoritative over files 00-08, which were left as originally written.
```
