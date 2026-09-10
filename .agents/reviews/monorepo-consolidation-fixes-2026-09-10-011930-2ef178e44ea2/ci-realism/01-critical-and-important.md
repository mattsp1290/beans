# Critical and Important Issues

Each finding is tagged **[EXECUTED]** (I ran it) or **[STATIC]** (reasoned only —
the local Docker engine is down, so `docker build` and `docker run` could not be
executed).

---

## CRITICAL

### C1. The `images` job's go.work assertion can never fail — it is vacuous

**Severity:** Critical
**File:** `.github/workflows/ci-apps-bean-counter.yml:137-142`
**Supporting file:** `apps/bean-counter/Dockerfile:37-58`
**[STATIC]** — the reasoning is airtight from the Dockerfile, but I could not run
`docker run` to demonstrate it.

The step is:

```yaml
      - name: API image carries no go.work
        run: |
          if docker run --rm --entrypoint sh bean-counter-api:ci -c 'ls /src/go.work' 2>/dev/null; then
            echo "go.work leaked into the runtime image" >&2
            exit 1
          fi
```

`/src` exists only in the **build** stage. `apps/bean-counter/Dockerfile:12` sets
`WORKDIR /src` under `FROM golang:1.25.7-alpine AS build`. The runtime stage
starts over at line 37 with `FROM alpine:3.22`, and the only thing that crosses
the stage boundary is line 54:

```dockerfile
COPY --from=build /out/bean-counter /usr/local/bin/bean-counter
```

Nothing in the runtime stage creates `/src`. So `ls /src/go.work` exits non-zero
on every possible input, the `if` is always false, and the step always passes.

Three consequences, in increasing order of how much they matter:

1. **The check is inert today.** It reports a guarantee it never tested.
2. **It stays inert under the regression it names.** If someone added
   `COPY go.work ./` to the build stage — the actual thing that would break the
   "`replace` alone resolves the library" property — the runtime image still
   would not contain `/src/go.work`, and this step would still be green.
3. **Failure of the probe itself reads as success.** `2>/dev/null` swallows
   `docker run`'s own errors. A typo'd image tag, a missing `sh`, or a daemon
   hiccup all produce a non-zero exit that this step interprets as "clean."

This matters more than a normal inert test because the commit message nominates
this job as the substitute for verification that could not be done locally:
"This job is also what stands in for the local docker build that could not be run
during this migration." It stands in for nothing.

To be fair to the surrounding step: the comment at lines 131-133 is correct, and
the property *is* proven — but by the `docker build` at line 135 succeeding, not
by the assertion. The Dockerfile never copies `go.work` and sets `ENV GOWORK=off`
(line 22), so a green build already demonstrates that the `go.mod` replace
suffices. The assertion adds zero signal on top of that.

Mechanics I checked and that are **not** the problem, so the fix does not need to
touch them: `alpine:3.22` ships busybox `sh`; `--entrypoint sh` correctly
overrides `ENTRYPOINT ["bean-counter"]` (Dockerfile:58) and makes `-c '...'` the
argument; and `USER bean-counter` / `WORKDIR /data` (lines 44-45) do not affect an
absolute-path `ls`. The command is well-formed shell — `shellcheck` on the step
body is clean **[EXECUTED]**. It is aimed at a path that does not exist.

**Suggested fix** — probe the build stage, where `/src` is real, and make the
runtime probe prove it reached a live filesystem before trusting absence as
evidence:

```yaml
      - name: API image carries no go.work
        run: |
          # Prove the probe reaches a real filesystem before reading its
          # failure as evidence of absence.
          docker run --rm --entrypoint sh bean-counter-api:ci \
            -c 'test -x /usr/local/bin/bean-counter'
          if docker run --rm --entrypoint sh bean-counter-api:ci \
               -c 'find / -xdev -name go.work -print -quit 2>/dev/null | grep -q .'; then
            echo "go.work leaked into the runtime image" >&2
            exit 1
          fi

      # The property the build-step comment claims lives in the build stage,
      # which is the only place /src exists.
      - name: Build stage resolved libs/beans without go.work
        run: |
          docker build --target build -f apps/bean-counter/Dockerfile -t bean-counter-build:ci .
          docker run --rm bean-counter-build:ci sh -c 'test -d /src/libs/beans'
          if docker run --rm bean-counter-build:ci sh -c 'test -e /src/go.work'; then
            echo "go.work was copied into the build context" >&2
            exit 1
          fi
```

The build stage is named (`AS build`, Dockerfile:10) so `--target build` works,
and the `golang:*-alpine` base sets no `ENTRYPOINT`, so `docker run img sh -c` is
the right invocation there. The `--target build` image is already fully cached by
the preceding full build, so this costs seconds, not a second compile.

---

## IMPORTANT

### I2. `ci-libs-beans.yml`'s new comment documents a failure mode that does not occur on a clean runner

**Severity:** Important
**File:** `.github/workflows/ci-libs-beans.yml:52-59`
**[EXECUTED]** — I reproduced the runner's toolchain and disproved the claim.

The comment now permanently in the workflow says:

```
      # The version must be built with a Go toolchain at least as new as
      # libs/beans/.golangci.yml's `run.go`. `go install pkg@version` builds
      # with that module's own go directive, and v2.1.6 declares go1.23, so it
      # exits 3 with "the Go language version (go1.23) used to build
      # golangci-lint is lower than the targeted Go version (1.25)".
```

"`go install pkg@version` builds with that module's own go directive" is not how
toolchain selection works. Under `GOTOOLCHAIN=auto` (the default), a module's `go`
directive is a **lower bound**: the toolchain used is `max(installed Go, module's
go directive)`. It never downgrades. On this runner `actions/setup-go` installs
go1.25.7 (from `go-version-file: libs/beans/go.mod`, whose directive is
`go 1.25.7`), so v2.1.6 would have been built with **go1.25.7**, which satisfies
`run.go: "1.25"`.

What I ran:

```
$ grep '^go ' .../golangci-lint/v2/@v/v2.12.2.mod   ->  go 1.25.0
$ grep '^go ' .../golangci-lint/v2/@v/v2.1.6.mod    ->  go 1.23.0

$ GOTOOLCHAIN=go1.25.7 GOBIN=... go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.1.6
$ ./golangci-lint version
golangci-lint has version v2.1.6 built with go1.25.7 ...

$ cd libs/beans && GOWORK=off .../golangci-lint run
0 issues.        # exit 0
```

So the previous review's blocker — "ci-libs-beans could not have passed" — does
not reproduce on a clean `ubuntu-latest`. The failure it observed is a
*local-machine* artifact: it happens when the developer's own Go is older than
1.25 (this machine reports `go version go1.23.4 darwin/arm64`) and
`GOTOOLCHAIN` is not allowed to upgrade, in which case v2.1.6 does get built with
go1.23 and does exit 3.

**The pin itself is fine and I would keep it.** v2.12.2 works (see 03), and it now
matches `apps/bean-counter/Makefile:2`, which removes a genuine version skew
between the two modules. The problem is only the stated reason, which is now a
durable comment that the next person will trust and reason from.

**Suggested fix** — keep the pin, replace the mechanism claim with the real one:

```yaml
      # Pinned to match apps/bean-counter/Makefile's GOLANGCI_LINT_VERSION, so
      # the two modules cannot drift onto different lint behaviour.
      #
      # Whatever version is pinned must be buildable by a toolchain at least as
      # new as libs/beans/.golangci.yml's `run.go` (1.25), or golangci-lint
      # exits 3. `go install pkg@version` uses max(installed Go, the module's go
      # directive) and never downgrades, so setup-go's go1.25.7 satisfies that
      # here — but a developer on an older local Go with GOTOOLCHAIN=local will
      # see the exit-3 failure that CI does not.
      - name: Install golangci-lint
        run: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
```

### I3. The root `Makefile` is the fan-out contract and no workflow ever runs it

**Severity:** Important
**Files:** `Makefile:1-43`; `.github/workflows/ci-workspace.yml:41-48`
**[EXECUTED]** — confirmed by reading all three workflows and by `make -n`.

This commit changed the root `Makefile` (`.PHONY` gained `tidy-check`;
`ci-integration` was rewritten from an inline `cd libs/beans && go test` into a
fan-out at lines 32-33). Nothing in CI invokes it:

- `ci-workspace` is the only unfiltered workflow, and its only two steps are
  `go work sync` + `git` checks and `go build ./libs/beans/... ./apps/bean-counter/...`.
  It never calls `make`.
- `ci-libs-beans` runs `make ci` with `defaults.run.working-directory: libs/beans`,
  so it exercises `libs/beans/Makefile`, not the root one.
- `ci-apps-bean-counter`'s backend job likewise runs the module Makefile.
- The root `Makefile` is in no workflow's path filter, so even a change to it
  starts only `ci-workspace`, which ignores it.

Net effect: a typo in the root fan-out — a target that exists in one module but
not the other, a broken `for` loop, a missing `.PHONY` — merges green and first
surfaces on a developer's machine. That is precisely the class of bug the
`ci-integration` rewrite in this commit was fixing, and the rewrite itself is
ungated.

I verified by hand that the contract currently holds **[EXECUTED]**: `make -n` on
`build`, `test`, `vet`, `lint`, `fmt-check`, `tidy-check`, `ci`, `ci-integration`
and `clean` all resolve in both modules, and `make -n ci-integration` correctly
expands to `test-integration` in each. But "I checked once by hand" is not a gate.

**Suggested fix** — add a cheap dry-run gate to the unfiltered workflow. `make -n`
needs no toolchain beyond `make` and catches every missing-target and expansion
error without running a compile:

```yaml
      # The root Makefile is the fan-out contract and is otherwise exercised by
      # no job: the per-module workflows run each module's own Makefile.
      - name: Root Makefile fan-out resolves in every module
        run: |
          for t in build test vet lint fmt-check tidy-check ci ci-integration clean; do
            make -n "$t" >/dev/null || { echo "root make target '$t' does not resolve" >&2; exit 1; }
          done
```

Add `'Makefile'` to both per-module workflows' path filters as well, so a root
Makefile change also starts the jobs that depend on the module Makefiles it
delegates to.

### I4. `ci-workspace`'s trigger shape doubles every run and diverges from the other two

**Severity:** Important
**File:** `.github/workflows/ci-workspace.yml:6-8`
**[EXECUTED]** — trigger shapes read from all three files; YAML parse confirmed
for all three (`ruby -ryaml`).

The three workflows do not agree on trigger shape:

| Workflow | `push` | `pull_request` |
| --- | --- | --- |
| `ci-workspace` | **all branches, all paths** | all branches, all paths |
| `ci-libs-beans` | `branches: [main]` + paths | all branches + paths |
| `ci-apps-bean-counter` | `branches: [main]` + paths | all branches + paths |

`ci-workspace`'s bare `push:` means that for any PR opened from a branch in this
repository, the `workspace` job runs **twice per push** — once for `push`, once
for `pull_request` — with identical results. The other two were deliberately
pinned to `main` on push (this commit added `branches: [main]` to
`ci-libs-beans`, lines 5-6, explicitly to match `ci-apps-bean-counter`), so
`ci-workspace` is now the odd one out on a dimension the commit was actively
normalizing.

This is not just cost. `CLAUDE.md:106-110` nominates `ci-workspace` as the one
workflow safe to require as a status check. Required checks resolve against the
`pull_request` event, so restricting `push` to `main` changes nothing about that
guidance while removing the duplicate.

**Suggested fix:**

```yaml
on:
  # push is scoped to main to match the two path-filtered workflows; without
  # the scope every PR branch runs this job twice, once per event.
  push:
    branches:
      - main
  # Deliberately unfiltered on pull_request: this is the backstop that must run
  # for every PR, including ones that touch neither module's paths.
  pull_request:
```
