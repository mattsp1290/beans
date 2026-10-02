# Native Rust server

`bn serve` now runs Axum, Tokio, Comrak and notify in the Rust executable.
It serves the existing Svelte board and wiki. No Go, Python, Node or second
`bn` process handles HTTP reads or mutations. System Git remains the Git
transport used by the shared native hub pipeline.

```sh
# Build the committed placeholder without Node.
cargo build --locked

# Build the existing app and embed every produced file in the release binary.
cd ui
npm ci
npm run build
cd ..
cargo build --release --locked

target/release/bn --hub /path/to/hub --project example serve --host 127.0.0.1 --port 3000
```

`--hub`, `--project`, `--actor`, `--branch` and `--no-sync` retain their native
CLI meanings. `serve` accepts `--host` and `--port`; browser opening is left to
the caller. Binding or watcher initialization failures exit unsuccessfully.
SIGINT and SIGTERM close SSE producers, drain HTTP and stop the watcher.

## HTTP contract

The existing UI routes and wire fields remain supported: health/projects;
project issues, ready, requests and plans; issue/request/plan details; graph;
docs tree and wildcard pages; indexed image assets; search; and `/api/events`.
Issue POST/PATCH, note, close, reopen, dependency POST and dependency DELETE
use `IssueMutation` through `Hub::mutate`. Request and plan HTTP resources are
read-only. A project called `_all` selects all projects. Project configuration
uses root-level `name` and `prefix`, as in `docs/beans.toml.example`; an absent
prefix is displayed as the project name.

Errors use `{"error":{"code":"...","message":"..."}}`. Validation and
malformed JSON return 400, missing resources 404, read-only mutations 405,
dependency cycles and interrupted/detached Git state 409, hub lock contention
423 and Git command/transport failures 502. Defaults exclude terminal issues
from the board, while an explicit status or `archived=true` includes them.
Project counts use configured terminal states and group held states with open.
Unknown API paths and absent asset/file paths never receive SPA HTML. Static
responses use explicit MIME types, `nosniff`, and HEAD omits the response body
while retaining its length.

## Rendering and filesystem boundaries

Comrak produces safe HTML with raw HTML disabled and unsafe URL schemes
filtered. Wikilinks resolve through the index, including aliases and heading
fragments; note embeds expand one level, while nested note embeds become links.
Indexed image embeds and Markdown images use the asset endpoint, with encoded
spaces supported. Code spans and fences preserve literal wikilinks/embeds.
Highlights, tables, tasks, footnotes, heading anchors and a heading TOC work.
Callouts accept case-insensitive Obsidian markers and custom titles. Existing
`.wikilink`, `.embed` and `.callout` CSS hooks remain present.

Rendered HTML bytes intentionally differ from Goldmark. Comrak uses its own
GFM heading slug/duplicate rules, emits TOC entries for all heading levels, and
omits raw HTML. Fold markers on callouts render expanded content; custom
callout kinds use the note presentation. Hashtags remain text. These are
presentation changes covered by semantic native assertions, not an assertion
that the old Goldmark byte-for-byte golden suite has passed.

Public paths require indexed assets or known doc/memory/handoff pages. Private
issue/config files cannot be read through those routes. Absolute paths, dot
components, traversal, double-encoded traversal, backslashes and symlinks are
rejected. Descriptor-relative `openat` with `O_NOFOLLOW` checks every component
and requires a regular file.

Two shared repairs accompany the HTTP adapter. Dependency add/remove compares
resolved issue identity, so an ID can remove an older basename/alias edge
without adding duplicate edges. Snapshot note loading uses the same
nofollow reader, preserving native filename bytes and preventing symlink
notes from leaking into search or transclusions. `Index::load_snapshot` still
performs no recovery or cleanup. Recovery-enabled `Index::load` retains its
previous behavior; server reads, watcher rebuilds and shared operation snapshots
use the recovery-free path.

The recursive watcher debounces filesystem bursts, rebuilds a serialized
snapshot, and emits named `reload` events. Git-internal and access events are
ignored. SSE has event IDs, reconnectable subscriptions and 15-second
heartbeats. No long-lived connection holds the index or hub mutation lock.

## Build and validation evidence

`build.rs` embeds the entire current `ui/dist` tree; it never runs Node. A
placeholder build is intentional for development and does not qualify a
release. Release pipelines must build the UI before Cargo. No generated UI
assets or generated `ui/dist/index.html` are committed. `BN_VERSION` can set
build metadata; otherwise the build uses the established
`git describe --tags --match 'v*' --always --dirty` behavior, with worktree
HEAD/index/ref inputs registered for Cargo rebuilds.

This slice ran `cargo test --locked --workspace` (297 tests, including 12
native server tests), `cargo fmt --all --check`, `cargo clippy --workspace
--all-targets --locked -- -D warnings`, UI tests (53), UI checking (zero errors
or warnings), UI production build, release Cargo build and `git diff --check`.
Native HTTP tests exercise actual localhost connections, real Git commits and
remote pushes, CLI reads of HTTP-authored history, malformed bodies, validation,
cycles, lock/Git conflicts, rendering, path security, MIME/HEAD, external edits,
config/new-directory/plan watcher reloads, debounce, SSE heartbeat/reconnect,
shutdown signals and startup failure. Snapshot tests preserve an actual named
`.backup` tree while concurrent readers and refreshes run.

A release checkpoint also served every embedded file with matching SHA256 and
MIME while `ui/dist` was temporarily absent and Node was absent from PATH;
SIGTERM exited zero. Its binary SHA256 was
`5c3aac40add211746ac8ef9ec13a7d983219901e17f7de9540bd4652acc99cf5` and version
`v0.2.0-132-gcbe3d98-dirty`. This is an evolving slice checkpoint, not an
immutable final-product acceptance identity. The coordinator owns the release
rebuild and browser audit at the frozen candidate after independent reviews.

The regression ledger now records 213 ported, one retired and 11 pending
entries. This slice adds precise passing server, heading, bounded-embed,
watcher and version mappings. The old renderer golden suite and remaining
resolver/tree regression mappings remain qualification work. Retained Go
fixtures/source removal, native x86 proof CI, repository `make ci`, and final
browser/manual acceptance belong to later coordination; this document does
not claim all 225 retained regressions or the Rust-only product are qualified.
