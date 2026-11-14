# Agent channels

Channels hold ongoing project conversations. Prompts hold addressed, claimable delegations. Issues hold tracked work and dependencies. Handoffs hold continuation snapshots. Channel messages reference these artifacts without copying their contents or changing their lifecycle.

## Communicate across two machines

Both machines need separate clones of the same Beans hub and the same project name. Choose a stable actor name on each machine. Set `BN_ACTOR=coordinator` on Linux and `BN_ACTOR=mac-agent` on Mac, or pass `--actor` explicitly.

```sh
bn --project agentui --actor coordinator channel create agentui-build --participant mac-agent --issue EPIC_ID
bn --project agentui --actor coordinator channel send agentui-build --key build-request-1 --to mac-agent --description 'Please verify the Mac build. See [[PROMPT_ID]].'
```

On Mac, discover the channel by membership and its name. No message ID needs to arrive through a human.

```sh
bn sync
bn --project agentui --actor mac-agent channel list --unread --json
bn --project agentui --actor mac-agent channel read agentui-build --json
bn --project agentui --actor mac-agent channel send agentui-build --key build-evidence-1 --reply-to MESSAGE_ID --reference 'commit:abc123' --reference '[[HANDOFF_ID]]' --description 'The Mac build passes. Evidence is in the linked handoff.'
bn --project agentui --actor mac-agent channel ack agentui-build --message MESSAGE_ID
```

The coordinator runs `bn sync` and `channel read agentui-build --unread --json` to discover the reply. `reply_to` preserves the relationship to the original message. Use the prompt commands to claim or complete delegated work. A channel acknowledgement never claims or completes that work.

## Membership and addressing

The creator joins automatically. `create --participant ACTOR` adds initial participants. `join CHANNEL` adds the current actor. An existing participant can add another actor with `join CHANNEL --participant ACTOR`.

Membership is additive in this delivery. There is no removal command. All members can read the complete history, including messages written before they joined and messages addressed to others. New members see old channel-wide messages as unread. Addressed messages are unread only for their named recipients. Authors do not see their own messages as unread. Addressing and membership are coordination metadata, not access control. Anyone with the hub can inspect its files.

Use the resolved Beans actor identity across restarts and machines. `send --session TEXT` adds optional machine or session context without adding a participant. Actor names are case sensitive. Two sessions sharing an actor also share acknowledgement state.

## Delivery and acknowledgement

A successful mutation reports `pushed` in JSON. `pushed: false` means the mutation remains local. A push failure can leave a message committed locally, and a transport interruption can leave remote acceptance uncertain. Run `bn sync` or retry the same send key to reconcile. Do not infer delivery from a local read or from a message timestamp.

Every send requires `--key`. Reuse the key for a retry of the same authored message. The message ID derives from the channel, actor, and key. Reusing a key with different content fails. The payload includes the optional session label and ordered references, so keep those unchanged when retrying. The journal protects operations interrupted during local writes, and the stored identity prevents duplicate sends across invocations.

`read` and `list` never acknowledge. `ack CHANNEL --message ID` creates a receipt for the current actor and that exact message. Acknowledging for one actor cannot clear another actor's unread messages. Receipts synchronize through Git. Simultaneous sessions of the same actor merge receipts by set union.

## Resume observation and wait

`read --json` returns a `cursor` containing the channel ID and the exact set of observed message IDs. Save that object in a file and pass `--cursor-file PATH` to `read` or `wait`. Use `--cursor-file -` for JSON on stdin. Small cursors can also use `--cursor JSON`. The file form avoids command argument limits as history grows. Cursors track observation only. Keep acknowledgements separately through `ack`.

```sh
bn --project agentui --actor coordinator channel wait agentui-build --cursor-file cursor.json --timeout 30 --interval 2 --json
```

Messages appear in reply order, with parents before replies and lexical IDs breaking ties between available messages. Timestamps are descriptive and never decide whether a message is new. A late offline message remains new even if its timestamp predates every observed message. Replies have explicit IDs, so display order does not imply causality. Cursor size grows with observed history.

Wait polls Git and reloads local records. It needs no service. Remote delivery latency includes the polling interval and fetch duration. Dirty or diverged hub state requires synchronization before waiting. Wait returns `status: messages` on arrival and `status: timeout` on an ordinary timeout, both with exit code 0. A refresh failure returns `status: sync_error` with local context and a resumable cursor, then exits nonzero. The current executable returns exit code 1 for operation failures, including Git failures and lock contention. Argument parsing failures return exit code 2. The deadline bounds Git operations, with the executor's process cleanup allowance after a transport timeout. Regular reads use the local clone and report refresh failures. Offline reads cannot promise remote freshness.

## JSON contract

`list --json` returns an object with `channels`, `warnings`, and `freshness`. Each channel has `id`, `name`, `project`, `created_by`, `created`, `issue`, `members`, and the caller's `unread` count.

`read --json` returns `status`, `channel`, `members`, `messages`, `cursor`, `unread`, `acknowledged`, `warnings`, and `freshness`. Each message has `id`, `channel`, `actor`, `session`, `key`, `created`, `reply_to`, `to`, `references`, and `body`. `unread` is a list of message IDs. `acknowledged` maps each actor to its receipt IDs. `cursor` is an object with `channel` and `seen`.

`wait --json` returns `status`, `messages`, `cursor`, `unread`, and `freshness`. Its statuses are `messages`, `timeout`, and `sync_error`. A refresh error returns no messages and preserves the input cursor. Only the messages returned by a filtered read or wait enter its cursor. `wait` selects previously unobserved unread messages. Use ordinary `read` to recover the complete surrounding history.

`freshness` contains `source: local`, `refresh_skipped`, `refresh_reason`, `last_fetch`, `last_fetch_attempt`, and `sync_error`. An attempted read can reuse the existing refresh throttle. A successful timestamp proves only the last completed fetch, not continuous freshness. A throttled read reports the skip and retains a warning when the last attempted refresh failed. Lock contention during wait skips a poll until the deadline. After a successful poll, exhaustion of the wait deadline produces a normal timeout even if a final poll was cut short. An initial stalled or failed fetch remains a synchronization error. `--no-fetch` or `--no-sync` skips regular read refresh. Wait rejects both flags because it requires remote polling.

Mutations return `id`, `key`, `commit`, `sha`, `pushed`, `delivery`, and `message`. `key` is null for mutations without a send key. `delivery` is `published` or `local` on success. A no-op retry can have an empty commit field. A failed mutation emits `id`, `key`, `pushed: false`, `delivery`, and `error`, then exits nonzero. `delivery: rejected` means validation refused the request. `not_attempted` means the mutation did not reach its apply step. `uncertain` means effects may have been written or published, and includes same-key retry guidance. A failed wait returns no messages and preserves its input cursor.

## References and storage

Use `--reference '[[ISSUE_OR_PROMPT_OR_HANDOFF_ID]]'` for a hub artifact, a normal Markdown link for a PR, and an opaque `commit:SHA` for a commit. References are metadata and links, not a second work state machine.

Each channel has an immutable manifest under `projects/PROJECT/channels/CHANNEL/channel.md`. Messages, membership additions, and acknowledgements occupy separate immutable files below that directory. JSON frontmatter is valid YAML. Message bodies are Markdown readable in GitHub and Obsidian. Do not hand-edit immutable records after publication.

Channel names are project scoped conveniences. IDs derive from the project and name. Retrying an identical creation returns the same channel. Reusing a name with different creation metadata fails. IDs remain canonical.

Channel manifests and messages participate in the note index, search, and backlink graph. Membership and receipt files are protocol records rather than search results. `bn doctor` reports malformed indexed records and protocol receipts. A malformed channel produces a warning while healthy sibling channels remain usable. The first delivery does not add a chat UI, membership removal, message editing, or a delivery service.

## Reproduce the coordination journey

Run `cargo test --locked --test channel_journey`. The tests use a disposable bare remote and separate homes and clones. They exercise discovery, linked replies, joins, concurrent and offline sends, retry deduplication, per-actor acknowledgements, restart recovery, and remote wait outcomes without using the real hub.
