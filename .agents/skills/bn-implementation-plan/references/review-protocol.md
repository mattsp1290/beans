# Independent plan reviews

After authoring and validating the initial draft, launch two independent reviewer subagents, concurrently when supported. The skill's calling agent owns plan edits; reviewers inspect and return findings only. Do not run the planned implementation or publish from reviewer agents.

Give each reviewer the verbatim user request, source repository root, local bundle path, confirmed operating-context answers, this review contract, and access to relevant source and external-dependency evidence. The bundle includes all request sections that need review. Do not share either reviewer's findings or suspected answers with the other.

- Reviewer A traces implementation completeness: missing change surfaces, control and data flow, dependencies, compatibility, migration, lifecycle and error paths, sequencing, and acceptance criteria.
- Reviewer B challenges architectural fit and verification: repository patterns, public boundaries, applicable security and performance implications, recovery, test coverage, and whether tests prove the requested behavior.

Every reviewer must map explicit user outcomes, constraints, non-goals, and compatibility promises to plan locations and acceptance criteria. Check that confirmed operating context is applied consistently. Check native bundle structure, Summary/section/graph consistency, and external ownership and readiness gates. Do not infer missing user answers or assume graph edges create issue blockers.

Return only evidence-backed findings, each with severity (`Critical`, `Important`, or `Minor`), plan location, repository evidence or missing evidence, the failure mode for an implementer, and a concrete correction. If there are no material findings, return exactly `No material findings`. Omit compliments and style preferences.

The calling agent evaluates each finding against user intent and source evidence. Apply corrections that improve correctness, completeness, sequencing, or verification. Reject unsupported, duplicative, cosmetic, or scope-expanding findings. Update affected sections, Summary, graph, and handoff together. Do not append a review transcript to the bundle.

After those revisions, launch a fresh third subagent for an adversarial review. Give it the same raw context and revised bundle, withholding previous reviews, dispositions, suspected defects, and proposed corrections. Its stance:

> Assume a competent coding agent follows this plan exactly and the change still fails or cannot be completed. Find the strongest ways that can happen.

Ask it to prioritize unverified assumptions, contradictory documents, hidden sequencing or ownership gates, partial failure and concurrency, rollback gaps, tests that pass despite broken behavior, scope drift, and stale or invented commands or interfaces. Require the same finding format. Evaluate and incorporate valid findings, then re-read and validate the full bundle.

A review stage completes only when its designated agent inspected the required artifacts and returned usable findings or the no-findings response. Retry a failed stage once with a fresh agent. If independent agents are unavailable or the retry fails, preserve the bundle and report the incomplete stage. Do not replace required independent review with self-review or mark the plan ready. If publication is otherwise possible, publish it as blocked with the missing review stage and exact unblock action recorded.
