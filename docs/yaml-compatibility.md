# Qualifying the upstream YAML reader contract

From a Linux checkout of the recorded target head, run:

```sh
make compat-yaml-upstream
```

The prerequisites are Git, Python 3.12+, Go 1.25.7, and the repository's locked
Rust 1.98.1 toolchain. The target sets `GOTOOLCHAIN=go1.25.7`; Rust uses
`rust-toolchain.toml` and Cargo's locked dependencies. A fresh runner downloads
the pinned yaml.v3 v3.0.1 module. No Node installation or existing hub is needed.

The gate independently archives immutable Go commit
`718726a580c19becd5fb57513be9e76fda40ea26`, injects test-only package harnesses, and
recaptures the complete AST census and production observations. It compares the
entire recapture with the committed corpus, including table/index identities,
original target expressions, exact source/projection bytes, dependency/source/
license/harness hashes, full models, and ordered diagnostic bytes. It then runs
the actual Rust domain test executable and requires 12,546 observations and zero
differences. Removing an upstream row and changing a successful expected status
must fail the same recapture/comparison checks; those controls run every time.

Each run retains its evidence under `.compat/yaml-upstream/run-*`.
`.compat/yaml-upstream/latest.json` identifies the latest directory and records
its success or failure, exact Git SHA, Linux/toolchain identity, immutable oracle,
source/dependency/harness hashes, report SHA256, and artifact digests. The run
contains `go-recapture.json`, `rust-observations.json`, `report.json`, command
logs, and `negative-controls.json`. Failed runs retain available artifacts and
the failure reason. The separate Linux CI job uploads this directory even on
failure. Pushes to `mvp/rust-yaml-compatibility` trigger the entire existing CI
workflow, including Go/UI, Rust, Verus, and codec coupling, plus this gate. A
successful local run does not stand in for those exact-head CI results.

## Read, edit, and reread the anchored hub-note fixture

The gate creates a real existing-style note beneath the run's demo directory:
`demo/projects/p/issues/p-one.md`. It contains required issue metadata, an
anchored unknown mapping, an unknown alias to it, a retained status comment, and
an existing Markdown body. It reads that saved file's exact bytes into explicit
Go and Rust inputs, then runs the fixed-Go issue harness and the actual Rust
domain executable separately. Both call their production issue readers and
encoders: read the complete original model, change status from `open` to
`in_progress`, encode, reparse, and encode the reread result.

The demo retains:

- `projects/p/issues/p-one.md`: original note bytes.
- `after.md`: the resulting edited note bytes.
- `go-input.json` and `rust-input.json`: exact inputs read from the saved note.
- `go-journey-observations.json` and `rust-journey-observations.json`: complete
  initial, edited, and reread models, stage errors, and encoded bytes.
- `splice-proof.json`: owned status span, unchanged source/destination prefix
  and suffix ranges, before/after hashes, and full-result equality checks.
- Separate Go and Rust execution logs.

For example, inspect the latest retained journey without changing a real hub:

```sh
BN_YAML_RUN=$(python3 -S -c 'import json; print(json.load(open(".compat/yaml-upstream/latest.json"))["directory"])')
cat "$BN_YAML_RUN/demo/projects/p/issues/p-one.md"
cat "$BN_YAML_RUN/demo/after.md"
cat "$BN_YAML_RUN/demo/splice-proof.json"
```

The gate asserts identical full Go/Rust results, identical edited/reread models,
and identical re-encoded bytes. It independently compares every byte outside
the changed status span. The unknown YAML, comments, and body remain untouched.
The fixture is isolated evidence; this journey performs no hub commits or pushes.
The Rust CLI remains a scaffold, so this demo deliberately runs the domain test
executable that exercises the real production codecs.

## Limits and historical outcomes

This is finite Beans projection compatibility, not full yaml.v3 conformance or
conformance to arbitrary upstream Go target types. Every upstream table row is
accounted for: 172 unmarshal rows and 74 node rows. Ten node `[encode]` cases are
upstream encode-only and do not claim decode qualification. The upstream
`TestNodeRoundtrip` owns that annotation handling; its corpus evidence is correct.

All 9,205 reviewed original observations remain unchanged. Supplemental
regressions and status/description edit projections bring the total to 12,546.
Successful notes preserve untouched bytes. Under user-approved scope revision 2,
88 successful manifest no-ops retain historical fixed-Go canonicalization rather
than claiming lossless manifest encoding; their identities are visible in the
report. One historical owned-status-anchor edit encodes successfully but loses
the owned anchor and makes an unknown alias invalid on reread. It remains an
explicit matching historical invalid-edit outcome, separate from valid edits.

This bounded qualification does not close full migration WP3 or WP4–WP9, adopt
executor issues, remove Go, merge to main, deploy, or qualify performance. Deferred
work remains tracked in beans-42nl. Optional malformed-directive diagnostic
follow-up beans-44d4 covers three generic differences outside this census; it is
not an exclusion from these qualified observations.
