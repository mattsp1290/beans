# Suggestions

---

## S1 — `resolve_embedded_migration_max` recomputes the repo root instead of reusing `REPO_ROOT_PHYS`, and that duplicate is fail-open

**File:** `/Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh:466-468`

```bash
root_phys="$(cd "$(git rev-parse --show-toplevel)" && pwd -P)" \
  || fatal "could not resolve the repository root"
```

`main` already ran `require_repo_root`, which set `REPO_ROOT_PHYS` to exactly
this value. Two problems with recomputing it:

**(a) The `|| fatal` cannot fire.** The `git` call is nested *inside* the `cd`
command substitution, so its non-zero exit is discarded and only its empty
output propagates. `cd ""` succeeds in bash and leaves `$PWD` alone, so the
assignment yields `$PWD` with status 0. Verified:

```bash
bash -c '
set -euo pipefail
fatal() { echo "FATAL: $*" >&2; exit 1; }
git() { return 128; }          # simulate: git unavailable / not a worktree
cd /private/tmp
root_phys="$(cd "$(git rev-parse --show-toplevel)" && pwd -P)" || fatal "could not resolve the repository root"
echo "no fatal; root_phys silently became [$root_phys]"'
# -> no fatal; root_phys silently became [/private/tmp]
```

Contrast `require_repo_root:499`, which guards the git call directly and *does*
abort under the same simulation:

```bash
toplevel="$(git rev-parse --show-toplevel 2>/dev/null)" || fatal "not inside a git worktree"
# -> FATAL: not inside a git worktree   (rc=1)
```

No exploit today: `require_repo_root` has already asserted `$PWD` equals the
toplevel and nothing `cd`s between there and line 466, so the degraded value
happens to be the right one. It is a latent fail-open, not a live one.

**(b) It is the drift surface the review brief asks about.** Two independent
notions of "the repo root" and two independent containment `case` statements
(466-473 and 251-256) now have to be kept in agreement by hand. The
`resolve_embedded_migration_max` copy already differs: it has no `-L` refusal, so
an in-tree symlink at `libs/beans` would be accepted there and rejected by the
helper.

**Fix:** delete lines 466-468 and use `REPO_ROOT_PHYS` (it is a global, already
non-empty at this point, and `require_in_repo` already fails closed on an empty
one). Better still, replace the whole hand-rolled block at 466-484 with
`require_in_repo` calls once `beans_dir` has been reduced to a repo-relative
path, so there is exactly one implementation of containment in the file.

---

## S2 — A trailing slash defeats the `-L` refusal (containment still holds, so this is spec drift, not a hole)

**File:** `/Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh:239-242`

`[ -L path/ ]` is false for a symlink-to-directory: the trailing slash forces
resolution. Measured against the real function:

```
in-tree symlink, plain                      libs/inlink is a symlink; it must be the in-repo path   rc=1
in-tree symlink, TRAILING SLASH                                                                     rc=0
out-of-tree symlink, plain                  libs/outlink is a symlink; …                            rc=1
out-of-tree symlink, TRAILING SLASH         libs/outlink/ resolves to /…/tmp.A3PTQkR2wk, outside …  rc=1
out-of-tree symlink + /.                    libs/outlink/. resolves to /…, outside …                rc=1
```

So the *security* property survives — the out-of-tree cases are still caught, by
the containment `case` rather than by `-L`. What is lost is the stricter stated
contract ("it must be the in-repo path"): `libs/inlink/` is accepted where
`libs/inlink` is refused. Not reachable today because every caller passes a
hardcoded literal with no trailing slash, but it is a booby trap for the next
person who adds an entry to the list at 518-525.

**Fix:** normalise before the `-L` test, e.g. `rel="${rel%/}"` right after
`local rel="$1"` (with a guard so a bare `/` is not emptied), and add a test for
`libs/beans-intree-link/`.

---

## S3 — Containment rejects the repository root itself, and nothing says whether that is intentional

**File:** `/Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh:252`

`"$REPO_ROOT_PHYS"/*` requires a strict descendant, so `require_in_repo .`
and `require_in_repo "$REPO_ROOT_PHYS"` both return 1 with a message that reads
oddly:

```
. resolves to /…/tmp.lhhHve2Vy2, outside the repository at /…/tmp.lhhHve2Vy2
```

Correct for the current callers (none passes the root) but confusing, and
untested — this mutation survives:

| # | Mutation | Result |
|---|---|---|
| M15 | line 252 → `"$REPO_ROOT_PHYS"\|"$REPO_ROOT_PHYS"/*)` (root itself accepted) | **52 passed, 0 failed — SURVIVED** |

Either document that a strict descendant is required, or add the root to the
accepted set — but pick one deliberately and pin it with a test.

---

## S4 — `EMBEDDED_MAX` is computed before the replace directive it depends on has been validated

**File:** `/Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh:1112-1116`, `547-549`

`main` runs `resolve_embedded_migration_max` (1115), which derives `beans_dir`
from `GOWORK=off go list -m`. The gate that proves the replace points at
`../../libs/beans` — `check_sanctioned_replace` — does not run until
`require_clean_local_ref` (549), inside `do_check`/`do_live`.

This **is** fail-closed for the modes that matter: both `do_check` and `do_live`
call `require_clean_local_ref` before any `remote_exec`, so an `EMBEDDED_MAX`
derived from an unsanctioned replace can never reach a remote phase. I traced
`main` → `do_dry_run` (1071-1080) as well: dry-run performs an `ssh … true`
connectivity probe and nothing else, so the only consequence there is that
`print_plan`'s `embedded_max:` line (1057) can report a number sourced from an
unvetted, merely-in-repo directory. A human reading a dry-run may treat that
number as authoritative.

Also worth noting: `resolve_embedded_migration_max`'s own containment check
proves `beans_phys` is *somewhere* inside the repo, not that it is the
`libs/beans` that `require_repo_root` validated. The two are tied together only
by `check_sanctioned_replace` running later.

**Fix:** move `check_sanctioned_replace apps/bean-counter/go.mod` out of
`require_clean_local_ref` and into `require_repo_root` (or into `main` between
1113 and 1115) so the replace is proven sanctioned before anything reads through
it, and assert `beans_phys` equals `$REPO_ROOT_PHYS/libs/beans` rather than
merely being contained by the root.

---

## S5 — TOCTOU: the local containment checks are advisory for the shipped artifact

**Files:** `…/deploy-production.sh:1113` (checks), `…:931-932` (remote build)

Answering the brief's question directly: the checks run once, at 1113. What
happens afterwards, in order, is `resolve_target_sha` (1114),
`resolve_embedded_migration_max` (1115), then the mode driver. Local re-reads
after the checks are `apps/bean-counter/**` via `make -C` (570-585), the frontend
tree (589, 611), the Dockerfile (608), and the repo-root docker context (610).

A swap between 1113 and those reads would change what the gates validate — but
it needs local write access to the worktree, and an attacker with that can
already do anything, so I do not rate it. The genuinely useful observation is the
other one: `require_clean_local_ref` (532-537) makes the local worktree provably
equal to `TARGET_SHA`, and the remote build at 931-932 rebuilds from the remote
checkout of the *same* SHA. So the local containment checks are authoritative for
`EMBEDDED_MAX` and the replace gate, and are a **proxy** for the shipped images —
they prove the SHA is free of the offending symlink, which is exactly why
extending them to the Dockerfile and frontend (Important 2) is worth doing, and
exactly why doing so is not the same as validating the remote host.

`resolve_embedded_migration_max` correctly re-resolves `migrations/postgres` at
480 rather than trusting `require_repo_root`'s earlier pass, which closes the
narrow window on the one value that matters.
