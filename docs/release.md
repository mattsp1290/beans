# Releasing bn

Releases are produced by `.github/workflows/release.yml` from an annotated
`vX.Y.Z` tag on `main`. Pushing the tag is the only human action: the workflow
builds, publishes, smoke-tests and promotes the release. GitHub Releases is the
only distribution origin; no crates.io or package-manager release is configured.

## What a release contains

Six assets, attached to the GitHub release for the tag:

| Asset | Content |
| --- | --- |
| `bn-linux-x86_64`, `bn-linux-aarch64` | Static musl binaries |
| `bn-macos-x86_64`, `bn-macos-aarch64` | macOS binaries |
| `bn-manifest.json` | Version, asset URLs and SHA-256 digests |
| `install.sh` | `distribution/install.sh` as of the tag |

Binaries are raw and unarchived, embed the complete UI and report `bn vX.Y.Z`.
The installer and `bn upgrade` (see [cli.md](cli.md)) both read the manifest
from `<base>/latest/download/bn-manifest.json`, or from
`<base>/download/<tag>/bn-manifest.json` for a pinned version, where `<base>`
is `https://github.com/mattsp1290/beans/releases` or `BN_RELEASE_BASE_URL`.

### Manifest contract (schema 1)

`distribution/manifest.py` writes the manifest:

```json
{
  "schema": 1,
  "version": "0.3.0",
  "tag": "v0.3.0",
  "assets": {
    "linux-x86_64": "https://github.com/mattsp1290/beans/releases/download/v0.3.0/bn-linux-x86_64"
  },
  "sha256": {
    "linux-x86_64": "<64 lowercase hex>"
  }
}
```

`version` is `X.Y.Z` and `tag` is `v` plus `version`. `assets` and `sha256`
hold the same four targets: `linux-x86_64`, `linux-aarch64`, `macos-x86_64` and
`macos-aarch64`. Every asset URL equals `<base>/download/<tag>/bn-<target>`
exactly; consumers compare for equality, not a prefix. Consumers reject any
`schema` other than `1`. The formatting is part of the contract because the
installer parses it with `awk`: two-space indentation, one key per line,
`"assets": {` and `"sha256": {` on their own lines, no `"` or newline inside a
string.

The manifest and the binaries share one origin, so the digest detects
corruption, not a compromised release. Artifact attestations are not produced.

## Workflow

| Job | Runs on | Does |
| --- | --- | --- |
| `validate` | tag | Requires an annotated tag on `main`, equal to the `Cargo.toml` version, with no published release and greater than every published release |
| `ci` | tag, dry run | Runs `ci.yml` for the commit, including `make verify`, and provides the built UI |
| `build` | tag, dry run | Builds each target with `BN_VERSION` set, checks `--version`, runs `distribution/serve-check.sh` and the installer tests, and runs the Git and server journeys against the release musl binary on Linux and the upgrade journeys on macOS |
| `package` | tag, dry run | Writes `bn-manifest.json` and collects the six assets |
| `publish` | tag | Creates a draft, attaches the assets, verifies them and publishes a prerelease |
| `smoke` | tag | On all four platforms: pinned HTTPS install, real `bn upgrade --version <tag> --force`, serve check |
| `promote` | tag | Refuses when a release at or above the tag is already published, marks the release latest, then installs through the documented latest URL and runs `bn upgrade --check --json` |

`releases/latest` never resolves to a draft or a prerelease, so the installer
default and `bn upgrade` only ever see a release that passed `smoke`. Tag runs
share one concurrency group and run one at a time.

The released musl binaries are built from the commit that `ci.yml` verified in
the same run, but they are not themselves the `make verify` subject: proof and
coupling evidence apply to the native Linux x86_64 gnu build. See
[verification.md](verification.md).

## Dry run

`gh workflow run release.yml -f version=v0.0.0` on `main` runs `ci`, the four
`build` entries and `package`, and skips `validate`, `publish`, `smoke` and
`promote`. It publishes nothing. Run it after any change to the workflow, the
build inputs or `distribution/`, and before tagging.

`distribution/serve-check.sh <bn>` is the functional check the workflow uses.
It must pass against `bin/bn` from `make release-build` and fail against a
`make build` binary that embeds the placeholder UI.

## Release checklist

1. Choose the version. Set `[package] version` in `Cargo.toml` and the `beans`
   entry in `Cargo.lock` to it; builds use `--locked`. Confirm with
   `make tidy-check` that nothing else in the lock changed. Merge through a
   normal pull request.
2. Confirm `make ci` and a dry run are green on that `main` commit.
3. On the merged commit: `git tag -a vX.Y.Z -m "bn vX.Y.Z"`.
4. `git push origin vX.Y.Z`. This publishes; it needs the repository owner's
   approval.
5. Watch the run: `gh run watch`.
6. Confirm `gh release view vX.Y.Z --json assets,isDraft,isPrerelease` shows
   six assets, not a draft and not a prerelease, and
   `gh release view --json tagName` shows the tag.
7. On a machine: run the installer, `bn --version`, `bn upgrade` (expect "up to
   date") and `bn upgrade --check --json`.

## Recovery

GitHub runs the workflow file from the tagged commit, so a workflow defect
cannot be fixed by re-running the same tag. Never delete or move a pushed tag
without the repository owner's approval.

- **Dry run fails:** fix on `main` and dispatch again. No tag is involved.
- **Tag run fails before `publish`:** no release exists. A transient failure can
  be re-run from the Actions UI. Otherwise fix on `main`, bump to the next patch
  version and tag again; the failed version number is spent.
- **`publish` fails:** a draft may remain. `releases/latest` ignores drafts, and
  re-running the job deletes and recreates the draft.
- **`smoke` fails:** a public prerelease remains that the installer default and
  `bn upgrade` never select. Delete that prerelease, fix on `main` and tag the
  next patch version.
- **`promote` fails after marking latest:** this is the only path to a bad
  latest release. Mark an older release latest with
  `gh release edit <older-tag> --latest`, or delete the release.

`validate`, `publish`, `smoke` and `promote` cannot be exercised by a dry run.
Do not re-run the jobs of a tag that a newer release has superseded; `promote`
refuses it.

An installer killed by `SIGKILL` can leave a `.bn-install.*` file in the install
directory. Nothing removes it automatically; delete it by hand.

A user rolls back with `bn upgrade --version <older-tag>` or
`BN_INSTALL_VERSION=<older-tag>` with the installer.

## Adding a target

The four targets are spelled out in each of these places; change them together:

- `distribution/manifest.py` (`TARGETS`) and `distribution/tests/run_tests.py`
  (`TARGETS`, `host_target`).
- `distribution/install.sh` (the `uname` mapping in `main`).
- `src/cli/upgrade.rs` (`target`).
- `.github/workflows/release.yml` (the `build` and `smoke` matrices and the
  `ASSETS` list in `publish`).
- The asset table above and the platform list in `README.md`.

## Versions and source builds

`make release-build` embeds the complete Svelte assets in `bin/bn`;
`make install` installs the same build into Cargo's bin directory. Make derives
the version from `git describe --tags --match 'v*' --always --dirty`, with
`VERSION=...` as an override and `dev` when Git is unavailable. Direct Cargo
builds use the same Git fallback and accept `BN_VERSION=...`. Restore
`ui/dist/index.html` to the committed placeholder afterwards; generated assets
remain ignored and must never be committed.

The pre-redesign tags `v0.1.0` and `v0.1.1` remain historical Git references
without GitHub releases.
