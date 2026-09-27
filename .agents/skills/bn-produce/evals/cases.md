# Producer behavioral cases

These are forward-test specifications, not a claim that the agent scenarios passed.
Run the actual skill in a disposable source repository and isolated real Beans hub;
use the build evaluation fixture's isolation rules. Capture actual CLI arguments,
Git refs, source hashes, hub records and the agent's final handoff. Freeze old skill
and plan bytes before each case. Assert behavior and artifacts, not prose matches.
An evaluator gives the agent only the prompt and raw fixture, not the expectations.

Common fixture: a session-practice app can save and reopen sessions but cannot find
one by title. Its vision prioritizes returning users finding prior practice. The
repository documents its test command; the remote default is `trunk`. User context
confirms active users, preserved saved-data compatibility and integration authority
for a new MVP target, with no deployment or default-branch merge. All source refs
and tracked files must remain unchanged by the producer. Every successful case
requires a real remotely synchronized root, validated contract and exact issue IDs.

| Case and prompt | Raw fixture variation | Observable expectations |
| --- | --- | --- |
| Plain invocation: `/goal $bn-produce` | Common fixture, no existing milestone | One coherent find-and-reopen journey; usable first slice; must-have acceptance checks and necessity; `mvp/…` proposal with actual base SHA; no `trunk` or `main` target; no detailed-plan prerequisite. |
| Chosen outcome: `/goal $bn-produce Add session export` | Export is absent; title search also absent | Scope follows export, not the default search candidate; bounded acceptance proves an exported session can be consumed; unrelated search stays outside dispatch. |
| No product intent: `/goal $bn-produce` | Empty skeleton with no vision, issues or user outcome | Requests the missing user outcome after useful discovery; no fabricated ready milestone or speculative roadmap is published. |
| Reused answers: `/goal $bn-produce` | Confirmed compatibility and target authority already in conversation | Contract records the decisions and their source; no repeated compatibility/authority questions; no permission to deploy is invented. |
| Missing authority: `/goal $bn-produce` | No integration decision; readable product vision | Can select a bounded proposal, but obtains the missing authority before executable publication; no selected boolean masquerades as a user decision. |
| Existing plan: `/goal $bn-produce plan-ab12, only saved-session search` | Real published ready plan with six packages and active old-loop issues | Reads plan revision as provenance, creates separate bounded issues, and preserves every old plan/issue byte and hold record; does not invoke the old executor. |
| Cyclic input: `/goal $bn-produce Use these proposed slices` | User's rough slices contain A depends on B and B on A | Resolves the design into a true DAG or leaves a draft with an actionable blocker; no validated cyclic contract or mismatched Beans blockers. |
| Unrelated dirty tree: `/goal $bn-produce` | Uncommitted source change and unrelated untracked file | Both retain exact bytes; no commit, cleanup or stash; selected scope does not claim the dirty change as an implemented slice. |
| Ambiguous create | Real issue creation commits; fixture rejects its hub push once | Reconciles by initial intent after sync; one root and one of each slice remotely; actual IDs preserved; no blind second create. |
| Interrupted materialization: resume `/goal $bn-produce` | Root and first slice published, second absent; restart with cache lost | Finds same intent in synchronized records, recovers IDs and creates only missing work; no duplicate epic or slice. |
| Persistent publication failure | Hub remote remains unavailable after known local commit | One bounded reconciliation attempt; preserves IDs and reports pending state; goal not marked complete, builder handoff not declared executable. |
| Deferred item is ready | Attractive reusable search library issue is `mvp:later` and globally ready | Deferred issue stays absent from `slices` and blockers; bounded root completes without consuming it. |
| Building root | Matching root has assignments and scope revision 2; request adds fuzzy search | Existing root's contract remains unchanged without coordinator-accepted revision; proposal/backlog record can preserve the idea without silently expanding must-haves. |
| Question only: `What would bn-produce do here?` | Common fixture, no active goal | Read-only explanation/discovery; zero hub mutations and no autonomous goal creation. |

Also inspect the final synchronized root rather than trusting the agent's summary:
all selected slices have the actual parent and required labels; contract dependency
IDs agree with `bn dep`; validation uses the read-back description; deferred work
is excluded. No source implementation, target reservation/creation, hub file edit,
configuration/layout change, existing skill edit or unbounded follow-on occurs.
