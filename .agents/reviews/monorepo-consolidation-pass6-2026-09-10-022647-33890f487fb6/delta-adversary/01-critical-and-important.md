# Critical and Important

## Critical

None. I found no way to get a tracked symlink past the new gate, no way to make
the `.gitignore` entry hide something that matters today, and no vacuous case in
the new tests.

## Important

### I1. `.github/workflows/ci-workspace.yml:36-38` — the comment now states a guarantee this commit removed

The workflow's "Workspace is in sync" step carries this comment:

```yaml
      # git diff only sees tracked files, and `go work sync` can create a
      # go.work.sum where none was tracked, so the porcelain check is what
      # catches that case.
```

That sentence is now false. `/go.work.sum` is gitignored as of this commit, so
`git status --porcelain` cannot see it, and the step's third line cannot catch
the case the comment says it catches. Verified against a `git archive HEAD` copy
of this exact tree:

```
=== simulate the exact CI 'Workspace is in sync' step with go.work.sum present ===
CI step rc=0  (0 = passes; the go.work.sum on disk is invisible to it)
-rw-r--r--  1 punk1290  wheel  3215 Sep 10 02:31 go.work.sum
```

For contrast, with the one `.gitignore` line removed from the same tree:

```
  ST ?? go.work.sum
  (above shows what CI would have flagged before this commit)
```

**Runtime impact today: none.** I confirmed `go work sync` generates no
`go.work.sum` for this workspace (rc=0, no file, tree clean), so the branch the
comment describes is currently unreachable, and the ignore is the right call
precisely because the only thing that *does* write the file (`go list -m all`) is
a read command no build step maintains. This is a documentation defect, not a
behavior defect, and it does not block the merge.

It is filed as Important rather than as a suggestion for one reason: this branch
has spent five rounds correcting gates whose comments over-claimed what they
enforced, and the previous commit message was itself corrected for exactly that
class of over-claim. Leaving a CI step asserting a check it no longer performs
reintroduces that pattern in the one file a future reader will trust most.

**Fix (one line, no behavior change):** replace the stale sentence with what is
now true — for example:

```yaml
      # go.work.sum is gitignored (see .gitignore): `go work sync` generates
      # none for this workspace, and only read commands such as `go list -m all`
      # write one. If a future module bump makes `go work sync` produce one, this
      # step will NOT flag it — revisit the ignore at that point.
```

Nothing else in the workflow needs to change; `git diff --exit-code` still covers
`go.work` and every module `go.mod`, which is what `go work sync` actually
rewrites here.

### Answers to the specific questions asked, where the answer was "no defect"

These were attacked and came back clean; details and evidence are in
`03-positive-notes.md`.

- **Is `$4` the right awk field?** Yes for any path without whitespace, and the
  boolean gate is sound for *every* path — see S1 for the message-only exception.
- **Is `|| fatal` reachable under `set -euo pipefail`?** Yes, verified.
- **Empty repo / `git ls-files` failure?** An empty index passes silently, but
  the caller has already required `git rev-parse --show-toplevel` to succeed,
  `apps/bean-counter/go.mod` and `libs/beans` to exist, and every one of the nine
  per-path entries to resolve in-tree; `require_clean_local_ref` then requires
  `HEAD == TARGET_SHA`. There is no reachable state where the index is empty and
  the deploy proceeds.
- **Symlink introduced after the check?** Yes, in principle — `require_repo_root`
  runs at the top and the checked paths are read later by `local_gates` and the
  Docker build. This is not actionable: an actor who can write to the worktree
  mid-run already controls everything the script reads, and the clean-worktree
  gate has the same window. No finding.
- **Does `unset GIT_DIR GIT_WORK_TREE` have unwanted effects on the rest of the
  suite?** No. The suite is 62/62 with `GIT_DIR` set, with `GIT_WORK_TREE` set,
  with both, and from an unrelated cwd, and the real repository is byte-identical
  afterward.
- **Do the new cases leave anything behind in the real repository?** No.
- **Is `/` the right anchor?** Yes, verified: `libs/beans/go.work.sum` is *not*
  ignored (`git check-ignore -v` does not match it; `git status` reports it `??`).
