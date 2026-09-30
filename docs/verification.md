# Verification tooling

The Rust workspace compiles with locked dependencies and the pinned compiler.
`crates/beans-kernel/src/retry.rs` implements the discard decision and opaque
three-attempt budget with same-source proof contracts. `src/splice.rs` validates
ordered, non-overlapping, in-bounds spans, enumerates unchanged intervals, and
translates their byte offsets without overflow. Native verification of the full
kernel and its guard mutations is pending; ordinary Cargo tests are not proof
evidence. Production callers are still pending. The Rust
entry point is a migration scaffold, not a usable replacement for `bn`.

The separate development-only probe under `tools/verification/probe` established
that ordinary Cargo compiles the same annotated executable body checked by the
pinned verifier. That probe does not establish either mandatory kernel obligation.

`tools/verification/pins.json` pins Verus release
`release/0.2026.09.27.3cf1832`, source commit
`3cf18325f0fd0c3040fbdec8c0f2255c0504c91a`, Rust 1.98.1, matching
`vstd` 0.0.0-2026-09-20-0158 and Z3 4.16.0. The release archive SHA256 and
individual verifier, Cargo integration, backend and solver binary digests are
recorded from the checksummed archive. The solver version is also checked at
runtime. Probe dependencies are locked in its separate Cargo.lock.

The released Linux binary supports x86_64 on Ubuntu 24.04. Qualification runs
on that native CI runner; all CI remains Linux-only. The installer rejects
other architectures explicitly. On Linux arm64, the pinned compiler compiled
and tested the probe with proof annotations erased, but the released verifier
cannot run there. No verification success is inferred from normal compilation.

On a supported native host:

```sh
rustup toolchain install 1.98.1 --profile minimal --component rustc-dev --component llvm-tools
make verify-toolchain
make verify-kernel
```

The target downloads the fixed release into ignored `.compat/verus`, verifies
its checksum before extraction and validates the executable digests. It never
uses a moving `latest` release. The qualification script builds/tests the probe
with ordinary Cargo and `--locked`, checks it with `cargo verus verify --locked`,
then deliberately changes its executable body. The mutated proof must fail
with a verifier error; a compile or setup failure is not accepted as a detected
proof violation. The original source is restored and must verify again.
Missing tools or any unexpected result fail the target and required CI job.

`make verify-kernel` checks `cargo verus verify -p beans-kernel --locked` from
the application workspace. It removes each discard guard, weakens exhaustion,
and removes the budget decrement in turn. Every mutation must produce a proof
error, and the restored source must verify again. Source restoration runs even
after failure. `.compat/verification/kernel.json` records source/lock digests
and each result; corresponding logs are uploaded by CI. A passing kernel target
includes the retry/budget and splice obligations. Neither the decision proof nor
the arithmetic proof establishes correctness of the Git or YAML adapters.

Normal compilation and tests run separately through `make rust-build rust-test
rust-check`. They use the same kernel files with annotations erased. The default
`make build` still builds Go, and no regression has been retired.

`tests/model.rs` explores two clients performing the same idempotent operation,
with three total pushes per client (including any post-restart attempts), zero or
one offline commit and hand-edit commit per clone, and at most one injected
interruption/restart per trace. It calls the actual retry helper and reconstructs
the opaque budget by replaying actual grants. Initial exploration checked 61,856
states. Its always properties check user-commit preservation, owned discard
targets, at most one remote effect, and the push bound. Sometimes properties
require witnesses for success, rejection, network failure, conflicting rebase,
dropped and empty operations, exhaustion, and restart. Missing outcome coverage
fails the test. This is a finite abstract decision model with injected Git
outcomes; real Git, filesystem effects, and crash recovery are WP4 obligations.

`tests/properties.rs` runs 512 generated cases for each of three checks:
pairwise range validity, offset translation against wider integer arithmetic,
and complete/source-ordered preservation of the complement of edited spans.
These tests check geometry, not the future codec's YAML parsing or byte copying.
Proptest persists shrunk failures for conversion into regression fixtures.

Logs and `.compat/verification/qualification.json` record commands, exit codes,
pins and source/lock digests. CI uploads those records, including failures.
Native qualification [run 36760127737](https://github.com/mattsp1290/beans/actions/runs/36760127737)
passed at `8019043dd47777bb545f96d3c206679ba8803b43`. The checked report and
proof output are preserved in `tests/contract/verus-toolchain-linux-x86_64.json`.
The probe verified once, its body mutation failed the increment postcondition
with exit 101, and the restored proof passed. The probe's
bounded integer test and proof do not satisfy either mandatory WP2 obligation.

The upcoming kernel proofs must check the same executable retry/discard,
attempt-budget and splice helpers used by production callers. No copied
proof-only implementation, admitted proof, `assume`, unconditional axiom or
`external_body` on those helper bodies is allowed. Proofs cover their specified
pure decisions; Git parsing, OS locks, filesystem effects, clocks, YAML syntax,
compiler/solver correctness and process boundaries remain trusted assumptions.
The production callers and these effect boundaries need independent tests.

Sources: [pinned release](https://github.com/verus-lang/verus/releases/tag/release/0.2026.09.27.3cf1832),
[installation support](https://github.com/verus-lang/verus/blob/release/0.2026.09.27.3cf1832/INSTALL.md),
[compiler components](https://github.com/verus-lang/verus/blob/release/0.2026.09.27.3cf1832/rust-toolchain.toml),
[solver version](https://github.com/verus-lang/verus/blob/release/0.2026.09.27.3cf1832/source/tools/get-z3.sh),
and [Cargo integration](https://verus-lang.github.io/verus/guide/cargo_verus.html).
