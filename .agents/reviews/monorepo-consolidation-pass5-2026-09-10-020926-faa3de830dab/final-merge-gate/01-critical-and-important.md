# Critical and Important

## Critical

None. Every gate the branch runs passes, the module graph is correct, the history is
intact, the tracker invariant holds, and the worktree is clean.

---

## Important

### I1 — `faa3de8`'s mutation-coverage claim is false: 4 of the 9 trusted paths fail zero tests

**Where:** commit message `faa3de830dab`, paragraph beginning "**The wiring was untested.**";
control at `/Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh`
(`require_repo_root`, the `for rel in …` loop); tests at
`/Users/punk1290/git/beans/apps/bean-counter/test/scripts/deploy-production_test.sh`.

The commit message states:

> "There are now eight cases that drive require_repo_root in a local `git init` fixture: a
> well-formed tree, a cwd below the root, and each of **the six trusted paths** replaced in
> turn by an out-of-tree symlink. **Every path in the list is now individually
> mutation-covered: swapping any one of them for a harmless path fails exactly one case,**
> and deleting the loop fails six."

Two of those three assertions do not hold against the tree.

**The list has nine entries, not six.** `require_repo_root` iterates:

```
  for rel in libs/beans \
             libs/beans/schema \
             libs/beans/schema/migrations \
             libs/beans/schema/migrations/postgres \
             apps/bean-counter \
             apps/bean-counter/go.mod \
             apps/bean-counter/Dockerfile \
             apps/bean-counter/frontend \
             "$COMPOSE_PROD"; do
```

Eight test cases is correct (`unset REPO_ROOT_PHYS` aside, cases 44–51 of the suite), and
six of them are symlink cases — but those six cover six of *nine* trusted paths, not "the
six trusted paths."

**"Swapping any one of them for a harmless path fails exactly one case" is measurably
wrong.** I copied `deploy-production.sh`, its test file and `apps/bean-counter/go.mod`
into a scratch tree (the test resolves `SCRIPT` from `$(dirname "${BASH_SOURCE[0]}")/../..`,
so a three-file copy reproduces the suite exactly — baseline 60/60 on the copy), then
replaced each loop entry in turn with a harmless duplicate of another entry:

| Loop entry swapped out | Tests failed |
| --- | --- |
| `libs/beans` | **0** |
| `libs/beans/schema` | **0** |
| `libs/beans/schema/migrations` | **0** |
| `libs/beans/schema/migrations/postgres` | 1 |
| `apps/bean-counter` | **0** |
| `apps/bean-counter/go.mod` | 1 |
| `apps/bean-counter/Dockerfile` | 1 |
| `apps/bean-counter/frontend` | 1 |
| `"$COMPOSE_PROD"` | 1 |

Four of nine entries — including `libs/beans` itself, the very path whose test case is
named `require_repo_root rejects a symlinked libs/beans` — can be deleted from the loop
with the suite still at 60/60.

**Why:** `require_in_repo` resolves directories physically (`cd "$rel" && pwd -P`), so a
deeper entry catches a symlink planted at any ancestor. With `libs/beans` symlinked
out of tree, the `libs/beans/schema/migrations/postgres` check still resolves outside
`$REPO_ROOT_PHYS` and still aborts. The same redundancy covers `libs/beans/schema`,
`libs/beans/schema/migrations`, and `apps/bean-counter` (caught by
`apps/bean-counter/go.mod`).

**"Deleting the loop fails six" is correct.** Verified two ways on the copy — excising the
whole `for … done` block, and replacing the `require_in_repo "$rel" || fatal …` call with
`true` — both give 54 passed / 6 failed, naming exactly the six symlink cases.

**Severity and impact.** This is a **documentation defect, not a security defect**. The
containment control is sound; the four uncovered entries are genuine defense-in-depth and
removing them would not weaken behavior. What is wrong is a permanent, load-bearing claim
in a commit message written specifically to correct a previous pass's *identical* class of
error ("the ten cases pinned the helper's internals while nothing ever called
require_repo_root"). Round 4 asserted a stronger property than it verified, in the
paragraph whose whole point was that the previous round had done exactly that. That is the
third such overstatement on this branch and it should be recorded rather than left to
stand.

**Fix (either is acceptable):** correct the claim in the merge commit — "six of the loop's
nine entries are individually covered; the other three `libs/beans*` prefixes and
`apps/bean-counter` are redundant with deeper entries, which resolve physically" — or add
four cases so the claim becomes true. If cases are added, note that a case for a redundant
entry has to assert *which* check fires, not merely that the call fails, or it will pass
for the wrong reason.

---

### I2 — `go.work.sum` is neither tracked nor ignored; a routine `go list -m all` then blocks `--check` and live deploys

**Where:** `/Users/punk1290/git/beans/.gitignore` (no `go.work.sum` entry),
`/Users/punk1290/git/beans/go.work` (tracked), gate at
`/Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh:536`
(`require_clean_local_ref`).

`go.work` is tracked (correctly — plan finding 3 required removing the old ignore entry).
`go.work.sum` is not tracked, has never been tracked (`git log --all -- go.work.sum` is
empty), and is not matched by any `.gitignore` pattern.

`go work sync` does not generate one for this workspace — I confirmed that from a clean
tree, and `IMPLEMENTATION-NOTES.md` records it accurately. But `go list -m all` in
workspace mode does. I bisected seven commands from a clean tree, deleting the artifact
between each:

| Command | Creates `go.work.sum`? |
| --- | --- |
| `go work sync` | no |
| `go build ./libs/beans/... ./apps/bean-counter/...` | no |
| `go -C libs/beans build ./...` | no |
| `go -C apps/bean-counter build ./...` | no |
| `go -C libs/beans test ./...` | no |
| `go -C apps/bean-counter test ./...` | no |
| **`go -C libs/beans list -m all`** | **yes — 3215 bytes** |
| **`go -C apps/bean-counter list -m all`** | **yes — 3215 bytes** |

After that, `git status --porcelain` prints `?? go.work.sum`, which breaks success
criterion 10 and trips `require_clean_local_ref`:

```
  porcelain="$(git status --porcelain)"        # untracked files included
  if [ -n "$porcelain" ]; then
    printf '%s\n' "$porcelain" >&2
    fatal "local worktree is not clean (tracked changes or untracked files); commit/stash first"
```

`do_check` (line 1091) and `do_live` (line 1101) both call it. `--dry-run` does not, which
is why the dry-run gate passes with the file present — I verified that too, so the gate the
branch runs never catches this.

The branch's own `.gitignore` carries a comment about exactly this trap class, written
after the stray root `bn` binary caused it:

```
# Anchored to the repository root so it cannot shadow a `bn` file or directory
# inside a module. A stray root-level `go build -o bn` used to leave a 27 MB
# untracked binary here that no pattern matched.
/bn
```

`go.work.sum` is the same shape of problem with a smaller file.

**This also contradicts the plan.** `03-go-module-restructure.md:149` says "Both `go.work`
and `go.work.sum` are committed", its acceptance criterion 3 (line 225) says "`go.work` and
`go.work.sum` are tracked by git", and `00-overview.md:249` lists `go.work.sum` as "new,
tracked". `IMPLEMENTATION-NOTES.md` retracts only `01-target-layout-and-module-graph.md`'s
AC2 on this point; doc 03's AC3 and doc 00's layout are left standing and unmet.

**Note this does not break CI.** `ci-workspace.yml`'s "Workspace is in sync" step is
`go work sync` → `git diff --exit-code` → porcelain check, and its own comment anticipates
this exact case. Because `go work sync` alone produces nothing and no `go list -m` runs
before the porcelain check, the step passes. I simulated it: `git_diff_rc=0`, porcelain
PASS.

**Fix — pick one and make the plan agree with it:**

1. Add `/go.work.sum` to `.gitignore` (anchored, matching the `/bn` precedent) and amend
   doc 03's AC3 and doc 00's layout listing to say it is deliberately untracked. Safe here:
   every sum it would contain is already in one of the two modules' `go.sum`.
2. Generate it once (`go list -m all`) and commit it, satisfying the plan as written. This
   costs a small ongoing drift risk, since `go work sync` will not refresh it.

Option 1 is the smaller change and matches what the workspace actually does.
