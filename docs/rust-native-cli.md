# Native Rust CLI command contract

The Rust binary implements the retained CLI with native shared operations. It uses
system Git for hub transactions. The commands below use the existing hub format;
existing IDs, unknown issue/request/handoff/memory frontmatter, authored body bytes,
configuration keys, and archived documents remain usable without conversion.

The 73-entry baseline census is accounted for as 59 runnable root/leaf/help entries,
8 command containers, 5 excluded shell-completion entries, and the later `serve`
entry. Clap supplies the `help [command]` helper. Go package imports/install are
outside the retained product. `serve` and UI embedding belong to the next slice.
No retained command or local flag is retired. `parents <id>` is an additional
read command alongside `children`.

Global flags are `--actor`, `--hub`, `--project`, `--json`, `--no-fetch`,
`--no-sync`, `--help` (`-h`), and `--version` (`-v`). `--branch` is the native
branch override from the first slice. Environment/configuration resolution retains
`BEANS_HOME`, `BEANS_HUB`, `BEANS_PROJECT`, `BN_ACTOR`, and `BN_CONFIG` plus user,
hub, and project configuration. Workflow keys merge in the documented precedence;
review, validation, and merge holds neither become ready nor satisfy blockers.

The following inventory is checked against the baseline by
`retained_command_and_flag_census_is_callable` in `tests/native/commands.rs`.
All commands also support generated help; container commands require a subcommand.
Flags with no default select no change/filter unless otherwise described.

| Command | Local flags |
| --- | --- |
| `archive` | `--all-projects`, `--dry-run`, `--older-than` default `30d` |
| `blocked` | `--all-projects` |
| `cache` | — |
| `clear` | — |
| `children <id>` | — |
| `close <id...>` | `--force`, `--reason` (`-r`), `--suggest-next` |
| `create <title>` | `--assignee`, `--blocked-by` repeatable, `--description` (`-d`), `--label` (`-l`) repeatable, `--parent`, `--priority` (`-p`) default `2`, `--silent`, `--type` (`-t`) default `task`, `--url` |
| `delete <id>` | `--force` |
| `dep` | — |
| `add <child> <parent>` | `--type` (`-t`) default `blocks` |
| `cycles` | — |
| `remove <child> <parent>` | `--type` (`-t`) default `blocks` |
| `tree [id]` | `--all-projects` |
| `doc` | — |
| `backlinks <path>` | — |
| `list [dir]` | `--global` |
| `new <path>` | `--global` |
| `doctor` | `--all-projects` |
| `forget <key>` | `--global` |
| `handoff` | — |
| `archive [id...]` | `--all-projects`, `--dry-run`, `--older-than` |
| `attach <handoff-id> <issue-id>` | — |
| `create [title]` | `--file`, `--issue`, `--silent` |
| `detach <handoff-id>` | — |
| `list` | `--all-projects`, `--archived`, `--issue`, `--limit` default `50`, `--older-than`, `--sort` default `created` |
| `restore <id...>` | — |
| `show <id>` | `--raw` |
| `import` | — |
| `bd <export.jsonl>` | `--dry-run`, `--force`, `--project` |
| `init <remote>` | — |
| `list` | `--all-projects`, `--archived`, `--assignee`, `--closed`, `--label`, `--limit` (`-n`) default `50`, `--sort` default `created`, `--status`, `--type` |
| `man` | — |
| `memories [keyword...]` | `--all`, `--limit` (`-n`) default `0`, `--tag`, `--type` |
| `note <id> <text...>` | — |
| `plan` | — |
| `get <id>` | `--output` |
| `init <title>` | `--output` |
| `link <plan-id> <node-id> <issue-id>` | `--force` |
| `list` | `--status` |
| `put <directory>` | — |
| `show <id>` | — |
| `status <plan-id>` | — |
| `unlink <plan-id> <node-id> <issue-id>` | — |
| `validate <directory>` | — |
| `prime` | — |
| `project` | — |
| `create <name>` | `--link` |
| `link <name>` | — |
| `list` | — |
| `show <name>` | — |
| `ready` | `--all-projects`, `--limit` (`-n`) default `0` |
| `remember <text...>` | `--global`, `--key`, `--tag` repeatable, `--type` |
| `reopen <id>` | — |
| `request` | — |
| `create <title>` | `--body-file`, `--description` (`-d`), `--issue` repeatable, `--label` (`-l`) repeatable, `--priority` (`-p`) default `2`, `--requested-by`, `--silent`, `--stdin` |
| `link <request-id> <issue-id...>` | — |
| `list` | `--all-projects`, `--label`, `--priority` (`-p`) (unset when absent), `--query`, `--status`, `--terminal` |
| `show <request-id>` | — |
| `unlink <request-id> <issue-id...>` | — |
| `update <request-id>` | `--body-file`, `--description` (`-d`), `--force`, `--label` repeatable, `--priority` (unset when absent), `--requested-by`, `--status`, `--stdin`, `--title`, `--unlabel` repeatable |
| `search <query...>` | `--all-projects`, `--include-archived-handoffs`, `--kind` |
| `show <id>` | `--include-archived-handoffs`, `--raw` |
| `status` | — |
| `sync` | — |
| `update <id>` | `--assignee`, `--claim`, `--description`, `--force`, `--label` repeatable, `--note`, `--parent`, `--priority` (unset when absent), `--status`, `--title`, `--type`, `--unlabel` repeatable |

`create`, `request create`, and `handoff create` print only the stable ID in plain
mode; `--silent` retains that guarantee. JSON mutation results expose `id`, `key`
(where useful), `commit`/`sha`, `pushed`, and `message`. `init --json` reports the
hub, remote, branch, and config path. `sync --json` reports synchronization and
hub status. Issue detail adds `description`, `body`, `log`, `path`, `project`, and
`backlinks` to metadata. Request, memory, and handoff detail includes body and path;
requests include log and issue links. Links retain `raw`/`target`/`display`/`heading`
semantic fields, and timestamps retain seconds/nanoseconds/offset fields. Plan
JSON exposes its graph, summaries, sections and lifecycle. Existing domain graph,
search and execution serializers retain their PascalCase field spellings. These
are intentional JSON presentation changes from Go; callers should use semantic
fields and should not compare pretty-print whitespace or timestamp spelling.

Plain issue/request/handoff `show` emits the stored markdown (`--raw` is accepted).
Lists use compact ID/title lines; graph/search/doctor/project/plan-validation/status
reports are structured JSON in either presentation mode. `man` emits a complete
roff manual to stdout, generated from the actual command tree: `bn man | man -l -`.
Root help, version, prime and man need no initialized hub. Root invocation succeeds
and prints help. Runtime failures exit 1; Clap argument failures exit 2. The former
Go not-found exit 3 and text goldens are replaced by an explicit failed status and
useful diagnostic, without retiring not-found detection or usage validation.

`close` requires `--reason` or `--force`; multiple IDs are separate commits and an
error leaves already completed IDs intact. `--suggest-next` prints newly available
work after closure; JSON includes a `next_ready` array in that result. Multi-ID
mutations emit one JSON record per operation (newline-delimited JSON). Leaving terminal status through update requires `--force`;
`reopen` restores archived issues to `issues/`, and repeated closes/reopens and
unchanged updates are no-ops. Update flags, including `--note`, form one operation
and commit. `dep --type parent-child` sets/removes the parent; `blocks` sets/removes
blockers. `parent` is an additional accepted synonym. Dependency and parent cycles
are rejected, and tree traversal remains bounded even over existing cycles.
`delete` refuses referenced issues unless forced. Forced deletion removes other
issues' structural parent/blocker links, retaining authored body links and other
record references for diagnostics, matching the former operation's ownership.

Request bodies select exactly one of `--description`, `--body-file` (including
`-`), or `--stdin`; no body selects the project/hub request template. An explicit
empty body suppresses that template. Request updates enforce the fixed lifecycle
unless forced. Issue links validate the entire requested batch before writing.
Handoff `--file -` reads stdin and preserves every final newline. Handoff IDs and
`--older-than` are alternative archive selectors, and `--all-projects` requires
an age selector. Active continuation discovery excludes archived notes by default.
Docs use project/hub `templates/doc.md` when present and refuse overwrite.

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

All hub writes/deletes use descriptor-relative no-follow containment and the
existing locked Git/journal/retry boundary. Failed effects retain partial bytes,
commits and recovery evidence for `bn sync` and diagnosis. No command resets or
recursively deletes the hub. `cache clear` takes the shared lock and removes
derived cache entries while retaining both `hub.lock` and `op-journal.json`;
recovery evidence is not derived cache. Read commands use snapshot indexing and
never perform plan/tree recovery or temporary-file cleanup. See
[rust-first-cli.md](rust-first-cli.md) for transaction and ownership details.
