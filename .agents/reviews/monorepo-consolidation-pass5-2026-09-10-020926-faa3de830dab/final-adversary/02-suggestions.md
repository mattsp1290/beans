# Suggestions

All mutation results below were produced against a sandbox copy of the script
and test file (`scripts/deploy-production.sh` + `test/scripts/deploy-production_test.sh`
+ `go.mod`, so `SCRIPT_DIR` resolves), baseline **60 passed, 0 failed**. Nothing
in `/Users/punk1290/git/beans` was modified.

---

## S1. Four of the nine containment-list entries survive removal — the commit message's coverage claim is not accurate

**File:** `apps/bean-counter/scripts/deploy-production.sh:522-533`,
`apps/bean-counter/test/scripts/deploy-production_test.sh:271-288`

The commit message for `faa3de8` states:

> Every path in the list is now individually mutation-covered: swapping any one
> of them for a harmless path fails exactly one case, and deleting the loop
> fails six.

The second half is true. The first half is not. Replacing each entry in turn
with a harmless duplicate:

| Entry removed from the list | Result |
|---|---|
| `libs/beans` | **SURVIVED** — 60/60 |
| `libs/beans/schema` | **SURVIVED** — 60/60 |
| `libs/beans/schema/migrations` | **SURVIVED** — 60/60 |
| `libs/beans/schema/migrations/postgres` | killed (1 case) |
| `apps/bean-counter` | **SURVIVED** — 60/60 |
| `apps/bean-counter/go.mod` | killed (1 case) |
| `apps/bean-counter/Dockerfile` | killed (1 case) |
| `apps/bean-counter/frontend` | killed (1 case) |
| `"$COMPOSE_PROD"` | killed (1 case) |
| whole loop → `true` | killed (6 cases) |

The four survivors are survivors because the victim fixtures symlink a
*directory* to an empty out-of-tree directory, so the deeper entry fires first
on "does not exist". For the containment property the shallower entries are
genuinely subsumed by the deeper ones — this is **not** a live hole. What is
untested is the one thing only the shallow entries provide: the `-L` rejection
of an **intermediate** component that is a symlink to an *in-tree* location
(which the deeper `cd … && pwd -P` check accepts, since it stays inside the
root).

Either add a case that exercises that — symlink `libs/beans` to a real in-tree
sibling and assert `require_repo_root` still rejects — or point the existing
victim symlinks at an out-of-tree directory that *does* contain the full
`schema/migrations/postgres` chain, which isolates each entry properly. Also
worth correcting the claim wherever it is repeated.

---

## S2. Round 2's physical-vs-logical fix has zero test coverage

**File:** `apps/bean-counter/scripts/deploy-production.sh:505-510`

Mutation — revert `here="$(pwd -P)"` / `toplevel="$(cd "$toplevel" && pwd -P)"`
to `here="$PWD"`: **SURVIVED, 60/60**. The regression that round 2 fixed (a
repository reached through a symlink aborts a legitimate deploy) is now
protected by nothing.

It is three lines to cover, and I confirmed both directions:

```bash
ln -s "$repo_root_tmp/repo" "$repo_root_tmp/via"
( cd "$repo_root_tmp/via" && require_repo_root )   # current code: rc=0
```

with the logical comparison it would abort:

```
logical compare: WOULD ABORT (/…/real/repo vs /…/viarepo)
```

Suggested case: `assert_eq "require_repo_root accepts a root reached through a
symlink" "0" "$( ( cd "$repo_root_tmp/via" && require_repo_root ) >/dev/null 2>&1; echo $? )"`.

Note this only works if the symlink is created *outside* the physicalized
fixture path — the existing fixture is already `pwd -P`-normalized, which is why
the regression is invisible today.

---

## S3. `resolve_embedded_migration_max`'s `REPO_ROOT_PHYS` guard is uncovered

**File:** `apps/bean-counter/scripts/deploy-production.sh:467-469`

Mutation — delete the `[ -n "$REPO_ROOT_PHYS" ] || fatal …` lines: **SURVIVED,
60/60**. Mutation — revert round 4's reuse to a recomputation via
`git rev-parse --show-toplevel`: **SURVIVED, 60/60**. So the exact defect round 4
fixed here is not pinned by anything.

I verified by execution that the guard does work today. Sourcing the real script
and forcing the broken ordering under strict mode:

```bash
( set -euo pipefail
  REPO_ROOT_PHYS=""
  go() { echo "/Users/punk1290/git/beans/libs/beans"; }   # stub, so go list is not the failure
  resolve_embedded_migration_max )
```

```
[deploy-production] ERROR: internal: REPO_ROOT_PHYS unset; require_repo_root must run first
rc=1
```

and with it set correctly: `embedded beans postgres migration max = 11`, rc=0.
It fails closed. The same stubbed-`go` technique makes a hermetic test case —
the harness already runs with `set +e`, so a `go() { … }` shadow in a subshell is
all it takes.

---

## S4. The fixture's `git init` honors an inherited `GIT_DIR`, and will re-initialize the caller's repository

**File:** `apps/bean-counter/test/scripts/deploy-production_test.sh:255-265`

`build_fixture_repo` runs `git init -q .` with the ambient environment. If the
suite is ever run from a context where git exports `GIT_DIR` — a `pre-commit` /
`pre-push` hook, `git rebase --exec`, `git bisect run` — that `git init` targets
the **caller's** repository, not the fixture. Demonstrated:

```
outer created
fixture has .git? -> NO          # GIT_DIR=$outer/.git git init -q . inside the fixture
```

No `.git` is created in the fixture at all; the outer repo is re-initialized.
Running the suite with `GIT_DIR`+`GIT_WORK_TREE` pointed at the real beans repo
gives:

```
FAIL - require_repo_root accepts a well-formed tree (expected '0', got '1')
59 passed, 1 failed
```

The suite does go red, so this is not silent — but the six victim cases all
"pass" vacuously in that state (they return 1 from the *toplevel mismatch*, not
from containment), so the mutation coverage evaporates while only one failure is
visible. Re-init is close to idempotent, but a test writing into a repository
outside its temp directory is worth eliminating regardless.

Fix: `env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git init -q .`, or
`unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE` at the top of the fixture subshell.
Setting `GIT_CEILING_DIRECTORIES` is not sufficient — it does not affect an
explicit `GIT_DIR`.

I confirmed the repository was unaffected by my own runs: `git diff HEAD --stat`
is empty and `git rev-parse HEAD` is still `faa3de830dab…`.

---

## S5. Dead `build_fixture_repo` call

**File:** `apps/bean-counter/test/scripts/deploy-production_test.sh:302`

The trailing `build_fixture_repo` after the victim loops rebuilds the fixture and
nothing reads it afterwards — the next section is `----- argument parsing -----`.
Harmless, but it reads as if a case were removed. Delete it, or add the case it
was leaving the tree clean for.

---

## S6. `"${ssh_args[@]}"` aborts under bash 3.2 (stock macOS `/bin/bash`)

**File:** `apps/bean-counter/scripts/deploy-production.sh:606-621`

Pre-existing, not a round-4 change, but it is in the gate path and it surfaced
during the portability sweep. Under `set -u`, bash 3.2 treats an empty array
expansion as unbound:

```
$ /bin/bash -c 'set -euo pipefail; a=(); printf "%s\n" "${a[@]}"'
/bin/bash: a[@]: unbound variable
```

`/bin/bash` on this machine is `3.2.57`. The shebang is `#!/usr/bin/env bash`, so
an operator with a Homebrew bash 5 in `PATH` never sees it; one without it hits
`ssh_args[@]: unbound variable` at the docker-build step of `local_gates`, after
all the test gates and before any remote work. It fails closed, so this is
cosmetic in safety terms — but the message is misleading. `"${ssh_args[@]+"${ssh_args[@]}"}"`
or an explicit bash-version assertion at the top would remove the ambiguity.
(GitHub's `ubuntu-latest` runner is bash 5, so CI is unaffected.)

---

## S7. Suite portability to a GNU/Linux runner

I could not execute the suite on Linux (no Docker on this machine), so this is
reasoning, flagged as such rather than claimed as verified. The round-4
additions use only `mktemp -d`, `cd … && pwd -P`, `git init -q`, `mkdir -p`,
`ln -s`, `rm -rf` and `: >` — all identical on GNU coreutils, and the fixture
never commits, so no `user.name`/`user.email` is required. The
`.github/workflows/ci-apps-bean-counter.yml:122` `deploy-scripts` job already
runs this file on `ubuntu-latest`, which is the real answer. The one cosmetic
difference: `build_fixture_repo`'s subshell output is not redirected, so on a
runner without `init.defaultBranch` set, git's default-branch advice prints into
the test output eight times. `git init -q -b main .` or a `>/dev/null 2>&1` on
the subshell would quiet it.
