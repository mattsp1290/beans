## Action Items

### Critical

_None._

### Important

None of these block the merge. All three must land before the next production deploy.

- [ ] [`bd` memory `bean-counter-prod-10-0-0-106-beans`] Correct the stale baseline: prod is at 8, and bean-counter pinned `beans v0.1.2-0.20260615002029-e52dce57b52c` embedding through **0008** (exact parity), not `v0.1.1`/`0007`. Commit `df029e9` superseded that state on 2026-06-14; `git show 6895dea:apps/bean-counter/go.mod` and the module cache confirm 0008.
- [ ] [`beans-ued` (P0, open) description] Replace "pinned beans v0.1.1, embedding through 0007 - older than prod" with the true baseline (embedded 0008 == prod 0008, exact parity; the monorepo raises it to 11). Currently contradicts `apps/bean-counter/deploy/README.md:104-107` and `IMPLEMENTATION-NOTES.md`, and it is the P0 gating a shared-Postgres migration decision.
- [ ] [`bean-counter-m0p` (P0, open) description] Repoint `.agents/plans/deploy/05-validation.md` → `.agents/plans/bean-counter-deploy/05-validation.md`, and `scripts/deploy-production.sh` → `apps/bean-counter/scripts/deploy-production.sh`. Same dead directory `e3875a8` fixed in `deploy/README.md`.
- [ ] [`bean-counter-am5`, `bean-counter-mkg` descriptions] Same repointing: unprefixed `scripts/deploy-production.sh`, and `.agents/plans/deploy/` in `mkg`.
- [ ] [`bean-counter-log` (P1, open) description] Reconcile with `beans-nlc`, which now owns the `git/bean-counter` → `git/beans` remote checkout move; the description still bootstraps the old path.
- [ ] [`/Users/punk1290/git/beans/apps/bean-counter/deploy/README.md:98`] Add the second first-deploy prerequisite to the "Before the first post-monorepo deploy" section: the infra host is still checked out at `$HOME/git/bean-counter` while the script now defaults to `$HOME/git/beans`; `beans-nlc` (P0) owns the move and must close first. The Rollback block at line 135 already says `cd ~/git/beans`, a path that does not exist on that host today.
- [ ] [`.agents/plans/monorepo-consolidation/IMPLEMENTATION-NOTES.md`] Record that plan `08-execution-handoff.md`'s "`bean-counter-m0p`, `bean-counter-log` and `bean-counter-am5` carry over unchanged" no longer holds — the migration's own hoist, script move and `--repo-dir` default falsified path references inside those descriptions.

### Suggestions

- [ ] [`/Users/punk1290/git/beans/libs/beans/Makefile:7`] Extend the `VERSION` comment to say no `libs/beans/v*` tag exists yet, so `--always` currently yields a bare short SHA (`bn --version` prints `e3875a8`, not `v0.1.1-308-g…`). Tracked as `beans-vgn`; today only commit `fb0b755` records it.
- [ ] [`.agents/plans/monorepo-consolidation/IMPLEMENTATION-NOTES.md`, "Stale acceptance criteria"] Complete the AC4 exception list — it names only the prod compose `context: ../../..`, but `apps/bean-counter/deploy/README.md:5` (`../../../.agents/plans/bean-counter-deploy/`) and the synthetic `../../../fiber-fork` fixtures in `test/scripts/deploy-production_test.sh:124,127,131` also match the criterion as literally written.
- [ ] [`.git/info/exclude`] Remove the `.agents/reviews/` entry (operator action; not a repository file). It still hides tracked review records from `git status`, weakens success criterion 10, and means this pass-4 directory needs `git add -f` exactly as `e3875a8` did. Worth a bead so it does not recur on the next clone.
- [ ] [`/Users/punk1290/git/beans/.github/workflows/ci-workspace.yml:52`] Add `fmt-check` to the root fan-out loop. It is the one root target that deliberately delegates to a single module, so it is the most likely to drift and the only one the check omits.
- [ ] [`/Users/punk1290/git/beans/.github/workflows/ci-apps-bean-counter.yml`, `deploy-scripts` job] Decide whether the two root seeding scripts (`setup-beads.sh`, `setup-multi-repo-beads.sh`) should be shellchecked. They match no path filter and are gated by nothing; not a regression from `main`, but this branch touched both and established the convention.
