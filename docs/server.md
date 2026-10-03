# Server reference

`bn serve` runs Axum, Tokio, Comrak and the filesystem watcher in the Rust executable. The board and wiki use the same native operations and Git transaction boundary as the CLI. System Git remains the external transport for hub changes.

## Build and start

```sh
make release-build
bin/bn --hub /path/to/hub --project example serve --host 127.0.0.1 --port 3000
# Open http://127.0.0.1:3000 in your browser.
```

`make release-build` and `make install` build the complete UI before compilation. For direct installation, run `make ui-install ui-build` then `cargo install --locked --path .`. `build.rs` embeds every current `ui/dist` file without invoking Node; the installed executable serves those bytes even if the build directory is absent. Plain `make build` can embed the committed placeholder without Node. A placeholder build does not provide the complete app. Restore the committed `ui/dist/index.html` after validation and never commit generated assets. See [release.md](release.md).

`serve` accepts `--host` (default `127.0.0.1`) and `--port` (default `3000`) alongside global CLI flags. `--hub`, `--project`, `--actor`, `--branch` and `--no-sync` retain their [CLI meanings](cli.md). The server reports its bound URL and stays in the foreground. It does not support `--open`; open the URL yourself, including when forwarding a headless session. The server has no authentication layer; access to the listener grants its configured read and issue-mutation access. The default binding is loopback. Binding or watcher initialization failures exit unsuccessfully. SIGINT and SIGTERM close SSE producers, drain HTTP and stop the watcher.

## HTTP routes and wire values

Read routes accept GET and HEAD. Project selectors use `project=NAME` or the server's selected `--project`; `_all` selects all projects in project paths or query selectors.

| Route | Behavior and filters |
| --- | --- |
| `/api/health` | Hub/project identity and ahead/behind counts |
| `/api/projects` | Project configuration, workflow and status counts |
| `/api/projects/:project/issues` | Issues; `archived`, `status`, `type`, `label`, `q` |
| `/api/projects/:project/ready` | Work whose configured status and blockers make it ready |
| `/api/projects/:project/requests` | Requests; `status`, `label`, `priority`, `q`, `terminal` |
| `/api/projects/:project/plans` | Plans; `status` |
| `/api/issues/:id`, `/api/requests/:id`, `/api/plans/:id` | Detail; `include_archived_handoffs` controls extra backlinks |
| `/api/graph` | Dependencies and parents; `project` or `all=true` |
| `/api/search` | `q`, comma-separated `kind`, `project`, `include_archived_handoffs` |
| `/api/docs/tree` | Indexed wiki paths for the selected project and global docs |
| `/api/docs/:path` | Known doc, memory or handoff page; `.md` may be omitted |
| `/api/assets/:path` | Indexed hub image bytes |
| `/api/events` | Server-sent reload events |

Known `created`, `updated` and log `at` fields are HTTP RFC3339 strings, preserving fractional nanoseconds and numeric offsets. CLI/domain JSON uses timestamp objects with `seconds`, `nanoseconds` and `offset_seconds`. HTTP issue relationships expose target IDs; CLI relationship objects retain `raw`, `target`, `display` and `heading`. Use the appropriate wire interface rather than copying timestamp or relationship representations between them. Request body queries use the native request filter, including case folding and surrounding-whitespace trimming.

Defaults exclude terminal issues from the board; an explicit status or `archived=true` includes them. Project counts use configured terminal states and group held states with open. Project configuration uses root-level `name` and `prefix`; an absent prefix is displayed as the project name. See [beans.toml.example](beans.toml.example).

| Mutation route | Method |
| --- | --- |
| `/api/projects/:project/issues` | POST to create |
| `/api/issues/:id` | PATCH to update |
| `/api/issues/:id/notes` | POST with nonempty `text` |
| `/api/issues/:id/close` | POST with nonempty `reason` |
| `/api/issues/:id/reopen` | POST |
| `/api/issues/:id/deps` | POST with `target` and relationship `type` |
| `/api/issues/:id/deps/:target` | DELETE; `type` defaults to `blocks` |

Issue mutations enter `IssueMutation` through `Hub::mutate`; requests and plans are read-only HTTP resources. Errors use `{"error":{"code":"...","message":"..."}}`. Validation and malformed JSON return 400, missing resources 404, read-only resource mutations 405, dependency cycles and interrupted/detached Git state 409, hub lock wait expiration 423 (`hub_locked`), Git command/transport failures 502 (`git_error`), and Git deadlines 504 (`git_timeout`). Unknown mutation routes can return 404. Transport body-limit failures retain their HTTP status. Unknown API paths and absent file/asset paths never receive SPA HTML. Embedded assets use explicit MIME types and `nosniff`; HEAD preserves headers and length while omitting the response body. Extensionless application routes fall back to the embedded index page.

## Markdown and search

Comrak produces safe HTML with raw HTML disabled and unsafe URL schemes filtered. Wikilinks resolve indexed names and aliases, including heading fragments. Note embeds expand one level; nested note embeds become links. Heading embeds render links instead of inserting block bodies into heading attributes. Indexed image embeds and Markdown images use the asset endpoint. Code spans/fences, authored links and image-alt text protect literal syntax and authored marker strings.

Highlights, tables, tasks, footnotes, heading self-links and a TOC for all heading levels are supported. One heading formatter and collision allocator assign IDs, self-link targets and outer TOC entries from the same visible text. Embedded headings do not enter the outer document's TOC. Hashtags become `.hashtag` links, such as `#project` linking to `/search?q=project`; the Search screen receives that query and searches indexed content. Hashtags inside code, authored links or image-alt text remain literal. Frontmatter is excluded from rendered document content.

Callouts accept case-insensitive Obsidian markers and custom titles, including nested content. `.wikilink`, `.embed`, `.hashtag`, `.callout` and `.callout-title` CSS hooks are retained. HTML layout and escaping follow Comrak, rather than a byte-identical older renderer. Heading slugs and duplicate suffixes follow its allocator. Raw HTML is omitted; callout fold markers render expanded content, and custom callout kinds use the note presentation. These deliberate presentation choices retain meaningful navigation, search, escaping and content semantics rather than literal HTML whitespace.

Local Markdown image destinations are percent-decoded exactly once before index lookup, then the canonical indexed path is encoded into the asset URL. Encoded spaces and angle-bracket space destinations work. Traversal, encoded separators, double encoding and scheme-bearing decoded destinations cannot become indexed asset rewrites.

## Filesystem and SVG boundaries

Public paths require indexed assets or known doc/memory/handoff pages. Issue/config files cannot be read through those routes. Absolute paths, dot components, traversal, double-encoded traversal, backslashes and symlinks are rejected. Descriptor-relative `openat` with `O_NOFOLLOW` checks every component and requires a regular file. Snapshot loading uses a bounded directory-descriptor chain, preserving native filename bytes while excluding symlink notes from search and transclusions.

Hub-authored SVG keeps its bytes and image MIME type, but GET and HEAD apply `Content-Security-Policy: sandbox; default-src 'none'; script-src 'none'; connect-src 'none'; img-src data:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'`. The sandbox grants neither scripts nor same-origin access. Self-contained diagrams, inline styling and embedded data images remain usable; external SVG resources are blocked. This protects direct navigation and active object embeds as well as image display. The policy does not apply to trusted embedded application HTML/JavaScript. MIME and SVG recognition are case-insensitive.

## Watching, recovery and validation

Server reads, watcher rebuilds and shared operation snapshots use recovery-free `Index::load_snapshot`; reads do not recover or remove interrupted plan artifacts. Recovery-enabled loading and subsequent writes retain explicit journal/tree recovery boundaries. Dependency add/remove compares resolved issue identity so IDs can remove older basename/alias edges without creating duplicate relationships. [cli.md](cli.md) describes locks, nonce ownership, bounded replay and preservation of unrelated authored bytes/history.

The recursive watcher debounces filesystem bursts, rebuilds a serialized snapshot and emits named `reload` events. Git-internal and access events are ignored. SSE has event IDs, reconnectable subscriptions and 15-second heartbeats. Long-lived connections do not hold the index or hub mutation lock.

Permanent native HTTP tests use actual localhost requests and real Git effects to check CLI/HTTP shared history, validation/error categories, cycles, rendering, path security, MIME/HEAD, external edits, configuration/new-directory/plan reloads, debounce, SSE heartbeat/reconnect, shutdown and startup failure. Rendering tests check attributes, heading/TOC collisions, protected literals, hashtag search and bounded embeds. These are regression assertions; the scoped kernel proofs do not prove the entire server. See [verification.md](verification.md) for proof and coupling limits.

[Git write coordination](git-coordination.md) describes the lock shared with CLI
writers and request-correlated diagnostic timings. Error categories survive
context wrappers; runtime Git failures are not validation errors.
