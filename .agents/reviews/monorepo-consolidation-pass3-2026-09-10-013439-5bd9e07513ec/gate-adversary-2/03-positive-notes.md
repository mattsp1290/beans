# Positive Notes

These are the attacks the brief asked for that **did not work**. Each was run,
not reasoned about.

## The containment `case` is genuinely closed

`case "$beans_phys" in "$root_phys"/*)` (line 426-429) survives both attacks the
brief flagged:

```
/repo-evil/libs/beans        OUTSIDE      <- sibling-prefix defeat closed by the trailing slash
/repo/libs/beans             INSIDE
/repoX                       OUTSIDE
/repo                        OUTSIDE      <- the root itself is not "inside", correctly

root=/re?o  /repo/libs/beans   OUTSIDE    <- quoting makes the metacharacter literal
root=/re?o  /re?o/libs/beans   INSIDE
root=/re*   /repo-evil/x       OUTSIDE
root=/re*   /re*/x             INSIDE
```

The `"$root_phys"` is inside double quotes *within* the pattern, which is
exactly what makes glob metacharacters in the root literal — a `case` pattern
built by unquoted expansion would not have this property. Both sides come from
`pwd -P`, so neither can contain `..` for the `/repo/../evil/x` shape to matter.

## `wc -l` does not undercount

`printf '%s\n' "$directives" | wc -l` always terminates the final line, so the
count is right even though command substitution stripped the file's own trailing
newline. Verified across fixtures:

```
no trailing newline (evil last, no \n)         parse_rc=0 count=2 gate_rc=1
sanctioned only, no trailing newline           parse_rc=0 count=1 gate_rc=0
sanctioned + trailing blank lines              parse_rc=0 count=1 gate_rc=0
CRLF everywhere                                parse_rc=0 count=2 gate_rc=1
CR on sanctioned line only                     parse_rc=0 count=2 gate_rc=1
```

## `awk`'s exit status is not discarded

`printf … | LC_ALL=C awk …` is the last command in `replace_directives`, so
`awk`'s status *is* the function's return value. With a stub `awk` that exits 3
on `PATH`:

```
replace_directives rc with failing awk = 3
gate rc with failing awk = 1
```

and with a stub `sed` exiting 4 the `|| return 1` handler fires:

```
ok.mod: could not parse its replace directives; refusing to guess
```

## `local stripped` is split from the assignment

Line 233-234 declares `local stripped` and assigns on the next line. The classic
`local x="$(cmd)"` bug — where `local`'s own success masks the substitution's
status and `|| return 1` never fires — is avoided.

## No gate-vs-`go` parser divergence across 18 fixtures

The dangerous direction is *gate accepts and `go` resolves somewhere hostile*.
I found no such row. The notable near-misses:

- `=> ../../libs/beans//../evil` — the `//`-strip escape that would make the gate
  read the sanctioned text. `go` treats bare `//` as a comment too, and resolves
  `../../libs/beans`. Gate and `go` agree.
- `=> ../../libs/beans\r` — `awk`'s `[[:space:]]` trim strips the CR and the gate
  accepts; `go` also strips it and resolves `../../libs/beans`. Agreement again.
- One-line block `replace ( … => ../../libs/evil )` — gate rejects, and `go` also
  rejects (`version "…" invalid: must be of the form v1.2.3`). Closing paren on
  the entry line: gate rejects, `go` reports unterminated block.
- Quoted target `=> "../../libs/evil"` — `go` accepts, gate rejects.
- NUL bytes: bash's command substitution drops them with a warning, but `go`
  rejects the file outright (`unexpected input character '\x00'`), and the gate
  rejects too.
- 8 MB single line followed by a hostile replace: parsed, both directives seen,
  rejected.
- Unreadable `go.mod` (mode 000), `go.mod` that is a directory, missing file: all
  return 1.

## Every mutating path runs the replace gate

- `main` → `require_repo_root` unconditionally, before mode dispatch.
- `do_check` → `require_clean_local_ref` → `check_sanctioned_replace` (line 482),
  then a `remote_check_payload` I read end to end: `git status`, `docker ps`,
  `docker inspect`, `docker network inspect`, two read-only `psql -tAc` selects,
  one `docker run --rm … grep` against the secret. No writes, no lock, no compose.
- `do_live` → `require_clean_local_ref` → `check_sanctioned_replace`, then
  `local_gates`, then the locked remote session.
- `do_dry_run` skips `require_clean_local_ref` but performs no remote mutation
  (`ssh -o BatchMode=yes … true`).
- `--force` and `--skip-smoke` are explicitly rejected in `parse_args` with a
  `fatal`. `--skip-integration`, `--skip-local-build` and `--no-rebuild` skip
  heavy gates only; none of them can reach a remote mutation without
  `check_sanctioned_replace` having returned 0.

**There is no argument combination that reaches a mutating remote phase without
the replace gate.**

## The pass-2 regression test is honest for the case it names

Removing `LC_ALL=C` from the `sed` (M1) produces exactly:

```
FAIL - parser sees both directives past an invalid UTF-8 byte (expected '2', got '0')
41 passed, 1 failed
```

while `replace hidden after an invalid UTF-8 byte rejected` still passes — the
count assertion is load-bearing and the rejection-only assertion is not, precisely
as the commit message claims. (The count is 0, not 1: under a UTF-8 locale BSD sed
aborted before emitting anything for this fixture, so the file read as "no replace
directive" and was rejected for the wrong reason. That is the failure mode the
count assertion exists to expose.)

The two control mutations confirm the harness has teeth: `-gt 1` → `-gt 2` fails
4 tests, exact-compare → substring-compare fails 6 including the real
`apps/bean-counter/go.mod`.

## Pass-1 and pass-2 regressions hold

- `=> ../../libs/beans-attacker-fork` and a commented-out replace are still
  rejected (covered by the existing suite, re-confirmed by the M9 control).
- A committed `libs/beans` symlink is refused by `require_repo_root` with the
  intended message.

## Repository left untouched

`git status --porcelain` is empty. All mutation testing was done on copies under
the session scratchpad; the only files written into the repo are the five review
files in this directory.
