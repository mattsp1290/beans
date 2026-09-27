# Disposable MVP evaluations

Run from the Beans source checkout:

```text
python3 .agents/skills/bn-build/evals/run_tests.py
```

Latest local mechanism verification: **18 tests passed in 96.756 seconds**. This
result includes the first-parent integration, publication and exclusive-home
regressions; it is not a forward-agent or product-pilot qualification claim.

This command builds the evaluated `cmd/bn` source offline into a fresh temporary
root and runs the public helper as subprocesses against actual Git repositories
and actual Beans mutations. Each case has independent builder/auditor source and
hub clones, local bare remotes, configuration, Beans caches, traces, and ownership
state. `BEANS_HOME`, `BEANS_HUB`, project, actor and Git global configuration are
explicit. Inherited `BN_CONFIG` and Git configuration overrides are removed;
the workflow override case adds one explicit disposable `BN_CONFIG`.
`GIT_ALLOW_PROTOCOL=file` prevents network Git transports. The Go compiler and
already cached dependency modules are read-only build inputs; `GOPROXY=off`,
`GOSUMDB=off`, `GOTOOLCHAIN=local` and a disposable Go build cache prevent downloads
and avoid changing global binaries. Missing cached build prerequisites fail the
evaluation. No fixture accesses the user's real Beans hub.

Before its first mutation the fixture asserts configuration/cache/hub paths are
inside its temporary root. Simulated external configuration/cache sentinels
outside both actor homes are preserved. Each test checks those bytes, all actual
remote paths, and the initial default branch SHA. The suite deletes its temporary
root after completion, including failure. Failure messages retain command results
but not a transcript of an agent run.

The mechanism suite covers strict contracts and symlink escapes; two-clone atomic
reservation races and milestone binding; existing/default target refusal; remote
rejection and unsupported atomic pushes; keeper liveness and cache loss; record
idempotency/conflicts; default/overridden ready membership and published assignment
recovery; false close, failed check, stale review and target ancestry; required
second reviewer for high risk; deferred work outside scope; exact-SHA acceptance;
duplicate request publication; real throttled stale audit reads; explicit sync,
dirty hub, fetch failure and hub lock contention. The lock test observes Beans'
actual 30-second lock timeout.

The integration regression creates diverging commits from A, advances the target
to B, and pushes M with parents B/C. Review A/C cannot approve M; refreshed review
B/C can. Relabeling the side-parent C as a fast-forward integration is rejected;
genuine historical fast-forward B remains valid in the target's first-parent
history. Demo evidence requires every slice's proved integration to be published
and imported by authoritative hub reconciliation. Final-PR evidence similarly
requires published manual acceptance of the exact SHA: a local record, published
but unreconciled note, or offline JSON import is insufficient. These are evidence
gates; the tests do not claim a human accepted the fixture or that a PR was created.

Recovery tests stop the real keeper after a target push, after publishing an
integration note, and after closing the issue. Restart inspection recovers once,
including lost local evidence, without republishing or changing the delivered
target. Two subprocesses also perform observed overlapping work in separate Git
worktrees; both tested results are merged onto the target. This proves process
and Git isolation mechanisms, not independent agent reasoning or review.

The barrier isolation test rejects absent role-specific home/hub configuration,
mismatched hubs and the shared default home before invoking Beans. Its default
home is simulated by replacing only Python's home resolver inside the test
subprocess; a disposable executable tripwire prevents accidental real Beans
access if the guard regresses. The user's HOME and real Beans configuration are
never changed or consulted by that test. Exclusivity of an arbitrary custom
home across unrelated processes remains an operating precondition.

The suite also exercises an actual `bn --no-sync` note commit: the barrier refuses
to publish it until exact inspected commit/intent/milestone evidence is supplied.

Review envelopes in mechanism tests are intentionally schema fixtures. They do
not claim a person or agent reviewed the sample code. Check exit results are
obtained from actual subprocesses. `assert_trace.py` validates captured argv,
outcomes and working directories; remote SHA assertions supply the default-write
proof. These checks cannot prevent every autonomous shell misuse, verify human
acceptance, or prove that instructions choose the correct tools.

## Forward agent runs

Create a retained environment for independently recorded agent runs:

```text
python3 .agents/skills/bn-build/evals/fixture_repo.py --output /absolute/empty/temp/path
```

The output and `context.json` contain the source-built binary, ready contract,
milestone/slice IDs, each actor's source path, environment and keys to unset. Pass
that context to the harness; prepend the disposable binary directory to PATH.
All resulting artifacts must stay under `isolation_root`. Use an actual goal invocation
and preserve a redacted transcript, independent reviewer outputs, actual Git/Beans
results, and observed checks/journeys. Stop lock keepers and live workers before
removing the retained environment.

The `cases/*/case.json` files are **scenario specifications, not executed evidence**.
They cover the plan's required builder scenarios; producer and auditor scenarios
also live with those skills. Do not change their status to passed from this
scripted suite. Only an actual recorded forward agent run can establish tool
selection, meaningful independent review, overlap, duplicate-finding repair
selection, recovery after every external mutation, old-executor refusal, missing
delegation behavior, or a truthful manual-test stop. The real consumer MVP pilot
and its elapsed-time/intervention measurements remain separate qualification gates.
