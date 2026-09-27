# Ownership, evidence, and recovery

The helper is a narrow standard-library Python CLI. It uses Git and the installed
Beans CLI as argv arrays; it never evaluates a command string or edits the hub.
Python 3.9+ and a POSIX host with `flock` are required. All three skills share the
[contract](contract.md). Run `python3 scripts/mvp_state.py --help` relative to
`bn-build`, or use the absolute script path from the source checkout.

## Exclusive hub checkout (all three roles)

Before any Beans mutation, give this role an exclusively owned hub clone. Never
run automated mutations or barriers against the user's shared default hub
checkout. Read `bn status --json` without mutation to discover the configured hub
remote, then use `bn init REMOTE` with the isolated environment below to clone it
into a new, empty role-specific directory under the
source Git common directory, for example
`bn-mvp/<key>/hubs/producer`, `hubs/builder`, or `hubs/audit-<invocation-id>`.
Set `BEANS_HUB` to that absolute path, `BEANS_HOME` to a separate role-specific
state/configuration directory, and `BEANS_PROJECT` explicitly for **every**
Beans/helper command in the invocation. `bn init` initializes the role-local configuration at the same time; never point it
at an unknown/empty remote. Preserve the already verified existing hub branch
and required identity without copying credentials. Clear inherited `BN_CONFIG`
unless an explicitly inspected consumer workflow file is intended. A shared
`BEANS_HOME` also shares lock/throttle/operation-journal state even when hubs differ;
it is not isolated. Do not alter the user's global config,
the backing repository's layout, or its workflow configuration. Resume only the
same role's recorded clone after checking its identity and clean/pending state;
never reuse another live role's checkout. Source workers do not mutate this hub.

The helper requires explicit `BEANS_HOME`/`BEANS_HUB` and rejects the standard
shared `$HOME/.beans` paths. It cannot discover arbitrary other writers.
This is a required exclusive-writer assumption, not an atomic feature of Beans.
The helper's cleanliness check and `bn sync` are separate transactions: Beans can
auto-commit hand edits introduced between them. Keep all other sessions, editors
and automation out of this dedicated checkout. Unexpected edits block work and
must be preserved. Arbitrary concurrent writes to that checkout are unsupported;
post-sync checks cannot undo a published hand edit. Separate hub clones safely
exchange published records through the existing remote and explicit barriers.
Producer/auditor establish the same cleanliness/pending/fetch/sync/readback
procedure directly through Git and Beans; they never borrow the builder token.

## Invocation and lock lifetime

Every command accepts `--repo SOURCE_ROOT --contract FILE`. The contract file is
JSON or Markdown containing exactly one `bn-mvp-contract` fence. The source root
must be the actual checkout/worktree root. Contract check paths and code areas
are relative to it; paths escaping through traversal or symlinks are rejected.

1. `validate-contract` returns `{contract, scope_digest}` without state mutation.
2. Generate a random caller token (at least 16 characters). Start
   `hold-lock --token TOKEN` as a persistent, foreground harness process. Wait for
   its flushed `{locked: true, pid, state_dir}` line and retain the process handle.
   Keep this process alive for the entire active coordinator invocation. It only
   holds a lock; it does not execute commands, run workers, or proxy a shell.
3. `reserve-target --token TOKEN` reserves ownership. If the user explicitly
   selected an existing non-default target, pass `--allow-existing-target`; its
   initial remote SHA must equal `base_sha`. This flag grants no authority by
   itself and does not bypass identity/default-branch validation.
4. Use `check-owner --token TOKEN` before dispatch, target publication, and after
   continuation. Stop mutations if the keeper dies or either reservation changes.
5. Terminate the keeper when this coordinator invocation ends. Resume with a new
   keeper/token and the existing local nonce. Preserve the nonce cache.

All commands except validation and the keeper require the live keeper and its
matching token. Helpers also serialize short local operations. Other worktrees
sharing the Git common directory contend on the same coordinator lock. Do not
pass the token to children or store it in Beans. A new keeper cannot prove an old
child stopped: consult the harness separately before replacing assignments.

State lives under `<git-common-dir>/bn-mvp/<contract.key>/`. `owner.json` stores
the nonce, expected reservation OID, and immutable repository/project/root/target
identity; `records.json` is an evidence cache; `publication.json` records the keys
observed at the latest authoritative reconciliation; `keeper.json` stores a PID and token
digest. State writes use private temporary files, fsync, and atomic rename. State
symlinks are rejected. These files are local recovery aids, never publication
proof. Do not copy their contents to notes or commit them.

## Remote exclusion

The parentless ownership commit contains only `version`, `target_ref`,
`milestone_id`, and a random nonce. The nonce and then the expected OID are saved
locally before the first push. A normal atomic push creates two refs pointing to
the same commit:

- `refs/heads/bn-mvp-owner/<digest(target_ref)>` reserves the source target.
- `refs/heads/bn-mvp-milestone/<digest(project + ":" + milestone_id)>` binds one
  root to one target across clones.

Here `digest` is SHA-256 of the canonical JSON string, as used by the helper.
Concurrent owners create unrelated commits; exactly one atomic push can win.
Servers without atomic push support fail closed. The helper verifies both refs
after push, including a failed response that may have followed a successful
remote update. It never creates or updates the source target itself.

Before reservation/check, configured fetch and push URLs must match the contract
identity. GitHub SSH/HTTPS equivalents normalize together. The advertised remote
HEAD must name the contract default branch. Neither `main`, that default, nor
the helper's ownership namespaces can be targets. The base must be a real commit.
Both remote reservation OIDs must equal the locally persisted expected OID to
resume. A lost local cache cannot recover ownership merely by reading its nonce
from the remote commit. There is no timeout or automatic takeover.

Manual transfer is an administrative action outside these skills: the user first
stops the old coordinator and all its children, preserves dirty worktrees and
unpublished branches, synchronizes/inspects Beans, and records the exact observed
old OIDs. An administrator can then conditionally replace both reservation refs
atomically, guarded by those exact old OIDs, with a new owner's parentless commit.
Only after readback verifies both new OIDs may the new owner start. Never change
the source target or default branch during transfer. The helper intentionally
does not provide a transfer, force-push, expiry, or deletion command. Keep owner
refs after completion as audit records. Old-owner checks fail after transfer.

## Typed evidence API

`record --token TOKEN --record FILE` accepts exactly the envelope keys `key`,
`type`, `scope_digest`, and `data`. It returns `{record, note, idempotent,
published: false}`. The returned one-line `note` is ready for `bn note ROOT NOTE`
using argv. Exact repetition is idempotent; a reused key with different content
fails with `conflict`. A successful local write is not proof of a published note.

Required `data` fields by type:

| Type | Fields |
| --- | --- |
| `scope` | `scope_revision` equal to the contract |
| `assignment` | `slice_id`, `assignment_id`, full `branch`, `worktree`, `status`; optional `replaces` |
| `review` | `slice_id`, `base_sha`, `head_sha`, `reviewer`, `verdict`, `evidence` string array |
| `integration` | `slice_id`, `base_sha`, `head_sha`, `integrated_sha`, `checks`, `reviews` record-key array |
| `finding-disposition` | `fingerprint`, `disposition`, `evidence` string |
| `demo` | `head_sha`, `checks`, `journey` string array, `runtime_identity` string |
| `manual-acceptance` | `head_sha`, `evidence` string |
| `final-pr` | `head_sha`, `url` with actual credential-free HTTPS PR URL |

Additional data may include timestamps, artifact locations/digests, toolchain
identity and limitations. Never include credentials, raw transcripts or Codex
session identifiers. A check result is `{argv: [...], cwd: ".", exit_code: 0}`
with optional supporting metadata. Integration can use relevant checks; the demo
must cover every command/cwd pair in the contract's full repository gate and have
published, target-proved integration records for every selected slice. Run an
authoritative reconciliation to establish publication before recording a demo.

Assignment status is `active`, `stopped`, or `integrated`. Its ID, slice, branch,
and worktree are immutable. Record intent before spawning; publish it and verify
synchronization. Only one active assignment per slice is allowed, bounded by
`max_in_flight`. A replacement names a stopped assignment for the same slice.
Record stopped only after reconciling live harness handles; the helper cannot
verify arbitrary worker/process activity. Worktrees may be absolute paths to
sibling checkouts; they cannot traverse `..` or use symlink paths.

Reviews use `pass`/`changes`, bind exact base/head/scope, and identify an independent
reviewer. Integration needs a passing review, or two distinct reviewer identities
for a high-risk slice (also supported by integration `data.risk: "high"`). The
helper verifies the reviewed head and integration commit are ancestors of the
exact fetched remote target. Integration is either the reviewed head (a fast-forward
from the reviewed base) or a direct two-parent merge whose first parent is that
base and second parent is that head. The integration SHA must also be on the
target's first-parent history; naming a merged side-branch head cannot disguise
an unreviewed merge as a fast-forward. A merge atop an advanced, unreviewed target
cannot become proved integration merely because all old commits remain ancestors.
It does not execute tests or attest reviewer independence: the harness and skill
must provide real evidence. A demo/acceptance/PR for an old target SHA is rejected;
a final PR record also requires published acceptance for that exact head and
scope, observed by authoritative reconciliation.

## Authoritative barrier and reconciliation

Use `reconcile --token TOKEN --hub HUB --bn BN_EXECUTABLE` for scheduling/recovery.
It checks the supplied hub equals Beans' resolved hub/project, refuses dirty or
untracked hub files, fetches the hub, rejects unidentified local pending commits,
runs `bn sync --json`, requires `ahead: 0` and `behind: 0`, verifies clean local
HEAD equals the remote branch, then reads the root with `bn show --json`. The
synchronized root contract must match the supplied contract. It records the
observed `hub_head`; ordinary throttled reads and timestamps are not freshness.

`sync-barrier --token TOKEN --hub HUB --bn BN_EXECUTABLE` performs that same barrier
and returns `{hub_head, issue, freshness: "synchronized"}` without scheduling.
Scope request/issue reads to this observed revision and check again before a
readiness or PR decision. Another writer can publish afterward; no snapshot can
prove the absence of future findings. Network failure/lock contention blocks the
decision. Inspect and retry once; do not hot poll or ignore `pushed: false`.

For an identified failed hub push, `--allow-pending FILE` accepts a JSON array of
`{sha, intent_key, milestone_id}` after inspection. Every SHA must name an exact
pending commit attributable to this root; changed paths are restricted to the
root and currently selected slice issue files. Other files/configuration/request
commits require separate manual recovery. The flag never allows dirty files or
silently blesses unrelated hand edits. Preserve evidence before recovery.

Reconciliation returns `scope_digest`, `target_sha`, `hub_head`, `freshness`,
`integrated`, `assignments`, `dispatchable`, `blocked`, `stale_records`, and
`pending_records`. It imports typed records from the root log and preserves local
intents, including assigned issues absent from `bn ready`. Provide
`--live-assignments FILE` with the harness's JSON array of live assignment IDs;
otherwise recovered assignments require inspection before replacement. Ongoing
assignments reserve their slot even under a changed scope. Dependencies unlock
only from current-scope, published integration evidence proved on the target;
closed issue status alone is insufficient. A pushed candidate with only a local
note remains pending: verify its proof, publish the existing record, then close
the slice without remerging it.

`reconcile --notes FILE` accepts an offline `bn show` JSON object or array of
envelopes. It diagnoses/reimports evidence but returns `freshness: "unverified"`
and no dispatchable work. The helper's dispatchable list is only a candidate
filter: the coordinator must also inspect live Beans dependencies, old-executor
ownership, incoming requests, assignments and code-area overlap before dispatch.

All contract changes alter the digest; note-only edits outside its fence do not.
Old-scope review/integration evidence cannot unlock work, but assignments remain
visible for recovery. Demo, manual acceptance and PR records also become stale
when the target SHA changes. Preserve all stale records as provenance; obtain
fresh affected checks and review/acceptance instead of rewriting them.

Errors are JSON `{error, message}` with nonzero exit status. Stable categories are
`invalid`, `unsafe-path`, `identity`, `ownership`, `locked`, `conflict`, `stale`,
`unsynchronized`, and `external`. No failure authorizes deleting user work,
stealing ownership, force-pushing a source target, or mutating the default branch.
