# bn: rules for agents

bn stores issues as markdown files in the hub repository at ~/.beans/hub. Every mutating command commits and pushes to that repository; you never commit hub files yourself. Reads use the local clone and fetch at most once a minute; run bn sync to force it.

## Where issues live

The project is the name of the git repository you are in: bn resolves it from the basename of `git rev-parse --show-toplevel`, then from the repository's origin URL. Pass `--project <name>` to override, or `--all-projects` on reads to see the whole hub. The first write inside a new repository creates `projects/<name>/` in the hub.

## Ids and links

Ids are `<prefix>-<hash>`, for example `beans-a3f2`; they are unique across the hub and never change. Issue files are `projects/<name>/issues/<id>-<slug>.md`; closed issues move to `archive/<year>/`. Links between issues are Obsidian wikilinks to the file basename: `blocked_by: ["[[beans-a3f2-title]]"]`. A blocker in another project is written the same way; bn resolves it hub-wide. An issue is ready when its status is active and every blocker is closed.

## Commands

- `bn ready` — issues with no open blockers, highest priority first
- `bn list [--status s] [--type t] [--label l] [--closed] [--all-projects]` — table of issues
- `bn show <id>` — one issue with blockers, children, backlinks, and log
- `bn create "title" [-d desc] [-p 0-4] [-t type] [-l label] [--blocked-by id] [--parent id] [--silent]` — new issue; `--silent` prints only the id
- `bn update <id> [--claim] [--status s] [--title t] [--description d] [--priority n] [--assignee a] [--label l] [--note text]` — edit fields; `--claim` sets in_progress and assigns you
- `bn note <id> <text>` — append a log note
- `bn close <id...> -r "reason"` — close with a reason; idempotent
- `bn reopen <id>` — back to the default status
- `bn delete <id> [--force]` — remove the file; refuses while other issues link to it unless forced
- `bn dep add <child> <parent>` — child is blocked by parent; `-t parent-child` sets the parent instead
- `bn dep remove <child> <parent>`, `bn dep tree [id]`, `bn dep cycles`, `bn children <id>`, `bn blocked`
- `bn search <query>` — issues, docs, and memories
- `bn remember "text" [--key k] [--global]` and `bn memories [keyword]` — persistent notes
- `bn doc new <path>`, `bn doc list`, `bn doc backlinks <path>` — wiki pages
- `bn plan init <title> --output <dir>`, edit, `bn plan validate <dir>`, `bn plan put <dir>` — project plans; use `bn plan get`, `list`, and `show` to read them
- `bn request create|list|show|update|link|unlink` — hub-native requests; requests own their canonical `issues` links, and only bn mutates them
- `bn status`, `bn sync`, `bn doctor` — hub state, force a pull and push, check for problems
- `bn serve` — the issues board, requests, and wiki in a browser (requests are read-only there)

Every read accepts `--json` for machine-readable output; use it instead of parsing tables.

## Log lines

Every change appends one line to the issue's `## Log` section: `- <time> <actor> (<repo>@<sha> <branch>): <event>`. Notes go there too. Do not edit or remove log lines.

## Hand edits

Editing files in the hub with an editor is fine; the next bn command commits them as `bn: hand edits`. Keep the frontmatter keys bn owns (id, title, type, status, priority, blocked_by, parent, created, updated) valid; add any other key you like.

## Exit codes

0 success, 1 usage or validation error, 2 not found, 3 git failure (run `bn sync`), 4 another bn holds the hub lock.
