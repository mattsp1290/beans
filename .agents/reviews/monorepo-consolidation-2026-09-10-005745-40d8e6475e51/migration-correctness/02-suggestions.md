# Suggestions (non-blocking)

## S1. The test fakes assert on `Prefix` but never on `AllRepos`, which is the field that could silently widen the query

**Files:** `/Users/punk1290/git/beans/apps/bean-counter/internal/handlers/deps/deps_test.go:49-51`,
`/Users/punk1290/git/beans/apps/bean-counter/internal/handlers/graph/graph_test.go:50-55`

The old fakes recorded a `string` prefix, so "the handler scoped the query" and "the
recorded value is `bc`" were the same statement. With a `ListFilter` they are not:
`ListFilter{Prefix: "bc", AllRepos: true}` passes every assertion in these tests and
returns every project's rows, because `store.ListIssues`, `ReadyIssues` and
`ListBlockingDeps` all drop the prefix `WHERE` clause when `AllRepos` is true
(`libs/beans/store/store.go:468`, `:527`, `:1000`). The adaptation is correct today; the
tests just no longer pin the property that matters most.

```go
if store.listFilter.Prefix != "bc" {
	t.Fatalf("listFilter.Prefix = %q, want bc", store.listFilter.Prefix)
}
if store.listFilter.AllRepos {
	t.Fatal("listFilter.AllRepos = true, want false: the deps view must stay project-scoped")
}
```

Same addition for `graph_test.go`'s `listFilter` and `depsFilter`. Worth adding to
`adapter_test.go` too if a fake store is available there.

## S2. `TestGraphStopsWhenListIssuesFails` now uses an empty `Prefix` as a proxy for "not called"

**File:** `/Users/punk1290/git/beans/apps/bean-counter/internal/handlers/graph/graph_test.go:76-78`

```go
if store.depsFilter.Prefix != "" {
	t.Fatalf("deps filter prefix = %q, want no deps call", store.depsFilter.Prefix)
}
```

The old `depsPrefix != ""` had the same weakness, so this is a faithful port rather than
a new one — but the rename is the moment to make it exact. A call with a zero-value
filter would satisfy this assertion. Record the call instead:

```go
type fakeStore struct {
	// ...
	depsCalled bool
	depsFilter appstore.ListFilter
}

func (s *fakeStore) ListBlockingDeps(_ context.Context, filter appstore.ListFilter) ([]appstore.DepEdge, error) {
	s.depsCalled = true
	s.depsFilter = filter
	return s.deps, s.depsErr
}

// ...
if store.depsCalled {
	t.Fatal("ListBlockingDeps was called after ListIssues failed")
}
```

## S3. Root `Makefile` lost `tidy-check` from `.PHONY` and dropped `install` entirely

**File:** `/Users/punk1290/git/beans/Makefile:5`, `:24`

Pre-rename the root `.PHONY` read
`build test vet lint tidy-check ci install`. It is now
`build test vet lint fmt-check ci ci-integration clean beans bean-counter` — `tidy-check`
is still a real target at line 24 but is no longer declared phony, and the `install`
target is gone with no replacement mentioned in `AGENTS.md` or `CLAUDE.md`
(`make beans TARGET=install` works, but nothing says so).

```make
.PHONY: build test vet lint fmt-check tidy-check ci ci-integration clean beans bean-counter
```

## S4. `ci-integration` shells out for `libs/beans` instead of fanning out like every other target

**File:** `/Users/punk1290/git/beans/Makefile:33-35`

```make
ci-integration:
	cd libs/beans && go test -tags=integration ./...
	$(MAKE) -C apps/bean-counter test-integration
```

This is the one place the root Makefile carries build logic, contradicting its own header
comment ("This file contains no build logic of its own"), and it bypasses `libs/beans`'
`GO` selection. Add a `test-integration` target to `libs/beans/Makefile` and fan out:

```make
ci-integration:
	$(MAKE) -C libs/beans test-integration
	$(MAKE) -C apps/bean-counter test-integration
```

## S5. `bn --version` regressed to a bare commit hash

**File:** `/Users/punk1290/git/beans/libs/beans/Makefile:7`

```make
VERSION ?= $(shell git describe --tags --match 'libs/beans/v*' --always --dirty 2>/dev/null || echo dev)
```

The subdirectory-prefixed `--match` is the right Go convention, but no `libs/beans/v*`
tag exists, so `--always` falls back to a bare abbreviated hash. Verified at HEAD:

```
$ git describe --tags --match 'libs/beans/v*' --always --dirty
40d8e64
$ git describe --tags --always --dirty        # the previous behavior
v0.1.1-305-g40d8e64
```

`AGENTS.md` acknowledges the fallback, but the practical result is that `bn --version`
stops carrying any semantic version at all. Either cut a `libs/beans/v0.1.2` tag as part
of the cutover, or make the fallback carry the lineage:

```make
VERSION ?= $(shell git describe --tags --match 'libs/beans/v*' --dirty 2>/dev/null \
             || git describe --tags --match 'v[0-9]*' --always --dirty 2>/dev/null \
             || echo dev)
```

Note also that once such a tag exists, `git describe` prints the full tag name, so
`bn --version` will read `libs/beans/v0.1.2-…`. Strip the prefix if that is not wanted:
`$(shell ... | sed 's|^libs/beans/||')`.

## S6. The deploy script's local phase has no working-directory guard

**File:** `/Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh:392`, `:359`, and `COMPOSE_PROD` at `:70`

Every local path is now repository-root-relative (`apps/bean-counter/go.mod`,
`apps/bean-counter/Dockerfile`, `apps/bean-counter/deploy/docker-compose.prod.yml`), and
the header says to run from the monorepo root, but nothing enforces it. Running from
`apps/bean-counter/` — the natural habit from the standalone repo, and where the script
itself lives — fails, though it fails loudly (`check_sanctioned_replace` reports
"no such file"; the `( cd apps/bean-counter && go list … )` subshell exits non-zero and
trips `fatal`). A one-line guard turns a confusing mid-flight abort into a clear message:

```bash
require_repo_root() {
  local top
  top="$(git rev-parse --show-toplevel 2>/dev/null)" \
    || fatal "not inside a git repository; run this from the monorepo root"
  [ "$top" = "$PWD" ] \
    || fatal "run this from the monorepo root ($top), not $PWD"
}
```

Call it first in `main`, before `resolve_target_sha`.

## S7. `README.md` links "beads" at this repository

**File:** `/Users/punk1290/git/beans/README.md:66`

```markdown
One [beads](https://github.com/mattsp1290/beans) tracker at the repository
```

The beads tracker (`bd`) is a separate tool — the pre-migration README named it as
`github.com/gastownhall/beads` in the `bn import` section. This link points the reader at
the repository they are already in. Either point it at the real beads project or drop the
link and leave the word plain.

## S8. Two root-level bead-seeding scripts still name pre-monorepo paths

**Files:** `/Users/punk1290/git/beans/setup-beads.sh`, `/Users/punk1290/git/beans/setup-multi-repo-beads.sh`

These reference `cmd/bn/app.go`, `store/store.go`, `schema/`, `docs/research/…` — all of
which now live under `libs/beans/`. They only pass those strings to `bd create`
descriptions, so nothing breaks, and the "sweep stale references" commit reasonably
treated them as historical. But they sit at the repository root, outside the `.agents/`
tree that `CLAUDE.md` declares as "a historical record… not rewritten when paths change",
so a reader has no signal that they are frozen. Move them under
`libs/beans/scripts/historical/` or `.agents/plans/`, or add a one-line header noting
that their paths predate the monorepo.

## S9. Path-filtered workflows and required status checks

**Files:** `/Users/punk1290/git/beans/.github/workflows/ci-apps-bean-counter.yml:47-62`,
`/Users/punk1290/git/beans/.github/workflows/ci-libs-beans.yml:200-212`

Two notes on the three-workflow split, which is otherwise well reasoned:

1. A path-filtered `pull_request` job never reports a status on a PR that touches
   neither path. If either of these is configured as a required check, such PRs block
   forever. `ci-workspace` is unfiltered and is the one to mark required — worth
   recording that in `CLAUDE.md` next to the CI paragraph so the branch-protection
   setting is not guessed later.
2. `ci-apps-bean-counter` restricts `push` to `branches: [main]`; `ci-libs-beans` does
   not. Harmless, but the asymmetry looks unintentional.

## S10. `ci-workspace`'s sync check cannot see a newly generated `go.work.sum`

**File:** `/Users/punk1290/git/beans/.github/workflows/ci-workspace.yml:298-301`

```yaml
      - name: Workspace is in sync
        run: |
          go work sync
          git diff --exit-code
```

`go work sync` produced no `go.work.sum` locally and none is committed, which is why this
passes. But `git diff --exit-code` only inspects tracked files, so if a future dependency
change causes one to be generated, CI stays green and the file is silently left
untracked — and `.gitignore` no longer ignores it. Make the check total:

```yaml
        run: |
          go work sync
          git diff --exit-code
          test -z "$(git status --porcelain)" || { git status --porcelain; exit 1; }
```

## S11. `fmt-check` now walks `frontend/node_modules`

**File:** `/Users/punk1290/git/beans/apps/bean-counter/Makefile:31-32`

```make
fmt-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './.git/*'))" || (echo "gofmt changes needed; run make fmt" >&2; exit 1)
```

The `./.git/*` exclusion is now inert — `.git` lives at the repository root, not in the
module — while `find .` from `apps/bean-counter` descends into `frontend/node_modules`
after `npm ci`. Correct (no `.go` files there) but needlessly slow, and the stale
exclusion misleads. Prefer:

```make
fmt-check:
	@test -z "$$(gofmt -l ./cmd ./internal ./test)" || (echo "gofmt changes needed; run make fmt" >&2; exit 1)
```

## S12. bean-counter's status vocabulary was left unreconciled with the library's

**Files:** `/Users/punk1290/git/beans/apps/bean-counter/internal/api/validate/validate.go:37-42`,
`/Users/punk1290/git/beans/apps/bean-counter/cmd/bean-counter/main.go:42-43`

bean-counter validates against a hard-coded five-state set:

```go
var allowedStates = map[string]struct{}{
	"open": {}, "in_progress": {}, "blocked": {}, "closed": {}, "done": {},
}
```

`libs/beans` HEAD's `model.DefaultWorkflowConfig()` defines eight, adding
`ready_for_review`, `ready_for_validation` and `ready_for_merge` as hold states, and
`store.New` falls back to that default because `AdapterConfig.Store` never sets
`Config.Workflow` (`libs/beans/store/store.go:73-78`).

I want to be exact: this is **not** a regression from the merge. bean-counter's HTTP layer
was already the stricter of the two, so the observable API behavior is unchanged, and
`main.go`'s `TerminalStates: {closed, done}` / `ActiveStates: {open}` still match the
library defaults, so `/ready` semantics are preserved. But the branch's stated premise is
that consolidation is what reconciles library/application drift, and this particular drift
was not reconciled or written down. Concretely: an issue the `bn` CLI puts into
`ready_for_review` in the shared database displays fine in bean-counter (reads are
tolerant) but cannot be moved out of that state through the API — the `PATCH` 400s at
`validate.IssueState`. Either wire `allowedStates` to `store.WorkflowConfig()`
(`libs/beans/store/store.go:82-85` exposes it) or file a bead recording the deliberate
divergence.
