# Positive Notes

Everything below was **executed**, not assumed, except where marked.

## The golangci-lint pin actually works against this config

`libs/beans/.golangci.yml` is not a trivial config to satisfy: `version: "2"`,
`run.go: "1.25"`, `linters.default: none` with a ten-linter allow-list, an empty
`settings:` key (line 23, which is valid but is the sort of thing a schema
tightening breaks), and a separate `formatters` block with
`goimports.local-prefixes`. v2.12.2 accepts all of it:

```
$ cd libs/beans && GOWORK=off .../golangci-lint run
0 issues.        # exit 0
$ .../golangci-lint version
golangci-lint has version 2.12.2 built with go1.26.8 ...
```

The pin also now equals `apps/bean-counter/Makefile:2`'s
`GOLANGCI_LINT_VERSION ?= v2.12.2`. Even though the stated reason for the bump is
wrong (see I2), aligning the two modules on one version is the right outcome —
it removes a real skew where the library and the application could disagree about
what `staticcheck` flags.

## `make ci`'s bare `golangci-lint run` will resolve on the runner

`libs/beans/Makefile:26` invokes `golangci-lint` from `PATH`, and the workflow
installs it with a plain `go install` that writes to `$(go env GOPATH)/bin` —
which nothing in the workflow explicitly adds to `PATH`. That is exactly the shape
that breaks, so I checked the action's source rather than assuming.
`actions/setup-go` calls `addBinToPath()`, which does:

```js
const bp = path.join(gp, 'bin');
if (!fs.existsSync(bp)) { await io.mkdirP(bp); }
core.addPath(bp);
```

So `/home/runner/go/bin` lands on `PATH` for every subsequent step, and the
comment at `ci-libs-beans.yml:48-51` explaining why CI has to put it there is
accurate about the requirement.

## The porcelain check is the right instinct, and it is stable rather than flaky

`ci-workspace.yml:44-45`:

```yaml
          git diff --exit-code
          test -z "$(git status --porcelain)" || { git status --porcelain; exit 1; }
```

The reasoning in the comment is correct — `git diff` genuinely cannot see a
newly created untracked `go.work.sum`, and there is no `go.work.sum` tracked in
this repo today, so that is a live gap the porcelain line closes.

The obvious risk with adding a porcelain gate is that it turns a benign artifact
into a red build. It does not here. I tested a fresh `git clone --no-local` at
`2ef178e` twice:

- **Warm module cache:** `go work sync` rc=0, `git diff --exit-code` rc=0,
  `git status --porcelain` empty.
- **Cold module cache** (`GOMODCACHE` pointed at an empty directory, so every
  dependency was downloaded from scratch — the fresh-runner case): same result.
  `go work sync` downloaded ~20 modules and still wrote nothing, created no
  `go.work.sum`, and left no untracked files.

Both the diff gate and the porcelain gate are therefore no-ops on a clean runner,
which is what a good gate looks like.

## `make tidy-check` in the backend job is clean on a fresh checkout

The new step at `ci-apps-bean-counter.yml:70-73` runs a `GOWORK=off go mod tidy`
followed by `git diff --exit-code go.mod go.sum`, under job-level `GOWORK: off`.
That is the highest-risk kind of gate to add, because a `go.sum` generated with a
workspace active can legitimately differ from one tidied standalone — which is
exactly the hazard the Makefile comment at `apps/bean-counter/Makefile:43-44`
describes. It is clean in practice: on a fresh clone at `2ef178e` I ran
`make tidy-check` in both `libs/beans` and `apps/bean-counter`, and afterwards
`git status --porcelain` was empty.

Step ordering also holds: `make build` (line 68) runs before `make tidy-check` and
writes `apps/bean-counter/bin/bean-counter`, but `tidy-check` only diffs
`go.mod go.sum`, and `bin/` is gitignored anyway.

## The root fan-out reaches a real target in both modules

`Makefile:32-33`'s rewrite from an inlined `cd libs/beans && go test
-tags=integration ./...` to a fan-out is only safe because `libs/beans` gained
`test-integration` in the same commit (`libs/beans/Makefile:18-20`, with
`.PHONY` updated at line 10). `make -n` confirms the whole contract:

```
$ make -n ci-integration
for m in libs/beans apps/bean-counter; do make -C $m test-integration || exit 1; done
go test -tags=integration ./...
go test -tags=integration ./...
```

I dry-ran all nine root targets (`build test vet lint fmt-check tidy-check ci
ci-integration clean`); every one resolves in both modules. `fmt-check` is
correctly special-cased to `apps/bean-counter` only, with the reason stated at
`Makefile:19-20`, and `tidy-check` is now in `.PHONY` (line 5). The header claim
that this file "contains no build logic of its own" is once again true.

## `.dockerignore` joining the filter is correct, and the file reasons carefully

Adding `.dockerignore` to `ci-apps-bean-counter`'s filters
(lines 13 and 23) closes a real hole: the file governs the API image's build
context and lives at the repository root, so it was previously ungated. The file's
own comment at lines 12-14 is right for a non-obvious reason — the UI image's
context is `./apps/bean-counter/frontend`, which has its own `.dockerignore`, so
a blanket root-level `frontend` exclusion would have been both ineffective there
and wrong for a future second UI.

I also checked that no `.dockerignore` pattern strips something the API build
needs: no tracked file under `libs/beans` matches `**/bin`, `**/*.db`,
`**/*.sqlite`, `**/.env*` or `**/node_modules`, and the only `.env*` match in the
repo (`apps/bean-counter/.env.example`) is not COPYed by the Dockerfile.

## Both compose files render, including the deliberately asymmetric contexts

**[EXECUTED]** with rc=0 for both:

```
docker compose -f apps/bean-counter/docker-compose.stack.yml config
docker compose -p bean-counter -f apps/bean-counter/deploy/docker-compose.prod.yml config
```

The interesting part is that the two files carry *different* relative build
contexts for the same Dockerfile — `context: ../..` in the stack file and
`context: ../../..` in `deploy/docker-compose.prod.yml` — because compose resolves
`build.context` against the compose file's own directory, not the invocation
directory. Both are correct, both comments say so, and `config` (which resolves
them to absolute paths) proves it rather than leaving it as prose. The explicit
`-p bean-counter` on the prod invocation also matches the safety rule the file's
header documents at lines 9-14.

## The `images` job's build step, unlike its assertion, does prove something

`docker build -f apps/bean-counter/Dockerfile -t bean-counter-api:ci .`
(line 135) is the real gate. The Dockerfile never copies `go.work` and sets
`ENV GOWORK=off` (line 22), so a green build is direct evidence that the
`replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans` resolves the
library on its own. The dependency-layer split is also correct in a way that is
easy to get wrong: `Dockerfile:18` copies `libs/beans/go.mod` and `go.sum` before
`go mod download`, because computing the build list requires reading the replace
target's `go.mod` even though its sources are not needed yet — and the comment at
lines 15-17 says exactly that. **[STATIC]** — I could not run the build.

## `docker compose` is available on `ubuntu-latest`

Compose v2 ships as a preinstalled Docker CLI plugin on the GitHub-hosted Ubuntu
images, so the `docker compose ... config` invocations at lines 147-150 need no
setup step. **[STATIC]** — verified against the local Compose v2 (v2.39.2), not
against a runner.
