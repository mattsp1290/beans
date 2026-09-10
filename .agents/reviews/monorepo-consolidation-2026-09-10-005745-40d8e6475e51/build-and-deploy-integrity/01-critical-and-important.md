# Critical and Important

## Critical

### C1 — The new replace gate accepts a replace pointing outside the sanctioned directory; the old gate rejected it

**File:** `/Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh:215`, `:227-248` (specifically `:231` and `:243`)

`check_sanctioned_replace` compares with `grep -F`, which is a **substring** match, not a
line match. Every check in the function therefore treats any line that merely *starts with*
the sanctioned text as sanctioned:

```bash
SANCTIONED_BEANS_REPLACE='replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans'
...
  if ! grep -qF "$SANCTIONED_BEANS_REPLACE" "$gomod"; then          # :231  substring
...
  if grep -E '^[[:space:]]*replace[[:space:]]' "$gomod" | grep -qvF "$SANCTIONED_BEANS_REPLACE"; then  # :243  substring
```

I sourced the real script and called the real function. Confirmed accepted (rc=0):

| `go.mod` content | Result |
| --- | --- |
| `replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans-attacker-fork` | **accepted** |
| `replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans/vendored-fork` | **accepted** |

The old gate this replaced was:

```bash
if grep -nE 'replace[[:space:]]+.*github\.com/mattsp1290/beans' go.mod >/dev/null 2>&1; then
  fatal "go.mod contains a local replace for $BEANS_MODULE; remove it before deploying"
fi
```

That pattern matched *any* replace naming the beans module, including both rows above. So
the property the commit message claims is preserved — "a replace aimed anywhere else must
still stop the deploy" — is exactly the property that was lost.

The second row is the dangerous one: `../../libs/beans/vendored-fork` lives inside the tree
that `apps/bean-counter/Dockerfile:31` copies wholesale (`COPY libs/beans ./libs/beans`), so
it passes the gate, passes the subsequent `go list -m -json` check, builds successfully in
the container, and ships. Nothing downstream catches it.

**Suggested fix** — stop substring-matching. Strip comments, normalise whitespace, and
require the set of real replace directives to equal exactly one sanctioned line. One
equality test then covers all four properties (present, exact, sole, not a block):

```bash
check_sanctioned_replace() {
  local gomod="$1"
  [ -f "$gomod" ] || { printf '%s: no such file\n' "$gomod" >&2; return 1; }

  # Real directives only: line comments cannot satisfy or hide anything.
  local directives
  directives="$(sed -e 's://.*$::' "$gomod" \
    | grep -E '^[[:space:]]*replace([[:space:]]|\()' || true)"

  # Block form (`replace (` ... `)`) would hide extra entries from the line scan below.
  if printf '%s\n' "$directives" | grep -qE '^[[:space:]]*replace[[:space:]]*\('; then
    printf '%s uses a replace block; only the single sanctioned replace line is allowed\n' "$gomod" >&2
    return 1
  fi

  local normalised
  normalised="$(printf '%s\n' "$directives" \
    | sed -e 's/[[:space:]]\{1,\}/ /g' -e 's/^ //' -e 's/ $//' \
    | grep -v '^$' || true)"

  if [ "$normalised" != "$SANCTIONED_BEANS_REPLACE" ]; then
    printf '%s must contain exactly one replace directive, and it must be: %s\n' \
      "$gomod" "$SANCTIONED_BEANS_REPLACE" >&2
    printf 'found: %s\n' "${normalised:-<none>}" >&2
    return 1
  fi
  return 0
}
```

I ran this replacement against every case in the existing test file plus all four bypasses
plus whitespace and trailing-comment variants: all 13 behave as intended, and the real
`apps/bean-counter/go.mod` still returns 0, so a deploy from a clean checkout is not
aborted by its own gate.

---

### C2 — `ci-libs-beans` installs a golangci-lint that cannot load `libs/beans/.golangci.yml`; the `make ci` step fails on every run

**File:** `/Users/punk1290/git/beans/.github/workflows/ci-libs-beans.yml:50-54`, with `/Users/punk1290/git/beans/libs/beans/.golangci.yml:4`

```yaml
      - name: Install golangci-lint
        run: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.1.6

      - name: CI gates
        run: make ci
```

`go install` of the pinned version works (I ran it with `GOWORK=off` from `libs/beans`, it
produced a binary), and setup-go does put `$(go env GOPATH)/bin` on PATH — so the two
questions the workflow comment worries about are both fine. The failure is elsewhere:
golangci-lint v2.1.6's own `go.mod` caps its language version, so the resulting binary
reports itself as built with go1.23.4, and `libs/beans/.golangci.yml:4` sets
`run.go: "1.25"`. Reproduced verbatim:

```
$ golangci-lint version
golangci-lint has version v2.1.6 built with go1.23.4 ...
$ cd libs/beans && GOWORK=off golangci-lint run ; echo $?
Error: can't load config: the Go language version (go1.23) used to build golangci-lint is lower than the targeted Go version (1.25)
3
```

`libs/beans/Makefile:31` is `ci: tidy-check vet lint test build`, so the `CI gates` step
fails at `lint` — every push and PR that touches `libs/beans/**`. The `run.go: "1.25"` line
is inherited unchanged from `main`'s root `.golangci.yml`; what is new on this branch is
pinning a lint binary too old to read it.

**Suggested fix** — mirror what `apps/bean-counter/Makefile:2-3` already does. That fixes
this, removes the version drift between CI and a developer's PATH, and deletes the install
step entirely:

```make
# libs/beans/Makefile
GO ?= go
GOLANGCI_LINT_VERSION ?= v2.12.2
GOLANGCI_LINT ?= $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

lint:
	$(GOLANGCI_LINT) run
```

```yaml
# .github/workflows/ci-libs-beans.yml — delete the "Install golangci-lint" step
      - name: CI gates
        run: make ci
```

Verified: v2.12.2 (`built with go1.26.8`) loads `libs/beans/.golangci.yml` and reports
`0 issues.` on the current tree.

---

## Important

### I1 — The replace gate is satisfied by a *commented-out* replace, i.e. by a `go.mod` with no replace at all

**File:** `/Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh:231`

Because `:231` scans the raw file, a `go.mod` whose only occurrence of the sanctioned text
is a comment passes the "must contain" check, and `:243` then finds no real `replace` lines
to object to. Confirmed rc=0 for:

```
module github.com/mattsp1290/beans/apps/bean-counter

go 1.25.7

// replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans
```

This is Important rather than Critical only because the immediately following
`go list -m -json` check at `:397` would fail on a `v0.0.0` require with no replace. It is
still a gate reporting "sanctioned" about a file with nothing sanctioned in it. The C1 fix
(comment stripping) closes it.

### I2 — A second replace can be hidden behind a trailing comment that repeats the sanctioned text

**File:** `/Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh:243`

`grep -qvF` selects lines that do *not* contain the sanctioned substring, so a real second
replace whose trailing comment contains that substring is filtered out and never reported.
Confirmed rc=0 for:

```
replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans
replace github.com/gofiber/fiber/v3 => /tmp/fork // replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans
```

Contrived, but it is the same root cause as C1 and the same fix closes it.

### I3 — The `replace (` block-form test case is vacuous: that branch has zero coverage

**File:** `/Users/punk1290/git/beans/apps/bean-counter/test/scripts/deploy-production_test.sh:116-125`

`block.mod` wraps everything in `replace ( ... )`, so no line in it begins with the word
`replace` followed by the module path — the "must contain" check at script `:231` fires
first and the function returns before ever reaching the block check at `:238`. Captured:

```
$ check_sanctioned_replace block.mod
block.mod must contain: replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans
```

The test asserts only `rc == 1`, so it passes for the wrong reason and the block-rejection
branch is never exercised. Add a case that actually reaches it — a sanctioned single line
*plus* a separate block (this one I confirmed does reject with the block message) — and add
the C1 bypasses as regression cases:

```bash
write_gomod blockplus.mod "module github.com/mattsp1290/beans/apps/bean-counter

go 1.25.7

$SANCTIONED

replace (
	github.com/gofiber/fiber/v3 => ../../../fiber-fork
)"
check_sanctioned_replace "$gomod_tmp/blockplus.mod" >/dev/null 2>&1
assert_rc "sanctioned line plus a replace block rejected" 1 $?

write_gomod suffix.mod "module github.com/mattsp1290/beans/apps/bean-counter

go 1.25.7

replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans/vendored-fork"
check_sanctioned_replace "$gomod_tmp/suffix.mod" >/dev/null 2>&1
assert_rc "in-tree suffix-extended replace target rejected" 1 $?

write_gomod comment.mod "module github.com/mattsp1290/beans/apps/bean-counter

go 1.25.7

// $SANCTIONED"
check_sanctioned_replace "$gomod_tmp/comment.mod" >/dev/null 2>&1
assert_rc "commented-out replace does not satisfy the gate" 1 $?
```

(The `trap 'rm -rf "$tmp" "$gomod_tmp"' EXIT` at `:75` does cover both temp directories —
that part is correct. See suggestion S12 for the one window it misses.)

### I4 — No CI job runs `apps/bean-counter`'s `tidy-check`, and that is the gate the container build depends on

**Files:** `/Users/punk1290/git/beans/.github/workflows/ci-apps-bean-counter.yml:42-66`; `/Users/punk1290/git/beans/apps/bean-counter/Makefile:38-40`

The backend job runs `fmt-check`, `vet`, `lint`, `test`, `build` — not `tidy-check`.
`libs/beans` does get it, via `make ci` (`libs/beans/Makefile:31`). The asymmetry matters
here specifically because this branch introduces `go.work`: as `CLAUDE.md` itself says, a
workspace-active `go mod tidy` can resolve through the sibling module and write a `go.sum`
that is incomplete for a standalone build — "which is exactly how the container builds."
`apps/bean-counter/Makefile:38-40` guards against that with `GOWORK=off go mod tidy`, but
nothing in CI invokes it. `ci-workspace`'s `go work sync` + `git diff --exit-code` is not a
substitute; it proves workspace coherence, not `GOWORK=off` completeness.

Consequence: an incomplete `go.sum` merges green and first surfaces at
`deploy-production.sh:450` (the local docker build), or — with `--skip-local-build` — on the
infra host after the `pg_dump`, mid-deploy.

```yaml
      - name: Test
        run: make test

      - name: Tidy check
        run: make tidy-check

      - name: Build
        run: make build
```

### I5 — Nothing in CI builds the API image, and `/.dockerignore` is in no workflow's `paths` filter

**Files:** `/Users/punk1290/git/beans/.dockerignore`; `/Users/punk1290/git/beans/.github/workflows/ci-apps-bean-counter.yml:6-22`

The root-context build is now the most fragile artifact in the repo: three separate `COPY`
source paths, a `WORKDIR` dance, and a `.dockerignore` that sits at the repository root,
three directories away from the Dockerfile it governs and owned by no module. A PR that
edits only `/.dockerignore` matches neither `ci-apps-bean-counter`'s nor `ci-libs-beans`'s
`paths`, so only `ci-workspace` runs — and `ci-workspace` never invokes Docker. Someone
adding, say, `libs` or `**/schema` to that file would merge green and break the deploy.

Add the file to the filter and add a build job that actually exercises the path:

```yaml
on:
  pull_request:
    paths:
      - 'apps/bean-counter/**'
      - 'libs/beans/**'
      - '.dockerignore'
      - 'go.work'
      - 'go.work.sum'
      - '.github/workflows/ci-apps-bean-counter.yml'
  # ...same addition under push.paths

  image:
    name: API image
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
      - uses: actions/checkout@v4
      # Context is the repository root, exactly as the deploy script builds it.
      - name: Build API image
        run: docker build -f apps/bean-counter/Dockerfile -t bean-counter-api:ci .
      - name: Build UI image
        run: docker build -t bean-counter-ui:ci ./apps/bean-counter/frontend
```

### I6 — The deploy's local gates never exercise `GOWORK=off` resolution except through the skippable Docker build

**File:** `/Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh:361`, `:397`, `:411-427`

The container resolves the library with `GOWORK=off` (`apps/bean-counter/Dockerfile:21`).
Every local gate resolves it *with* the workspace active:

```bash
  beans_dir="$( cd apps/bean-counter && go list -m -f '{{.Dir}}' "$BEANS_MODULE" 2>/dev/null )"   # :361
  if ! ( cd apps/bean-counter && go list -m -json "$BEANS_MODULE" ) >/dev/null 2>&1; then          # :397
  run make -C apps/bean-counter test                                                               # :411
```

`:397`'s failure message is "module graph is not deployable" — but "deployable" for this
repo means resolvable without `go.work`, and that is not what it tests. The only gate that
does test it is the Docker build at `:450`, which `--skip-local-build` removes. Set
`GOWORK=off` on the two `go list` calls so the gate validates the same resolution path the
image uses:

```bash
  beans_dir="$( cd apps/bean-counter && GOWORK=off go list -m -f '{{.Dir}}' "$BEANS_MODULE" 2>/dev/null )" \
    || fatal "go list -m $BEANS_MODULE failed with GOWORK=off; module graph is not deployable"
...
  if ! ( cd apps/bean-counter && GOWORK=off go list -m -json "$BEANS_MODULE" ) >/dev/null 2>&1; then
```

Adding `run make -C apps/bean-counter tidy-check` to `local_gates()` (alongside the I4 CI
job) would close the `go.sum`-completeness half of the same gap.
