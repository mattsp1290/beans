## Action Items

### Critical
- [ ] [.github/workflows/ci-apps-bean-counter.yml:137-142] The `ls /src/go.work` assertion is vacuous — `/src` exists only in the build stage (`apps/bean-counter/Dockerfile:12`), never in the `alpine:3.22` runtime stage (line 37), which receives only `/usr/local/bin/bean-counter` (line 54). Probe the build stage via `docker build --target build` instead, and make the runtime probe assert it reached a live filesystem before treating a non-zero exit as absence; drop the `2>/dev/null` that turns a broken `docker run` into a pass.

### Important
- [ ] [.github/workflows/ci-libs-beans.yml:52-57] Rewrite the comment: `go install pkg@version` uses `max(installed Go, module go directive)` and never downgrades, so v2.1.6 would have built with the runner's go1.25.7 and passed. Verified by building v2.1.6 with `GOTOOLCHAIN=go1.25.7` and running it against `libs/beans` (0 issues, exit 0). Keep the v2.12.2 pin; justify it as parity with `apps/bean-counter/Makefile:2`.
- [ ] [Makefile:1-43] The root fan-out contract is exercised by no workflow — `ci-workspace` never calls `make`, the per-module jobs run each module's own Makefile, and the root `Makefile` is in no path filter. Add a `make -n` dry-run gate to `ci-workspace`, and add `'Makefile'` to both per-module path filters.
- [ ] [.github/workflows/ci-workspace.yml:6-8] Scope `push:` to `branches: [main]` to match the two path-filtered workflows; the bare `push:` runs this job twice per push on every same-repo PR branch. Keep `pull_request:` unfiltered so the `CLAUDE.md:106-110` required-check guidance still holds.

### Suggestions
- [ ] [.github/workflows/ci-libs-beans.yml:64-65] Call `make test-integration` (added in this commit at `libs/beans/Makefile:18-20`) instead of re-inlining `go test -tags=integration ./...`.
- [ ] [.github/workflows/ci-libs-beans.yml:64] Add a `docker version` preflight before the testcontainers-backed integration step, matching `.github/workflows/ci-apps-bean-counter.yml:184-185`.
- [ ] [libs/beans/Makefile:42] `rm -rf $(dir $(BIN))` expands to `rm -rf ./` under `make clean BIN=bn` (executed). Adopt the `BIN_DIR` shape already used at `apps/bean-counter/Makefile:5-6,59`.
- [ ] [apps/bean-counter/Makefile:35] The hand-enumerated `./cmd ./internal ./test` list will silently skip a future top-level package. Prefer `find . -name '*.go' -not -path './frontend/*'`, which keeps both the node_modules fix and automatic coverage.
- [ ] [.github/workflows/*.yml] Add a `concurrency` group with `cancel-in-progress` on `pull_request`, so PR churn does not queue duplicate 20-minute `images` runs.
- [ ] [.github/workflows/ci-apps-bean-counter.yml:122-150] Add buildx + `type=gha` layer caching to the `images` job; it currently repeats a full in-container `go mod download` and `npm ci` on every run.
- [ ] [.github/workflows/ci-apps-bean-counter.yml:144-150] Consider one smoke start of the API image against its baked-in sqlite defaults (`Dockerfile:47-48`) and a `/api/v1/readyz` poll — `docker compose config` validates rendering only, never runtime.
- [ ] [.github/workflows/ci-apps-bean-counter.yml:147-150] Note in a comment that `docker compose config` needs no daemon (verified locally with the engine down), so this step is reproducible without Docker running.
