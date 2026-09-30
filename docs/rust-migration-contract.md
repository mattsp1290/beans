# Rust migration contract

The migration branch is `feat/go-to-rust`. The immutable Go oracle is revision
`718726a580c19becd5fb57513be9e76fda40ea26`; it is retained in Git history.
Rust has not replaced any production behavior. The implementation scope is the
WP1–WP9 plan in `.agents/plans/go-to-rust/`.

## Reproduce the current oracle

Use Go from `go.mod`, system Git, and Python 3.12 or newer. Python is a
development dependency; the final executable will not invoke it. The harness
uses only Python's standard library and runs with `-S` to exclude ambient site
packages and instrumentation.

```sh
make compat-reference
diff -u tests/contract/commands.json .compat/reference/commands.json
make compat-cli compat-http compat-test
python3 -S tools/compat/build_reference.py --output .compat/version-override --version contract-override
```

`make compat-reference` archives the recorded revision into a temporary
directory and builds it with VERSION `migration-oracle`, without VCS stamping.
It embeds that revision's committed UI placeholder. `.compat/reference/build.json`
records source SHA, linked version, binary digest, Go/Git versions, OS/architecture,
and UI digest. Binaries stay ignored. This target does not use the current
working tree as application source, does not install the reference binary, and
does not modify the user's hub.

`tests/contract/commands.json` comes from the actual Cobra tree after Fang
initialization. It includes hidden `man`, aliases, local and inherited flags,
types, shorthand, defaults, annotations, and direct argument-validator probes
for zero through eight operands. Executable cases separately exercise actual
help, missing arguments, unknown flags, and one/two trailing operands.
Completion and its two hidden request protocols are inventoried as retired.
The current root help/man snapshots still contain Go's completion listings;
WP6 must implement the explicit removal exception without waiving other bytes.

The CLI corpus stores argv, stdin bytes, cwd role, fixture choice, environment
overrides, stdout/stderr bytes, exit status, changed/deleted file bytes, Git
status, refs, ancestry and commit messages. The runner creates an isolated
user directory and BEANS_HOME/BEANS_HUB per case, disables global/system Git
configuration and prompts, and uses no remote credentials. Seed commits have
fixed author/committer metadata. Reads skip fetch. PTY cases use separate
stdout/stderr terminals at 80 columns and 24 rows and preserve ANSI/CRLF bytes.
PTY capture currently requires POSIX.

The real-server HTTP corpus covers all 22 registered routes, with success or
validation/not-found cases. GET cases include HEAD, trailing slash, and uppercase
static-prefix variants (the SSE route currently covers only its GET stream). It
also covers parameter case, traversal, malformed JSON,
SPA fallback and the initial SSE frame, then checks shutdown and filesystem/Git
post-state. The server binds localhost only, uses disposable fixtures, and has
readiness/request/shutdown deadlines.

Only these variables are normalized in the current corpus:

- The exact allocated fixture root in CLI diagnostics and HTTP health `hub`.
- The transport Date header, after validating its format and proximity to UTC now.
- Health Content-Length is adjusted for the fixture-root substitution; GET
  responses first validate it against actual body length. HEAD has no body.

There is no general whitespace, timestamp, ID, ANSI or JSON normalization.
Negative candidate tests independently alter exit status, a JSON field, hub
file bytes and whitespace and require rejection. `CANDIDATE_BINARY=/path/to/bn`
selects a candidate for Make's comparison targets; this is a development control,
not a product flag. Candidates must use the recorded version/UI when compared.
Capture is explicit and must run against the immutable Go executable, never
against Rust to bless a mismatch.

## Regression and storage evidence

`tests/contract/regressions.json` inventories all 225 existing Go test functions
with source locations and proposed Rust destination files. Every destination
is marked pending until its meaningful counterpart passes. None is retired.
Table-driven subcases and assertions still require manual port review; the
function count alone cannot establish regression parity.

`tests/fixtures/go-baseline/` preserves 84 original testdata artifacts byte for
byte, including lossless frontmatter, request, renderer, import and vault cases.
The source revision is recorded in `tests/contract/fixtures.json`.
`tests/fixtures/hub/` supplies fixed issue/request/plan/handoff/memory/doc data,
including archives, for the executable oracle.

## Recorded baseline checks and remaining WP1 work

On Linux arm64 with Go 1.26.0 and Git 2.43.0, `make test` passed. Node 24.12.0
was present outside the default PATH; using its bin directory, `make ui-install
ui-test ui-check` passed (53 UI tests, zero Svelte/TypeScript diagnostics).
The recorded build and performance metadata are in `tests/contract/baseline-linux-arm64.json`
and `tests/contract/performance-linux-arm64.json`.

The performance harness performs three warmups and twenty timed samples for
startup/version and an end-to-end 5,000-issue list/load. It records every sample,
median, p95 and range. Compare Rust on the same hardware/build conditions;
investigate median regressions exceeding 20% beyond baseline noise.

WP1 is still in progress. Required work includes successful mutation journeys
for every command family with validated ID/time/nonce relationships and remote
post-state; signals; custom/malformed configuration; additional
flag grammar interactions and raw/stdin modes; full embedded assets; and a
native Linux/macOS/Windows validation matrix. Initial SSE connection coverage
does not establish watcher/debounce/reload/reconnect parity. Framework/compiler/
solver pins must be tested and recorded before WP2 begins.

The failure corpus covers every documented exit status (0–4), including an
actual wrong-branch Git preflight failure and an exclusive POSIX lock held by
the harness until the executable's 30-second timeout. The latter has a 40-second
harness deadline, so each complete CLI replay includes at least 30 seconds of
lock-wait time. A temporary root substitution is the only change to its diagnostic.

`tests/contract/toolchain-candidates.json` records registry-observed candidate
crate versions and their declared Rust requirements, plus a released Verus pin
and its Linux asset checksum. The release requires Rust 1.98.1 and provides an
x86 Linux asset; this arm64 host requires source-build qualification or execution
on the supported native runner. These are candidates, not a tested lockfile.
Sources: [Verus release](https://github.com/verus-lang/verus/releases/tag/release/0.2026.09.27.3cf1832),
[pinned compiler requirement](https://github.com/verus-lang/verus/blob/release/0.2026.09.27.3cf1832/rust-toolchain.toml),
and [Cargo verification guide](https://verus-lang.github.io/verus/guide/cargo_verus.html).

The only authorized cutover compatibility removals are Go imports/installation
and shell completion. Other differences block cutover. No Rust implementation,
Verus proof, native-platform parity, mixed-client recovery, or completed migration
is claimed by this initial corpus.
