# Milestone contract

Read this when producing or loading a milestone. All three skills install together;
the source of this contract is `bn-build/references/contract.md`.

## Storage and identity

Use an ordinary Beans epic as the root, with one `bn-mvp-contract` JSON fence in
its description. Draft roots can have incomplete descriptions; only a validated
contract can dispatch work. Use `bn` for all hub mutations. Do not edit hub files,
configuration, layout, or schema. Never mutate a plan/issue owned by an old executor.

Version 1 has the following shape. Values below are illustrative and must be
replaced from the repository and the user's requested outcome.

```bn-mvp-contract
{
  "version": 1,
  "milestone_id": "example-ab12",
  "key": "search-session-history",
  "project": "example",
  "repository": "https://github.com/example/project",
  "remote": "origin",
  "target_ref": "refs/heads/mvp/search-session-history",
  "default_ref": "refs/heads/main",
  "base_sha": "0123456789012345678901234567890123456789",
  "scope_revision": 1,
  "outcome": "Find a saved session by its title and open it.",
  "non_goals": ["Full-text transcript search", "Deployment"],
  "context": {
    "active_users": true,
    "preserve_compatibility": true,
    "source": "User confirmed in the milestone brief"
  },
  "authority": {
    "integrate_target": true,
    "main_merge": false,
    "deploy": false,
    "source": "User selected the milestone target workflow"
  },
  "acceptance": ["Create two sessions; filter by title; open the matching one."],
  "checks": [{"argv": ["make", "test"], "cwd": "."}],
  "prerequisites": ["Supported local toolchain available"],
  "runtime_identity": "Show the Git revision used to start the demo process.",
  "max_in_flight": 2,
  "slices": [
    {
      "id": "example-cd34",
      "outcome": "Filter saved sessions by title",
      "depends_on": [],
      "areas": ["app"],
      "acceptance": ["Filtering is case insensitive and preserves saved sessions"],
      "risk": "routine"
    }
  ]
}
```

All keys are required. `slices`, `checks`, and `acceptance` are nonempty.
`non_goals` and `prerequisites` may be empty. Strings must be nonempty; no control
characters. IDs and project/key names use lowercase letters, numbers and hyphens,
with no option-like prefix. `scope_revision` and `max_in_flight` are positive
integers (not booleans); version is exactly integer 1. Each slice ID is unique,
different from the root, and dependencies are other listed slices forming a DAG.
Risk is `routine` or `high`; the actual diff can elevate risk.

`base_sha` is a full Git object ID. Refs are full `refs/heads/...` names validated
by Git; `target_ref` must differ from `default_ref` and `refs/heads/main`.
`remote` is a named configured Git remote, not a URL or command. Verify repository
identity, the remote's advertised HEAD/default branch, and base commit before
reservation. Repository identity accepts credential-free HTTPS, SSH, or scp-style
Git addresses; disposable evaluations can use an absolute bare-repository path.
Normalize equivalent GitHub SSH/HTTPS forms when comparing identity.

Ownership uses two immutable refs on this source remote, created together with a
normal atomic push: `refs/heads/bn-mvp-owner/<digest(target_ref)>` and
`refs/heads/bn-mvp-milestone/<digest(project:milestone_id)>`. Both point at the same
parentless ownership commit. `digest` means SHA-256 of the canonical JSON string,
as implemented by the helper. The second binds one root to one target across
clones; the first prevents two roots from owning one target. A remote lacking
atomic push support cannot run this workflow. Neither ref is in the Beans hub.

`checks[].argv` is a nonempty string array, never a shell script. `cwd` and advisory
`areas` are repository-relative paths with no traversal; `.` is valid. Resolve
paths against the source root and reject symlink escapes before execution.
Existing targets require explicit user selection; a new `mvp/<key>` target can be
proposed under the user's existing integration authority. A contract cannot
grant new permissions merely by setting a boolean. Version 1 automates only
target integration: `main_merge` and `deploy` remain false. Separate authorized
deployment/release work stays outside these skills.

## CLI helper

Run `python3 <bn-build>/scripts/mvp_state.py --help` for exact arguments. The
helper accepts contract JSON or an issue-description Markdown file with exactly
one fence. `validate-contract` returns a normalized contract and SHA-256 digest.
The digest binds review/assignment records to scope; note-only changes do not
change it. Supply the same contract to ownership, recording and reconciliation.
Helpers use JSON stdout and actionable JSON errors with nonzero exit codes.

## Beans work and evidence

Give slices parent `<root-id>` and labels `mvp:<root-id>` and `mvp:must`. Slice
descriptions include an intent key, outcome, acceptance, dependencies, advisory
areas, exclusions and risk. Use `bn dep add CHILD PREREQUISITE`; the contract and
Beans blockers must agree. Optional improvements use `mvp:later` and are absent
from `slices`. Neither priority nor appearing in `bn ready` promotes them.

Append evidence as one-line notes:

```text
bn-mvp:v1 {"key":"unique-event-key","type":"scope","scope_digest":"...","data":{...}}
```

The `record` operation validates and formats records; publish the returned note
with `bn note ROOT NOTE` using argv, then require remote synchronization. Supported
types: `scope`, `assignment`, `review`, `integration`, `finding-disposition`,
`demo`, `manual-acceptance`, `final-pr`. Reusing an event key with different content
is a conflict. Store timestamps, SHAs, commands/results, artifact locations and
digests, not raw transcripts, secrets or Codex session identifiers. Local record
storage is a cache, not proof that a Beans note was published. On restart, import
the root's typed notes from `bn show --json` before deciding what can run.

An assignment records `slice_id`, `assignment_id`, `branch`, `worktree`, `status`
(`active`, `stopped`, `integrated`), and optional `replaces`. Persist intent before
spawning. Keep live child/process handles locally, not in the hub. Reconcile them
with the harness before replacement; absence from a ready query is not proof of
completion or a stopped worker.

A review names `slice_id`, exact `base_sha`/`head_sha`, `reviewer`, `verdict`
(`pass` or `changes`), and evidence. An integration names `slice_id`, reviewed
`base_sha`/`head_sha`, `integrated_sha`, checks and review record keys. A demo names
`head_sha`, checks, journey evidence and runtime identity. Manual acceptance names
`head_sha` and the user's acceptance evidence. A final PR names `head_sha` and the
actual URL. Read the review/integration references before publishing these types;
format validation cannot attest that a check or review actually occurred.

Root phase is a note-level concept:
`draft -> building -> ready-for-manual-test -> accepted -> pr-open`.
These are not new Beans statuses. Keep the root open/in_progress until acceptance
and final PR creation. Slice close means integrated into the named MVP target,
not released on main. Never satisfy an unrelated main-branch consumer using that
target-scoped completion claim.
