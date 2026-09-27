---
name: bn-produce
description: Select and publish one bounded, executable MVP milestone for the current repository in Beans. Use for a plain producer invocation, a chosen user outcome, or a scoped subset of existing work; does not implement the milestone or require a detailed plan.
---

# Beans MVP Producer

Produce one finite handoff: an epic, its must-have slices, and useful deferred work.
Publication requires an active explicitly requested goal, such as `/goal $bn-produce`;
an ordinary question permits explanation and read-only discovery only. The goal
includes scoped Beans publication, not source edits, implementation, target creation,
or changes to existing plans and skills.

Read the shared [contract](../bn-build/references/contract.md) and
[milestone selection and publication](references/milestone-selection.md). Install
these skills together; stop with the missing dependency if the sibling contract
or helper is absent. Discover the helper's arguments with its `--help`.

1. Resolve the invocation repository, instructions, Git identity/default branch,
   dirty state, Beans project and CLI capabilities. Read relevant product intent,
   current behavior, checks and existing work. Use project overrides consistently.
   Preserve unrelated changes. Before mutation, create/resume the exclusive
   producer hub clone required by the shared recovery procedure; never mutate the
   shared default hub checkout. Do not invoke another planning skill by default.
2. Accept the user's outcome or select the smallest coherent missing user journey
   supported by repository evidence. A plain invocation is sufficient when product
   intent is clear. Ask only for material unresolved direction or authority; if
   intent is absent, obtain the desired outcome instead of inventing a product.
3. Reuse confirmed compatibility and integration authority from this conversation
   or the current contract, recording their source. Resolve missing consumer/data
   compatibility or target integration authority before declaring work executable.
   Do not infer new permission from a contract boolean or repeat settled questions.
4. Define demonstrable acceptance, repository checks, prerequisites, runtime
   identification, non-goals and a one-day scope hypothesis. Prefer a thin usable
   first slice and roughly 2–6 slices overall; this is a heuristic, not a quota.
   Every must-have has an acceptance check and a reason the journey needs it.
   Keep optional platform work, polishing and unrelated ideas deferred.
5. Materialize the root and dependency DAG idempotently through `bn`, following the
   reference's synchronization and restart procedure. Validate the complete shared
   contract and verify published IDs, relationships and remote state. An incomplete
   or unsynchronized root remains draft and cannot dispatch a builder.
6. Return the actual milestone ID, outcome, target proposal, acceptance journey,
   key deferrals and `/goal $bn-build <actual-id>`. This is the finite stopping
   point; mark the producer goal complete only after verified publication.
   A blocker is not a completed producer goal.

An existing detailed plan is read-only provenance. Select a bounded subset into
new issues; do not rewrite its lifecycle, run the old loop, or adopt issues owned
by an active executor. An actively building milestone's must-have set changes
only through an explicit scope revision accepted by its coordinator. Plain
reinvocation must first reconcile an existing matching root, not create a new one.
