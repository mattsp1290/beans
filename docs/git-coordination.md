# Git write coordination

Every cooperating Rust `bn` writer targeting an ordinary clone uses state at
`<canonical-parent>/.beans-state/<canonical-hub-basename>/`. It contains the
persistent `hub.lock`, `op-journal.json`, `last-fetch`, and `last-fetch-attempt`.
The clone parent must be writable. State components and files reject symlinks
and unsafe types; the state never becomes hub content. Native filename bytes
remain part of the identity.

Different `BEANS_HOME` values do not split this lock. Symlink, relative and dot
aliases of an existing clone resolve to the same canonical target. Separate
clones have separate namespaces, even with one user home. Initialization locks
this namespace before clone, rechecks absence, and preserves failed clones.
Root targets, the reserved basename `.beans-state`, bare repositories and linked
worktrees are unsupported. Bind-mount aliases and concurrent clone relocation
or replacement are outside this identity guarantee.

Mutations, sync, initialization and cache clear wait up to 30 seconds for the
exclusive advisory lock. Refresh tries once and uses the local snapshot when
busy; it rechecks its throttle under the lock. No FIFO ordering is promised.
External Git processes and editors do not participate in this transaction lock;
reads are not coherent filesystem snapshots. `doctor` does not fetch or recover.
`cache clear` takes the same lock and removes exactly `last-fetch` and
`last-fetch-attempt`. It preserves the lock inode, journal, unknown files,
directories, temporary ownership records and other clones' state. Missing stamps
are harmless; partial removal errors fail the command.

## User policy

Only the current user's `config.toml` supplies this policy. Shared hub workflow
and `BN_CONFIG` do not override it.

```toml
[git]
lock_timeout = "30s"
command_timeout = "30s"
network_timeout = "60s"
cleanup_timeout = "10s"
diagnostics = false
```

Missing keys use these defaults. Duration strings must be finite and fit signed
64-bit nanoseconds; negative values, wrong TOML types and zero command/network/
cleanup budgets fail before Git or hub effects. `lock_timeout = "0s"` tries once.
Help, version, man and prime do not load this policy.

Every production Git command uses one Unix process-group executor, null stdin,
disabled terminal prompts and SSH batch authentication. Hooks and credential
helpers still run. Local steps use `command_timeout`; clone, fetch, push and
ls-remote use `network_timeout`. Rebase abort gets a fresh `cleanup_timeout`.
These are per-step limits, not a whole-operation deadline. Both pipes drain
concurrently until EOF, including when the Git leader exits first. Output over
64 MiB on either stream fails explicitly rather than silently truncating semantic
output. Git output and staging paths retain native bytes.

At timeout the group receives SIGTERM, then SIGKILL after 250 ms; the direct child
is reaped. Ordinary remaining group helpers are terminated before return even
when they close their pipes. This intentionally interrupts hanging hooks.
Helpers that escape with `setsid` and uninterruptible kernel I/O are outside the
bounded termination guarantee. Process cleanup failures stop the operation.

## Failure and recovery

Only requested-ref `push --porcelain` rejection records authorize retries.
Non-fast-forward/fetch-first records are contention. Receiver compare-and-swap
failures additionally require the same ref's expected-versus-actual object-ID
diagnostic. Hook prose, generic ref-lock errors, permissions, authentication,
transport failures and unknown output stop immediately. Mutation and sync each
use the existing verified budget of at most three actual pushes; no fetch or
rebase follows an exhausted third push. Only a proven single nonce-owned commit
can be discarded for replay. Unowned history and authored bytes survive.

Ordinary initial fetch transport failure can still work offline. Initial fetch
timeout or cleanup failure stops before Apply. Rebase failure gets bounded abort;
timeout never authorizes destructive replay. Apply, stage and commit failure keep
partial effects and the journal. Publication failure keeps committed local
history and evidence: a lost push response may mean the remote already advanced.
Run `bn sync` after resolving the cause; it converges without reapplying an
already-authored event. Failed recovery requires manual reconciliation.

CLI runtime failures return exit 1. HTTP categories are `hub_locked` (423),
`git_timeout` (504), `git_error` (502), and `git_conflict` (409) for interrupted,
detached or conflicting state. Ordinary validation remains 400.

All cooperating binaries must upgrade together; old home-cache locks are not
bridged. A legacy `<BEANS_HOME>/cache/op-journal.json` with no canonical journal
blocks writes and initialization. Reads, doctor and cache clear never adopt or
remove that legacy evidence. To reconcile it:

1. Stop every writer and back up both the hub and legacy journal.
2. Inspect and reconcile interrupted files/history in the intended clone.
3. If the journal belongs to that clone, explicitly place the preserved journal
   at its canonical sidecar `op-journal.json`, then run the new `bn sync`.
4. Preserve unrelated legacy journals untouched; do not infer ownership or delete
   evidence just to unblock a command.

Rollback also requires stopping writers, preserving sidecar evidence and dirty/
unpushed content, and reconciling before changing binaries. Do not run old and
new writers together.

## Diagnostic timings

With `diagnostics = true`, stderr receives one-line JSON records with
`type = "bn.git"`, `schema_version = 1`, random `operation_id`, `operation`,
`event`, `phase`, `duration_ms`, `result`, and optional `push_attempt`. CLI setup
queries and hub steps share the invocation ID; HTTP workers have independent
request IDs. Events include Git steps, lock wait/hold, Apply, push attempts,
refresh skipped-busy/skipped-fresh and final operation durations. Elapsed time
uses a monotonic clock. Nested operation records share the same ID.

Records contain no arguments, paths, URLs, actors, subjects, document content,
environment or raw error text. stdout/JSON product output is unchanged and the
default is off. A bounded queue delivers complete lines through one stderr
worker; backpressure and sink failure may drop events. Producers never wait on
that sink while holding a transaction lock. CLI exit allows at most 50 ms for
queued diagnostics after locks release. Delivery is neither durable nor an
audit log. Native tests qualify process and Git behavior; the verified retry
kernel does not prove scheduler or filesystem atomicity.
