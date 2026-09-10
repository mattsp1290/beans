# Positive notes

## The parser rewrite is correct, and I could not defeat the grammar

`replace_directives` (`deploy-production.sh:222-237`) is a real `go.mod` parser
for the directive it cares about, not a pattern match, and it holds up. The
structure that makes it work:

```awk
    /^[[:space:]]*replace[[:space:]]*\(/ { inblock = 1; next }
    inblock && /^[[:space:]]*\)/         { inblock = 0; next }
    inblock                              { line = $0 }
    !inblock && /^[[:space:]]*replace[[:space:]]/ { … }
```

Treating **every** non-blank line inside an open block as a directive is the
right call. It means an unclosed `replace (` does not swallow the rest of the
file — the following `require`/`go` lines all surface as directives, the count
exceeds one, and the gate rejects. I tried to use an unclosed block to hide a
hostile entry and it fails closed every time. The same property makes the block
form safe to *accept* now, which is what let the commit drop the previous
"reject the block form outright" rule without losing ground.

## The exact-equality comparison closes the first pass's finding completely

`first="$(… head -n 1)"` then `[ "$first" != "$SANCTIONED_BEANS_REPLACE" ]`
(`:277-281`) is the right shape, and it kills both original attacks. Reverting
the function to the pre-`2ef178e` `grep -F` implementation and re-running the
suite unchanged:

```
FAIL - sanctioned replace with odd spacing accepted (expected rc 0, got 1)
FAIL - block form holding only the sanctioned entry accepted (expected rc 0, got 1)
FAIL - commented-out replace rejected (declares nothing) (expected rc 1, got 0)
FAIL - sibling path with the sanctioned text as a prefix rejected (expected rc 1, got 0)
FAIL - deeper path with the sanctioned text as a prefix rejected (expected rc 1, got 0)
FAIL - second replace hidden behind a repeated sanctioned comment rejected (expected rc 1, got 0)
32 passed, 6 failed
```

The suite kills the old gate on exactly the cases the commit message claims.

## The comment stripping happens to match Go's own lexer

`sed -e 's://.*::'` is more aggressive than it looks — it strips `//` from
anywhere on the line, including the middle of a filesystem path. My first
hypothesis was that this was exploitable via
`=> ../../libs/beans//../../../evil`, which the gate accepts as sanctioned. It
is not: Go's `go.mod` lexer treats that `//` as a comment too, and
`GOWORK=off go list -m -f '{{.Dir}}'` on that exact file returns the *real*
`libs/beans`. The gate and the module loader agree. Any form where they would
disagree requires quoting, and quoting survives normalization and fails the
equality check.

## Every rejection fires for the reason its label names

I dumped the actual stderr message for all eight `rejects` fixtures. Each one
takes the branch its label describes — the three "no directive" cases hit
`declares no replace directive`, the three path cases hit
`declares "replace …"; the only allowed replace is …`, and the three multi-entry
cases hit `declares 2 replace directives`. None is passing for an incidental
reason. Mutation testing confirms it from the other side: dropping the
`count > 1` branch (`:270-276`) fails exactly the three multi-entry cases;
dropping the exact-equality branch (`:278-282`) fails exactly the three path
cases; making `write_gomod` (`test/…:88`) drop the fixture body fails all four
`accepts` cases; making it write only the first fixture and then no-op fails all
eight `rejects` cases. **No vacuous case found.**

## `GOWORK=off` on both `go list` calls is the load-bearing fix

`:396` and `:445`. Without it `go.work` satisfies the module lookup regardless of
what the `replace` says, which would have made the text gate above it decorative —
the gate would check one thing and the command below it would prove a different
one. The inline comment at `:441-444` says exactly this, and it matches how the
`Dockerfile` builds (`ENV GOWORK=off`, `go.work` never copied). The gate, the
verification command, and the image now agree on one resolution mechanism.

## The guard is on every path that can change production

`main` (`:1003`) runs `require_repo_root` before `resolve_target_sha`, and both
`do_check` (`:975`) and `do_live` (`:985`) open with `require_clean_local_ref`,
which runs the replace gate at `:440`. `do_dry_run` skips it and correspondingly
mutates nothing (`print_plan` plus `ssh … true`). There is no `--force`, and the
argument parser rejects one (`--force rejected` is an asserted test case). I found
no path to a deploy that skips the gate.

## `require_clean_local_ref` makes the gate meaningful rather than advisory

`git status --porcelain` must be empty **and** `HEAD` must equal `TARGET_SHA`
(`:420-436`), and `resolve_target_sha` only accepts a SHA reachable from
`origin/main`. That chain is what makes a text check on the local `go.mod`
equivalent to a check on what actually ships: the file cannot be a local edit,
and the remote builds the same SHA. A linked `git worktree` satisfies this
correctly — I tested it — because the worktree's `HEAD` and shared refs still
bind it to the same history.

## Bash 3.2 discipline

Avoiding arrays (`:257-260`) is not superstition: `${#arr[@]}` on an empty array
under `set -u` really is an unbound-variable error on the bash 3.2 macOS ships,
and this script is documented as running from a Mac. Getting the platform
constraint right in the comment is what let me find the Critical finding — it is
the same "this runs on BSD userland" reasoning applied to `sed` instead of
`bash`.

## Clean tooling

`shellcheck apps/bean-counter/scripts/deploy-production.sh
apps/bean-counter/test/scripts/deploy-production_test.sh` is silent, and the
suite is 38/38 on this machine.
