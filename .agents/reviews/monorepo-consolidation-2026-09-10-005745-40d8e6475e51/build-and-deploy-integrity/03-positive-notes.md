# Positive Notes

## The Dockerfile's `COPY` set is provably complete, and the caching split is correct for the right reason

`apps/bean-counter/Dockerfile:18-19` copies only the two `go.mod`/`go.sum` pairs before
`go mod download`, with the comment explaining why the replace target's `go.mod` has to be
there ("module graph reads the replace target's go.mod ... even though its source is not
needed yet"). That is exactly right, and it is the non-obvious part — a naive port would have
copied `libs/beans` wholesale into the dependency layer and destroyed the cache on every
library edit.

The source layer:

```dockerfile
COPY libs/beans ./libs/beans
COPY apps/bean-counter/cmd ./apps/bean-counter/cmd
COPY apps/bean-counter/internal ./apps/bean-counter/internal
```

I checked this against reality rather than against the comment.
`GOWORK=off go list -deps ./cmd/bean-counter` returns exactly `apps/bean-counter/cmd/...`,
eleven `apps/bean-counter/internal/...` packages, and `libs/beans/{model,repo,schema,store}` —
every one inside the copied set, nothing outside it. `GOWORK=off go build ./cmd/bean-counter`
succeeds from a clean state, confirming the `replace` alone is sufficient. And the
`go:embed migrations/*/*.sql` justification for copying the whole library
(`libs/beans/schema/schema.go:18`) is a real constraint, not a hedge.

## `GOWORK=off` placement is right and does not leak

`ENV GOWORK=off` at `Dockerfile:21` persists through the later `WORKDIR`/`RUN` in the same
stage, so the actual `go build` at `:36` inherits it, while stopping at the `FROM alpine:3.22`
boundary — the runtime stage is untouched, and I confirmed by diffing against the
pre-migration Dockerfile that every `BN_*` default, the non-root user and `/data` are byte-for-byte
unchanged. Deliberately not copying `go.work` and relying on the `replace` alone makes the
image immune to a future `.dockerignore` edit that omits a workspace file.

## Both compose files' context depths were verified by rendering, not by reading

`apps/bean-counter/docker-compose.stack.yml:22` uses `context: ../..` and
`apps/bean-counter/deploy/docker-compose.prod.yml:36` uses `context: ../../..` — different
depths, correctly derived from each file's own directory. `docker compose config` on both
resolves `build.context` to `/Users/punk1290/git/beans`, and the prod `ui` context to
`/Users/punk1290/git/beans/apps/bean-counter/frontend`. The commit message notes that
rendering the prod file is what surfaced the pre-existing `./frontend` bug, which had been
silently resolving to a never-existent `apps/bean-counter/deploy/frontend` — that is the right
way to find this class of defect, and the fix (`context: ../frontend`) is correct.

## `.dockerignore`'s `**/bin` is doing real work

Not theoretical: `libs/beans/bin/bn` is 28 MB and `apps/bean-counter/bin/bean-counter` is
33 MB on the current tree, both produced by `make build`. Without that pattern every
root-context `docker build .` would ship 62 MB of stale binaries to the daemon. Keeping
`**/.beads` and `**/.dolt` alongside it matters for the same reason now that the tracker lives
at the root of the build context.

## The workflows keep `uses:` inputs repo-root-relative while `run` steps use `working-directory`

This is the single easiest thing to get wrong when adding `defaults.run.working-directory`,
and all three workflows get it right:

```yaml
    defaults:
      run:
        working-directory: apps/bean-counter
    ...
      - uses: actions/setup-go@v5
        with:
          go-version-file: apps/bean-counter/go.mod
          cache-dependency-path: apps/bean-counter/go.sum
```

`ci-apps-bean-counter.yml`'s `deploy-scripts` job also correctly *omits* the job-level default
because its two commands are repo-root-relative, and `ci-workspace.yml:23-25` documents why
`go-version` is pinned literally rather than via `go-version-file` (no root module) instead of
leaving a future reader to rediscover it.

## `libs/beans/**` in `ci-apps-bean-counter`'s path filter

```yaml
# libs/beans/** is in the filter because a library change reaches this
# application through the filesystem replace directive. Omitting it would let a
# breaking library change merge green.
```

This is the one path-filter entry a monorepo conversion most often forgets, and the comment
records the causal chain rather than just the fact.

## The deploy script's remote payloads were repointed completely, not partially

I grepped the whole script for path references and found no stragglers. Both the local gate
(`:450`) and the remote build block (`:773-774`) gained `-f apps/bean-counter/Dockerfile` with
`.` as context, and both moved the UI build to `./apps/bean-counter/frontend`. The remote
heredoc's `cd "$repo_dir"` (`:634`) followed by root-relative Docker invocations is consistent
with the new `$HOME/git/beans` remote checkout default. Half-migrated remote payloads are the
usual failure mode here and this one is clean.

## Structural safety properties preserved through the move

- `deploy-production.sh:75` — the EXIT trap in the test harness does cover both temp
  directories, contrary to what a quick read of the restructured code suggests.
- `:917-919` — base64-encoding the multi-line preflight summary before it crosses `ssh` argv,
  with the comment explaining that ssh flattens argv into a re-parsed command string. Correct
  and non-obvious.
- `:453-457` — the "plain `if` (not `cmd && ...`)" note preventing a false test from tripping
  `set -e` in an assignment.
- `deploy-production_test.sh:15` — `SCRIPT_DIR` derived from `BASH_SOURCE`, which makes the
  suite cwd-independent. Verified: it passes 31/31 run from `/tmp` as well as from the repo
  root, so the CI job's repo-root invocation and a developer's in-directory invocation agree.
- The clean-worktree gate widening to the whole monorepo, with the reasoning written into the
  header ("the deploy records a single monorepo SHA, so uncommitted changes anywhere make that
  SHA an incomplete description of what was tested"). Strictly stronger than before, and
  correctly justified.

## `libs/beans/Makefile:6` — subdirectory-prefixed version tags

```make
VERSION ?= $(shell git describe --tags --match 'libs/beans/v*' --always --dirty 2>/dev/null || echo dev)
```

Matching Go's own convention for a module in a subdirectory means an application's future tag
can never be read as a library version. The accompanying `AGENTS.md` note that a wrong
`LDFLAGS` path "produces an empty version string with no build error" is the kind of failure
mode worth writing down.

## `apps/bean-counter/docker-compose.yml` correctly left untouched

The third compose file in the tree defines only dev Postgres and MySQL containers with no
`build:` stanzas, so it needed no context repointing — and it did not get any. Leaving a file
alone for a verified reason is as much a correct migration decision as changing one.
