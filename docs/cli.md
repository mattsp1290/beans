# CLI reference

`bn` implements issues, requests, plans, docs, memories and handoffs in one Rust executable. System Git transports hub changes; no additional product process implements commands. Existing document IDs, unknown frontmatter, authored body bytes and configuration remain usable without conversion. [format.md](format.md) defines the stored format; [server.md](server.md) describes HTTP and rendering.

## Commands and flags

The inventory below includes every public command, container and generated help entry. Container commands require a subcommand. Use `bn COMMAND --help`, `bn help COMMAND`, or nested `bn help plan put` for argument descriptions; the help helper accepts command names, rather than its own `--help` flag. `bn` with no arguments prints help successfully. `bn man` generates a roff manual from the actual command tree: `bn man | man -l -`.

Global flags accepted by commands are `--actor`, `--hub`, `--project`, `--branch`, `--json`, `--no-fetch`, and `--no-sync`. `-h`/`--help` is available on commands; `-v`/`--version` belongs to the root. Boolean flags accept a bare flag or `=true`/`=false`. Flags without defaults select no change or filter unless their help says otherwise. Repeated `--label`, `--blocked-by`, `--issue`, and `--tag` values are accepted where listed.

`BEANS_HOME`, `BEANS_HUB`, `BEANS_PROJECT`, `BN_ACTOR` and `BN_CONFIG` select paths, project, actor and workflow configuration. Workflow keys merge with precedence `BN_CONFIG` > project `beans.toml` > hub `beans.toml` > defaults. Review, validation and merge hold statuses neither become ready nor satisfy blockers. See [beans.toml.example](beans.toml.example).

| Command and arguments | Local flags |
| --- | --- |
| `bn [COMMAND]` | `-v, --version` |
| `bn serve` | `--host <host>` (default `127.0.0.1`), `--port <port>` (default `3000`) |
| `bn parents <id>` | — |
| `bn archive` | `--all-projects[=<all-projects>]` (default `false`), `--dry-run[=<dry-run>]` (default `false`), `--older-than <older-than>` (default `30d`) |
| `bn blocked` | `--all-projects[=<all-projects>]` (default `false`) |
| `bn children <id>` | — |
| `bn close <id>...` | `--force[=<force>]` (default `false`), `-r, --reason <reason>`, `--suggest-next[=<suggest-next>]` (default `false`) |
| `bn create <title>` | `--assignee <assignee>`, `--blocked-by <blocked-by>`, `-d, --description <description>`, `-l, --label <label>`, `--parent <parent>`, `-p, --priority <priority>` (default `2`), `--silent[=<silent>]` (default `false`), `-t, --type <type>` (default `task`), `--url <url>` |
| `bn delete <id>` | `--force[=<force>]` (default `false`) |
| `bn dep <COMMAND>` | — |
| `bn dep add <child> <parent>` | `-t, --type <type>` (default `blocks`) |
| `bn dep cycles` | — |
| `bn dep remove <child> <parent>` | `-t, --type <type>` (default `blocks`) |
| `bn dep tree [id]` | `--all-projects[=<all-projects>]` (default `false`) |
| `bn list` | `--all-projects[=<all-projects>]` (default `false`), `--archived[=<archived>]` (default `false`), `--assignee <assignee>`, `--closed[=<closed>]` (default `false`), `--label <label>`, `-n, --limit <limit>` (default `50`), `--sort <sort>` (default `created`), `--status <status>`, `--type <type>` |
| `bn note <id> <text>...` | — |
| `bn ready` | `--all-projects[=<all-projects>]` (default `false`), `-n, --limit <limit>` (default `0`) |
| `bn reopen <id>` | — |
| `bn show <id>` | `--include-archived-handoffs[=<include-archived-handoffs>]`, `--raw[=<raw>]` |
| `bn update <id>` | `--assignee <assignee>`, `--claim[=<claim>]` (default `false`), `--description <description>`, `--force[=<force>]` (default `false`), `--label <label>`, `--note <note>`, `--parent <parent>`, `--priority <priority>`, `--status <status>`, `--title <title>`, `--type <type>`, `--unlabel <unlabel>` |
| `bn request <COMMAND>` | — |
| `bn request create <title>` | `--body-file <body-file>`, `-d, --description <description>`, `--issue <issue>`, `-l, --label <label>`, `-p, --priority <priority>` (default `2`), `--requested-by <requested-by>`, `--silent[=<silent>]` (default `false`), `--stdin[=<stdin>]` (default `false`) |
| `bn request link <id> <issue-id>...` | — |
| `bn request list` | `--all-projects[=<all-projects>]` (default `false`), `--label <label>`, `-p, --priority <priority>`, `--query <query>`, `--status <status>`, `--terminal[=<terminal>]` (default `false`) |
| `bn request show <id>` | — |
| `bn request unlink <id> <issue-id>...` | — |
| `bn request update <id>` | `--body-file <body-file>`, `-d, --description <description>`, `--force[=<force>]` (default `false`), `--label <label>`, `--priority <priority>`, `--requested-by <requested-by>`, `--status <status>`, `--stdin[=<stdin>]` (default `false`), `--title <title>`, `--unlabel <unlabel>` |
| `bn handoff <COMMAND>` | — |
| `bn handoff archive [id]...` | `--all-projects[=<all-projects>]` (default `false`), `--dry-run[=<dry-run>]` (default `false`), `--older-than <older-than>` |
| `bn handoff attach <id> <issue-id>` | — |
| `bn handoff create [title]` | `--file <file>`, `--issue <issue>`, `--silent[=<silent>]` (default `false`) |
| `bn handoff detach <id>` | — |
| `bn handoff list` | `--all-projects[=<all-projects>]` (default `false`), `--archived[=<archived>]` (default `false`), `--issue <issue>`, `--limit <limit>` (default `50`), `--older-than <older-than>`, `--sort <sort>` (default `created`) |
| `bn handoff restore <id>...` | — |
| `bn handoff show <id>` | `--raw[=<raw>]` (default `false`) |
| `bn plan <COMMAND>` | — |
| `bn plan get <id>` | `--output <output>` |
| `bn plan init <title>` | `--output <output>` |
| `bn plan link <plan-id> <node-id> <issue-id>` | `--force[=<force>]` (default `false`) |
| `bn plan list` | `--status <status>` |
| `bn plan put <directory>` | — |
| `bn plan show <id>` | — |
| `bn plan status <plan-id>` | — |
| `bn plan unlink <plan-id> <node-id> <issue-id>` | — |
| `bn plan validate <directory>` | — |
| `bn prime` | — |
| `bn sync` | — |
| `bn cache <COMMAND>` | — |
| `bn cache clear` | — |
| `bn doc <COMMAND>` | — |
| `bn doc backlinks <path>` | — |
| `bn doc list [dir]` | `--global[=<global>]` (default `false`) |
| `bn doc new <path>` | `--global[=<global>]` (default `false`) |
| `bn doctor` | `--all-projects[=<all-projects>]` (default `false`) |
| `bn forget <key>` | `--global[=<global>]` (default `false`) |
| `bn import <COMMAND>` | — |
| `bn import bd <export.jsonl>` | `--dry-run[=<dry-run>]` (default `false`), `--force[=<force>]` (default `false`) |
| `bn init <remote>` | — |
| `bn man` | — |
| `bn memories [keyword]...` | `--all[=<all>]` (default `false`), `-n, --limit <limit>` (default `0`), `--tag <tag>`, `--type <type>` |
| `bn project <COMMAND>` | — |
| `bn project create <name>` | `--link[=<link>]` (default `false`) |
| `bn project link <name>` | — |
| `bn project list` | — |
| `bn project show <name>` | — |
| `bn remember <text>...` | `--global[=<global>]` (default `false`), `--key <key>`, `--tag <tag>`, `--type <type>` |
| `bn search <query>...` | `--all-projects[=<all-projects>]`, `--include-archived-handoffs[=<include-archived-handoffs>]`, `--kind <kind>` |
| `bn status` | — |
| `bn help [COMMAND]...` | — |

## Output and mutation semantics

`create`, `request create`, and `handoff create` print only the stable ID in plain
mode; `--silent` retains that guarantee. JSON mutation results expose `id`, `key`
(where useful), `commit`/`sha`, `pushed`, and `message`. `init --json` reports the
hub, remote, branch, and config path. `sync --json` returns `{"synced":true,"status":{...}}`, with `ahead` and `behind` inside `status`. `status` reports hub Git state and read-only project resolution: an explicit `--project` takes precedence over `BEANS_PROJECT` and repository detection. Successful resolution includes `project`, `project_dir` and `resolution`; unresolved selection is reported in `resolution` without creating a project. Issue detail adds `description`, `body`, `log`, `path`, `project`, and
`backlinks` to metadata. Request, memory, and handoff detail includes body and path;
requests include log and issue links. Links retain `raw`/`target`/`display`/`heading`
semantic fields, and CLI timestamps are objects with `seconds`, `nanoseconds`, and `offset_seconds`. Plan
JSON exposes its graph, summaries, sections and lifecycle. CLI dependency nodes/edges and search hits use PascalCase field spellings, such as `ID`, `Title`, `From` and `To`. Treat these semantic fields as the interface; presentation whitespace is not a byte-stability contract. HTTP timestamp fields use RFC3339 strings instead, as described in [server.md](server.md).

Plain issue/request/handoff `show` emits the stored markdown (`--raw` is accepted).
Lists use compact ID/title lines; graph/search/doctor/project/plan-validation/status
reports are structured JSON in either presentation mode. `man` emits a complete
roff manual to stdout, generated from the actual command tree: `bn man | man -l -`.
Root help, version, prime and man need no initialized hub. Root invocation succeeds
and prints help. Runtime failures exit 1; Clap argument failures exit 2. A separate not-found exit 3 is retired: runtime failures, including missing records, use exit 1 and a diagnostic. Automation should distinguish argument errors (2) from runtime errors (1), and inspect JSON semantic fields rather than old plain-text layouts.

`close` requires `--reason` or `--force`; multiple IDs are separate commits and an
error leaves already completed IDs intact. `--suggest-next` prints newly available
work after closure; JSON includes a `next_ready` array in that result. Multi-ID
mutations emit consecutive JSON objects, one per operation. Pretty printing can span multiple lines; consume a JSON stream rather than assuming one object per physical line. Leaving terminal status through update requires `--force`;
`reopen` restores archived issues to `issues/`, and repeated closes/reopens and
unchanged updates are no-ops. Update flags, including `--note`, form one operation
and commit. `dep --type parent-child` sets/removes the parent; `blocks` sets/removes
blockers. `parent` is an additional accepted synonym. Dependency and parent cycles
are rejected, and tree traversal remains bounded even over existing cycles.
`delete` refuses referenced issues unless forced. Forced deletion removes other
issues' structural parent/blocker links, retaining authored body links and other
record references for diagnostics, preserving references outside structural relationship ownership.

Request bodies select exactly one of `--description`, `--body-file` (including
`-`), or `--stdin`; no body selects the project/hub request template. An explicit
empty body suppresses that template. Request updates enforce the fixed lifecycle
unless forced. Issue links validate the entire requested batch before writing.
Handoff `--file -` reads stdin and preserves every final newline. Handoff IDs and
`--older-than` are alternative archive selectors, and `--all-projects` requires
an age selector. Active continuation discovery excludes archived notes by default.
Archive batches validate every explicit ID before writes, and age selection is
recomputed from the locked snapshot on every transaction retry. Handoff archive
uses one commit for the selected batch; JSON dry runs return `dry_run`, `count`,
and `ids`, including an empty array when nothing qualifies.
Docs use project/hub `templates/doc.md` when present and refuse overwrite. Doc
listing includes global docs with the selected project's docs; without a project,
it lists all docs. `--global` selects only global docs. Project linking appends
only the remote value and preserves authored TOML comments, keys, and tables.

Plan init/get require a new `--output` directory. Put captures a validated complete
bundle before Git effects, checks optimistic `updated` revisions and immutable
slug/created fields, and advances revisions for changed replacements. Stale puts
require `bn plan get` and a merge of local changes; completed plans are immutable.
Put refreshes the local revision only if the captured source still matches. Bundle
capture/get preserves authored manifest bytes and section bytes. Plan link/unlink
rereads the current graph, preserves unrelated manifest comments and body, requires
a matching expected issue for unlink, and changes references idempotently. Only
captured/validated bundle files may be replaced or removed; no directory-wide
cleanup or guessed ownership is used. Unexpected authored bundle files cause a
validation error and remain untouched.

`import bd` parses JSONL natively, retaining source issue IDs, mapping issue and
memory records, bodies, actors, closed archives and supported dependencies. It
reports malformed rows, unmapped edge types and unresolved targets. Validation,
containment and overwrite checks complete before writes. `--dry-run` writes
nothing; `--force` permits replacing selected records while preserving unrelated
metadata through the native codecs. Retrying an identical import produces no new
commit. `doctor` reports malformed/duplicate/unresolved documents and cycles and
fails when problems are found; `dep cycles` fails when cycles are found.


## Shell and browser integration

The shell-completion command family (`completion bash`, `zsh`, `fish`, `powershell`) is retired. `bn` does not generate or install shell-specific completion scripts. Its supported discovery interfaces are generated help and `bn man`; configure aliases or completion in your shell separately when needed.

The browser-open flag `serve --open` is also retired. The server binds only the requested host/port and reports its URL without launching a desktop process. Run `bn serve --host 127.0.0.1 --port 3000`, then open `http://127.0.0.1:3000` in your browser or with your operating system's URL opener. This works for local, headless and forwarded sessions without requiring a browser-launch dependency in the executable. `serve` is a foreground process; use a second terminal for other commands.

## Transaction and byte ownership

Writes and deletes use descriptor-relative no-follow containment and the shared locked Git/journal boundary. Hand edits are committed separately before the requested mutation. Each invocation freezes its actor, timestamp, source repository/branch/code SHA and Bn-Run nonce and has at most three push attempts. A replay re-reads the latest document and recalculates eligibility and status transitions. A surviving owned commit retains its original log; a dropped operation can be replayed without duplicating the note. Only a proven single invocation-owned commit can be discarded for replay; unowned commits and authored bytes remain protected. Transport/authentication/hook failures do not authorize history deletion.

Failure preserves necessary local commits and recovery evidence for `bn sync` and diagnosis. There is no blanket hub reset or recursive hub deletion. `cache clear` takes the shared lock and removes derived cache entries while retaining `hub.lock` and `op-journal.json`; recovery evidence is not derived cache. Read commands use snapshot indexing without plan/tree recovery or temporary-file cleanup. Malformed physical frontmatter encoding is rejected before edits; arbitrary body bytes and unrelated frontmatter intervals remain lossless. See [verification.md](verification.md) for the proof, property, model and production-test boundaries.
