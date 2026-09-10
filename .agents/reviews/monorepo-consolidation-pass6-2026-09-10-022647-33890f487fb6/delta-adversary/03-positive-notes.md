# Positive Notes

This is what I tried to break and could not. Each item is something I actually
ran, not something I read and approved.

## The tracked-symlink gate

### It detects in every checkout configuration I could construct

A single fixture with five tracked symlinks under `libs/beans` and
`apps/bean-counter` — nested three levels deep, at the pathspec root, with a
space in the path, with a newline in the path, and plain — detected 5/5 in all
of the following:

| Configuration | Detected |
| --- | --- |
| staged, nothing committed yet | 5/5 |
| after `git commit` | 5/5 |
| clone with `core.symlinks=false` | 5/5 |
| sparse checkout that never materialized `apps/` | 5/5 |
| `--skip-worktree` / `--assume-unchanged` set on the entries | 5/5 |
| pathspec root itself is the symlink (`libs/beans`, `apps/bean-counter`) | 2/2 |

The `core.symlinks=false` row is the strongest argument for this change existing.
In that clone the worktree entry is a **regular file** containing the target
path:

```
worktree file type: -
index modes: 5
```

`require_in_repo`'s `[ -L "$rel" ]` test is blind to that by construction — the
worktree has no symlink to find — and the physical resolution succeeds, because
the "symlink" is an ordinary in-tree file. Reading the index is the only thing
that catches it. Round 5's framing ("a committed symlink is invisible to
`git status`") understates the case: it is also invisible to the per-path gate on
any platform or configuration where symlinks are not materialized.

The sparse-checkout row is the same argument from the other direction: the
structural check sees the symlink even when the tree containing it was never
written to disk.

### `$4` is the correct field, and the boolean gate is sound for every path

`git ls-files -s` emits `<mode> <object> <stage>\t<path>`, and under awk's default
splitting that puts the path at `$4` for any path without whitespace. More
importantly, there is no record for which a `120000` row produces an *empty*
`$4` — a path always follows the tab and awk skips leading separators — so
`[ -n "$tracked_links" ]` cannot be evaded. I specifically hunted for an evasion
using trailing-space filenames, whitespace-only filenames and C-quoted paths and
found none. The only consequence of the field choice is the truncated diagnostic
in S1.

Newline-containing paths are C-quoted by git and arrive as one line
(`"apps/bean-counter/link\nwith-newline"`), so there is no multi-record splitting
and no way to smuggle a second entry into the listing.

### The error path is genuinely reachable, and the `local` split is the right idiom

`local tracked_links` on its own line followed by `tracked_links="$(...)" || fatal`
avoids the classic `local x=$(cmd)` trap where `local`'s own exit status masks the
command's. And `pipefail` does propagate out of a command substitution, so a
failing `git` really does take the `||` branch:

```
--- confirm pipefail is inherited by the command substitution subshell ---
PIPEFAIL PROPAGATES (|| taken)
--- inside a non-git directory ---
FATAL: enumerate failed
rc=9
```

This is not a dead branch, which is worth stating because it looks like one.

### The class rejection is the right call

Round 5's reasoning holds up under attack: I could not name a per-path list that
would be complete. Any file under either tree that a gate reads, or that ends up
in an image, meets the stated criterion, and the set grows with the codebase. A
structural invariant ("no tracked symlinks here") is auditable in one command and
cannot silently fall behind. It is also stricter than strictly necessary — it
rejects a benign in-repo symlink too — and for a script guarding a deploy against
a Postgres it does not own, that is the correct direction to err.

## The `go.work.sum` ignore

### The bug is real and the fix works — both reproduced

In a `git archive HEAD` copy of this exact tree:

```
=== go work sync ===        rc=0, no go.work.sum produced, tree clean
=== go list -m all ===      rc=0, go.work.sum written, 3215 bytes
=== git status --porcelain WITH the ignore ===     (clean)
=== same tree WITHOUT the ignore line ===          ?? go.work.sum
```

That `??` is exactly what `require_clean_local_ref` aborts on. Round 5's claim
that `go work sync` is a no-op for this workspace is accurate, so nothing was
ever supposed to be committed, and ignoring rather than tracking is right: a file
that only *read* commands generate has no maintainer and no meaningful diff.

### The anchor is correct

`/go.work.sum` matches the root file and does not shadow a module-level one:

```
$ git check-ignore -v go.work.sum
.gitignore:33:/go.work.sum	go.work.sum
$ git check-ignore -v libs/beans/go.work.sum
  NOT ignored -> anchor is correct
$ git status --porcelain -- libs/beans/go.work.sum
  ?? libs/beans/go.work.sum
```

This matches the existing `/bn` precedent in the same file and its rationale
comment. `go.work` itself remains tracked, confirmed via `git ls-files -- go.work`.

## The test changes

### Both new cases are precisely mutation-covered

Every mutation failed exactly one case, and the correct one:

- Deleting the whole tracked-symlink block: **61/62**, fails only
  `require_repo_root rejects a TRACKED symlink not in the path list`.
- `"120000"` → `"120001"`: **61/62**, fails only that same case — so the case is
  pinned to the mode value, not merely to the block's existence.
- `here="$(pwd -P)"` → `here="$PWD"`: **61/62**, fails only
  `require_repo_root accepts a repo reached through a symlinked path`. Round 2's
  fix now has the coverage that round 5 correctly reported it lacked.

The tracked-symlink case is also rejecting for the *right* reason:
`apps/bean-counter/Makefile` is not in the per-path list, and the loop passes on
that fixture, so the only thing that can produce the `1` is the new check —
which the deletion mutation confirms directly.

### The `GIT_DIR` guards are real and fail loudly, not silently

Removing both `unset GIT_DIR GIT_WORK_TREE` and the two `env -u` prefixes, then
running with `GIT_DIR`/`GIT_WORK_TREE` pointed at a throwaway repository:

```
FAIL - require_repo_root accepts a well-formed tree (expected '0', got '1')
FAIL - require_repo_root accepts a repo reached through a symlinked path (expected rc 0, got 1)
60 passed, 2 failed
```

The guards are load-bearing. Note the useful shape of this: the *reject* cases go
vacuously green (which is what round 5 diagnosed) while the *accept* cases go red,
so the suite as it now stands would not have hidden the problem indefinitely.
Fixing it at the source anyway was the right call — a vacuous reject case is a
gate that reports success while testing nothing.

The `env -u` on `git init` in particular prevents the worst outcome: without it,
`git init` honors an inherited `GIT_DIR` and re-initializes the **caller's**
repository. That is a test suite that can damage the tree it is run from.

### The suite is hermetic and leaves nothing behind

62/62 in all four hostile environments, and the repository is byte-identical
afterward:

```
=== suite with hostile GIT_DIR ===        62 passed, 0 failed
=== suite with hostile GIT_WORK_TREE ===  62 passed, 0 failed
=== suite with BOTH ===                   62 passed, 0 failed
=== suite from an unrelated cwd ===       62 passed, 0 failed
=== post-state ===                        (clean)
REPO UNCHANGED
```

Fixture teardown is sound: `rm -rf "${repo_root_tmp:?}/repo"` uses the `:?` guard
so an unset variable cannot turn it into `rm -rf /repo`, the `repo-link` symlink
is removed immediately after its case, and the `trap ... EXIT` covers all six
temp directories. The `repo_root_rc` helper's comment about taking the subshell's
status rather than an inner `echo $?` (which `fatal`'s `exit` would skip) is a
detail that is easy to get wrong and is right here.

## Live-repository verification

- Zero tracked symlinks anywhere in the repository, not just under the deployed
  trees: `git ls-files -s | awk '$1=="120000"'` is empty.
- No submodules / gitlinks (`160000`) under either tree, and no `.gitmodules`.
- `shellcheck` clean on both changed shell files.
- Deploy dry-run succeeds end to end, including `require_repo_root` (proved by
  `embedded beans postgres migration max = 11`, which is computed immediately
  after it) and the SSH connectivity probe.
- `git status --porcelain` and `git diff HEAD --stat` both empty after all
  experiments.
