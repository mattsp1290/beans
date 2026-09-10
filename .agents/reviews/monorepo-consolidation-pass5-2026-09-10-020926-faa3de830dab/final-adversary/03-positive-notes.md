# Positive Notes

These are the attacks from the brief that I ran and that the code survived. Each
was executed, not reasoned about.

## `"$COMPOSE_PROD"` in the containment list is safe, and is the right call

The brief asked whether checking a *variable* rather than a literal is a
weakness, and whether `parse_args` running before `require_repo_root` can
desynchronize what is checked from what is used. It cannot:

- `COMPOSE_PROD` is assigned exactly once, at `deploy-production.sh:66`, and
  `grep -n COMPOSE_PROD` shows every other occurrence is a read (`:531`, `:1062`,
  `:1094`, `:1111`).
- The assignment is `COMPOSE_PROD="apps/…"`, **not** `${COMPOSE_PROD:-…}` — unlike
  `HEALTH_TIMEOUT` at `:71`, it is not environment-overridable. An operator
  cannot point it anywhere.
- `parse_args` (`:370-414`) has no `--compose-file` / `--compose-prod` case, and
  its `*)` arm fatals on any unknown argument.

So today the variable is the literal. And the ordering is not just harmless but
*correct by construction*: because `parse_args` runs first, a future
`--compose-file` flag would be validated by the very check that consumes it, and
an out-of-tree value would be rejected by `require_in_repo` rather than silently
used. Checking the variable is strictly better than hard-coding the string here.

Mutation confirms it is load-bearing: replacing `"$COMPOSE_PROD"` in the loop
with a harmless path fails exactly `require_repo_root rejects a symlinked
apps/bean-counter/deploy/docker-compose.prod.yml`.

## `resolve_embedded_migration_max` fails closed on a reordering

Only one caller exists — `main:1123`, two lines after `require_repo_root` at
`:1121`. `do_dry_run`, `do_check` and `do_live` do not call it. `REPO_ROOT_PHYS`
is written in exactly one place (`:512`) and initialized to `""` at `:216`, so
`set -u` never bites and the empty value is reachable only by breaking the
order.

Executed the broken ordering directly under `set -euo pipefail`, with `go`
stubbed so `go list` is not the thing that fails:

```
[deploy-production] ERROR: internal: REPO_ROOT_PHYS unset; require_repo_root must run first
rc=1
```

The guard fires, and it fires via the explicit `|| fatal` rather than relying on
`set -e` semantics — which is the robust form, since `[ … ] || fatal` in a list
is exempt from `set -e` anyway. Round 4's stated reason for the change (the old
nested command substitution swallowed a git failure, and `cd "" && pwd -P`
succeeds, so `root_phys` silently became `$PWD`) is real and correctly fixed.

## The `git init` fixture is faithful, and the accept case is not vacuous

The fixture creates every one of the nine paths the containment loop checks —
`libs/beans/schema/migrations/postgres` (plus a `0001_init.sql`),
`apps/bean-counter/{go.mod,Dockerfile,frontend}`,
`apps/bean-counter/deploy/docker-compose.prod.yml` — and `git init -q .` gives
`require_repo_root` a real toplevel. Nothing in `require_repo_root` reads
anything the fixture lacks, so "accepts a well-formed tree" is not passing
around a missing precondition. Proven, rather than assumed: deleting
`REPO_ROOT_PHYS="$toplevel"` fails that case and only that case, so it is
genuinely reaching the end of the function.

`repo_root_rc` captures the status correctly. `require_repo_root` reaches failure
only through `fatal`, which `exit 1`s, so taking the *subshell's* status is the
only thing that works — the commit message's parenthetical about `echo $?` inside
the subshell never running is accurate. Every failure path returns exactly 1, so
the `assert_eq … "1"` comparisons are exact rather than "nonzero".

## Round 4's mutation claims that do hold

Verified in a sandbox copy, baseline 60/60:

| Mutation | Result |
|---|---|
| whole containment loop → `true` | 6 failures — the six named `require_repo_root rejects a symlinked …` cases |
| `"$REPO_ROOT_PHYS"/*` → `"$REPO_ROOT_PHYS"*` | 1 failure — `sibling path sharing the root prefix rejected` |
| remove the `-L` rejection in `require_in_repo` | 4 failures, incl. `in-tree symlink rejected` |
| remove the `REPO_ROOT_PHYS` unset-guard in `require_in_repo` | 1 failure — `unset REPO_ROOT_PHYS rejected` |
| drop `apps/bean-counter/Dockerfile` from the list | 1 failure, the matching named case |
| drop `apps/bean-counter/frontend` | 1 failure, the matching named case |
| drop `apps/bean-counter/go.mod` | 1 failure, the matching named case |
| drop `libs/beans/schema/migrations/postgres` | 1 failure, the matching named case |

The sibling-prefix fix is real: moving the symlink to an intermediate component
means `rel` is a real directory reached through it, the `-L` branch no longer
short-circuits, and the widening mutation that survived at 52/52 last round now
dies. That was the previous reviewer's sharpest finding and it is properly
closed.

## The remote side is consistent with the local gate

Traced what the remote payload trusts. It `git checkout --detach "$target_sha"`
and then asserts `git rev-parse HEAD` equals it (`:928-929`), and refuses to run
with tracked changes (`:810-813`), so the remote tree's *tracked* content is
byte-identical to the locally-validated one — the local containment check does
therefore describe what the remote builds. Untracked files on the remote are
tolerated but cannot shadow a tracked path.

`remote_exec` invokes the payload as `bash -euo pipefail -s --` (`:363`), so the
`docker build … | tee` pipelines at `:939-940` are not masked by `tee`'s exit
status. That one is easy to get wrong and it is right here.

## Housekeeping

`shellcheck apps/bean-counter/scripts/deploy-production.sh
apps/bean-counter/test/scripts/deploy-production_test.sh` is clean. The real
suite runs 60 passed, 0 failed. `git ls-files -s` shows the repository currently
contains **no** committed symlinks anywhere, so the gate is not papering over an
existing violation.
