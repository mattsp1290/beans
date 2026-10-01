# Verification tooling

The Rust workspace compiles with locked dependencies and the pinned compiler.
`crates/beans-kernel/src/retry.rs` implements the discard decision and opaque
three-attempt budget with same-source proof contracts. `src/splice.rs` validates
ordered, non-overlapping, in-bounds spans, enumerates unchanged intervals, and
translates their byte offsets without overflow. Native Linux x86_64 verification
passed in [run 36772355158](https://github.com/mattsp1290/beans/actions/runs/36772355158)
at `5af204fa3244840ddde484d0cfa2b7a701471a21`: 12 kernel obligations verified,
all ten guard mutations failed a proof, and the restored source verified again.
The report, diagnostics and source/lock digests are preserved in
`tests/contract/verus-kernel-linux-x86_64.json`; the requirement audit is in
`tests/contract/wp2-acceptance.json`. WP2 is accepted. Domain codecs now call the
splice helpers through `src/domain/byte_edit.rs`; tests check their actual
parsed source ranges and copied bytes. The Git pipeline's retry/discard callers
remain a WP4 obligation. The Rust
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
These tests check geometry, not YAML parsing or byte copying. The domain suite
separately checks production codec byte preservation and actual copy ranges.
Proptest persists shrunk failures for conversion into regression fixtures.

Logs and `.compat/verification/qualification.json` record commands, exit codes,
pins and source/lock digests. CI uploads those records, including failures.
Native qualification [run 36760127737](https://github.com/mattsp1290/beans/actions/runs/36760127737)
passed at `8019043dd47777bb545f96d3c206679ba8803b43`. The checked report and
proof output are preserved in `tests/contract/verus-toolchain-linux-x86_64.json`.
The probe verified once, its body mutation failed the increment postcondition
with exit 101, and the restored proof passed. The probe's
bounded integer test and proof do not satisfy either mandatory WP2 obligation.

The kernel proofs check the same executable retry/discard,
attempt-budget and splice helpers used by production callers. No copied
proof-only implementation, admitted proof, `assume`, unconditional axiom or
`external_body` on those helper bodies is allowed. Proofs cover their specified
pure decisions; Git parsing, OS locks, filesystem effects, clocks, YAML syntax,
compiler/solver correctness and process boundaries remain trusted assumptions.
Verus builtins and vstd's standard-library specifications also form part of the
trusted proof boundary. The bounded model assumes its abstract Git outcomes and
idempotent Apply semantics; it does not establish those real effects. Production
callers and these effect boundaries need independent tests in WP3–WP5.

Plan bundle loading and exclusive scaffold creation use Linux filesystem
operations outside the kernel proofs. The immutable Go bundle corpus and actual
Rust-to-Go snapshot reader check captured size, text, path, ordering and
filesystem rejection behavior. Tests compare source trees before and after
loads, including rejected size boundaries. They do not prove race freedom or
all filesystem fault behavior. The scaffold writer consumes its owned `File`
before calling `libc::close` exactly once, because Rust's `File` destructor
discards close errors while Go's writer reports them. Descriptor ownership,
the Linux syscall/errno interface and filesystem write/close outcomes remain
trusted boundaries. `libc` is pinned to the existing locked version; this FFI
call has no Verus correctness claim. Further effect/fault qualification remains
part of the migration acceptance work.

Configuration encoding is qualified against 350 immutable Go writer cases and
actual Go file loads of every Rust-generated output. The byte-oriented writer
preserves Go's nil/empty distinctions, sorted transition keys, control escapes
and raw invalid UTF-8. Of these outputs, 324 are accepted by Go and 26 reproduce
Go's rejection; encoding does not promise that arbitrary Go strings form valid
TOML. Rust hub, project and user file loaders now match 240 captured Go load
cases and the readback of all 350 Go writer outputs. These checks cover missing
files, directories, parent files, symlinks, defaults, typed errors, ignored
unknown keys, Unicode case folding and selected malformed TOML. Inputs remain
unchanged after successful and rejected reads. Six original loader/round-trip
regressions and a 512-case Unicode round-trip property supplement the corpus.
Neither encoder changes the input configuration.

The TOML decoder retains actual parser byte spans for typed key-context errors.
For duplicate-key diagnostics it replaces only the offending key in temporary
memory and reparses to recover the table/dotted/inline context; repaired data is
never returned or written. Selected syntax errors use observed Go wording.
Other TOML syntax/error cases, error ordering when several faults coexist,
filesystem race/fault behavior, workflow-file decoding and workflow source
precedence remain migration obligations. Configuration parsing and filesystem
effects are outside the verified pure kernel contracts. Shared Linux path-error
formatting retains the existing bundle behavior and raw-byte path storage.

Sources: [pinned release](https://github.com/verus-lang/verus/releases/tag/release/0.2026.09.27.3cf1832),
[installation support](https://github.com/verus-lang/verus/blob/release/0.2026.09.27.3cf1832/INSTALL.md),
[compiler components](https://github.com/verus-lang/verus/blob/release/0.2026.09.27.3cf1832/rust-toolchain.toml),
[solver version](https://github.com/verus-lang/verus/blob/release/0.2026.09.27.3cf1832/source/tools/get-z3.sh),
and [Cargo integration](https://verus-lang.github.io/verus/guide/cargo_verus.html).
