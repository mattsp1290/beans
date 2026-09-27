# Select and publish one milestone

## Choose a usable outcome

Inspect enough of the vision, existing journeys, tests and scoped Beans work to
identify a concrete gap. Prefer a journey such as “save a practice session, find
it by title, and reopen it” over “build a reusable search platform.” State the
starting user state, actions and visible result. Reuse existing proven interfaces;
identify necessary fixtures and the critical path. Advisory code areas help avoid
conflicts but are not a rigid path allowlist.

At least the first slice must run a user-visible end-to-end path. Share interface
decisions before parallel slices; dependencies reflect behavior, not file order.
Record why each slice is indispensable. Deferred issues need benefit, evidence
and a revisit trigger; exclude them from the contract even when globally ready.
If broad uncertainty prevents executable scope, identify one bounded discovery
task and leave implementation readiness unresolved rather than writing a large
specification or pretending a draft is executable.

Use the shared contract's exact fields, not a new plan schema. Preserve reused
decisions with their source. Future consumer compatibility is specific to that
consumer; do not copy answers from the Beans skill-authoring project. Missing
material context or integration authority needs a user answer. No new feature
flag policy is imposed by this skill. `main_merge` and `deploy` stay false.

Discover the remote's actual default branch; neither it nor `main` is a valid
target. With existing authority for the target workflow, propose a new
`mvp/<key>` and record the inspected source base SHA. An existing non-default
branch requires explicit user selection and inspection. The producer does not
create or reserve the target; the builder does so after validating ownership.

## Publication and recovery

Read the shared [state and recovery procedure](../../bn-build/references/state-and-recovery.md)
before publishing. Inspect `bn prime`, `bn --help` and relevant subcommand help
for `list`, `show`, `create`, `update`, `dep`, `note`, `status`, and `sync`; discover
plan/request commands only when those inputs or outputs are needed. Use a capable
available binary consistently. Missing capability is a blocker, not permission to
edit hub files, change hub layout/configuration, or install over the user's CLI.

Before deciding whether a mutation already happened, establish the shared sync
barrier: inspect status and hub cleanliness without modifying it, identify any
pending commits, then sync only this workflow's known pending work. Require a clean
hub with `ahead: 0` and `behind: 0`; record the observed hub HEAD. Unrelated hand
edits or unidentified pending commits must not be silently published. Throttled
ordinary reads, exit zero and recent timestamps alone do not prove fresh state.

1. Select a stable milestone key before creation and retain it across retries.
   Derive mutation intent keys from repository/project, milestone key, and logical
   slice or deferred item identity, not an attempt timestamp. Put each intent in
   the initial description. Search fresh project-scoped issues, including closed
   and archived records without result truncation, and inspect matching bodies.
   Compare outcome and provenance, not only the title. One producer materializes
   a milestone; conflicting concurrent producers stop for reconciliation.
2. Create or recover the ordinary epic as draft. Preserve returned real IDs and
   publication results. Do not fabricate IDs to make validation pass. Create or
   recover each must-have issue with the root parent and shared labels; put its
   outcome, necessity, acceptance, dependencies, areas, exclusions and risk in the
   description. Use the supported description argument via argv; do not assume
   issue creation accepts the request command's `--body-file` option.
3. Materialize only the selected DAG with `bn dep add CHILD PREREQUISITE`, checking
   for cycles and agreement with the contract. Publish bounded deferrals through
   normal issues/requests, linked to this milestone; issues use `mvp:later`.
   Resolve duplicate candidates to one canonical ID with recorded reasoning;
   do not delete them or dispatch ambiguous dependencies.
4. Fill the root's one `bn-mvp-contract` fence with actual issue IDs, inspect the
   fresh root before updating, and preserve concurrent notes and unrelated text.
   Use the sibling helper's `validate-contract` operation following runtime help.
   Publish scope evidence in the shared note format through `bn`, not just the
   local cache. Producer scope publication does not require reserving the builder's
   target. Revision conflicts require reconciliation, never blind overwrite.
5. After any ambiguous mutation failure, inspect the synchronized records by
   intent before retrying. A failed push can leave a committed issue. Retry sync
   once for known pending work, then re-read; persistent failure stops publication
   with the actual IDs and pending state preserved. `pushed: false` is not success.
6. Require a final synchronization barrier and read back root, slice parents,
   labels, blockers and scope evidence. Validate that read-back contract, compare
   its digest to the selected scope, and verify each deferred ID is excluded.
   Report the verified observed revision; do not claim future atomic exclusivity.

When supplied an existing plan, retain its actual ID and observed revision as
provenance and state the selected subset and exclusions. Leave the plan and its
execution issues unchanged. If a root is already building, publish only permitted
backlog candidates or a scope-change proposal for its coordinator; do not silently
expand `slices`, switch targets or reset its scope revision.
