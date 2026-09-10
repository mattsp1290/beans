# Positive notes

This lane exists to find intent that later commits quietly undid. I looked hard and did not
find any. The two reactive rounds tightened the migration rather than eroding it. Each item
below is marked **executed** (I ran it) or **reasoned** (I read it).

## Nothing from the migration was undone

### `GOWORK=off` discipline — strengthened in four places, weakened in none

**Reasoned**, from `git diff 40d8e64 5bd9e07`:

- Both `go list -m` calls in the deploy script's deployability gate now run with
  `GOWORK=off`. The commit message states the reason correctly: with the workspace active,
  `go.work` satisfies those calls even when the `replace` is missing or wrong, masking the
  exact property the gate above them had just checked.
- `apps/bean-counter`'s `tidy-check` — which pins `GOWORK=off` specifically because a
  workspace-active tidy can write a `go.sum` that is incomplete for the standalone build
  the container performs — ran in **no** CI job before `2ef178e`. It is now a step in the
  backend job.
- A new `images` job builds the API image from the repository root and then probes the
  named `build` stage to assert `/src/go.work` does not exist, with a positive control
  (`test -f /src/libs/beans/go.mod`) first so a broken `docker run` cannot read as a pass.
  That is a well-constructed probe.
- Job-level `GOWORK: 'off'` remains on both per-module workflows and on the integration
  matrix; the Dockerfile still sets `ENV GOWORK=off` and still never `COPY`s `go.work`.

**Executed**, confirming D3 holds today: `go build ./...` succeeds in both modules both
with the workspace active and with `GOWORK=off`; `go test`, `go vet` and `golangci-lint`
are clean under `GOWORK=off` in both.

### Decision D7 (per-module lint policy) — untouched

**Executed.** `git log main..HEAD -- libs/beans/.golangci.yml apps/bean-counter/.golangci.yml`
shows the last commit to either was `14da22e` (the module rename). Neither fix round
touched them. The policies still differ substantively — `libs/beans` uses
`linters.default: none` with an explicit allow-list including `bodyclose`, `rowserrcheck`
and `sqlclosecheck`; `apps/bean-counter` uses `default: standard` plus `errorlint`,
`gocritic`, `nilerr`, `nolintlint`, `wastedassign` — exactly as finding 7 described.

The *invocation* styles still differ too, which is the half most likely to get "tidied"
away by a fix round: `libs/beans/Makefile` calls `golangci-lint` from `PATH`,
`apps/bean-counter/Makefile` pins `go run ...@v2.12.2`. The divergence is documented in
three places (`ci-libs-beans.yml`'s install step, `AGENTS.md`, `CLAUDE.md`) and tracked as
`beans-cjy` "Unify the two .golangci.yml policies and lint invocation styles", which is
open and `bd ready`. Deferred work that is actually recorded as deferred work.

### "The root Makefile contains no build logic" — restored, not weakened

The root `Makefile`'s own header asserts this rule. At `40d8e64` the file violated it:

```make
ci-integration:
	cd libs/beans && go test -tags=integration ./...
	$(MAKE) -C apps/bean-counter test-integration
```

`2ef178e` replaced that with a fan-out and added a `test-integration` target to
`libs/beans/Makefile` so the fan-out has somewhere to land. This is the opposite of the
failure mode I was looking for — a fix round finding and removing the one place the earlier
migration had broken its own stated rule.

### "Exactly one AGENTS.md / CLAUDE.md, one BEADS INTEGRATION marker" — holds

**Executed.** `git ls-files | grep -E '(AGENTS|CLAUDE)\.md$'` returns exactly `AGENTS.md`
and `CLAUDE.md`, both at the root. `git grep -c "BEGIN BEADS INTEGRATION"` returns `1` for
each (the other hits are `.beads/hooks/*`, which are the tooling that manages the block).
`06-beads-and-agent-config.md` acceptance criteria 6 and 7 both satisfied.

Every file the plan said to delete after the import is gone: `apps/bean-counter/`'s
`LICENSE`, `setup-beads.sh`, `.beads/`, `.github/`, `.claude/`, `.gitignore`, `AGENTS.md`
and `CLAUDE.md` are all absent (executed).

## Gates I mutation-tested rather than trusted

### `ci-workspace`'s Makefile fan-out gate is real, not vacuous

The new step runs `make -n "$t"` for eight targets and claims that proves each delegated
target exists in every module. A dry-run loop is exactly the shape of check that quietly
does nothing, so I tested the semantics in a scratch Makefile outside the repo: GNU make
executes recipe lines containing `$(MAKE)` even under `-n`, so it really does recurse, and
a target missing from a submodule exits `2`. **Executed:** control target exits 0, missing
target exits 2. The gate works as advertised, and all eight targets resolve on this branch.

### `libs/beans` really does enforce gofmt through golangci-lint

`AGENTS.md` and the root `Makefile` both justify excluding `libs/beans` from `fmt-check` on
the grounds that golangci-lint's formatters cover it. If that were wrong, gofmt would be
ungated for the library. **Executed:** I introduced a formatting-only mutation into
`libs/beans/version/version.go` (`var ` → `var    `) and ran `golangci-lint run ./version/...`:

```
version/version.go:11:1: File is not properly formatted (gofmt)
1 issues: * gofmt: 1
```

Restored immediately; `git status --porcelain` empty. The claim is true and the exclusion is
safe.

### The round-2 golangci-lint pin bump is sound, and I closed the gap it left

`2ef178e` changed `ci-libs-beans.yml` from `@v2.1.6` to `@v2.12.2` because v2.1.6 built with
its own `go 1.23` directive cannot run a config whose `run.go` targets 1.25. Correct — but
nothing had then verified that `libs/beans` is actually *clean* under v2.12.2; the local
`golangci-lint` on `PATH` is v2.1.6. **Executed:**
`GOWORK=off go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run ./...`
in `libs/beans` → `0 issues.`

The commit's stated reasoning also checks out: v2.12.2's `go.mod` declares `go 1.25.0` with
no `toolchain` directive (**executed**, read from the module cache), so a runner set up from
`go-version-file: libs/beans/go.mod` (1.25.7) builds it without any toolchain switch, and
the resulting binary satisfies the config's 1.25 target.

## Everything else that held

- **History migration matches D4 and D5 exactly.** `git log --follow -- libs/beans/store/store.go`
  → 41 commits, oldest `39d689c Extract bn into beans module`. `git log` *without* `--follow`
  on `apps/bean-counter/internal/server/app.go` → 5 commits at the new path, oldest
  `433d900 ralph: iteration 1 checkpoint - initialize Go Fiber skeleton`, which is the
  property `git subtree` would not have given. Move commit `58d9abc` is 112 `R100` entries
  and nothing else — pure renames. `git remote -v` shows only `origin →
  git@github.com:mattsp1290/beans.git`; no `bean-counter-rewrite` scratch remote survived.
  (All executed.)
- **Beads consolidation is exact.** **Executed:** exported the live database and diffed it
  against the committed archive `.agents/plans/monorepo-consolidation/bean-counter-issues.jsonl`.
  All 54 `bean-counter-*` ids present, none missing, none extra. All 80 archived dependency
  edges present, none lost. The only two extra edges are this session's new cross-project
  blockers on `bean-counter-m0p` (`→ beans-nlc`, `→ beans-ued`), which is growth, not drift.
  `bd stats` 170/17/152; `bd ready` returns bean-counter's ready issues, satisfying
  `06-beads-and-agent-config.md` acceptance criterion 3.
- **The full root gate is green and non-mutating.** **Executed:** `make ci` exits 0 —
  vet, lint (both modules, both invocation styles), test, build, and `tidy-check` in both
  modules — and `git status --porcelain` is empty afterward. `go work sync` likewise leaves
  the tree clean, so `ci-workspace`'s "Workspace is in sync" step (including the porcelain
  check `2ef178e` added to catch an untracked `go.work.sum`) passes.
- **The deploy surface is green.** **Executed:** `shellcheck` exit 0 on both scripts;
  `deploy-production_test.sh` → `42 passed, 0 failed`; `--dry-run` exits 0 and prints a plan
  whose repo directory, compose path and image contexts are all monorepo-shaped.
- **`.gitignore` reconciliation works as specified.** **Executed:** `git check-ignore go.work`
  exits non-zero (the workspace is trackable) and `git check-ignore bn` exits 0 via the
  anchored `/bn` rule that finding 8 called for — with a comment explaining the anchoring so
  it cannot shadow a `bn` inside a module.
- **The `bn` version path survived the rename.** Risk table's high-severity item was a
  missed `LDFLAGS` module path producing an empty version with no build error. **Executed:**
  `bn --version` → `bn version 5bd9e07`, non-empty, derived from the
  `git describe --tags --match 'libs/beans/v*'` scheme that keeps an application's tag from
  ever being read as a library version.
- **The reference sweep's `.agents/` exclusion held in both directions.** **Executed:** zero
  occurrences of the old library import prefix, of `github.com/mattsp1290/bean-counter`, or
  of `$HOME/git/bean-counter` anywhere outside `.agents/`; and the dated review records
  under `.agents/` were correctly left unrewritten. `setup-beads.sh` and
  `setup-multi-repo-beads.sh` got explicit `HISTORICAL:` headers rather than being edited or
  deleted — the right call for one-shot scripts whose stale paths are inert string
  arguments.
- **The one behavioral change on the branch is a test strengthening, not a loosening.**
  `2ef178e` touched two `_test.go` files: the handler fakes in `deps_test.go` and
  `graph_test.go` asserted only `Prefix`, and now also assert `AllRepos` — the field that
  would silently drop the prefix `WHERE` clause and return every project's rows. The commit
  reports mutation-testing it by injecting `AllRepos:true` and confirming the assertion
  fires. No non-test Go source changed after the import rewrite (executed).
