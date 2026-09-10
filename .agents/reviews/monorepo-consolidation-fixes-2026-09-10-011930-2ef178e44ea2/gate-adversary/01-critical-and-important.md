# Critical and Important

---

## CRITICAL — One non-UTF-8 byte anywhere in `go.mod` hides every later `replace` from the gate

**Severity:** Critical (verified bypass, fails open)
**File:** `apps/bean-counter/scripts/deploy-production.sh:223` (the `sed` in
`replace_directives`), consumed at
`apps/bean-counter/scripts/deploy-production.sh:261`

### What happens

```bash
replace_directives() {
  sed -e 's://.*::' "$1" | awk '
```

and

```bash
directives="$(replace_directives "$gomod" | sed '/^[[:space:]]*$/d')"
```

BSD `sed` (macOS) does not pass unknown bytes through. Under a UTF-8 locale it
aborts the entire stream with `RE error: illegal byte sequence` as soon as a
record contains a byte sequence that is not valid UTF-8, exiting non-zero **after
having already written the records it processed**. `check_sanctioned_replace`
never looks at that exit status — the assignment on line 261 discards it, and
because the only caller is `check_sanctioned_replace ... || fatal` (line 440),
`set -e` is suspended for the entire function body, so the non-zero status cannot
abort anything either.

The result is a truncated view of the file. Order the sanctioned replace first,
put one Latin-1 byte anywhere after it, and every subsequent `replace` directive
is invisible: the gate sees exactly one directive, it equals
`$SANCTIONED_BEANS_REPLACE`, and it returns 0.

### Exact fixture

```
module github.com/mattsp1290/beans/apps/bean-counter

go 1.21

require github.com/mattsp1290/beans/libs/beans v0.0.0

replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans
// caf<0xe9> latin-1 comment
replace github.com/mattsp1290/beans/libs/other => ../../libs/evil
```

The `<0xe9>` is a single raw byte — an `é` saved as Latin-1, which is what any
editor without UTF-8 defaults produces. It does not have to be in a comment, and
it does not have to be near the hostile line; anywhere between the sanctioned
replace and the hostile one works.

### Command that demonstrates it

```bash
D=$(mktemp -d)
printf 'module github.com/mattsp1290/beans/apps/bean-counter\n\ngo 1.21\n\nrequire github.com/mattsp1290/beans/libs/beans v0.0.0\n\nreplace github.com/mattsp1290/beans/libs/beans => ../../libs/beans\n// caf\xe9 latin-1 comment\nreplace github.com/mattsp1290/beans/libs/other => ../../libs/evil\n' > "$D/go.mod"

bash -c '
  source /Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh
  set +e
  echo "gate sees:"; replace_directives "'"$D"'/go.mod" 2>/dev/null
  check_sanctioned_replace "'"$D"'/go.mod" 2>/dev/null \
    && echo "ACCEPTED (BYPASS)" || echo "rejected"
'
```

Observed output on this machine (`awk version 20200816`, BSD sed,
`LANG=en_US.UTF-8`):

```
gate sees:
github.com/mattsp1290/beans/libs/beans => ../../libs/beans
ACCEPTED (BYPASS)
```

Reversing the two `replace` lines (hostile first) yields `rejected` — `sed` dies
before emitting anything, `$directives` is empty, and the "declares no replace
directive" branch fires. The attack requires only that the sanctioned line come
first, which is also the natural ordering.

### The deploy really proceeds

The two things downstream of this gate both accept the same file. On a real
two-module fixture:

```bash
$ GOWORK=off go list -m -json github.com/mattsp1290/beans/libs/beans >/dev/null 2>&1 \
    && echo "go list OK -> gate passes"
go list OK -> gate passes
```

So `require_clean_local_ref`'s second half (line 445) passes, and Go's own
`go.mod` parser is entirely happy with the file — the invalid byte sits inside a
comment, which Go's lexer skips without validating encoding. Nothing else between
here and the remote deploy re-checks the replace set.

### Root cause is locale, and the status is available but unused

```bash
$ LC_ALL=C  # same fixture
rejected
$ LC_ALL=C replace_directives "$D/go.mod"
github.com/mattsp1290/beans/libs/beans => ../../libs/beans
github.com/gofiber/fiber/v3 => ../evil        # both directives now visible
```

And with `pipefail` on (which is the live-deploy state), the pipeline *does*
report the failure — it is simply thrown away:

```bash
$ set -o pipefail;  replace_directives "$D/go.mod" >/dev/null 2>&1; echo $?
1
$ set +o pipefail;  replace_directives "$D/go.mod" >/dev/null 2>&1; echo $?
0
```

Note the second line: the unit-test harness runs `set +e +u +o pipefail`
(`deploy-production_test.sh:20`), so a fix that relies on `pipefail` alone would
be untested by the suite. Check the status explicitly.

### Suggested fix

Force the byte-oriented locale *and* make a parser failure fail closed, rather
than either one alone:

```bash
replace_directives() {
  # LC_ALL=C: BSD sed/awk abort the stream on a byte that is not valid UTF-8
  # under a UTF-8 locale, silently truncating the directive list. Byte
  # semantics make the parse total. The `|| return 1` is the belt to that
  # braces: a parser that dies must never look like "no more directives".
  LC_ALL=C sed -e 's://.*::' -- "$1" | LC_ALL=C awk '
    ...unchanged...
  '
}

check_sanctioned_replace() {
  local gomod="$1"
  [ -f "$gomod" ] || { printf '%s: no such file\n' "$gomod" >&2; return 1; }

  local directives count first
  if ! directives="$( LC_ALL=C; set -o pipefail
        replace_directives "$gomod" | sed '/^[[:space:]]*$/d' )"; then
    printf '%s: could not parse replace directives (parser failed)\n' "$gomod" >&2
    return 1
  fi
  ...
}
```

A regression test belongs with it, but see the caveat in `04-action-items.md`:
the suite runs on `ubuntu-latest`, where GNU `sed` passes the byte through and
the bypass does not reproduce, so the test must assert the *directive list*
(both entries present), not just the rejection.

---

## IMPORTANT — `require_repo_root` compares a logical `$PWD` to a physical toplevel, so a symlinked checkout can never deploy

**Severity:** Important (fails closed; blocks a legitimate deploy)
**File:** `apps/bean-counter/scripts/deploy-production.sh:414-415`

```bash
  [ "$toplevel" = "$PWD" ] \
    || fatal "run this from the repository root ($toplevel), not $PWD"
```

`git rev-parse --show-toplevel` returns the **physical** path (symlinks
resolved). Bash's `$PWD` is the **logical** path (symlinks preserved, because
`cd` is logical by default). They disagree for any checkout reached through a
symlinked path component — a symlinked `~/git`, a repo under `/tmp` (which is
`/private/tmp` on macOS), or an `~/src -> /Volumes/…` arrangement.

Demonstration against a fixture repo with the same layout:

```bash
$ ( cd $R/real && require_repo_root && echo PASS )
PASS
$ ( cd $R/link && require_repo_root )      # link -> real, same repository
[deploy-production] ERROR: run this from the repository root (…/rr/real), not …/rr/link
```

Nothing is wrong with that checkout; the guard just cannot recognise it. Because
there is no `--force`, the operator's only options during an incident are to
`cd` to the physical path or edit the deploy script.

### Suggested fix

Compare physical to physical:

```bash
require_repo_root() {
  local toplevel here
  toplevel="$(git rev-parse --show-toplevel 2>/dev/null)" \
    || fatal "not inside a git worktree; run this from the repository root"
  # -P: git reports the physical path, and bash's $PWD is logical, so a repo
  # reached through a symlinked path (a symlinked ~/git, /tmp on macOS) would
  # otherwise never match.
  here="$(pwd -P)"
  [ "$toplevel" = "$here" ] \
    || fatal "run this from the repository root ($toplevel), not $here"
  [ -f apps/bean-counter/go.mod ] && [ -d libs/beans ] \
    || fatal "$here does not look like the beans monorepo (expected apps/bean-counter/go.mod and libs/beans/)"
}
```

For the record, I also tested the two cases the review brief asked about and both
behave correctly: a **linked `git worktree`** passes (its `--show-toplevel` is its
own root, and `require_clean_local_ref`'s clean + `HEAD == TARGET_SHA` checks plus
`resolve_target_sha`'s `origin/main` reachability check still bind it to the same
history), and running from `apps/bean-counter` aborts as intended.

---

## IMPORTANT — The gate validates the directive *text*; nothing constrains what `libs/beans` is

**Severity:** Important (defense-in-depth gap; requires a commit on `main`)
**File:** `apps/bean-counter/scripts/deploy-production.sh:254-283` (the gate) and
`:416` (`[ -d libs/beans ]`)

`check_sanctioned_replace` proves the go.mod says `=> ../../libs/beans`. It does
not prove `libs/beans` is the in-repo library. `libs/beans` can be a git-tracked
symlink pointing anywhere, including out of the worktree, and every guard passes:

```bash
# fixture: libs/ is a real directory, libs/beans is a committed symlink to ../../outside
$ git ls-files -s
100644 …	apps/bean-counter/go.mod
120000 …	libs/beans                       # mode 120000 = symlink, tracked

$ ( cd $R && require_repo_root && echo "require_repo_root: PASS" )
require_repo_root: PASS
$ ( cd $R && check_sanctioned_replace apps/bean-counter/go.mod && echo "replace gate: PASS" )
replace gate: PASS
$ ( cd $R && migration_max_from_dir libs/beans/schema/migrations/postgres )
42
```

`[ -d libs/beans ]` follows the link, so `require_repo_root` cannot see it, and
`git status --porcelain` is clean because the symlink is committed.

The consequence I actually verified is about the **parity gate**, not the image:
`resolve_embedded_migration_max` (line ~396) resolves `.Dir` through the replace
and reads `$beans_dir/schema/migrations/postgres`, so `EMBEDDED_MAX` — the value
that decides whether the deploy is allowed to touch the shared production
Postgres — is computed from a directory outside the repository, from a path that
no gate constrains. In the fixture above the deploy would carry `EMBEDDED_MAX=42`
into the remote parity comparison. An artificially *low* value is the dangerous
direction: it makes a genuinely-newer image look parity-safe.

I could **not** verify what `COPY libs/beans ./libs/beans` does with a symlinked
directory (no Docker daemon available on this machine), so I am making no claim
about whether hostile code would actually reach the image — only that the parity
input escapes the repo and that the symlink is invisible to every check in the
script.

### Suggested fix

Assert the target is a real in-repo directory, next to the text check:

```bash
require_repo_root() {
  ...
  [ -f apps/bean-counter/go.mod ] && [ -d libs/beans ] \
    || fatal "$here does not look like the beans monorepo (expected apps/bean-counter/go.mod and libs/beans/)"
  # The replace gate checks the directive TEXT; this checks what that text
  # points AT. A committed symlink would otherwise satisfy both the gate and
  # -d, and feed the production parity gate a migration set from outside the
  # repository.
  [ ! -L libs/beans ] \
    || fatal "libs/beans is a symlink ($(readlink libs/beans)); the deploy requires the in-repo library"
  [ "$(cd libs/beans && pwd -P)" = "$here/libs/beans" ] \
    || fatal "libs/beans does not resolve inside $here"
}
```

---

## What I tried that did **not** produce a bypass

Recorded so the next pass does not repeat it. Every one of these was run against
the real `replace_directives` / `check_sanctioned_replace` and, where the answer
depended on Go's own parser, cross-checked with `GOWORK=off go list -m -f
'{{.Dir}}'` on a real two-module fixture (`go1.23.4 darwin/arm64`).

| Attack | Gate | Go | Verdict |
|---|---|---|---|
| `=> ../../libs/beans//../evil` (`//` inside the path) | accepts, parses as sanctioned | resolves to `libs/beans` — Go treats `//` as a comment too | **not a bypass**; `sed -e 's://.*::'` matches Go's lexer here |
| `replace(` with no space + extra entry | rejected (2 directives) | — | fail-closed |
| block opened, never closed | accepts when it holds only the sanctioned entry; any extra line becomes a directive → rejected | — | correct |
| two separate `replace ( … )` blocks | rejected (2 directives) | — | fail-closed |
| `) github.com/… => ../../libs/evil` inside a block (awk's `^\s*\)` swallows it) | accepts | `syntax error (expected newline after closing paren)` | fail-closed — Go rejects the file before any build |
| CRLF line endings, hostile and sanctioned | correct in both directions (`\r` is `[[:space:]]`) | — | correct |
| versioned LHS `replace mod v0.1.0 => ../../libs/beans` | rejected (text differs) | — | fail-closed |
| quoted target `=> "../../libs/beans"` | rejected | resolves fine — this is legal go.mod | over-strict, fails closed (see `02-suggestions.md`) |
| backquoted target | rejected | `invalid quoted string` | fail-closed |
| multi-line raw string spanning the line-based parser | rejected | `unexpected newline in string` | fail-closed |
| `"../..\x2flibs/evil"` (escape hides the separator from a text check) | rejected (quotes survive normalization) | resolves to `libs/evil` | fail-closed |
| NBSP (`\xc2\xa0`) as a separator | rejected | — | fail-closed |
| embedded NUL byte | rejected (both directives still emitted) | — | fail-closed |
| leading spaces / tabs before `replace` | normalized correctly | accepted | correct |
| commented-out replace as the only one | rejected, "declares no replace directive" | — | correct |
| sanctioned line inside a `require (` block | not emitted | — | correct |

I also confirmed the gate is on every path that can change production:
`main` (line 1003) runs `require_repo_root` before anything else, and both
`do_check` (line 975) and `do_live` (line 985) begin with `require_clean_local_ref`,
which runs `check_sanctioned_replace` at line 440. `do_dry_run` does not — it only
prints the plan and runs `ssh … true`, mutating nothing.
`shellcheck` is clean on both the script and the test file.
