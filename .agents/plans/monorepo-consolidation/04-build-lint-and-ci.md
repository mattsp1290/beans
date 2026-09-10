# 04 — Build, lint, and CI

Goal: one command at the repository root runs every gate, each module keeps its own
authoritative build rules, and GitHub Actions runs the right jobs for a given change.

Prerequisite state: [03-go-module-restructure.md](03-go-module-restructure.md) complete.
Both modules build and test from their new locations.

## Existing state

`libs/beans/Makefile` (formerly the beans root Makefile) defines `build`, `test`, `vet`,
`lint`, `tidy-check`, `ci`, and `install`. `ci` is `tidy-check vet lint test build`. It
runs `golangci-lint` from `PATH`.

`apps/bean-counter/Makefile` defines `build`, `vet`, `lint`, `fmt`, `fmt-check`, `test`,
`test-integration`, `run`, and `clean`. It has no aggregate `ci` target. It pins
golangci-lint with `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2`
rather than using `PATH`.

The two lint invocations differ, and per decision D7 in [00-overview.md](00-overview.md)
each module keeps its own `.golangci.yml`. The version pinning also differs. Do not
unify either. Unification is deferred follow-up work; WP9 files a Beads issue for it.

## Root Makefile

`Makefile` at the repository root is a new file. It fans out; it contains no build logic
of its own.

```make
MODULES := libs/beans apps/bean-counter

.PHONY: build test vet lint fmt-check ci ci-integration clean beans bean-counter

build:
	@for m in $(MODULES); do $(MAKE) -C $$m build || exit 1; done

test:
	@for m in $(MODULES); do $(MAKE) -C $$m test || exit 1; done

vet:
	@for m in $(MODULES); do $(MAKE) -C $$m vet || exit 1; done

lint:
	@for m in $(MODULES); do $(MAKE) -C $$m lint || exit 1; done

# libs/beans has no fmt-check target; gofmt is enforced there by golangci-lint
# formatters. Only apps/bean-counter exposes fmt-check.
fmt-check:
	$(MAKE) -C apps/bean-counter fmt-check

ci: vet lint test build
	$(MAKE) -C libs/beans tidy-check

ci-integration:
	cd libs/beans && go test -tags=integration ./...
	cd apps/bean-counter && $(MAKE) test-integration

beans:
	$(MAKE) -C libs/beans $(TARGET)

bean-counter:
	$(MAKE) -C apps/bean-counter $(TARGET)
```

`ci` deliberately excludes `ci-integration`. Integration tests in both modules use
testcontainers and require a running Docker daemon. Keeping them in a separate target
means a developer without Docker can still run the full non-integration gate.

`libs/beans/Makefile`'s `tidy-check` runs `go mod tidy` then `git diff --exit-code go.mod go.sum`.
In the monorepo that `git diff` is evaluated against paths relative to the current
directory, so it still checks only `libs/beans/go.mod` and `libs/beans/go.sum`. Add an
equivalent `tidy-check` target to `apps/bean-counter/Makefile`:

```make
tidy-check:
	GOWORK=off $(GO) mod tidy
	git diff --exit-code go.mod go.sum
```

`GOWORK=off` matters here for the reason given in
[03-go-module-restructure.md](03-go-module-restructure.md) step 6: a workspace-active
tidy can produce a `go.sum` that is incomplete for a standalone build.

Add the same `GOWORK=off` to `libs/beans/Makefile`'s `tidy-check`. It has no sibling
dependency today, but the third application will not change that and consistency is
cheap.

## Version derivation

`libs/beans/Makefile` line 3 computes:

```make
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
```

In the monorepo this picks up any tag on the repository, including tags created for
`apps/bean-counter` or the third application. Change it to match only library tags:

```make
VERSION ?= $(shell git describe --tags --match 'libs/beans/v*' --always --dirty 2>/dev/null || echo dev)
```

`--dirty` now reflects the whole monorepo working tree, not just `libs/beans`. That is a
behavior change and is acceptable: a dirty tree anywhere means the build is not
reproducible from a commit.

Go's convention for a module in a subdirectory is that its tags are prefixed with that
subdirectory: `libs/beans/v0.2.0`. The existing `v0.1.0` and `v0.1.1` tags do not match
this pattern and are left in place as historical markers. Creating the first
`libs/beans/v*` tag is deferred work, not part of this migration; until one exists,
`--always` makes `git describe` fall back to a commit hash.

## GitHub Actions

Delete `.github/workflows/ci.yml`. Delete `apps/bean-counter/.github/` in its entirety
(WP3 already does this). Add three workflows.

### `.github/workflows/ci-workspace.yml`

Always runs. Proves the workspace itself is coherent.

```yaml
name: ci-workspace
on:
  push:
  pull_request:
permissions:
  contents: read
jobs:
  workspace:
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25.x'
          cache: true
      - name: Workspace is in sync
        run: |
          go work sync
          git diff --exit-code go.work go.work.sum
      - name: Build every module
        run: go build ./libs/beans/... ./apps/bean-counter/...
```

`setup-go`'s `go-version-file` input accepts one `go.mod`, and there is no root
`go.mod`, so this workflow pins the version literally. The per-module workflows below
use `go-version-file` pointed at their own module.

### `.github/workflows/ci-libs-beans.yml`

```yaml
name: ci-libs-beans
on:
  push:
    paths:
      - 'libs/beans/**'
      - 'go.work'
      - 'go.work.sum'
      - '.github/workflows/ci-libs-beans.yml'
  pull_request:
    paths:
      - 'libs/beans/**'
      - 'go.work'
      - 'go.work.sum'
      - '.github/workflows/ci-libs-beans.yml'
permissions:
  contents: read
defaults:
  run:
    working-directory: libs/beans
jobs:
  gates:
    runs-on: ubuntu-latest
    timeout-minutes: 20
    env:
      GOWORK: off
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: libs/beans/go.mod
          cache: true
      - run: make ci
      - run: go test -tags=integration ./...
```

`GOWORK: off` at job level makes this job prove that `libs/beans` builds standalone.

### `.github/workflows/ci-apps-bean-counter.yml`

Preserves the four jobs from bean-counter's existing `ci.yml` — backend, frontend,
deploy-scripts, integration matrix — with paths repointed. The backend job additionally
sets `GOWORK: off` so it proves the `replace` directive is sufficient without the
workspace.

Path filter for every job:

```yaml
    paths:
      - 'apps/bean-counter/**'
      - 'libs/beans/**'
      - 'go.work'
      - 'go.work.sum'
      - '.github/workflows/ci-apps-bean-counter.yml'
```

`libs/beans/**` is in the filter because a library change can break the application
through the filesystem `replace`. Omitting it would let a breaking library change merge
green.

Path repointing required inside this workflow, relative to the repository root:

| Existing value | New value |
| --- | --- |
| `go-version-file: go.mod` | `go-version-file: apps/bean-counter/go.mod` |
| `working-directory: frontend` | `working-directory: apps/bean-counter/frontend` |
| `cache-dependency-path: frontend/package-lock.json` | `cache-dependency-path: apps/bean-counter/frontend/package-lock.json` |
| `shellcheck scripts/deploy-production.sh test/scripts/deploy-production_test.sh` | `shellcheck apps/bean-counter/scripts/deploy-production.sh apps/bean-counter/test/scripts/deploy-production_test.sh` |
| `bash test/scripts/deploy-production_test.sh` | `bash apps/bean-counter/test/scripts/deploy-production_test.sh` |
| `go test -tags=integration ./test/integration ...` | run with `working-directory: apps/bean-counter` |

The backend job's `make fmt-check`, `make vet`, `make lint`, `make test`, and
`make build` steps keep their names and gain `working-directory: apps/bean-counter`.

Node version 24 and the `npm ci`, `npm run test`, `npm run check`, `npm run build`
sequence are unchanged. Those scripts exist in
`apps/bean-counter/frontend/package.json`.

The integration matrix entries `TestMySQLCRUDDepsAndReadyOverHTTP` and
`TestPostgresCRUDDepsAndReadyOverHTTP` are unchanged; both tests live in
`apps/bean-counter/test/integration/`.

## Branch protection

Path-filtered workflows do not run for changes outside their paths. If the repository
requires named status checks for merging, a workflow that never runs leaves its check
pending forever and blocks the pull request.

Before merging the migration, check:

```bash
gh api repos/mattsp1290/beans/branches/main/protection 2>&1 | head -20
```

A `404` or `Branch not protected` means no required checks exist and the path filters
are safe. If required checks are configured, either remove `ci-libs-beans` and
`ci-apps-bean-counter` from the required list and require only `ci-workspace`, or drop
the path filters. Record which option was taken.

This check is a prerequisite, not an assumption. Do not enable path filters without
running it.

## Acceptance criteria

1. `make ci` at the repository root passes.
2. `make ci-integration` passes on a machine with Docker running.
3. `make beans TARGET=build` and `make bean-counter TARGET=test` both work.
4. `libs/beans/bin/bn` reports a non-empty version after `make build`.
5. Each of the three workflow files parses: `gh workflow list` shows all three after the
   branch is pushed.
6. A pull request touching only `libs/beans/**` triggers `ci-workspace` and
   `ci-libs-beans`, and also `ci-apps-bean-counter` because `libs/beans/**` is in its
   filter.
7. A pull request touching only `apps/bean-counter/frontend/**` triggers `ci-workspace`
   and `ci-apps-bean-counter`, and not `ci-libs-beans`.
8. The branch-protection check above was run and its outcome recorded.

## Deferred work

- Unify the two `.golangci.yml` policies and the two golangci-lint invocation styles.
- Create the first `libs/beans/v*` tag and document the release procedure.
- Consider a shared composite action for the repeated Go setup steps once the third
  application lands.
