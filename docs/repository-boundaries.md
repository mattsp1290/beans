# Repository boundaries

The product is the Rust workspace (`src`, `crates/beans-kernel`, `build.rs`) and
its embedded Svelte UI. Build, install, test and release commands use locked
Cargo. System Git is an external runtime dependency. Python is used only for
repository agent workflows, disposable evaluations and development proof tools;
it never launches a substitute product implementation.

Native tests own authored fixtures and static expected values under
`tests/fixtures`. Original filenames and origin hashes identify historical
inputs; imported issue text is opaque authored data. Historical source-test
citations in test comments identify regression provenance, not executable
oracles. The completed cutover census is archived outside the tracked product.

`src/vault/glob.rs`, `remote_url.rs` and `remote_ip.rs` are native Rust adaptations
of BSD-licensed algorithms. Their required Go Authors notices and
`src/vault/GO_LICENSE` remain attribution; no Go implementation or toolchain is
built or invoked. Earlier dated entries in `docs/decisions.md`, the tracked
`.claude/plans` and the dated agent-skill qualification record describe historical
revisions. Current architecture and commands are in `AGENTS.md`, `CLAUDE.md`,
`README.md`, `docs/release.md` and `docs/verification.md`.
