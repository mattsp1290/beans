---
name: bn-audit
description: Audit one immutable Beans MVP milestone snapshot, reproduce its acceptance journey, and publish deduplicatable findings through Beans requests. Use for optional milestone scrutiny, not implementation, ongoing branch monitoring, or manual product acceptance.
---

# Beans Snapshot Auditor

Assess one snapshot and publish one scoped report. Publication requires an active
explicitly requested goal, such as `/goal $bn-audit <milestone-issue-id>`; ordinary
questions permit explanation and read-only discovery only. Read the shared
[contract](../bn-build/references/contract.md) and
[finding and publication procedure](references/findings.md). All three skills
install together; missing sibling resources block the workflow. Inspect helper
and `bn` runtime `--help` rather than assuming a matching CLI version.

1. Resolve the consumer repository, instructions, project and milestone. Validate
   the contract, establish the shared synchronization barrier, and freeze its
   scope revision/digest and the fetched remote target's exact SHA. Use an exclusive
   auditor hub clone for mutations/barriers, as the shared recovery procedure
   requires; never mutate the shared default hub checkout. Record the
   contract's starting base SHA for the assembled diff. Reject invalid or missing
   identity; do not silently audit the current local branch instead.
2. Inspect a separate detached checkout of that SHA, preserving the builder's
   worktree. Exercise the accepted journey and applicable checks with disposable
   data and identify the running revision. Source remains read-only; generated
   artifacts belong in disposable locations. Run only read-only checks unless the
   contract explicitly permits the disposable fixture's setup and teardown.
   Production mutation, deployment, source repair and target writes are excluded.
3. Delegate distinct surfaces when useful and available: an independent journey
   reviewer and correctness/maintainability reviewer may run concurrently, each
   given the same scope and immutable base/head. A small audit needs one reviewer.
   Reproduce findings or give concrete failure reasoning; do not manufacture
   independent review when delegation is unavailable. The auditor owns the report.
4. Classify findings as must-fix, deferred or unverified, with stable fingerprints
   and snapshot provenance. Publish and verify a milestone-linked Beans request,
   including a no-findings report when supported by completed checks. Follow the
   reference's bounded reconciliation for duplicate or failed publication.
5. Return the actual request ID, evaluated SHA, coverage, blockers and relevant
   findings. Complete the audit goal only after this fixed snapshot report is
   verified published. Another snapshot requires another finite invocation;
   never poll the moving target indefinitely.

The auditor does not claim implementation ownership, close slices, edit the scope,
create repair issues or grant acceptance. The coordinator revalidates findings on
its current target and owns repair/disposition. If it has stopped, include
`/goal $bn-build <actual-milestone-id>` explicitly: publishing a request does not
wake a goal. A changed target is not approved by this audit, and unresolved or
unrun required acceptance is never reported as passing.
