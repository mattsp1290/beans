# Native Rust first CLI slice

The Rust `bn` binary now dispatches `init`, `create`, `ready`, `list`, `show`,
`update --claim`, `note`, `close`, `status`, and `sync` directly. It invokes
system Git for repository effects; it does not invoke the Go binary or Python.
The retained command families are now implemented; see [rust-native-cli.md](rust-native-cli.md) for the complete command/flag inventory. Server/distribution wiring remains a later slice.

Hub, project, actor, and workflow overrides use the existing Beans environment
and configuration vocabulary. `--branch` overrides the hub branch; otherwise
initialization discovers the remote HEAD, with `main` for an empty remote.
Initialization saves the remote and branch while preserving unknown user TOML
keys. Existing hub configuration and issue files are retained; parsed issue
mutations preserve unknown frontmatter and authored bytes through the domain
codec. Plain output is deliberately simpler; `--json` provides structured
command results, and `show` without JSON emits the retained issue document.

Every write, sync, and fetch acquires the existing `cache/hub.lock` flock.
A busy lock fails promptly with retry guidance instead of waiting 30 seconds.
Read commands continue with a local, read-only index snapshot when fetch or
locking fails; snapshot loading never recovers plan backups or deletes temps.
Read fetches honor the configured throttle. Status reports ahead/behind,
remote, dirty files, last read fetch time, and journal recovery presence.

`--no-sync` intentionally succeeds after a local commit. Ordinary failed
pushes fail the command and identify the retained local commit, with `bn sync`
guidance. Failed apply, staging, commit, and replay effects retain the shared
`op-journal.json`, partial files, and Git history for recovery; this replaces
the former unconditional reset/clean after failed Apply. Sync commits retained
partial work as recovery history. No failed rollback can erase its recovery
signals because this pipeline never rolls back partially applied filesystem
writes. Only exact generated temporary files with separate ownership records
under `.git/bn-temp-owners` and verified untracked state are cleaned under the
lock. Tracked candidates and unregistered authored drafts survive regardless
of `plan.md.tmp`, `.bn-write-`, or `.bn-plan-` names. Issue indexing uses a
read-only snapshot and does not perform unrelated plan recovery.

Push attempts consume the verified kernel budget. After a conflicting rebase,
only a clean, verified sole local HEAD with the current run's random nonce
trailer can be discarded and replayed. Unknown facts, failed abort, prior-run
nonces, and unrelated commits preserve history and fail. Issue operations freeze
log context and time and reread documents on every apply. After a rebase or
owned reset, `Operation::after_rebase` receives the fallibly verified presence
of this invocation's nonce-bearing commit anywhere in current ancestry. A
surviving commit suppresses replay duplication; a dropped identical patch
reapplies this invocation's note separately from the competing writer's note.
No new log marker or hub schema is introduced. Separate same-text notes remain
separate invocations; unchanged updates and repeated closes remain no-ops.

Scaffolding prevalidates its entire batch, and issue destinations are checked
before any write. `write_hub_file` anchors writes to directory descriptors,
opens ancestors with no-follow semantics, and renames within the opened parent.
Project, projects-root, issues-directory, and destination-file symlinks fail
without changing external bytes. The generic config writer remains separate
for intentional configuration paths outside the hub. `PreparedHubWrite`
provides the same write boundary with exact temporary recovery evidence;
its normal drop clears both temporary and evidence. Whole-batch containment
rejection deliberately strengthens the old scaffolding helper's skip/follow
behavior for symlinks and non-directory ancestors.

`tests/native_cli.rs` exercises disposable real Git remotes, including empty
and seeded initialization, the complete first issue journey and second-clone
history, races, duplicate prevention, hand edits, offline/no-sync work, failed
writes, malformed documents, locking, interrupted/detached checkouts, nonce
ownership, bounded retries, recovery, and configuration overrides. These tests
exercise production Rust boundaries; the existing domain suite continues to
check preserved stored content and IDs.
