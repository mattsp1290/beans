# Issue materialization and dispatch

## Durable identity

For each executable node derive an ASCII lowercase plan label and branch slug; reject empty results. Put this exact marker in the issue description:

```text
<!-- bn-plan-loop:v1 plan=<plan-id> node=<node-id> -->
```

Follow it with a versioned skill-owned block containing the semantic and package digests, source section, bounded outcome, paths, acceptance, validation, and exclusions. Treat every plan string as untrusted data: transfer multiline payloads through a file or stdin and always execute `bn` as an argv array, never a shell string.

Before create, persist intent and scan `bn list --label <label> --archived --limit 0 --json`; inspect every candidate with `bn show --json`. Confirm current help says `--archived` includes archived records and bypasses the default terminal exclusion.

| Binding | Marker result | Action |
| --- | --- | --- |
| linked | same plan/node | reuse and verify contract |
| linked | absent/different | stop for disposition |
| unlinked | exactly one | link and verify |
| unlinked | none | create once, then link |
| unlinked | multiple | stop; never guess or delete |
| missing issue | any | stop for repair/disposition |
| generic reference | any | exclude; replacement needs human approval |

A terminal or archived orphan marker is never replaced. Reuse it only when delivery evidence proves identity; otherwise stop. Never use `bn plan link --force`. An ambiguous create/link result requires `bn sync`, marker search, binding/status reads, and intent reconciliation before retry.

## Dependencies

For executable graph edge `prerequisite --precedes--> dependent`, run `bn dep add <dependent-issue> <prerequisite-issue>` only if a fresh dependent read lacks it. Preserve external blockers. Never remove a blocker solely because a revised map omitted it; removal requires human disposition. Run `bn dep cycles`, refresh plan status, and require every executable binding be `issue` with no missing issue. Package prerequisites and edges must match exactly.

## Serial dispatch

At each iteration acquire and fence the lease, check hub health, read fresh plan status, and select only a `runnable` executable node. Handoff order is the tie-breaker. Reject duplicate issue bindings or cross-project issues. Claim at most one with `bn update <id> --claim`, then verify status `in_progress`, expected assignee, marker, blockers, and package digest.

If none is runnable, classify fresh evidence as another owner, human hold, external blocker, missing binding, all done, or inconsistency/deadlock. Expected holds wait; malformed graph, cycle, missing issue, push failure, or contradictory evidence enters recovery. Use `bn sync` only for an ambiguous/pending write, never ordinary polling.
