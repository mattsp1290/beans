# Positive Notes

## `require_in_repo` survived every escape I could construct — no bypass found

I sourced the real script and drove the real function against twenty input
shapes. Fixture:

```bash
source /Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh
set +e +u +o pipefail
repo="$(cd "$(mktemp -d)" && pwd -P)"; out="$(cd "$(mktemp -d)" && pwd -P)"
mkdir -p "$repo/libs/beans/schema/migrations/postgres" "$out/postgres"
REPO_ROOT_PHYS="$repo"
ln -s "$out"        "$repo/libs/outlink"
ln -s "$repo/libs/beans" "$repo/libs/inlink"
ln -s "$out/nope"   "$repo/libs/dangling"
mkdir -p "$repo/libs/a b" "$repo/libs/gl*ob" "$repo/libs/br[ack]et" "$repo/libs/nl"$'\n'"line"
mkfifo "$repo/libs/fifo"
( cd "$repo" && require_in_repo "<case>" )
```

Results — every escape rejected, every legitimate path accepted:

| Input | rc | Rejected by |
|---|---|---|
| `libs/outlink` (symlink out of tree) | 1 | `-L` |
| `libs/outlink/` (trailing slash defeats `-L`) | 1 | containment |
| `libs/outlink/.` | 1 | containment |
| `libs/outlink/postgres` | 1 | containment |
| `libs/inlink` (in-tree symlink) | 1 | `-L` |
| `libs/dangling` (dangling symlink) | 1 | `-e` — **correct**, fail closed |
| `../../etc/passwd` | 1 | `-e` |
| `/etc/passwd` (absolute) | 1 | containment (`/private/etc/passwd`) |
| `""` (empty string) | 1 | `-e` |
| `.` / the repo root itself | 1 | containment (strict descendant) |
| `libs/beans/..` | 0 | resolves to `$repo/libs` — in tree, correct |
| `libs/a b` (space) | 0 | correct |
| `libs/gl*ob` (glob star) | 0 | correct — quoting in the `case` pattern holds |
| `libs/br[ack]et` (brackets) | 0 | correct |
| `libs/nl\nline` (embedded newline) | 0 | correct |
| `libs//beans` (double slash) | 0 | correct |
| `libs/fifo` (FIFO, non-dir non-regular) | 0 | correct |

Two design points I want to call out because they are easy to get wrong and this
code gets them right:

- **The file branch's un-resolved `basename` re-append (line 246) is safe.** It
  looks like the previous pass's bug repeating, but it is not: `-L` at 239 has
  already excluded a symlinked leaf, and the dirname is resolved with
  `cd … && pwd -P` at 244, so `$dir/$(basename "$rel")` *is* the physical path.
  Mutating either half of that reasoning is caught (M5, M11, M12 all die).
- **The quoted `case` pattern is glob-safe.** `"$REPO_ROOT_PHYS"/*` keeps the
  expansion literal, so a repo root containing `*`, `?` or `[` cannot widen the
  match. Verified with `libs/gl*ob` and `libs/br[ack]et` fixtures.

## Rejecting a dangling symlink via `-e` is the right call

The brief asks whether this is correct. It is: a dangling link's target does not
exist *yet*, so nothing can be said about where it will point, and the deploy
has no `--force`. Failing closed with "does not exist" is the only sound answer.

## Every branch inside `require_in_repo` is covered by a test that kills it

Mutation testing of the helper's internals — eight mutations, all killed:

| # | Mutation | Killed by |
|---|---|---|
| M1/M13 | empty-`REPO_ROOT_PHYS` guard removed / neutered | `unset REPO_ROOT_PHYS rejected` |
| M2/M14 | `-e` existence check removed | `missing path rejected` |
| M3/M12 | `-L` symlink refusal removed | `in-tree symlink rejected` |
| M4 | dir branch stops resolving physically | `symlinked intermediate component rejected` |
| M5 | file branch stops resolving its dirname | `go.mod behind a symlinked parent rejected` |
| M8 | `-d` → `-e` (files take the dir branch) | `real file inside the repo accepted` |
| M10 | containment `case` always accepts | `symlinked intermediate component rejected` + `go.mod behind a symlinked parent rejected` |
| M11 | `-L` applied to the dirname instead of the leaf | `in-tree symlink rejected` |

The only helper-internal branch no test kills is the `/` separator in the
containment prefix (M6), covered in `01-critical-and-important.md`.

## The tests are genuinely portable — not self-fulfilling on macOS

The brief flags a specific risk: cases that only pass because macOS `mktemp -d`
hands back a `/var → /private/var` symlink. That risk is real in principle and
the suite defends against it correctly at lines 180-182, which normalise both
fixture roots with `cd … && pwd -P`. I verified the suite is indifferent to the
shape of `TMPDIR`:

```bash
# Linux-like: TMPDIR is already a physical path
TMPDIR=/…/scratchpad/lintmp bash apps/bean-counter/test/scripts/deploy-production_test.sh | tail -1
# -> 52 passed, 0 failed

# macOS-like, exaggerated: TMPDIR is itself a symlink
mkdir -p /…/realtmp; ln -sfn /…/realtmp /…/linktmp
TMPDIR=/…/linktmp bash apps/bean-counter/test/scripts/deploy-production_test.sh | tail -1
# -> 52 passed, 0 failed

# default macOS /var/folders/…
bash apps/bean-counter/test/scripts/deploy-production_test.sh | tail -1
# -> 52 passed, 0 failed
```

No assertion depends on the platform's symlink layout, and `${repo_tmp}-evil` is
derived from the already-normalised root, so it lands beside the fixture on both
platforms. This suite will pass on the GNU/Linux runner.

## Ordering and reachability are sound

Answering the brief's third question: `REPO_ROOT_PHYS` is initialised to `""` at
top level (line 216), so `set -u` never trips on it; `require_repo_root` (1113)
is the only writer and the only caller of `require_in_repo`; and the helper's
own `[ -z "$REPO_ROOT_PHYS" ]` guard fails closed if that order is ever
disturbed. There is no path to `check_sanctioned_replace`,
`resolve_embedded_migration_max`, or any remote mutating phase with the root
empty or stale. `EMBEDDED_MAX` derived from an unvetted replace target cannot
reach a remote phase either — both `do_check` and `do_live` run
`require_clean_local_ref` (and therefore the replace gate) before `remote_exec`.
See S4 for the residual dry-run reporting wrinkle.

## `resolve_embedded_migration_max` genuinely fixed the pass-3 bug

Line 480 resolves `migrations/postgres` itself with `cd … && pwd -P` and applies
containment to the result (481-484), rather than resolving `libs/beans` and
appending three unresolved components. That is the correct fix for the reported
defect, and it independently re-resolves after `require_repo_root`'s earlier
pass, which closes the TOCTOU window on the one value that gates the shared
database.

`migration_max_from_dir` is also symlink-indifferent by construction: it derives
the max from *filenames* (`"${base%%_*}"`), so symlinked `.sql` files inside a
validated directory cannot understate the count, and `[ -e "$f" ]` skips dangling
entries.

## Repository was not modified

All mutation work was done on copies under the session scratchpad. Confirmed
after the fact:

```
$ git status --porcelain --ignored=no
$ git diff HEAD --stat
```

Both empty.
