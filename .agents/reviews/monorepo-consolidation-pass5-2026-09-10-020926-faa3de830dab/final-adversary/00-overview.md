# Review — Final Adversary

- **Branch:** `monorepo-consolidation`
- **HEAD reviewed:** `faa3de830dab5ee8bcf0f3889910b0f7febc5838`
- **Date:** 2026-09-10
- **Reviewer:** Final Adversary (`final-adversary`)
- **Role:** Last adversarial look at the deploy gates and their new test wiring.
- **Pass:** 5

## Stats

`git show --shortstat faa3de8`:

```
14 files changed, 1278 insertions(+), 7 deletions(-)
```

Of which the code under review is:

```
apps/bean-counter/scripts/deploy-production.sh          | 14 +-
apps/bean-counter/test/scripts/deploy-production_test.sh| 72 ++++++-
apps/bean-counter/deploy/README.md                      | 16 ++
```

The remaining 11 files are the pass-4 review records.

## Summary

Round 4's three substantive changes hold up under direct attack. `COMPOSE_PROD`
is a variable but never a mutable one: it is assigned once with a plain
assignment (not `${COMPOSE_PROD:-…}`, so no environment override), there is no
`--compose-file` flag, and no code path reassigns it — so `parse_args` running
before `require_repo_root` cannot desynchronize what is checked from what is
used, and if such a flag were ever added the check would follow the flag and
reject an out-of-tree value. `resolve_embedded_migration_max` is reached from
exactly one caller (`main`, immediately after `require_repo_root`), and its
`[ -n "$REPO_ROOT_PHYS" ]` guard was confirmed by execution to fire and exit 1
under `set -euo pipefail` when the ordering is broken — it fails closed. The new
`git init` fixture is faithful to all nine paths the containment loop checks,
and the "accepts a well-formed tree" case is provably non-vacuous (deleting
`REPO_ROOT_PHYS="$toplevel"` makes exactly that case fail). Mutation testing
confirms round 4's headline claims: deleting the containment loop now fails six
cases, widening `"$REPO_ROOT_PHYS"/*` to `"$REPO_ROOT_PHYS"*` now fails the
sibling-prefix case, removing the `-L` rejection fails four, and each of
`apps/bean-counter/Dockerfile`, `apps/bean-counter/frontend` and `$COMPOSE_PROD`
is killed by its own named case.

One real gap remains, and it is the same defect class round 4 fixed, one file
over. `require_repo_root` returns **0** on a tree where
`apps/bean-counter/Makefile`, `apps/bean-counter/frontend/Dockerfile` and
`apps/bean-counter/frontend/package.json` are committed symlinks pointing out of
the repository — demonstrated with a working fixture. The Makefile drives every
local gate (`make -C apps/bean-counter test`, `test-integration`, `vet`, `lint`,
`fmt-check`) whose `PASS` lines become the recorded `LOCAL_PREFLIGHT` audit, and
`frontend/Dockerfile` is the build recipe for the UI image that actually ships.
Both meet, verbatim, the inclusion criterion the code's own comment states
("read from the worktree and decides either what ships or what a safety gate
concludes"). Secondary to that: four of the nine list entries survive removal
with the suite green, so the commit message's claim that every path is
individually mutation-covered is not accurate; round 2's physical-vs-logical
path fix has zero coverage (reverting it leaves 60/60); and the fixture's
`git init` honors an inherited `GIT_DIR`, which was shown to re-initialize the
caller's repository instead of the fixture.

## Verdict

**REQUEST_CHANGES**

One Important finding with a demonstrated reproduction and a one-line structural
fix. Everything else is a suggestion. No file in the repository was modified;
`git diff HEAD --stat` is empty and `HEAD` is still `faa3de8`.
