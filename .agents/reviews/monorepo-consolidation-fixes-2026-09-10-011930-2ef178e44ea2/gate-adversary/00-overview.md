# Gate Adversary — second-pass review of `2ef178e`

- **Branch:** `monorepo-consolidation`
- **Date:** 2026-09-10
- **Reviewer:** Gate Adversary (`gate-adversary`)
- **Role:** Try to defeat the rewritten deploy replace gate and the repo-root guard, treating them as security controls rather than lint.

## Summary

The rewrite is a real improvement: `replace_directives` + exact-equality is
strictly stronger than the `grep -F` substring test it replaced, and I could not
defeat the *parser* itself. I threw the whole go.mod grammar at it — `replace(`
with no space, unclosed blocks, two blocks, a `)`-prefixed entry, CRLF, tabs,
versioned left-hand sides, quoted and backquoted targets, `\x2f` escapes, NBSP,
NUL bytes, `//` embedded in the replace path — and in every case the gate either
agreed with the real `go` module loader or failed closed. I cross-checked each
candidate against `go list -m` on a real two-module fixture rather than reasoning
about it. Mutation testing confirms the 12 fixture cases in the test file are
non-vacuous: reverting to the old `grep -F` gate kills 6 of them, dropping the
`count > 1` branch kills 3, dropping the exact-equality branch kills 3, and
neutering `write_gomod` kills 4 or 8 depending on the mutation.

However, the gate is defeatable at the byte level, not the grammar level. On
macOS — the platform the code comment explicitly says this script runs on — BSD
`sed` **aborts the whole stream** with `RE error: illegal byte sequence` the
moment it reads a line containing a byte that is not valid UTF-8 under the
ambient `en_US.UTF-8` locale. It exits after emitting the lines it already
processed, `check_sanctioned_replace` never inspects that exit status, and every
`replace` directive after the offending byte becomes invisible to the gate. A
`go.mod` whose first replace is the sanctioned one, followed anywhere by a single
Latin-1 byte (a `// café` comment is enough), followed by an arbitrary hostile
`replace`, is **accepted by the gate and parsed cleanly by Go** — I verified both
halves. That is the same class of failure the first pass found, one layer down,
and it fails open. Two lower-severity findings follow: `require_repo_root`
compares a logical `$PWD` to a physical `git rev-parse --show-toplevel`, so it
rejects any checkout reached through a symlinked path; and the gate validates the
directive *text* while nothing constrains what `libs/beans` actually is — a
committed symlink out of the tree passes every guard and feeds the production
parity gate a migration max read from outside the repository.

## Verdict

`REQUEST_CHANGES`

## Commit stats — `2ef178e`

```
$ git show --shortstat 2ef178e
 15 files changed, 243 insertions(+), 67 deletions(-)
```
