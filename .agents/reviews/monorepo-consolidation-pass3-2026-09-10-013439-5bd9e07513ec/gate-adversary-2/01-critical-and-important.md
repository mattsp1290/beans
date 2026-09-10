# Critical and Important

All fixtures below were built in a temp dir and driven against the real script
by `source`ing it and calling the real functions. Set `S` to a scratch dir for
every reproduction.

---

## CRITICAL 1 — The containment check verifies a path that is not the path it reads: `EMBEDDED_MAX` can still be sourced from outside the repository

**`apps/bean-counter/scripts/deploy-production.sh:421-431`**

```sh
beans_phys="$(cd "$beans_dir" && pwd -P)" \
  || fatal "could not resolve $beans_dir"
case "$beans_phys" in
  "$root_phys"/*) : ;;
  *) fatal "$BEANS_MODULE resolves to $beans_phys, outside the repository at $root_phys" ;;
esac

EMBEDDED_MAX="$(migration_max_from_dir "$beans_phys/schema/migrations/postgres")"
```

`beans_phys` is fully resolved and correctly contained. The string built on
line 431 then appends three more components that are **not** resolved.
`migration_max_from_dir` globs `"$dir"/*.sql`, and the glob traverses whatever
`schema`, `migrations` or `postgres` happen to be. Any one of them being a
symlink out of the tree puts the number back outside the repository, which is
the exact property the pass-2 fix was written to establish.

`require_repo_root` does not help: `[ ! -L libs/beans ]` tests only that one
component.

### Impact

`EMBEDDED_MAX` is passed to the remote as argument 11 (`--check`) and argument
18 (live), and both payloads gate on it in the same direction:

```
apps/bean-counter/scripts/deploy-production.sh, remote_deploy_payload:
  if [ "$embedded_max" -gt "$db_max" ]; then
    echo "FAIL: bean-counter embedded migrations ($embedded_max) NEWER than prod ($db_max); would migrate shared schema" >&2
```

The gate fires only when the embedded max is **greater** than the DB. Sourcing
a *smaller* number is therefore the useful attack, and that is what the fixture
below produces (1 instead of 7). This runs inside the flock'd mutating session,
immediately before `pg_dump` and `compose up`.

### Reproduction

Fixture builder (`$S/mkrepo.sh`), a minimal repo of the same shape:

```sh
#!/usr/bin/env bash
set -euo pipefail
R="$1"; rm -rf "$R"; mkdir -p "$R"
mkdir -p "$R/libs/beans/schema/migrations/postgres" "$R/apps/bean-counter"
printf 'module github.com/mattsp1290/beans/libs/beans\n\ngo 1.25.7\n' > "$R/libs/beans/go.mod"
printf 'package beans\n' > "$R/libs/beans/beans.go"
for n in 0001 0002 0003 0004 0005 0006 0007; do : > "$R/libs/beans/schema/migrations/postgres/${n}_x.sql"; done
cat > "$R/apps/bean-counter/go.mod" <<'GM'
module github.com/mattsp1290/beans/apps/bean-counter

go 1.25.7

require github.com/mattsp1290/beans/libs/beans v0.0.0

replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans
GM
printf 'package main\nfunc main(){}\n' > "$R/apps/bean-counter/main.go"
( cd "$R" && git init -q && git add -A && git -c user.email=a@b -c user.name=a commit -qm init )
```

Hostile out-of-tree migration set with a lower max, then the committed symlink:

```sh
mkdir -p "$S/outside/schema/migrations/postgres"
: > "$S/outside/schema/migrations/postgres/0001_x.sql"

bash "$S/mkrepo.sh" "$S/r1"
rm -rf "$S/r1/libs/beans/schema"
ln -s "$S/outside/schema" "$S/r1/libs/beans/schema"
( cd "$S/r1" && git add -A && git -c user.email=a@b -c user.name=a commit -qm evil )
```

Driver:

```sh
#!/usr/bin/env bash
set -uo pipefail
source /Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh
set +e
cd "$1" || exit 9
TARGET_SHA="$(git rev-parse HEAD)"; EMBEDDED_MAX=""
gs="$(git status --porcelain)"
require_repo_root        >/dev/null 2>&1; rr=$?
resolve_embedded_migration_max >/dev/null 2>&1
require_clean_local_ref  >/dev/null 2>&1; rc=$?
printf 'clean=%s require_repo_root=%s require_clean_local_ref=%s EMBEDDED_MAX=%s\n' \
  "$([ -z "$gs" ] && echo yes || echo no)" "$rr" "$rc" "${EMBEDDED_MAX:-ABORTED}"
```

Observed (every symlink depth works, and a symlinked individual `.sql` works
too):

```
baseline (no symlinks)                               clean=yes require_repo_root=0 require_clean_local_ref=0 EMBEDDED_MAX=7
libs/beans/schema -> outside                         clean=yes require_repo_root=0 require_clean_local_ref=0 EMBEDDED_MAX=1
libs/beans/schema/migrations -> outside              clean=yes require_repo_root=0 require_clean_local_ref=0 EMBEDDED_MAX=1
libs/beans/schema/migrations/postgres -> outside     clean=yes require_repo_root=0 require_clean_local_ref=0 EMBEDDED_MAX=1
```

Every gate green, worktree clean, HEAD == TARGET_SHA, and the number that
decides whether a shared production database may be migrated came from
`$S/outside`.

By contrast, the same fixture with `libs` itself symlinked (`$S/r2`) *is*
caught — but by this containment check, not by `require_repo_root`, and the
message says "outside the repository", not "symlink":

```
D. COMMITTED libs -> outside repo   require_repo_root=0
   rem: ERROR: github.com/mattsp1290/beans/libs/beans resolves to .../outlibs/beans, outside the repository at .../r2
```

### Suggested fix

Resolve the directory that is actually read, and containment-check *that*:

```sh
local mig_phys
mig_phys="$(cd "$beans_phys/schema/migrations/postgres" && pwd -P)" \
  || fatal "could not resolve the embedded migrations directory"
case "$mig_phys" in
  "$root_phys"/*) : ;;
  *) fatal "embedded migrations resolve to $mig_phys, outside the repository at $root_phys" ;;
esac
EMBEDDED_MAX="$(migration_max_from_dir "$mig_phys")"
```

That still leaves a symlinked individual `*.sql` file inside a contained
directory (fixture `r6` above, `0007_x.sql -> /dev/null`, produced
`EMBEDDED_MAX=3`), which only affects the *name*-derived number, not the
content — but the robust form is to reject symlinks under `libs/beans`
outright, e.g. `find libs/beans -type l -print -quit` must be empty.

---

## IMPORTANT 2 — `[ ! -L libs/beans ]` is the only structural check, and the same hole is open on the `apps/bean-counter` side

**`apps/bean-counter/scripts/deploy-production.sh:458-459`**

The comment on this check states the principle exactly right: "The replace gate
checks the TEXT of the directive; this checks what the text points at." The
principle is applied to one path. The file the replace gate *reads* —
`apps/bean-counter/go.mod` — and the module directory the local gates build
from are not checked at all.

### Reproduction

```sh
mkdir -p "$S/outapp"
cat > "$S/outapp/go.mod" <<'GM'
module github.com/mattsp1290/beans/apps/bean-counter

go 1.25.7

require github.com/mattsp1290/beans/libs/beans v0.0.0

replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans
GM
printf 'package main\nfunc main(){}\n' > "$S/outapp/main.go"

# F: the whole module directory is an out-of-tree symlink
bash "$S/mkrepo.sh" "$S/r7"
rm -rf "$S/r7/apps/bean-counter"; ln -s "$S/outapp" "$S/r7/apps/bean-counter"
( cd "$S/r7" && git add -A && git -c user.email=a@b -c user.name=a commit -qm e )

# G: only the gated file is an out-of-tree symlink
bash "$S/mkrepo.sh" "$S/r8"
rm -f "$S/r8/apps/bean-counter/go.mod"; ln -s "$S/outapp/go.mod" "$S/r8/apps/bean-counter/go.mod"
( cd "$S/r8" && git add -A && git -c user.email=a@b -c user.name=a commit -qm e )
```

Driven with the same driver as Critical 1:

```
F. apps/bean-counter symlink -> outside        clean=yes repo_root=0 clean_ref=0 EMBEDDED_MAX=7
G. apps/bean-counter/go.mod symlink -> outside clean=yes repo_root=0 clean_ref=0 EMBEDDED_MAX=7
```

### Honest scoping of the impact

This is **not** a replace-gate bypass. I flipped `$S/outapp/go.mod` to
`=> ../../libs/evil` under fixture G and the gate still rejected it, because it
reads through the symlink at gate time and the build reads the same live file:

```
apps/bean-counter/go.mod declares "replace .../libs/beans => ../../libs/evil";
the only allowed replace is "replace .../libs/beans => ../../libs/beans"
replace gate rc=1
```

What it *does* break is the script's own stated safety property (header comment,
lines 29-33): "the deploy records a single monorepo SHA, so uncommitted changes
anywhere make that SHA an incomplete description of what was tested." With an
out-of-tree symlink the content that was gated is not described by the recorded
SHA at all, and `git status --porcelain` stays empty across arbitrary changes to
the symlink target — I edited `$S/outapp/go.mod` between two `git status` runs
and both printed empty. On the remote, the same committed symlink resolves
against the *remote's* filesystem, so the bytes validated locally are not
necessarily the bytes built there.

### Suggested fix

Replace the single-component test with a tree-level one in `require_repo_root`,
covering both module roots:

```sh
local link
link="$(find libs/beans apps/bean-counter -type l -print -quit 2>/dev/null)"
[ -z "$link" ] \
  || fatal "symlink inside the deployed tree: $link; the deployed sources must be in-repo files"
```

(That also subsumes Critical 1's `schema` case and the `libs`-as-symlink case,
turning an incidental catch into a designed one with an accurate message.)

---

## IMPORTANT 3 — Three of the four pass-2 fixes have no regression coverage; mutating them leaves the suite at 42/42

**`apps/bean-counter/test/scripts/deploy-production_test.sh`**

I copied `scripts/deploy-production.sh`, `test/scripts/deploy-production_test.sh`
and `go.mod` into a scratch tree (the test resolves `SCRIPT` from its own
`BASH_SOURCE`, so a copy is self-contained), mutated the copy, and ran the suite.
Each mutation was diffed against the original to confirm it applied — the
`applied=` column is the number of changed lines.

| Mutation | applied | Result |
|---|---|---|
| M1 drop `LC_ALL=C` from `sed` (line 234) | 4 | **CAUGHT** — `FAIL - parser sees both directives past an invalid UTF-8 byte (expected '2', got '0')` |
| M2 drop `LC_ALL=C` from `awk` (line 236) | 2 | 42 passed, 0 failed |
| M3 revert to a plain `sed \| awk` pipeline (discards sed's status) | 4 | 42 passed, 0 failed |
| M4 drop the `\|\| { …; return 1; }` fail-closed at line 273 | 5 | 42 passed, 0 failed |
| M5 delete `[ ! -L libs/beans ]` (line 458) | 2 | 42 passed, 0 failed |
| M6 delete the containment `case` (lines 426-429) | 4 | 42 passed, 0 failed |
| M7 revert `require_repo_root` to logical `$PWD` (line 448) | 3 | 42 passed, 0 failed |
| M8 (control) `-gt 1` → `-gt 2` | 2 | 38 passed, 4 failed |
| M9 (control) exact compare → substring compare | 2 | 36 passed, 6 failed |

M8/M9 confirm the harness detects real regressions in the parts it *does* cover,
so M2–M7 passing is coverage absence, not harness failure.

The pass-2 commit message says the locale test was mutation-tested and that "the
count case fails, the rejection case still passes". M1 reproduces that exactly
and the claim is honest. But `grep -n 'require_repo_root\|resolve_embedded_migration_max\|symlink\|root_phys'`
over the test file returns nothing: the two path-trust fixes and the
status-capture fix are asserted only by the commit message.

### Suggested fix

Add hermetic tests that build a throwaway repo (the `mkrepo.sh` above is ~15
lines) and assert:

- `require_repo_root` rejects a symlinked `libs/beans` (kills M5);
- `require_repo_root` accepts a repo reached through a symlinked parent (kills M7 — this is the availability half of the pass-2 fix and is currently unasserted);
- `resolve_embedded_migration_max` rejects a `libs/beans` resolving outside the root (kills M6);
- `EMBEDDED_MAX` equals the in-repo max when `libs/beans/schema` is a symlink out of the tree, or the call aborts (this is Critical 1's test — it fails today).

For M3/M4 see Suggestions: I could not construct a fixture where reverting them
changes the verdict, so they are correct-but-inert rather than untested-and-load-bearing.
