# Releasing bn

Releases are annotated tags `vX.Y.Z` on `main`. The module lives at the
repository root, so plain `v*` tags are what `go install` and `git describe`
use.

1. Make sure `main` is green (`make ci`) and the working tree is clean.
2. Tag and push:
   ```bash
   git tag -a v0.2.0 -m "bn 0.2.0: hub vault redesign"
   git push origin v0.2.0
   ```
3. Smoke test the module path in a fresh directory:
   ```bash
   GOBIN=$(mktemp -d) go install github.com/mattsp1290/beans/cmd/bn@v0.2.0
   $GOBIN/bn --version
   ```
   `go install` builds against the committed `ui/dist/index.html`
   placeholder, so that binary serves the API and the CLI but not the board.
   For a binary with the UI, run `make release-build` from the tagged
   checkout and distribute `bin/bn`.
4. `bn --version` from a checkout prints `git describe` output
   (`v0.2.0`, or `v0.2.0-3-gabc1234-dirty` between releases).

The pre-monorepo tags `v0.1.0` and `v0.1.1` point at an older tree and are
kept as history.
