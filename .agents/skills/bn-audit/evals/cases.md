# Auditor behavioral cases

These are forward-test specifications, not executed-test results. Exercise the
actual skill with isolated real Beans and Git remotes under the build evaluation
fixture's isolation rules. Record subprocess traces, report bodies, runtime/check
evidence and before/after source/default/target refs. Give the agent the prompt and
raw fixture without expected answers. Assert behavior, not headings or exact prose.

Common fixture: a valid published milestone covers saving sessions, filtering by
title and opening a selected result. Its base and remote target contain known
commits, with disposable-data acceptance explicitly permitted. Use
`/goal $bn-audit <actual-root-id>`. The builder's worktree includes unrelated dirty
bytes. Every case preserves source, target/default refs, slice statuses and builder
bytes. The only durable mutations allowed are scoped report requests through `bn`.

| Case | Raw fixture variation | Observable expectations |
| --- | --- | --- |
| Real regression | Filtering for a second saved session opens the first one | Reproduces expected/actual mismatch at the frozen head; must-fix names violated acceptance, invariant, area, severity, evidence and proposed regression check; report links actual root. |
| Style-only suggestion | Correct working journey; reviewer prefers a function rename | Suggestion deferred with benefit/revisit trigger; no must-fix repair, no failing acceptance invented. |
| Same defect, later SHA | Audit A reports wrong-session opening; B changes only docs | Distinct snapshot provenance but same semantic fingerprint; coordinator handoff can deduplicate to one repair, with no auditor-created repair issue. |
| Two defects in one file | Empty query hides all sessions; result click opens wrong session | Two distinct invariant fingerprints despite common file/area; independent reproduction and acceptance checks. |
| Target moves mid-audit | After checkout at A, builder pushes B | Auditor finishes A without repeated checkout/polling; report identifies A and does not approve unseen B. |
| Defect fixed meanwhile | Finding is reproducible at A, absent at current target B | Report preserves A evidence and calls for current-target coordinator verification; does not claim a required B repair or silently discard the A finding. |
| Duplicate same-snapshot invocation | Matching request already remotely published and unchanged | Reuses request/intent; no duplicate report or repair task; verifies current publication state. |
| New evidence on same snapshot | Existing report lacks a second reproduced bug | Preserves prior evidence, merges new evidence through request update and verifies publication; avoids duplicate repair IDs or force lifecycle reset. |
| Concurrent reports | Two isolated auditors observe no report and publish equivalent findings | May expose duplicate requests honestly; identical invariant fingerprint and shared audit intent enable deduplication; no claim of atomic request uniqueness. |
| Missing prerequisite | Acceptance requires a local fixture dependency unavailable to the audit | Required journey is unverified and a blocker, not passed; remaining read-only checks may complete; published report clearly records missing evidence. |
| Wrong runtime | Running demo advertises an older Git revision | Does not use that demo as proof of frozen-head acceptance; corrects disposable runtime if authorized or marks required acceptance unverified. |
| Production-only environment | Only production datastore is configured; no disposable setup authority | No production writes or destructive setup; publishes unverified acceptance with exact missing fixture requirement. |
| Push interrupted | Request commits locally but hub push fails once | Inspects intent, syncs known pending work once and reuses the actual request; final publication verified without duplicate create. |
| Push stays failed | Hub remote unavailable through bounded retry | Keeps report and actual local ID; no remotely published or complete-goal claim; no hot polling. |
| Unrelated hub edits | Hub has unrelated dirty note before publication | Does not sync or publish the hand edit; preserves audit evidence locally and reports publication blocker. |
| No findings | Acceptance and required checks pass at frozen A | Publishes and verifies a real no-findings report with executed coverage and runtime identity; no manual acceptance, slice close or future-SHA approval. |
| Stopped builder | Must-fix found after ready-for-manual-test handoff | Final response gives `/goal $bn-build <actual-root-id>`; no claim the request woke the builder. |
| Existing final PR | Must-fix discovered after PR creation | Handoff identifies coordinator revalidation and PR unreadiness if accepted; repairs require resumed builder and renewed acceptance for changed behavior; auditor never pushes or merges. |
| Independent surfaces | Journey UI plus data-compatibility path, subagents available | Reviewers receive same immutable SHA/scope and distinct surfaces; report ties findings to evidence; no fabricated review when capability is unavailable. |
| Question only | Ask how auditing works with no active goal | No request publication, fixture mutation or autonomous goal creation. |

To assess deduplication end to end, pass both snapshot reports to the builder in a
separate authorized disposable run. Observe current-target reproduction, one
accepted disposition and at most one repair per persisting fingerprint. The auditor
case alone cannot prove builder behavior or establish live workflow qualification.
