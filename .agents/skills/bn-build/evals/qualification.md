# Qualification record — 2026-09-27

These are observed runs, not expected outcomes copied from scenario specifications.
No general causal speedup is established by one pilot.

## Automated checks

- Beans `make ci`: passed (53 UI tests, Svelte checks/build, Go tests/vet/lint/build/tidy).
- Existing `bn-plan-loop` suite: all 22 tests passed, including real Beans scenarios.
- Official skill-authoring validator: all three new skills passed.
- All 25 tracked existing skill files retained identical SHA-256 hashes.
- New mechanism suite: all 18 tests passed in 96.756s, using source-built Beans,
  isolated role homes/hubs, actual Git remotes and subprocess outcomes. See README
  for the enforced properties.

The source implementation is additive: three new skill directories and one guide.
Existing workflow configuration, statuses, CLI behavior and skills are unchanged.

## Actual forward runs

A producer recovered an existing fixture root, published the validated must-have
DAG and deferred punctuation work. A builder reserved both ownership refs,
held the local lock, used isolated worktrees, obtained independent exact-SHA
reviews, integrated two dependent CLI slices, and published a runnable demo plus
handoff. The first integration immediately unlocked the dependent slice. Actual
commands printed `hello` and `HELLO`, and both tests passed. The optional issue
remained open; the root stopped at `ready-for-manual-test`. No human acceptance,
final PR, deployment or default-branch update was simulated.

Fixture target: `d937917bad199365222e143395e99d013619d97b`.
Default branch remained `29ac3d9ac00ea0cc0e38fb38b4fc65dcb76e8294`.
The auditor published verified request `fixture-r-8unu` for that fixed snapshot.
This audit exercised role instructions; the same implementing agent ran it, so it
was not counted as additional independent approval. The coordinator's separate
reviews supplied independence. Implementation was serial because the selected
slices were dependent; no parallel-agent speedup is claimed.

Five additional actual forward guard runs verified:

- Refusing an old-executor-owned issue preserved its claim and hold evidence.
- A passing unit test with a failing real CLI acceptance journey produced a
  must-fix audit request (`fixture-r-dpdy`) and no fabricated demo.
- A simulated lack of independent review capability preserved a correct candidate
  and stopped integration; no self-review was passed off as independent.
- A real remotely successful issue create followed by a simulated lost response
  recovered its one draft root by stable intent, with zero create retries.
- The same defect reproduced on two snapshots retained both audit reports and
  resolved to exactly one repair issue during repeated current-code intake. The
  repair remained pending scope revision; no completion was inferred.

Setup fault injections were distinguished from role actions. Actual command
traces, independent review artifacts, runtime outputs and negative-effect checks
were retained locally. The source contains concise redacted results, not raw
conversation logs or credentials. These runs do not imply every case.json scenario
was executed. Unexecuted scenario files remain explicitly specified-not-run.

## Defects found and resolved during qualification

Real Beans tests exposed its status.remote URL shape and `note — ` log-event prefix;
both initially broke synchronization/recovery and are covered by regressions.
Independent review found stale-base integration proof: integration must now be
on target first-parent history and either the reviewed fast-forward head or a
merge of the exact reviewed base/head. This also prevents relabeling a merged
side-branch head as a fast-forward.

Review reproduced a race between a caller's clean-worktree check and Beans'
auto-commit sync. Every new role therefore requires an exclusive hub checkout and
BEANS_HOME/cache. The helper rejects missing isolation environment and standard
shared default paths. Arbitrary concurrent edits to that dedicated checkout remain
unsupported; this is not a new atomic Beans guarantee. Native conditional claims
and conditional clean synchronization are deferred requests `beans-r-4w3d` and
`beans-r-lgl9`, linked to implementation `beans-0a9w`.

## Real Interviewprep pilot

Milestone `interviewprep-q1wk`, target `mvp/saved-interview-navigation`.
Brief started 2026-09-27T22:35:09.442252Z; producer finished/approved scope frozen
2026-09-27T22:39:22.104502Z (4m 13s). The scope is lifecycle filtering, honest
loaded-page/empty-state behavior and exact saved-session resume, preserving APIs
and stored data. Server-wide filtering is deferred as `interviewprep-55mz`;
a focused Flutter runner command is deferred as `interviewprep-riyq` after
observing fresh-worktree/bootstrap overhead in this pilot.

UI implementation started 22:43:53Z; first passing widget verification 22:47:52Z;
full `check --offline` completed by 22:50:59Z. UI commit
`7782862138914b2c5ff19b0db47de002126ffd32` passed independent review, all 58 app
tests and static analysis before target-scoped integration. The browser-proof
slice starts only after that integration. This is a serial critical path, not an
observed parallel implementation run.

A pre-change full check failed browser startup with a database-ownership-loss
error. Focused smoke passed on retry and the subsequent full UI check passed.
The failure remains in elapsed time and reported acceptance failures; no backend
fix or waived gate is attributed to it.

Final target is `4b38d335e7c960d98973a2d2c9c6b23065456b3e`. The exact default
`doctor`, `check`, and `smoke:web` commands passed. Full check ran from
23:07:49.792929Z to 23:10:50.674691Z on the unchanged clean tracked source.
A fresh independent auditor reviewed the assembled ten-file diff, independently
passed all five focused widget tests and the real browser journey, and published
verified no-findings request `interviewprep-r-0opg`. Its browser run completed at
23:13:44.978934Z; actual served `main.dart.js` SHA-256 was
`85f1330e92f9060a88fcfd225a9e48c0713c5c59e8612c1a5f7180307fecc206`.
The coordinator synchronized and inspected that request at hub
`dc73fa1d62f5cc9944794ce8849b30c13b1010c6` before declaring readiness.

The real browser journey used 51 API-created active sessions and a distinct saved
revision: selected lifecycle semantics, empty categories with Load more retained,
reset to All, exact saved route/source, return, and unchanged persisted data.
Mixed finishing/completed records and paging append also passed widget fixtures;
they were not fabricated as real API browser coverage. Native/provider features
were outside the milestone. A separate disposable local demo from the exact target
was started for manual testing, with served artifact identity checked.

Recorded implementation friction: one fresh-worktree setup failure, six failed
browser-harness journey attempts (two paging assumptions, three source-focus
attempts, one return activation), and one independent review finding in the
optional Go integration-test entrypoint. These were fixed and all required gates
passed afterward. The pre-change database-ownership failure above is separate.
Reviews consisted of one passing UI review, one smoke review requiring changes
then passing, and the fresh independent assembled audit. No failures were waived.

The demo and `ready-for-manual-test` phase were published and reconciled at
2026-09-27T23:18:39.281596Z: **43m 30s brief-to-demo**, **39m 17s
approved-scope-to-demo**, **zero intermediate human decisions**. Both slices were
integrated into the remote non-default target. No main update or deployment
occurred, and no unresolved must-fix finding remained at the stated cutoff.
**Qualification passed** the bounded build-to-demo target (under 24 hours, at most
one intervention, required gates intact). Manual user acceptance and final PR
remain a separate phase; this is one small pilot, not a general speedup estimate.
Pilot implementation workers inherited the parent conversation; the final skill
now explicitly prefers isolated child context when the harness supports it. That
context/cost improvement was not measured in this pilot.
Usage accounting split into cached input, uncached input and output is unavailable;
no dollar cost is inferred. Wall-clock metrics use elapsed intervals, never the
sum of concurrent agent durations.
