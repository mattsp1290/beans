# Lifecycle, preflight, and reconciliation

## Invocation and identity

Resolve the repository with `git rev-parse --show-toplevel`, the private Git directory with `git rev-parse --git-dir`, and the shared directory with `git rev-parse --git-common-dir`. Resolve project and hub from `bn status --json`, preserving any user-supplied `--project`, `--hub`, and `--actor` in every call. Never switch executables during a run.

Plan identity is the plan ID plus its semantic digest. Published `updated` is a concurrency revision, not semantic identity. Branch identity is plan ID, node ID, base OID, branch name, reviewed head, and remote head. Persist only local lease/checkout identity, observed evidence, and mutation intents; Beans and Git remain authoritative.

## Authority and CLI preflight

Read repository instructions, run `bn prime`, and inspect worktree, remotes, default-branch metadata, CI, and build commands. Verify help for `plan show/status/get/validate/put/link`, `create`, `show`, `list`, `ready`, `update`, `note`, `close`, `dep add/remove/cycles`, `status`, and `sync`. Prefer the installed binary; in a Beans source checkout only, build `bin/bn` if required and then use it consistently.

Fetch the code remote read-only. Resolve the default branch from repository guidance, then `refs/remotes/<remote>/HEAD`; report it before mutation. Reject dirty worktrees, missing remotes, existing incompatible branches, squash/rebase-only delivery policies, another actor's claim, or evidence of a separate executor.

Read plan show/status and retrieve a fresh bundle. Require lifecycle `ready`, meaningful overview/packages/handoff, exactly one valid application-context block and execution map, complete acceptance evidence, no blocking decision, one-to-one graph coverage, acyclic matching prerequisites, and permitted bindings. Reference-only nodes must not resolve to issues anywhere in the hub.

Load workflow through the bundled Go oracle, which calls the same `issue.LoadWorkflow` implementation as `bn`. `BN_CONFIG` is total precedence and its extension must be `.toml`, `.yaml`, or `.yml`; otherwise Beans merges defaults, hub, then project per nonempty key. Reject unknown workflow keys and invalid structure. `ready_for_review`, `ready_for_validation`, and `ready_for_merge` must exist and be neither active nor terminal; `in_progress` must exist and a terminal state must be available. Stop if the repository-owned oracle cannot be built; do not substitute an approximate parser.

## Local state and fencing

State lives under `<git-common-dir>/bn-plan-loop/runs/<stable-plan-id>/`. Start `bn_plan_loop.py lease-session` as a yielded process for each active phase. It holds one OS lock until an explicit JSONL `release`, increments a fencing token, and records process/start identity. Bind that owner/token and the checkout incarnation using the session's `claim-checkout` operation; after a hold, the new lock owner may rebind only that same plan/incarnation. Reserve session `run` for read-only commands and reversible validation. Route Git mutations through `git` requests and Beans mutations through `bn` requests; both transaction specs persist a typed plan/node intent, the resolved executable identity, exact recovery argv, evidence format, and a nonempty expectation. They execute argv without a shell, verify authoritative evidence, and clear the intent only on a match. Create operations must persist an output-independent, archived-inclusive plan-label query and exact node marker before mutation; `{mutation_stdout}` may aid immediate verification but is not sufficient create recovery. `reconcile-bn` accepts only the matching intent identity, requires the original executable (and literal Git for Git recovery), and derives commands from the persisted contract, so callers cannot substitute unrelated success evidence. `fenced-bn` is recovery-only when no active session exists. A pending intent blocks a new session or mutation: abort the session while preserving evidence, then run the locked reconciliation matching that persisted contract, or stop when evidence is ambiguous. A new owner must prove the old process is absent and reconcile any preserved intent; age never proves staleness. Changed checkout incarnation or ambiguous ownership requires human disposition. Release the session before a human wait, but use session `release-checkout` only after verified plan completion or explicit human-authorized abandonment; never release it merely to bypass a conflict.

A mutation intent stores exact mutation argv and moves durably from `prepared` to `started` with a gated child PID. The child cannot execute until that record is durable, and it durably records `launched` before `exec`. Under the executor lock, reconciliation may clear a `prepared` intent or a `started` intent whose gated child is gone because no mutation could have launched; it must never replay either implicitly. A `launched` intent remains ambiguous until its persisted authoritative postcondition is proven.

## Legal phases

```text
preflight -> materializing -> runnable -> in_progress -> ready_for_review
ready_for_review -> in_progress | ready_for_validation
ready_for_validation -> in_progress | ready_for_merge
ready_for_merge -> terminal-unverified -> delivered
all delivered -> plan-completing -> complete
```

The human owns approval, merge, and issue closure. The executor owns claim, review publication, final validation, and plan lifecycle publication. Unknown statuses and transitions outside this graph stop.

## Resume reconciliation

Refresh Git refs and read `bn plan show/status`, `bn show`, and `bn status` before deciding:

| Evidence | Continue |
| --- | --- |
| `in_progress`, owned branch, clean linear head | implementation or feedback |
| `ready_for_review` | read-only wait for note/status |
| `ready_for_validation`, atomic approval matches reviewed remote head | final validation |
| `ready_for_merge` | read-only wait for merge and terminal status |
| terminal and reviewed head ancestor of refreshed base | record delivered base and continue |
| terminal but reviewed head absent from base | request merge or reopen; do not advance |
| merged but nonterminal | give exact `bn close <id> -r "..."`; keep waiting |
| local/remote divergence | stop; never discard or force-push |
| ambiguous hub mutation | `bn sync`, fresh reads, reconcile before retry |
| semantic digest changed | re-ingest; started/held scope change needs disposition |

For unstarted compatible drift, update only the skill-owned issue-description block, preserve human text, and note the new package digest. Never create a new run identity merely because the plan changed.

## Plan completion

Fresh status must show lifecycle `ready`, execution `done`, mismatch true, zero runnable/in-progress/held/blocked/missing, and terminal issue bindings. Revalidate every reviewed head against refreshed base and run the handoff integration gate. Retrieve into a new temp child, change only manifest status to `complete`, validate and put. On one stale revision, retrieve/reconcile/retry once. Verify put reports pushed, hub ahead is zero, fresh show/status says lifecycle complete, execution done, mismatch false. Never forge `updated` or directly edit the hub.

After all completion evidence has been persisted and the active goal is ready to complete, release the matching checkout-incarnation claim. Keep run state and feature branches as audit/recovery evidence; cleanup follows repository policy.
