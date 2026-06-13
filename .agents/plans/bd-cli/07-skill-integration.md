# 07 — Skill integration (`/big-change`, `/new-project`)

Goal: a Claude Code agent runs `/big-change` or `/new-project` on a **new
project** and gets a real, dispatchable backlog authored into Postgres via `bn`,
which the orchestrator (Postgres tracker adapter) can then process.

## What the skills use today

Both `SKILL.md`s (in `~/.claude/skills/` and `~/git/dotfiles/.agents/skills/`)
invoke: `bd init`, `bd create` (`-d -p -l -t --silent`), `bd dep add <child>
<parent>`, `bd ready`. They capture ids via `ID=$(bd create … --silent)` and wire
deps with `bd dep add`. (They also reference `bd add` only to say "don't use it.")

Because `bn` mirrors exactly these commands + flags + the `--silent` id contract
(01), the skill bodies change **only the binary name** `bd` → `bn` on the relevant
invocations.

## Greenfield model (the decision)

- **`beans` keeps `bd`/Dolt** for its own backlog + memory — untouched.
- **New projects use `bn`/Postgres.** So the skills must select the tracker per
  project, not globally.

Recommended mechanism (corrected per `09 §E`): a **committed per-repo marker**,
**not** a `${TRACKER:-bd}` env default.

- A repo that uses `bn` carries a checked-in marker (e.g. a `.bn` file / a
  `tracker: bn` key in a project config). The skill detects the marker and selects
  `bn`; absence ⇒ `bd`. This is deterministic per-repo and committed, so an agent
  cannot **silently** pick the wrong tracker (the failure mode of an env default
  that's easy to forget to set).
- Provide a `bn`-analogue for the skills' existing `.beads`-dir init guard (the
  skills check for/instantiate `.beads`; they need the equivalent "is this a `bn`
  project / `bn init`" check).
- Avoid `${TRACKER:-bd}`: the skill body is an **LLM-emitted bash script** whose
  prompt + worked examples hardcode `bd`; a default-to-`bd` env switch both
  contradicts "new projects use bn" and risks model non-compliance. The committed
  marker removes the ambiguity.

> These skills live in the user's **dotfiles** (`~/git/dotfiles/.agents/skills/
> {big-change,new-project}/SKILL.md` and the `~/.claude/skills/` copies). Editing
> them is a dotfiles change, out of this repo — call it out as a separate, small
> follow-up PR, and keep `bn` itself usable standalone so the skill edit is
> optional (an agent can call `bn` directly).

## End-to-end on a new project

1. `bn init --prefix=<proj>` (or auto on first `create`) → registers the project
   in Postgres.
2. `/big-change`/`/new-project` (with `TRACKER=bn`) author the plan: `bn create`
   the tasks (capturing ids), `bn dep add` the ordering, producing a backlog.
3. The orchestrator, pointed at the same Postgres (postgres-tracker adapter,
   `tracker.kind: postgres`), polls `FetchCandidates` → dispatches the ready,
   unblocked issues → agents work them → `Close` writes back to Postgres.
4. `bn ready`/`bn list`/`bn show` give humans/agents the same visibility `bd` did.

This closes the loop the postgres-tracker plan needed: **`bn` is the authoring
front door, the adapter is the orchestrator's read/close path, one Postgres.**

## Non-goals / cautions

- Don't shadow `bd` on PATH (decision: distinct `bn`). The skills opt in.
- Don't auto-migrate beans. The greenfield boundary is deliberate.
- Memory in the skills: if a new-project skill uses `bd remember`, it likewise
  becomes `$TRACKER remember` (05). Keep it consistent with the issue commands.
- The skill edits are **out-of-repo** (dotfiles); sequence them after `bn` is
  buildable and tested (08), and keep `bn` fully usable without the skill change.
