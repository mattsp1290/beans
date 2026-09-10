# Suggestions (non-blocking)

## S1. `ci-libs-beans` inlines the integration command instead of using the target this commit added

**File:** `.github/workflows/ci-libs-beans.yml:64-65`
**Related:** `libs/beans/Makefile:18-20`

This commit added `test-integration` to `libs/beans/Makefile` so the root
`ci-integration` fan-out has something to call. The workflow still hardcodes the
same command:

```yaml
      - name: Integration tests
        run: go test -tags=integration ./...
```

Two places now define "how libs/beans runs its integration suite," and they will
drift the first time one gains a `-timeout`, a `-count=1`, or a package scope.
Contrast `ci-apps-bean-counter`, whose backend job routes everything through
`make`.

```yaml
      - name: Integration tests
        run: make test-integration
```

## S2. `ci-libs-beans`'s integration step needs Docker but never checks for it

**File:** `.github/workflows/ci-libs-beans.yml:64-65`
**Contrast:** `.github/workflows/ci-apps-bean-counter.yml:184-185`

`libs/beans/Makefile:18` now documents that the suite requires Docker
("the suite uses testcontainers"), and I confirmed the build-tagged files exist
(`libs/beans/schema/schema_integration_test.go`,
`libs/beans/store/store_integration_test.go`). The bean-counter integration job
has a `Check Docker` / `docker version` preflight so a daemon problem fails with
an obvious message rather than a testcontainers timeout eating most of the
20-minute budget. This job has none.

```yaml
      - name: Check Docker
        run: docker version

      - name: Integration tests
        run: make test-integration
```

## S3. `libs/beans`' `clean` expands to `rm -rf ./` under a command-line `BIN` override

**File:** `libs/beans/Makefile:42`
**[EXECUTED]**

```make
BIN := bin/bn
...
clean:
	rm -rf $(dir $(BIN))
```

With the default this is safe — `make -C libs/beans -n clean` prints `rm -rf bin/`.
But command-line variables override even `:=`, and `$(dir bn)` is `./`:

```
$ make -C libs/beans -n clean BIN=bn
rm -rf ./
```

`apps/bean-counter/Makefile:5-6,59` already has the safer shape — a separate
`BIN_DIR` that `clean` removes directly, so no `$(dir ...)` is involved. Mirror it:

```make
BIN_DIR := bin
BIN     := $(BIN_DIR)/bn
...
clean:
	rm -rf $(BIN_DIR)
```

Low likelihood, but the blast radius of the one bad expansion is the whole module
directory, and the fix is three lines.

## S4. `fmt-check`'s hand-enumerated directory list will silently stop covering new code

**File:** `apps/bean-counter/Makefile:31-35`

The move away from a bare `find .` is right — it really did descend into
`frontend/node_modules`, and `-not -path './.git/*'` really is inert now that the
module is not a repository root. But the replacement enumerates three fixed
directories:

```make
	@test -z "$$(gofmt -l ./cmd ./internal ./test)" || (...)
```

I confirmed all three exist and that no `.go` file in the module lives outside
them today **[EXECUTED]**. The failure mode is future drift: an
`apps/bean-counter/main.go`, a new `pkg/`, or an `api/` tree is simply not
checked, and nothing announces the gap — the gate stays green while coverage
shrinks. An exclusion-based form keeps the enumeration automatic:

```make
fmt-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './frontend/*'))" \
	  || (echo "gofmt changes needed; run make fmt" >&2; exit 1)
```

`./frontend` is the only non-Go subtree in the module, and it is where
`node_modules` lands, so a single exclusion restores both properties.

## S5. No workflow declares a `concurrency` group

**Files:** all three workflows (`.github/workflows/*.yml`)

None of the three sets `concurrency`, so pushing three times to a PR branch
queues three full runs of everything, including the new 20-minute `images` job
that builds two container images from scratch. Superseded runs are pure waste and
they delay the run whose result anyone will read.

```yaml
concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: ${{ github.event_name == 'pull_request' }}
```

Guarding `cancel-in-progress` on the event keeps pushes to `main` from cancelling
each other while still collapsing PR-branch churn.

## S6. The `images` job rebuilds both images from zero on every run

**File:** `.github/workflows/ci-apps-bean-counter.yml:122-150`
**[STATIC]** — cannot be timed here; the Docker engine is down.

There is no layer cache, so each run re-executes `go mod download` for the whole
`apps/bean-counter` graph inside the container plus a full `npm ci` for the UI —
work the `backend` and `frontend` jobs, which do have `setup-go` / `setup-node`
caching, have already done on the same runner pool. The 20-minute timeout is
probably enough, but this will be the long pole on every PR that touches
`libs/beans/**`.

`docker/setup-buildx-action` plus GitHub Actions cache is the usual remedy:

```yaml
      - uses: docker/setup-buildx-action@v3

      - name: Build API image
        uses: docker/build-push-action@v6
        with:
          context: .
          file: apps/bean-counter/Dockerfile
          tags: bean-counter-api:ci
          load: true
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

Note that `load: true` is required for the later `docker run` assertions to see
the image.

## S7. The `images` job builds the images but never proves they start

**File:** `.github/workflows/ci-apps-bean-counter.yml:144-150`
**[STATIC]**

`docker compose config` validates that the YAML renders and that the relative
build contexts resolve — I confirmed both files render, rc=0, with the daemon down
**[EXECUTED]** — but it does not build, run, or check anything about the images'
runtime behaviour. Given that both compose files define a `wget`-based healthcheck
against `/api/v1/readyz` and `/healthz`, a very cheap addition to the job is one
smoke start of the API image against its baked-in sqlite defaults:

```yaml
      - name: API image starts and reports ready
        run: |
          cid=$(docker run -d -p 18080:8080 bean-counter-api:ci)
          trap 'docker rm -f "$cid" >/dev/null' EXIT
          for _ in $(seq 30); do
            curl -fsS http://127.0.0.1:18080/api/v1/readyz >/dev/null && exit 0
            sleep 1
          done
          docker logs "$cid" >&2
          exit 1
        # The image defaults to BN_DRIVER=sqlite / BN_DSN=file:/data/bean-counter.db
        # (Dockerfile:47-48), so this needs no database.
```

This would also, incidentally, catch the `USER bean-counter` / `/data` ownership
class of bug that a build-only job cannot see.

## S8. `docker compose config` does not need a daemon — worth knowing for local reproduction

**File:** `.github/workflows/ci-apps-bean-counter.yml:147-150`
**[EXECUTED]**

Both `config` invocations succeeded on this machine with the Docker engine down
(`Cannot connect to the Docker daemon`), including the prod file's
`external: true` `symphony` network and its `${BN_DSN_SECRET:-...}` bind mount —
`config` resolves defaults and paths but validates neither external network
existence nor host path existence. That is a genuinely useful property: this step
is reproducible locally without Docker running. Worth a one-line comment in the
workflow so nobody assumes the whole job needs a daemon.
