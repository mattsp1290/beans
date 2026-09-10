# Suggestions

## S1 — `[ "$count" -gt 1 ]` fails open if `count` is ever non-numeric; `-ne 1` is free

**`apps/bean-counter/scripts/deploy-production.sh:285-286`**

```sh
count="$(printf '%s\n' "$directives" | wc -l | tr -d '[:space:]')"
if [ "$count" -gt 1 ]; then
```

`wc -l` does not undercount here — I verified that separately and it is in the
positive notes. The narrow issue is the failure mode. The only caller is
`check_sanctioned_replace … || fatal`, which suspends `set -e` inside the
function, so if `count` were ever empty or non-numeric bash prints
`[: : integer expression expected`, `[` returns 2, the `if` is simply false, and
execution falls through to the single-line comparison — which passes whenever
the *first* directive is the sanctioned one, regardless of how many follow:

```
$ count=""; directives=$'github.com/mattsp1290/beans/libs/beans => ../../libs/beans\ngithub.com/x/o => ../../libs/evil'
$ if [ "$count" -gt 1 ]; then echo ">1 taken"; else echo ">1 NOT taken (rc=$?)"; fi
bash: [: : integer expression expected
  >1 NOT taken (rc=2) -> falls through to first-line compare
  first==sanctioned -> would return 0 (ACCEPT with 2 directives)
```

I could not make `count` non-numeric with any input I tried, so this is
hardening, not a live bypass. `[ "$count" != "1" ]` (or `-ne 1` guarded by a
numeric `case`) closes the fall-through by construction and reads no worse.

---

## S2 — The gate is blind to a `replace` line prefixed by a non-`[[:space:]]` byte; only `go`'s own parser stops it, and that runs afterwards

**`apps/bean-counter/scripts/deploy-production.sh:236-247`**

`awk`'s recogniser is anchored `^[[:space:]]*replace[[:space:]]`. Under `LC_ALL=C`
that is space/tab/NL/VT/FF/CR only, so a UTF-8 BOM or a NBSP before `replace`
makes the directive invisible to the gate:

| fixture (evil replace preceded by…) | gate | what `go mod edit -json` resolves |
|---|---|---|
| baseline sanctioned | 0 | `../../libs/beans` |
| `\xef\xbb\xbf` (BOM) | **0 (accepts)** | GO-REJECTS |
| `\xc2\xa0` (NBSP) | **0 (accepts)** | GO-REJECTS |
| `\v` | 1 | GO-REJECTS |
| `\f` | 1 | GO-REJECTS |
| `\r` | 1 | `../../libs/evil` |
| latin-1 `\xe9` in a comment | 1 | `../../libs/evil` |
| tab after `replace` | 1 | `../../libs/evil` |

The important column is the join: **every row where `go` accepts the evil
replace, the gate rejects.** So this is not a bypass today. It is a bypass one
`go` release away, and the only thing standing behind it is the
`go list -m -json` call further down `require_clean_local_ref`.

Two options, either is fine:

1. Cheap: also reject any line matching `[Rr][Ee][Pp][Ll][Aa][Cc][Ee]` that the
   grammar did *not* classify, i.e. treat "there is a `replace` token I could
   not parse" as a parse failure (`return 1`), so the gate is independently
   sound instead of leaning on `go`.
2. Better: derive the directive list from `GOWORK=off go mod edit -json` and
   compare `.Replace[].Old.Path`/`.New.Path` exactly, keeping the current awk
   parser as a second, independent opinion that must agree. That removes the
   whole class of gate-vs-`go` divergence rather than testing for it.

---

## S3 — The commit message's claim about the command-substitution status capture is stronger than anything I could demonstrate

**`apps/bean-counter/scripts/deploy-production.sh:233-236, 273-277`**

The comment says the command substitution "is what lets a parser failure reject
rather than silently shorten the directive list." With `LC_ALL=C` in place I
could not find an input where reverting it changes the verdict:

- M3 (back to `sed | awk`) and M4 (drop the `|| return 1` handler) both leave the
  suite at 42/42.
- The natural candidate — an unreadable `go.mod` — returns 1 either way: with the
  handler it prints "could not parse its replace directives; refusing to guess",
  without it `directives` is empty and the `[ -z "$directives" ]` branch prints
  "declares no replace directive". Both `return 1`.
- `[ -f ]` passes for a mode-000 file, a directory, a FIFO, NUL-bearing files and
  an 8 MB single line; all of them fail closed.

The code is right and I would keep it. The suggestion is to soften the comment to
"defence in depth: with `LC_ALL=C` sed can now only fail on I/O, but a partial
read must never be treated as a complete directive list" — the current wording
implies a demonstrated behaviour, and a future reader auditing it will spend the
same hour I did looking for the case.

Also: `LC_ALL=C` on the `awk` side (line 236) is unasserted — M2 removes it and
the suite stays at 42/42, including the latin-1 fixture. Keep it, but the comment
attributes the whole fix to the `sed` side, which matches what the tests actually
prove.

---

## S4 — `resolve_embedded_migration_max` runs before any content gate

**`apps/bean-counter/scripts/deploy-production.sh:1045-1050`**

```sh
main() {
  parse_args "$@"
  require_repo_root
  resolve_target_sha
  resolve_embedded_migration_max     # <- go list -m on an unvetted go.mod
  case "$MODE" in ...
```

`check_sanctioned_replace` lives inside `require_clean_local_ref`, which only
`do_check` and `do_live` call. So `go list -m` runs against a `go.mod` whose
replace set has not been vetted, and `--dry-run` prints `embedded_max` in the
plan without any of the three content gates having run.

This is not exploitable as written — `go list -m` performs no build, `--dry-run`
does nothing but `ssh … true`, and the containment `case` covers the resolved
directory. But moving `resolve_embedded_migration_max` after
`require_clean_local_ref` (or hoisting `check_sanctioned_replace` into `main`)
would make the ordering match the intent, and would mean the dry-run plan shows
a number that passed the same gates the live plan's did.

---

## S5 — `migration_max_from_dir` derives the number from filenames only

**`apps/bean-counter/scripts/deploy-production.sh:186-199`**

Unrelated to the pass-2 fixes, noted because Critical 1 lands here: the max is
`${base%%_*}` of each `*.sql` name. A symlinked `0007_x.sql -> /dev/null` inside
an otherwise contained directory drops `EMBEDDED_MAX` from 7 to 3 (fixture `r6`).
If the parity number is meant to describe *what is compiled into the image*, the
authoritative source is the `embed.FS` the library actually ships, not the
directory listing. Deriving it from a `go run`/`go list` against the embedded
filesystem would make the local number and the shipped number the same object.

(I did check the `set -e` interaction in the loop —
`[ "$num" -gt "$max" ] && max="$num"` as the final command of the body does *not*
abort the function when the comparison is false; a directory with `0001_a.sql`,
`0002_a.sql`, `0002_b.sql` returns 2 with rc 0. No finding.)
