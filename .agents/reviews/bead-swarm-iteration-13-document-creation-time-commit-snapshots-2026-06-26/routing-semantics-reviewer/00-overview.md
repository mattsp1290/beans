# Routing Semantics Reviewer

- Branch: `bead-swarm/iteration-13-document-creation-time-commit-snapshots`
- Base: `main`
- Date: 2026-06-26
- Reviewer: Routing Semantics Reviewer
- Slug: `routing-semantics-reviewer`
- Role: Checks whether the docs accurately describe repo resolution, identity matching, and `creation_commit` persistence semantics.
- Overall verdict: REQUEST_CHANGES
- Stats: 4 files changed, 77 insertions before review fixes; docs plus iteration metadata.

The change documents creation-time `HEAD` snapshots across the README and repo routing specs. The new snapshot rules mostly match the implementation, including registered repo identity comparison, local-only `file://` identity, best-effort failure behavior, and dirty state omission. One existing paragraph in the routing spec contradicted the new prefix-mismatch wording and implementation behavior, so the review requested changes.
