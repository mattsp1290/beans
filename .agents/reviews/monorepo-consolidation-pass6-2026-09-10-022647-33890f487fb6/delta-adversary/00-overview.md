# Review Overview

- **Branch:** `monorepo-consolidation`
- **Commit under review:** `33890f487fb6c9a75c8b16a037f9bdade1eeee8f` ("Address the fifth review pass")
- **Date:** 2026-09-10
- **Reviewer:** Delta Adversary (`delta-adversary`)
- **Role:** Review only what pass five added — the tracked-symlink gate, the `go.work.sum` ignore, and the new test coverage.
- **Verdict:** `APPROVE`

## Stats

`git show --shortstat 33890f4`:

```
 15 files changed, 1132 insertions(+), 1 deletion(-)
```

Of those 15 files, 11 are review records under
`.agents/reviews/monorepo-consolidation-pass5-.../` and one is
`IMPLEMENTATION-NOTES.md`. The three files in scope are `.gitignore` (+6),
`apps/bean-counter/scripts/deploy-production.sh` (+19), and
`apps/bean-counter/test/scripts/deploy-production_test.sh` (+29/-1).

## Summary

I attacked all three changes empirically and could not break any of them. The
`git ls-files -s | awk` gate detects a tracked symlink in every hostile
configuration I could construct — staged-but-uncommitted, committed, nested
several levels deep, at the pathspec root itself, with a path containing spaces,
with a path containing a newline, under `core.symlinks=false` (where the
worktree entry is a *regular file* and the older `[ -L ]` per-path gate is blind
to it), under a sparse checkout that has not materialized `apps/` at all, and
under `skip-worktree`/`assume-unchanged`. `$4` is the correct awk field for a
well-formed path; the `|| fatal` is genuinely reachable, because `pipefail` does
propagate out of the command substitution (verified). The `/go.work.sum` anchor
is correct — it does not shadow `libs/beans/go.work.sum` — and I reproduced both
the original failure (`go list -m all` writes a 3215-byte `go.work.sum` that
`git status --porcelain` reports as `??`) and the fix. Both new test cases are
precisely mutation-covered: deleting the tracked-symlink block fails exactly the
new tracked-symlink case, and reverting `here="$(pwd -P)"` to `here="$PWD"` fails
exactly the symlinked-path case. Removing the `unset`/`env -u` guards turns the
suite red rather than leaving it silently green. The suite is 62/62 normally,
with `GIT_DIR` set, with `GIT_WORK_TREE` set, with both set, and from an
unrelated cwd; the deploy dry-run succeeds; shellcheck is clean; and the real
repository has zero tracked symlinks anywhere, let alone under the deployed
trees. Two things are worth fixing but neither is a gate bypass and neither
blocks merge: the diagnostic listing truncates a symlink path at its first space
(the gate still fires — only the operator-facing message is wrong), and the
comment in `.github/workflows/ci-workspace.yml` now asserts a guarantee that the
new `.gitignore` entry deliberately removed.

## What I ran

- Full shell suite (62/62) four ways: baseline, `GIT_DIR=` set, `GIT_WORK_TREE=`
  set, both set, and from `/tmp`.
- Four mutations of the script and one of the test file, each reverted.
- Seven purpose-built git fixtures probing `git ls-files -s` output shape.
- `go work sync` and `go list -m all` in an isolated `git archive` copy of HEAD.
- The deploy dry-run (`--ref main --dry-run`) against the live repository.
- `shellcheck` on both changed shell files.

The repository is unchanged: `git status --porcelain` is empty and
`git diff HEAD --stat` is empty after all of the above.
