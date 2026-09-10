# Pass 3 — Gate Adversary II

- **Branch**: `monorepo-consolidation`
- **HEAD reviewed**: `5bd9e07513ecc5e132ec2aa03002b12a3f3bed5c` ("Address the second review pass")
- **Date**: 2026-09-10
- **Reviewer**: Gate Adversary II (`gate-adversary-2`)
- **Role**: Re-attack the deploy gates after the locale and symlink fixes, and check the fixes did not open a new hole.
- **Stats** (`git show --shortstat 5bd9e07`): 6 files changed, 119 insertions(+), 22 deletions(-)

## Summary

The pass-2 parser fix holds. I ran 18 differential fixtures comparing
`check_sanctioned_replace`'s verdict against what real `go` resolves from the
same `go.mod`, and found **no case where the gate accepts a file that `go`
resolves to a non-sanctioned path** — including the `//`-inside-a-path escape
(`=> ../../libs/beans//../evil`), which `go` also treats as a comment, so gate
and `go` agree. `wc -l` does not undercount (the `printf '%s\n'` always
terminates the last line, verified against no-trailing-newline, trailing-blank,
CRLF and NUL fixtures), `awk`'s exit status is *not* discarded (it is the last
element of the pipeline, so it is `replace_directives`'s return value — a stub
`awk` exiting 3 makes the gate return 1), the `case "$beans_phys" in
"$root_phys"/*)` containment is genuinely closed against both the sibling-prefix
defeat (`/repo-evil` vs `/repo`) and glob metacharacters in the root, and every
mutating path reaches `check_sanctioned_replace` before it can touch the remote.
The pass-2 regression test is honest for the case it names: removing `LC_ALL=C`
from `sed` makes exactly "parser sees both directives past an invalid UTF-8
byte" fail (2 → 0) while the rejection-only assertion still passes, which is
precisely the distinction the commit message claimed was load-bearing.

The fix is nonetheless **incomplete in the same direction it was aimed**.
`resolve_embedded_migration_max` containment-checks `beans_dir`, then reads
`"$beans_phys/schema/migrations/postgres"` — a path whose `schema`,
`migrations` or `postgres` component may itself be a symlink out of the tree.
With a committed `libs/beans/schema` symlink, `git status --porcelain` is empty,
`require_repo_root` returns 0, `require_clean_local_ref` returns 0, and
`EMBEDDED_MAX` comes back **1 instead of 7**, from outside the repository. The
remote parity gate fails only when `embedded_max > db_max`, so understating it is
exactly the direction that lets this deploy migrate a Postgres bean-counter does
not own. Separately, `[ ! -L libs/beans ]` is the only structural check in the
tree and covers one component: `apps/bean-counter` and `apps/bean-counter/go.mod`
can both be out-of-tree symlinks with every gate green, and `libs` as a symlink
is caught only incidentally, by a containment check in a different function. And
three of the four pass-2 fixes have zero regression coverage — deleting the
`libs/beans` symlink refusal, deleting the containment `case`, or reverting
`require_repo_root` to logical `$PWD` each leaves the suite at 42/42.

## Verdict

**REQUEST_CHANGES**
