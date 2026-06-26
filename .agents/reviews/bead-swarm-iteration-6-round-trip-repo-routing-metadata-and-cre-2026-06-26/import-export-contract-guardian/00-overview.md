# Import Export Contract Guardian Review

Branch: bead-swarm/iteration-6-round-trip-repo-routing-metadata-and-cre
Date: 2026-06-26
Reviewer: Import Export Contract Guardian
Reviewer slug: import-export-contract-guardian
Role: Checks JSONL compatibility, round-trip semantics, validation behavior, and user-facing import/export contracts.

The change extends bn JSONL export/import with optional repo metadata and creation_commit support. It exports nested repo routing data, parses repo payloads during import, and inserts repo links through the store import path with focused tests for old JSONL compatibility, commit validation, unresolved repo identity, and remote-url import.

Overall verdict: REQUEST_CHANGES

Stats: 6 files changed, 371 insertions, 1 deletion, 2 commits at review time.

