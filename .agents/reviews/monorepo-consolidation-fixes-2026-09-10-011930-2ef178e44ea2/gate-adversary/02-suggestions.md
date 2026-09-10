# Suggestions (non-blocking)

---

## 1. The gate rejects a quoted replace target, which is legal `go.mod`

**File:** `apps/bean-counter/scripts/deploy-production.sh:278`

`replace github.com/mattsp1290/beans/libs/beans => "../../libs/beans"` is valid
`go.mod` syntax and resolves correctly — I confirmed it:

```bash
$ GOWORK=off go list -m -f '{{.Dir}}' github.com/mattsp1290/beans/libs/beans
…/gotest/repo/libs/beans
```

but the gate rejects it, because normalization keeps the quotes and exact
equality then fails. `go mod edit -replace=...` does not emit quotes today, so
this is unlikely to bite, and it fails closed. If you want to close the gap
without weakening the comparison, unquote a target that is quoted end-to-end
before comparing:

```bash
  first="$(printf '%s\n' "$directives" | head -n 1)"
  # A fully-quoted path is legal go.mod and resolves identically; strip the
  # quotes rather than rejecting the form. Anything else keeps its quotes and
  # therefore still fails the exact-equality check below.
  first="$(printf '%s' "$first" | sed -e 's:=> "\([^"]*\)"$:=> \1:')"
```

Alternatively, leave it and document in the function comment that quoted forms
are deliberately unsupported — this is a gate, and "the one blessed spelling" is
a defensible contract.

---

## 2. `resolve_embedded_migration_max` runs before the replace gate

**File:** `apps/bean-counter/scripts/deploy-production.sh:1003-1007`

```bash
main() {
  parse_args "$@"
  require_repo_root
  resolve_target_sha
  resolve_embedded_migration_max     # reads through the replace...

  case "$MODE" in
    ...
    check)   do_check ;;             # ...and only here does the replace gate run
```

`resolve_embedded_migration_max` resolves `.Dir` through an as-yet-unvalidated
`replace` and computes `EMBEDDED_MAX` from whatever it points at. In `--check`
and live mode `require_clean_local_ref` aborts afterwards, so nothing reaches the
remote — but the ordering means the safety *input* is derived before the check
that validates its provenance, and in `--dry-run` the printed plan reports a
value the gate never blessed. Hoisting the cheap text gate above it makes the
dependency order match the trust order:

```bash
main() {
  parse_args "$@"
  require_repo_root
  # The replace gate is a pure text check and gates where the migration set is
  # read from, so it must run before anything resolves through the replace.
  check_sanctioned_replace apps/bean-counter/go.mod \
    || fatal "apps/bean-counter/go.mod failed the beans replace gate"
  resolve_target_sha
  resolve_embedded_migration_max
```

(keeping the call inside `require_clean_local_ref` as well, or dropping it there —
either is fine, it is idempotent.)

---

## 3. `sed`/`awk` argument safety: use `--`

**File:** `apps/bean-counter/scripts/deploy-production.sh:223`

```bash
  sed -e 's://.*::' "$1" | awk '
```

`"$1"` is the only non-option argument; a path beginning with `-` would be
consumed as an option. Today the sole caller passes the literal
`apps/bean-counter/go.mod`, so this is hygiene rather than a live bug, but the
function reads as a general-purpose helper:

```bash
  LC_ALL=C sed -e 's://.*::' -- "$1" | LC_ALL=C awk '
```

---

## 4. `--dry-run` reports a plan no gate has validated

**File:** `apps/bean-counter/scripts/deploy-production.sh:963-972`

`do_dry_run` prints the plan (including `EMBEDDED_MAX`) and checks SSH, but never
calls `require_clean_local_ref`, so neither the clean-worktree check, the
`HEAD == TARGET_SHA` check, nor the replace gate runs. That is defensible — a dry
run mutates nothing — but it means `--dry-run` cannot be used as a
"would this deploy pass?" rehearsal, which is what an operator will reach for
first. Either run the local gates in dry-run mode, or say so in the plan output:

```bash
do_dry_run() {
  print_plan
  log "dry-run: local gates (clean worktree, HEAD==target, replace gate) NOT run; use --check for those"
```

---

## 5. The replace-gate suite runs only where the Critical bypass cannot reproduce

**File:** `.github/workflows/ci-apps-bean-counter.yml` (the `scripts` job that
runs `bash apps/bean-counter/test/scripts/deploy-production_test.sh`)

The suite runs on `ubuntu-latest`, i.e. GNU `sed`, which passes an invalid UTF-8
byte through instead of aborting. The Critical finding in
`01-critical-and-important.md` therefore does not reproduce in CI at all. A
regression test written as "this fixture is rejected" would be green on CI
whether or not the fix landed. Assert the parsed output instead, which is
platform-independent:

```bash
# Both directives must be visible to the parser regardless of encoding: BSD sed
# aborts the stream on a non-UTF-8 byte under a UTF-8 locale, which used to hide
# every directive after it.
printf '%s\n// caf\xe9\n%s\n' "$SANCTIONED_LINE" \
  "replace github.com/gofiber/fiber/v3 => ../../../fiber-fork" > "$gomod_tmp/enc.mod"
out="$(replace_directives "$gomod_tmp/enc.mod" 2>/dev/null | wc -l | tr -d '[:space:]')"
assert_eq "non-UTF-8 byte does not truncate the directive list" "2" "$out"
```

Note that the harness runs `set +o pipefail` at line 20; if the fix is written to
depend on `pipefail` propagating out of `replace_directives`, add a
`set -o pipefail` around this block or the test proves nothing.

---

## 6. Consider adding the byte-level and structural cases as fixtures

**File:** `apps/bean-counter/test/scripts/deploy-production_test.sh:103-131`

The 12 fixtures cover the grammar well. Three cheap additions cover the classes I
probed that are currently unasserted, and all three would have caught a
regression in the parser rewrite:

```bash
rejects "replace hidden behind a non-UTF-8 byte rejected" \
  "$SANCTIONED_LINE
// caf$(printf '\xe9')
replace github.com/gofiber/fiber/v3 => ../../../fiber-fork"
rejects "CRLF hostile replace rejected" \
  "$(printf 'replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans-fork\r')"
rejects "versioned left-hand side rejected" \
  "replace github.com/mattsp1290/beans/libs/beans v0.1.0 => ../../libs/beans"
```
