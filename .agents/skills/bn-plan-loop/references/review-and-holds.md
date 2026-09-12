# Review, human holds, and delivery

## Branch and evidence

Fetch the remote default branch and capture its OID without checking out or updating the local default branch. From that OID create `bn-plan/<plan-id>/<sequence>-<node-slug>`, unless authoritative helper, issue, and Git evidence recovers the exact branch. Add a contract note before editing. Implement mapped scope, run reversible checks, stage explicit paths, commit without bypassing hooks, and push normally. Before any hold, require local head equals remote feature head.

Evidence notes use `bn-plan-loop:v1 phase=<phase> plan=<id> node=<id> head=<full-sha> base=<full-oid> branch=<name> ...`. Store digests rather than secret-bearing logs or full review bodies.

## Independent review gate

Read installed `review`, `fix-review`, and `thermo-nuclear-code-quality-review` skill contracts. Run two independent reviewer subagents against the immutable base/head diff, then the maintainability review. Bind artifacts to issue/node/base/head. Apply Critical and Important findings and actionable maintainability findings within scope, revalidate, commit and push fixes, and rerun any review invalidated by a changed head or base. Note final review/validation digests, set `ready_for_review`, and verify with fresh JSON.

## Human approval and validation

Before waiting, run `monitor_bn.py snapshot`, persist its fingerprint, release the executor lease, and start exactly one yielded `monitor` for that fingerprint. It uses only issue, plan-status, hub-status, and remote-ref reads. It detects a transition before startup, refuses overlap, and tolerates two transient failures before failing on the third.

Feedback moves work to `in_progress` (if the human has not already done so), reconciles human commits, and repeats implementation, validation, and review.

Approval is exactly one human command:

```text
bn update <issue-id> --status ready_for_validation --note "bn-plan-loop:v1 approve plan=<plan-id> node=<node-id> head=<full-reviewed-sha>"
```

The executor never writes an approval marker. Validate the fresh issue JSON with `bn_plan_loop.py verify-approval`: it requires unique adjacent status/note log entries with matching actor/repository/SHA/branch and reviewed head and rejects executor authorship. Then use read-only hub history to prove one hub commit introduced both entries. Timestamps may straddle one second only under that atomic commit proof. Reject status-only, stale, duplicate, conflicting, or multi-commit evidence.

At `ready_for_validation`, fetch the feature branch and require its remote head equals the approved/reviewed head. Run the full issue validation and applicable repository integration gate, note evidence, enter `ready_for_merge`, and verify. On failure return to `in_progress` and repeat review.

At `ready_for_merge`, tell the human to merge by fast-forward or merge commit so the reviewed head remains an ancestor, and then close the issue. Never merge or close for them. Terminal status triggers—not replaces—fresh ancestry verification. After delivery, note the containing base OID and retain the feature branch for repository policy.

Unknown/custom statuses, premature closure, divergence, edited historical feedback, or contradictory notes require human disposition. Silence and elapsed time are never approval.
