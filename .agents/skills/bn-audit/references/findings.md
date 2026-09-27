# Findings for a fixed snapshot

## Evidence and identity

Keep the initial remote target SHA fixed even if the branch moves during checks.
Record the repository, milestone ID, scope revision/digest, starting base SHA,
evaluated head SHA, runtime identity and observed hub revision. Give reviewers
the accepted journey, relevant compatibility constraints, diff and evidence;
reviewers do not inherit authority to repair, publish, or use production data.

For each finding record the affected area, severity, normalized failure invariant,
expected and actual behavior, reproduction or concrete failure reasoning, evidence
paths/digests, proposed acceptance check, classification and stable fingerprint.
Do not store secrets or session transcripts. Severity alone does not decide scope:

- **Must-fix:** violates the accepted journey, data/security/compatibility
  constraints, or an applicable repository gate. State the violated requirement.
- **Deferred:** a beneficial improvement outside those requirements. Give its
  benefit and a concrete revisit trigger; style preference alone is not a blocker.
- **Unverified:** evidence is insufficient or a prerequisite/check is unavailable.
  Name what is missing and how to establish it. If required acceptance cannot be
  proven, this remains a delivery blocker rather than a passing or clean audit.

Use a deterministic fingerprint such as SHA-256 of canonical JSON containing
`repository`, `milestone_id`, `area`, and `failure_invariant`. Canonicalize identity,
whitespace and ordering; describe the invariant semantically rather than embedding
line numbers or transient stack values. Exclude head/base SHA, scope revision,
timestamps, request IDs and reviewer identity from the fingerprint; keep them as
provenance instead. Reuse an existing matching invariant when reobserved. Two
defects in one file differ by invariant: “empty query hides saved sessions” and
“opening a result selects a different session” must have different fingerprints.
Fingerprint equality suggests a duplicate; verify semantic equivalence before
consolidation. Do not collapse unrelated failures under a broad “search broken.”

## Publish and reconcile

Read the shared [state and recovery procedure](../../bn-build/references/state-and-recovery.md).
Verify runtime help for request creation/list/show/update, status and sync, including
the project's override. All report mutations go through `bn`; never directly edit
the hub or change its configuration/layout. Source and target refs remain unchanged.

Before deduplication or mutation reconciliation, inspect hub cleanliness and pending
commits and establish the shared sync barrier. Do not publish unrelated hand edits
or unidentified pending commits. Require clean state, sync success with zero ahead
and behind, and record the read-only observed hub HEAD before scoped reads. Ordinary
throttled reads do not prove freshness; failure blocks publication, not just a warning.

Derive an audit intent from repository/project, milestone, scope digest and snapshot
SHA; include it in the initial body. Inspect all relevant project requests, including
terminal requests, by this key and milestone link before creating anything. Prepare
the report in a temporary body file and use the installed syntax for
`bn request create <title> --body-file <path> --issue <milestone-id> --json`.
Include completed checks and their outcomes, findings grouped by classification,
missing coverage, provenance and the explicit limits of snapshot approval.

Reuse an unchanged existing report for the same intent. If a repeated audit yields
new evidence on that snapshot, read the latest request and preserve prior evidence
and concurrent additions while updating it through the CLI; do not reopen terminal
lifecycle or overwrite another writer's content by force. Reconcile contested
content once, then preserve the new report locally and report the blocker. A report
with zero findings still lists checks actually run and coverage not established.

After ambiguous create/update or `pushed: false`, inspect by intent before retrying;
the local commit may already exist. Sync known pending work once, then verify the
actual request body, milestone link, remote synchronization and clean status. Stop
on persistent failure and report local versus remote state accurately. No duplicate
repair tasks are created by this auditor. Concurrent duplicate requests can exist
because the CLI does not guarantee atomic uniqueness; identify their shared intent
and fingerprints for coordinator deduplication, without deleting evidence.

## Builder handoff

Only the coordinator adds must-have repairs to an active milestone. It reproduces
each finding on the current target or records evidence that it is superseded, then
records accepted/rejected/deferred disposition and creates at most one repair per
stable fingerprint. Old snapshot evidence is neither automatically actionable nor
automatically obsolete. Deferred issues, if created by the coordinator, retain the
milestone link and `mvp:later` label and stay outside the must-have list.

Publishing does not signal or resume a stopped goal. Give the explicit
`/goal $bn-build <actual-id>` action when the builder is stopped. If a final PR is
already open, an accepted must-fix finding makes it unready; the resumed builder
repairs the target and obtains renewed manual acceptance for changed behavior.
The auditor never merges that PR or claims the current branch was approved.
