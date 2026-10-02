# Native qualification

`make verify-native` runs the locked Rust workspace tests without a historical
ledger dependency. The additional cutover gate, `make qualify-native-dispositions`,
checks all 225
baseline scenarios in `tests/contract/regressions.json`. Each retained entry
names an exact test compiled into a Rust test binary, including its suite.
Missing selectors, pending entries, duplicate source identities, unjustified
retirements and old cross-read export hooks fail the gate. Both gates execute the native suite. The generated report
is `.verification/native/report.json`; it records the checkout SHA, ledger
hash, platform, suite counts and distinct executable selectors. A selector audit
establishes that a named regression runs, not that every possible input or every
facet of an older test is covered.

The tests own their inputs and assertions. `tests/fixtures/native-baseline`
contains the enduring authored files and import exports previously stored with
Go sources; their bytes are unchanged. `tests/contract/fixtures.json` records
source paths and SHA256 provenance. Committed corpora under `tests/contract`
retain historical origin metadata and explicit expected values. Reading these
static values does not build or execute a reference client. Native tests do not
launch Go, require `tools/compat`, export cross-read candidates or access the old
source fixture directories. Future expected-value changes must be justified by
the stored format or intended native behavior, rather than regenerated blindly.

The one retirement is literal CLI text formatting. Native command journeys
instead assert IDs, semantic JSON fields, stored bytes, exit behavior and pushed
Git history. Markdown deliberately uses Comrak: tests assert headings/TOC,
links/aliases, bounded embeds, callouts, GFM and escaping rather than literal
Goldmark HTML. Hashtags and embeds are represented by link/text AST nodes,
with generated embed markup emitted only at its intended node. Comrak handles
heading IDs, self-links and escaped metadata through one collision allocator;
the same formatter callback records the outer TOC without scanning embedded
headings. Regression tests preserve authored marker literals in code, plain
text and image attributes, and check mixed tagged/plain heading collisions.
Heading embeds render links so block bodies cannot corrupt heading attributes. Hashtag links use the supported `q` search parameter; a real
HTTP search follows that rendered query and finds a document through its tags. Plan export refuses an existing authored destination instead of
replacing its whole tree. Native tests check destination bytes remain intact,
escaping bundle paths are rejected before hub writes, valid exports contain the
selected bundle, and legacy interrupted backups recover byte-for-byte before
subsequent writes. These changed behaviors are recorded on the relevant ledger
entries, not presented as exact old output parity.

## Real production effects

The native CLI suite invokes the compiled Rust binary and system Git against
isolated bare repositories and two clones. It exercises lock contention,
read-only locked snapshots, interrupted/detached checkouts, offline and no-sync
writes, partial apply/stage/commit errors, journals and recovery, push rejection
and replay, bounded exhaustion, successful and dropped rebases, stranded nonce
ownership and identical same-second notes. Assertions retain authored bytes,
unowned history, both writers' effects and exactly one effect per invocation.
The HTTP suite uses real loopback requests, native mutations, watcher reloads,
SSE reconnect/heartbeat/shutdown and independent Git observers. CLI and HTTP
assertions share production operations without an executable Go oracle.

Snapshot index reads open directory components and file leaves relative to
nofollow descriptors. One walk reuses already opened directory descriptors;
the cache holds at most 16 directories and is discarded before returning,
including on walk failure. A real CLI process with 100 projects and a 64-file
descriptor limit verifies that indexing does not silently drop notes. Each leaf is
still opened with `O_NOFOLLOW|O_NONBLOCK` and checked as a regular file. A
negative control replaces a cached directory path with an external symlink:
the current walk reads only the original pinned directory, while a fresh read
rejects the replacement. Leaf symlinks and traversal also fail. This optimization
does not give the index atomic whole-hub snapshot semantics.

## Same-source proof and coupling

On native Linux x86_64 Ubuntu 24.04, install the pinned components and run:

```sh
rustup toolchain install 1.98.1 --profile minimal --component rustfmt --component clippy --component rustc-dev --component llvm-tools
make rust-check verify
```

`make verify` comprises the native gate, toolchain qualification, kernel proof,
production codec coupling and production retry coupling. CI runs on `main`,
`mvp/rust-only-cutover`, `mvp/rust-only-cutover-qualification` pushes and pull
requests. All generated verification evidence lives in ignored `.verification`.

`tools/verification/pins.json` fixes Verus release
`release/0.2026.09.27.3cf1832`, source commit
`3cf18325f0fd0c3040fbdec8c0f2255c0504c91a`, Rust 1.98.1, matching
`vstd` 0.0.0-2026-09-20-0158 and Z3 4.16.0. Installation checks both archive and
individual executable hashes. It rejects unsupported hosts; ordinary compilation
on aarch64 is not a Verus proof success. Historical reports committed under
`tests/contract` describe their recorded revisions only. Current proof evidence
must come from the current candidate's native x86_64 CI run.

`make verify-toolchain` qualifies Cargo erasure and verification of the same
annotated executable body using the development probe. A deliberately incorrect
body must fail with a verifier error; setup or compile failure cannot count as a
rejected proof. Restored source must verify again. This probe establishes the
toolchain path, not application correctness.

`make verify-kernel` checks the shipped retry/budget and splice sources. The retry
contract bounds the push budget and authorizes discarding only a current owned
operation HEAD with exactly one local commit. The splice contracts check ordered,
non-overlapping, in-bounds spans and unchanged interval/offset geometry without
overflow. Ten negative controls weaken ownership, current HEAD, single-commit,
operation presence, push exhaustion/decrement, overlap/bounds/order and overflow
checks. Every mutation must fail a proof and restored sources must verify again.
The script rejects unapproved assume/admit/axiom/external-body escape hatches.

`make verify-codec-coupling` instruments the real production library with the
pinned compiler's LLVM tools. Thirteen isolated native tests cover issue,
request, memory and handoff encoders in UTF-8 and UTF-16 LE/BE, plus plan graph
splices. They derive independent physical ranges, check exact changed bytes and
unchanged copy intervals, and observe the actual compiled kernel helper bodies.
A parse-only control must have zero helper entries. Changed production caller
inventory requires extended coupling cases.

`make verify-retry-coupling` observes actual `decide_retry` and
`take_push_attempt` bodies while real-Git production journeys exercise owned
replay, unowned hand-edit preservation, bounded exhaustion and stale nonce
refusal. Every case must execute both helpers. A read-only fake resolver control
must execute neither. Profiles, LLVM exports, logs and source/binary/lock hashes
are retained alongside the report. These are observations of compiled bodies,
not a proof that Git or the operating system implements a model.

Property tests independently check byte preservation, splice geometry, scalar
and path rules, duration arithmetic, graph workflows and generated edit
sequences. `tests/model.rs` explores two clients with injected Git outcomes,
offline/user commits, ownership and restart, using the production retry kernel.
It asserts bounded attempts and retention of unowned commits. Its finite state
space is not a filesystem, Git, parser or concurrent I/O model.

The trusted boundary includes the compiler, Verus/Z3, operating system, Git and
I/O adapters. Kernel proofs do not establish YAML interpretation, parser span
construction, crash/fsync durability or whole-hub transaction correctness.
Production coupling, properties, model exploration and real journeys provide
separate evidence for those adapters within the scenarios they execute.
