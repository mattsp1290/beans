# Verification tooling

The production Rust kernel has not been implemented or verified. The current
artifact is a development-only toolchain probe under `tools/verification/probe`.
It establishes whether ordinary Cargo compiles the same annotated executable
body that the pinned verifier checks. It does not establish retry/discard or
splice correctness, and it has no production caller.

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
```

The target downloads the fixed release into ignored `.compat/verus`, verifies
its checksum before extraction and validates the executable digests. It never
uses a moving `latest` release. The qualification script builds/tests the probe
with ordinary Cargo and `--locked`, checks it with `cargo verus verify --locked`,
then deliberately changes its executable body. The mutated proof must fail
with a verifier error; a compile or setup failure is not accepted as a detected
proof violation. The original source is restored and must verify again.
Missing tools or any unexpected result fail the target and required CI job.

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
