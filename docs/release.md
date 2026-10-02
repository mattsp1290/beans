# Releasing bn

Releases use annotated `vX.Y.Z` tags on `main`. Publication is a separate manual
operation; no crates.io or package-manager release is configured.

1. Validate a clean tagged checkout with `make ci` and native Linux x86_64
   `make verify` using the pinned compiler components.
2. Run `make release-build`; this embeds the complete Svelte assets in `bin/bn`.
   Smoke-test CLI, HTTP asset responses and shutdown before distribution.
3. Install locally with `make install`, or run `make ui-install ui-build` followed
   by `cargo install --locked --path .`. Cargo's configured install root owns the
   binary; no globally installed command is replaced during evaluation.
4. Verify `bn --version`. Make uses `git describe --tags --match 'v*' --always
   --dirty`, with `VERSION=...` as an override and `dev` when Git is unavailable.
   Direct Cargo builds use the same Git fallback and accept `BN_VERSION=...`.
5. Restore `ui/dist/index.html` to the committed placeholder. Generated assets
   remain ignored and must never be committed.

The pre-redesign tags `v0.1.0` and `v0.1.1` remain historical Git references.
