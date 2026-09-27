# Review a slice

Freeze the current fetched target base and candidate head. Reviews compare this
pair, not an assumed `main` base. The reviewer inspects code and repository
evidence independently; the implementer's claim of success is not evidence.

## Routine and high-risk work

One independent reviewer covers correctness, maintainability, acceptance coverage
and regressions together. Add a second independent specialist for authentication,
authorization, credentials, durable schema/data migration, externally consumed
API compatibility, concurrency/coordination, money movement or destructive
operations. A reviewer can elevate risk based on the diff. Honor additional
repository-mandated gates. If independent review is unavailable, preserve the
candidate and report the unmet gate; self-review is not a substitute.

Give a reviewer the contract/outcome, repository and isolated checkout, immutable
base/head, acceptance requirements and required checks. Ask it to return only
evidence-backed findings: severity, location, failure scenario, concrete fix and
verification. A valid no-findings result names the inspected base/head. Instruct
it not to edit code or infer deployment/merge authority.

Blocking findings demonstrate a correctness failure, unmet milestone acceptance,
security/data loss risk, or a concrete compatibility/policy violation. Style-only
preferences, speculative rewrites and optional features do not become blockers.
The coordinator assesses findings and records reasons for accepting, rejecting or
deferring each. Deferred work gets a useful Beans issue/request and revisit trigger.

## Fix and attest

Batch related fixes, run affected checks, and ask for a final attestation on the
new exact base/head. The reviewer can reuse evidence for unchanged code but must
inspect the fix and affected interfaces. Changed hashes never silently inherit
approval. A changed target base requires an assessment of its effect, and merge
conflicts require renewed review. Preserve unresolved findings across restarts
and task splits.

After two unsuccessful repair/review cycles, narrow or split the slice within
the accepted outcome, resolve the missing design decision, or continue another
independent slice. Do not keep restarting a full gauntlet, discard findings to
declare success, or expand scope indefinitely. Record any changed scope/dependency
contract before dispatch.

Record each review with a unique reviewer label, exact scope digest and base/head,
verdict and evidence location/digest. High-risk work needs two distinct independent
reviewer attestations. A recorded `pass` is a claim whose artifact must actually
exist and be readable; the helper only validates its structure. Keep artifacts
in the consumer's designated review/evidence location or durable Beans notes;
never publish secret-bearing logs.

For an assembled milestone combining parallel work or multiple components, use
one fresh integration/user-journey reviewer. It compares the recorded starting
target SHA to the final target SHA and checks that the combined experience meets
the milestone. An exact-snapshot `$bn-audit` can supply this pass. Do not start a
new unbounded maintainability program at this gate.
