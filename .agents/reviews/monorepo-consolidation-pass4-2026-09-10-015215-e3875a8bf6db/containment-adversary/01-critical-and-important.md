# Critical and Important Findings

No Critical findings. Three Important.

Every claim below was produced by running the real functions sourced from
`/Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh`.
All mutation work was done on a copy under the session scratchpad; the repo was
never modified (see `03-positive-notes.md` for the cleanliness proof).

---

## IMPORTANT 1 — The containment control's *call sites* are untested; the whole loop can be deleted and the suite stays green

**Files:**
- `/Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh:518-525`
- `/Users/punk1290/git/beans/apps/bean-counter/test/scripts/deploy-production_test.sh:167-238`

The ten new cases drive `require_in_repo` directly, with `REPO_ROOT_PHYS` and
the fixture tree assigned by hand. Nothing in the suite ever calls
`require_repo_root`. So the tests pin the helper's *internals* and leave its
*wiring* completely unguarded — which is the exact regression the test file's
own header comment says these cases exist to prevent:

> A previous round fixed this with no test at all - deleting the check left the
> suite green - so these cases exist to make that impossible.

Mutation results (each mutation applied to a scratchpad copy of the script, then
`bash apps/bean-counter/test/scripts/deploy-production_test.sh` run against it):

| # | Mutation | Result |
|---|---|---|
| M16 | line 524 `require_in_repo "$rel" \|\| fatal …` → `true` (entire wiring removed) | **52 passed, 0 failed — SURVIVED** |
| M17 | `libs/beans/schema/migrations/postgres` dropped from the list at line 521 (duplicated the `migrations` entry) | **52 passed, 0 failed — SURVIVED** |

M17 is the pointed one: `migrations/postgres` is the directory that feeds
`EMBEDDED_MAX`, the number the commit message and the code comments both name as
the reason the helper exists. Silently removing it from the guarded list costs
the suite nothing.

**Reproduce (M16):**

```bash
SP=/tmp/mut && rm -rf "$SP" && mkdir -p "$SP/apps/bean-counter/scripts" "$SP/apps/bean-counter/test/scripts"
cp /Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh   "$SP/apps/bean-counter/scripts/"
cp /Users/punk1290/git/beans/apps/bean-counter/test/scripts/deploy-production_test.sh "$SP/apps/bean-counter/test/scripts/"
cp /Users/punk1290/git/beans/apps/bean-counter/go.mod "$SP/apps/bean-counter/"
# neutralise the only call site of the new control
perl -0pi -e 's{^    require_in_repo "\$rel" \|\| fatal .*$}{    true}m' \
  "$SP/apps/bean-counter/scripts/deploy-production.sh"
( cd "$SP" && bash apps/bean-counter/test/scripts/deploy-production_test.sh | tail -1 )
# -> 52 passed, 0 failed
```

**Fix:** add a case that exercises `require_repo_root` end to end. It needs a
real git worktree, which is cheap:

```bash
root="$(cd "$(mktemp -d)" && pwd -P)"
mkdir -p "$root/libs/beans/schema/migrations/postgres" "$root/apps/bean-counter"
: > "$root/apps/bean-counter/go.mod"
( cd "$root" && git init -q . )
( cd "$root" && require_repo_root >/dev/null 2>&1 ); # expect rc 0
# then make ONE guarded component a symlink out of the tree and expect a non-zero
# exit (require_repo_root calls fatal, so run it in a subshell and assert on $?).
```

Assert both directions, and assert it for **each** entry in the list — a
per-entry loop in the test is what kills M17.

---

## IMPORTANT 2 — `require_repo_root` does not guard the paths that decide what ships: the Dockerfile, the frontend build context, and the prod compose file

**Files:**
- `/Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh:518-525` (the list)
- `…:589`, `…:608`, `…:611` (local gates read them)
- `…:931-932` (the remote deploy payload builds from the same two paths)
- `…:66` (`COMPOSE_PROD`)

The guarded list is `libs/beans`, `libs/beans/schema`,
`libs/beans/schema/migrations`, `libs/beans/schema/migrations/postgres`,
`apps/bean-counter`, `apps/bean-counter/go.mod`. It covers everything that feeds
`EMBEDDED_MAX` and the replace gate. It covers nothing that feeds the images.

Unguarded paths the script trusts:

| Path | Used at | What it decides |
|---|---|---|
| `apps/bean-counter/Dockerfile` | 608 (local gate), **931 (remote build)** | the API image — the container that runs the beans migrations against the shared Postgres |
| `apps/bean-counter/frontend` | 589 (`npm ci/check/test/build`), 611 (local UI image), **932 (remote UI image)** | the entire UI build context and everything the frontend gate validates |
| `apps/bean-counter/deploy/docker-compose.prod.yml` | passed to the remote at 1086/1103, used as `-f "$compose_prod"` at 698/748/830 | service definitions, ports, and the volume wiring the script's own comment at 910 says must never be disturbed ("NEVER -v: shared volume is owned by $symphony_project") |
| `apps/bean-counter/Makefile` | `make -C apps/bean-counter …` at 570/578/583-585 | every local gate recipe |

Git stores symlinks as mode `120000` blobs, so all four can be *committed*
symlinks pointing out of the tree. That means `git status --porcelain` stays
empty, `require_clean_local_ref`'s `HEAD == TARGET_SHA` assertion holds, the ref
is reachable from `origin/main` — and the invariant this whole script is built
on ("the recorded SHA describes what was tested and what ships") is false for
the images, with no gate objecting.

**Demonstrated.** Fixture and run:

```bash
base="$(cd "$(mktemp -d)" && pwd -P)"; repo="$base/repo"; evil="$base/evil"
mkdir -p "$repo/libs/beans/schema/migrations/postgres" "$repo/apps/bean-counter/deploy"
: > "$repo/libs/beans/schema/migrations/postgres/0007_x.sql"
: > "$repo/apps/bean-counter/go.mod"
mkdir -p "$evil/frontend" "$evil/deploy"
printf 'FROM scratch\n' > "$evil/Dockerfile.pwned"
printf 'services: {api: {image: pwned}}\n' > "$evil/deploy/compose.pwned.yml"
ln -s "$evil/Dockerfile.pwned"         "$repo/apps/bean-counter/Dockerfile"
ln -s "$evil/frontend"                 "$repo/apps/bean-counter/frontend"
ln -s "$evil/deploy/compose.pwned.yml" "$repo/apps/bean-counter/deploy/docker-compose.prod.yml"
( cd "$repo" && git init -q . && git add -A )
( cd "$repo" && git ls-files -s apps/bean-counter/Dockerfile apps/bean-counter/frontend \
                                apps/bean-counter/deploy/docker-compose.prod.yml )

source /Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh
set +e +u +o pipefail
( cd "$repo" && require_repo_root ); echo "require_repo_root rc=$?"
```

Observed:

```
120000 apps/bean-counter/Dockerfile
120000 apps/bean-counter/deploy/docker-compose.prod.yml
120000 apps/bean-counter/frontend

require_repo_root rc=0            <-- passes
```

And with `REPO_ROOT_PHYS="$repo"`, calling the helper on each of them directly:

```
apps/bean-counter/Dockerfile is a symlink; it must be the in-repo path                      -> rc=1
apps/bean-counter/frontend is a symlink; it must be the in-repo path                        -> rc=1
apps/bean-counter/deploy/docker-compose.prod.yml is a symlink; it must be the in-repo path  -> rc=1
```

The control already rejects exactly these. It is simply not pointed at them.

**Impact, stated precisely.** An attacker needs commit access to `origin/main`
(the same access every other gate here already assumes) plus content at the
symlink target on the machine doing the build. The local gate machine and the
infra host both resolve the same committed link, so a target that exists on both
(`$HOME`, `/tmp`, another checkout on the infra host, a path a prior deploy left
behind) yields: an API image built from an attacker-chosen Dockerfile against a
repo-root context — that image is the one that runs migrations on the shared
Postgres — and a UI image whose whole build context is off-tree. The compose
file is weaker: it is read only on the remote after `git checkout --detach`, so
a local check proves only that the *SHA* carries no symlink there. That is still
worth having, since the remote checks out the same SHA, but do not oversell it
as remote protection.

**Caveat I owe you:** Docker was not running in this environment
(`docker info` → `DOCKER_DOWN`), so I did **not** execute a build through a
symlinked `-f` path or a symlinked context. What I proved is that the script
passes its own containment gate on such a tree and then hands those paths
straight to `docker build`. If you want the docker-side behaviour nailed down
before acting, run the two builds against the fixture above.

**Fix:** extend the loop at 518-525 with `apps/bean-counter/Dockerfile`,
`apps/bean-counter/frontend`, `apps/bean-counter/Makefile`, and `$COMPOSE_PROD`
(use the variable, not a literal, so the two cannot drift). All four are already
regular files/directories on this branch, verified with `git ls-files -s`, so
the addition is non-breaking:

```
$ git ls-files -s apps/bean-counter/Dockerfile apps/bean-counter/Makefile \
                  apps/bean-counter/deploy/docker-compose.prod.yml
100644 apps/bean-counter/Dockerfile
100644 apps/bean-counter/Makefile
100644 apps/bean-counter/deploy/docker-compose.prod.yml
```

---

## IMPORTANT 3 — The test named for the containment prefix never reaches the containment check; the `/` separator is unverified

**File:** `/Users/punk1290/git/beans/apps/bean-counter/test/scripts/deploy-production_test.sh:225-232`
**Guards:** `/Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh:252`

```
# A sibling whose name merely starts with the repo root must not satisfy the
# containment prefix match.
sibling="${repo_tmp}-evil"
mkdir -p "$sibling/libs/beans"
ln -s "$sibling/libs/beans" "$repo_tmp/libs/beans-link"
assert_eq "sibling path sharing the root prefix rejected" "1" "$(in_repo_rc libs/beans-link)"
```

`libs/beans-link` is a symlink, so `[ -L "$rel" ]` at line 239 fires first and
the function returns before the `case` is evaluated. The assertion passes for
the wrong reason. Proved by capturing the rejection message:

```bash
( cd "$repo" && require_in_repo libs/beans-link )
# libs/beans-link is a symlink; it must be the in-repo path
```

Consequence — the separating `/` in `"$REPO_ROOT_PHYS"/*` at line 252 is not
covered by any test:

| # | Mutation | Result |
|---|---|---|
| M6 | line 252 `"$REPO_ROOT_PHYS"/*)` → `"$REPO_ROOT_PHYS"*)` | **52 passed, 0 failed — SURVIVED** |

```bash
sed -i '' 's|^    "\$REPO_ROOT_PHYS"/\*) return 0 ;;$|    "$REPO_ROOT_PHYS"*) return 0 ;;|' \
  "$SP/apps/bean-counter/scripts/deploy-production.sh"
( cd "$SP" && bash apps/bean-counter/test/scripts/deploy-production_test.sh | tail -1 )
# -> 52 passed, 0 failed
```

The two cases that *do* reach the `case` ("symlinked intermediate component" and
"go.mod behind a symlinked parent") both resolve into a second, independent
`mktemp -d`, whose path shares no prefix with the fixture root — so they kill a
missing containment check (M10 below) but not a missing separator.

The shipped code is correct; I confirmed the real behaviour directly by pointing
`REPO_ROOT_PHYS` at the `-evil` sibling and passing a non-symlink directory:

```
libs/beans resolves to /…/tmp.99pPY9k9Ny/libs/beans,
outside the repository at /…/tmp.99pPY9k9Ny-evil        rc=1
```

**Fix:** make the sibling case use a path that is *not* itself a symlink, so it
must be rejected by the prefix rather than by `-L`. For example, keep the
fixture tree where it is and move the root:

```bash
sibling="${repo_tmp}-evil"; mkdir -p "$sibling/libs/beans"
saved_root="$REPO_ROOT_PHYS"; REPO_ROOT_PHYS="$sibling"
assert_eq "prefix-sharing sibling root does not satisfy containment" "1" "$(in_repo_rc libs/beans)"
REPO_ROOT_PHYS="$saved_root"; rm -rf "$sibling"
```

That case fails under M6 and passes as shipped.
