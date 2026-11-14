//! Disposable multi-home public-CLI coordination journeys.
use serde_json::Value;
use std::{
    fs,
    os::unix::fs::PermissionsExt,
    path::{Path, PathBuf},
    process::{Child, Command, Output, Stdio},
    thread,
    time::{Duration, Instant},
};

struct Journey(PathBuf);
struct Agent {
    home: PathBuf,
    actor: &'static str,
}

impl Journey {
    fn new() -> Self {
        let mut random = [0; 8];
        getrandom::fill(&mut random).unwrap();
        let root = std::env::temp_dir().join(format!(
            "bn-channels-{}-{}",
            std::process::id(),
            u64::from_ne_bytes(random)
        ));
        fs::create_dir_all(&root).unwrap();
        git(
            &root,
            &["init", "--bare", "--initial-branch=main", "remote.git"],
        );
        Self(root)
    }
    fn remote(&self) -> PathBuf {
        self.0.join("remote.git")
    }
    fn agent(&self, name: &str, actor: &'static str) -> Agent {
        let home = self.0.join(name);
        fs::create_dir_all(&home).unwrap();
        Agent { home, actor }
    }
    fn clone_agent(&self, name: &str, actor: &'static str) -> Agent {
        let agent = self.agent(name, actor);
        fs::create_dir_all(agent.home.join(".beans")).unwrap();
        git(
            &self.0,
            &[
                "clone",
                self.remote().to_str().unwrap(),
                agent.hub().to_str().unwrap(),
            ],
        );
        agent
    }
}
impl Drop for Journey {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}
impl Agent {
    fn hub(&self) -> PathBuf {
        self.home.join(".beans/hub")
    }
    fn command(&self, args: &[&str]) -> Command {
        let mut command = Command::new(env!("CARGO_BIN_EXE_bn"));
        command
            .args(["--json", "--project", "agentui", "--actor", self.actor])
            .args(args)
            .current_dir(&self.home)
            .env("HOME", &self.home)
            .env("BEANS_HOME", self.home.join(".beans"))
            .env("GIT_CONFIG_GLOBAL", "/dev/null")
            .env("GIT_CONFIG_NOSYSTEM", "1")
            .env("GIT_TERMINAL_PROMPT", "0")
            .env("BN_NO_UPDATE_CHECK", "1")
            .env_remove("BEANS_HUB")
            .env_remove("BEANS_PROJECT")
            .env_remove("BN_ACTOR")
            .env_remove("BN_CONFIG")
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        command
    }
    fn spawn(&self, args: &[&str]) -> Child {
        self.command(args).spawn().unwrap()
    }
    fn output(&self, args: &[&str]) -> Output {
        finish(self.spawn(args))
    }
    fn ok(&self, args: &[&str]) -> Value {
        let output = self.output(args);
        parse_success(output)
    }
    fn sync(&self) {
        let output = self.output(&["sync"]);
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
    }
    fn read(&self, channel: &str, unread: bool) -> Value {
        self.sync();
        if unread {
            self.ok(&["--no-fetch", "channel", "read", channel, "--unread"])
        } else {
            self.ok(&["--no-fetch", "channel", "read", channel])
        }
    }
    fn send(&self, channel: &str, key: &str, body: &str) -> Value {
        self.ok(&[
            "channel",
            "send",
            channel,
            "--key",
            key,
            "--description",
            body,
        ])
    }
}
fn finish(mut child: Child) -> Output {
    let deadline = Instant::now() + Duration::from_secs(35);
    loop {
        if child.try_wait().unwrap().is_some() {
            return child.wait_with_output().unwrap();
        }
        if Instant::now() >= deadline {
            let _ = child.kill();
            let output = child.wait_with_output().unwrap();
            panic!(
                "CLI journey exceeded 35 seconds: {}",
                String::from_utf8_lossy(&output.stderr)
            );
        }
        thread::sleep(Duration::from_millis(10));
    }
}
fn parse_success(output: Output) -> Value {
    assert!(
        output.status.success(),
        "stdout={} stderr={}",
        String::from_utf8_lossy(&output.stdout),
        String::from_utf8_lossy(&output.stderr)
    );
    serde_json::from_slice(&output.stdout).unwrap_or_else(|error| {
        panic!(
            "invalid JSON {error}: {}",
            String::from_utf8_lossy(&output.stdout)
        )
    })
}
fn git(dir: &Path, args: &[&str]) -> String {
    let output = Command::new("git")
        .arg("-C")
        .arg(dir)
        .args(args)
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .env("GIT_CONFIG_NOSYSTEM", "1")
        .env("GIT_AUTHOR_NAME", "Journey")
        .env("GIT_AUTHOR_EMAIL", "journey@localhost")
        .env("GIT_COMMITTER_NAME", "Journey")
        .env("GIT_COMMITTER_EMAIL", "journey@localhost")
        .env("GIT_TERMINAL_PROMPT", "0")
        .output()
        .unwrap();
    assert!(
        output.status.success(),
        "git {args:?}: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    String::from_utf8(output.stdout).unwrap().trim().to_owned()
}
fn id(value: &Value) -> &str {
    value["id"].as_str().unwrap()
}
fn messages(value: &Value) -> &[Value] {
    value["messages"].as_array().unwrap()
}
fn contains(value: &Value, message_id: &str) -> bool {
    messages(value)
        .iter()
        .any(|message| message["id"] == message_id)
}
fn hook(path: &Path, body: &str) {
    fs::write(path, format!("#!/bin/sh\n{body}\n")).unwrap();
    fs::set_permissions(path, fs::Permissions::from_mode(0o755)).unwrap();
}

#[test]
fn agents_discover_reply_acknowledge_and_resume_across_independent_clones() {
    let journey = Journey::new();
    let coordinator = journey.agent("linux-coordinator", "LinuxCoordinator");
    let initialized = coordinator.output(&["init", journey.remote().to_str().unwrap()]);
    assert!(
        initialized.status.success(),
        "{}",
        String::from_utf8_lossy(&initialized.stderr)
    );
    let epic = coordinator.ok(&["create", "AgentUI collaboration", "--type", "epic"]);
    let prompt = coordinator.ok(&[
        "prompt",
        "create",
        "Inspect Mac build",
        "--to",
        "MacAgent",
        "--description",
        "Inspect the Mac build and record evidence.",
        "--issue",
        id(&epic),
    ]);
    let created = coordinator.ok(&[
        "channel",
        "create",
        "agentui-build",
        "--participant",
        "MacAgent",
        "--issue",
        id(&epic),
    ]);
    assert_eq!(created["pushed"], true);
    let channel = id(&created).to_owned();
    let retry_create = coordinator.ok(&[
        "channel",
        "create",
        "agentui-build",
        "--participant",
        "MacAgent",
        "--issue",
        id(&epic),
    ]);
    assert_eq!(
        id(&retry_create),
        channel,
        "retried create returns the same stable channel"
    );
    let changed_create = coordinator.output(&[
        "channel",
        "create",
        "agentui-build",
        "--participant",
        "DifferentAgent",
        "--issue",
        id(&epic),
    ]);
    assert!(
        !changed_create.status.success(),
        "the same name cannot silently replace channel membership"
    );
    let original = coordinator.send(
        &channel,
        "review-mac-build",
        "Please inspect the **Mac build** and reply with evidence.",
    );
    let original_id = id(&original).to_owned();
    assert_eq!(original["delivery"], "published");
    let mac = journey.clone_agent("mac-agent", "MacAgent");
    let discovered = mac.ok(&["channel", "list", "--unread"]);
    let channels = discovered["channels"].as_array().unwrap();
    assert_eq!(
        channels.len(),
        1,
        "Mac discovers its channel without an ID relay"
    );
    assert_eq!(channels[0]["id"], channel);
    assert_eq!(channels[0]["unread"], 1);
    let collision = mac.output(&[
        "channel",
        "create",
        "agentui-build",
        "--participant",
        "LinuxCoordinator",
        "--issue",
        id(&epic),
    ]);
    assert!(
        !collision.status.success(),
        "a competing creator cannot replace an existing channel"
    );
    let discovered_channel = channels[0]["id"].as_str().unwrap();
    let first_read = mac.read(discovered_channel, true);
    assert!(contains(&first_read, &original_id));
    assert!(
        contains(&mac.read(&channel, true), &original_id),
        "inspection must not acknowledge"
    );
    let evidence_file = mac.home.join("evidence.md");
    fs::write(
        &evidence_file,
        "# Mac build evidence\n\nThe executable started and its check passed.\n",
    )
    .unwrap();
    let evidence = mac.ok(&[
        "handoff",
        "create",
        "--file",
        evidence_file.to_str().unwrap(),
        "--issue",
        id(&epic),
    ]);
    let commit = git(&mac.hub(), &["rev-parse", "HEAD"]);
    let commit_reference = format!("commit:{commit}");
    let handoff_reference = format!("[[{}]]", id(&evidence));
    let reply = mac.ok(&[
        "channel",
        "send",
        &channel,
        "--key",
        "mac-build-result",
        "--description",
        "Mac build checked. See the evidence.",
        "--reply-to",
        &original_id,
        "--to",
        "LinuxCoordinator",
        "--reference",
        &commit_reference,
        "--reference",
        &handoff_reference,
        "--session",
        "mac-session-1",
    ]);
    let reply_id = id(&reply).to_owned();
    let coordinator_inbox = coordinator.read(&channel, true);
    let reply_message = messages(&coordinator_inbox)
        .iter()
        .find(|m| m["id"] == reply_id)
        .unwrap();
    assert_eq!(reply_message["reply_to"], original_id);
    assert!(
        reply_message["references"]
            .as_array()
            .unwrap()
            .contains(&Value::String(commit_reference))
    );
    assert!(
        reply_message["references"]
            .as_array()
            .unwrap()
            .contains(&Value::String(handoff_reference))
    );
    assert!(
        contains(&coordinator.read(&channel, false), &original_id),
        "full history retains reply context"
    );
    mac.ok(&["channel", "ack", &channel, "--message", &original_id]);
    let untouched_prompt = mac.ok(&["prompt", "show", id(&prompt)]);
    assert_eq!(
        untouched_prompt["status"], "open",
        "channel acknowledgement must not claim or complete a prompt"
    );
    assert!(!contains(&mac.read(&channel, true), &original_id));
    let reviewer = journey.clone_agent("third-participant", "Reviewer");
    reviewer.ok(&["channel", "join", &channel]);
    let reviewer_unread = reviewer.read(&channel, true);
    assert!(
        contains(&reviewer_unread, &original_id),
        "a new member receives historical broadcasts"
    );
    assert!(
        !contains(&reviewer_unread, &reply_id),
        "a directed message is unread only for its recipients"
    );
    assert_eq!(
        reviewer_unread["cursor"]["seen"].as_array().unwrap().len(),
        1,
        "an unread cursor records only returned messages"
    );
    let unseen_history = reviewer.ok(&[
        "--no-fetch",
        "channel",
        "read",
        &channel,
        "--cursor",
        &reviewer_unread["cursor"].to_string(),
    ]);
    assert!(
        contains(&unseen_history, &reply_id),
        "filtering unread messages must not consume other history through the cursor"
    );
    assert!(
        contains(&reviewer.read(&channel, false), &reply_id),
        "addressing is not history access control"
    );
    reviewer.ok(&["channel", "ack", &channel, "--message", &original_id]);
    assert!(
        contains(&coordinator.read(&channel, true), &reply_id),
        "one actor's acknowledgement does not clear another actor's inbox"
    );
    let resumed_mac = journey.clone_agent("restarted-mac-on-another-machine", "MacAgent");
    assert!(
        !contains(&resumed_mac.read(&channel, true), &original_id),
        "acknowledgements survive restart and machine change"
    );
    let notice = coordinator.send(
        &channel,
        "restart-notice",
        "MacAgent must still discover new work after restarting.",
    );
    assert!(contains(&resumed_mac.read(&channel, true), id(&notice)));
    assert!(
        contains(&mac.read(&channel, true), id(&notice)),
        "two sessions share identity without destructive reads"
    );
    resumed_mac.ok(&["channel", "ack", &channel, "--message", id(&notice)]);
    assert!(
        !contains(&mac.read(&channel, true), id(&notice)),
        "two sessions intentionally share explicit actor acknowledgements"
    );

    let offline = mac.ok(&[
        "--no-sync",
        "channel",
        "send",
        &channel,
        "--key",
        "late-offline",
        "--description",
        "Written offline before the cursor was saved.",
    ]);
    assert_eq!(offline["pushed"], false);
    assert_eq!(offline["delivery"], "local");
    let saved_cursor = coordinator.read(&channel, false)["cursor"].clone();
    assert!(!contains(&coordinator.read(&channel, false), id(&offline)));
    mac.sync();
    coordinator.sync();
    let late = coordinator.ok(&[
        "--no-fetch",
        "channel",
        "read",
        &channel,
        "--cursor",
        &saved_cursor.to_string(),
    ]);
    assert!(
        contains(&late, id(&offline)),
        "a cursor must not skip a late-arriving older offline message"
    );
    let resumed = coordinator.ok(&[
        "--no-fetch",
        "channel",
        "read",
        &channel,
        "--cursor",
        &late["cursor"].to_string(),
    ]);
    assert!(
        messages(&resumed).is_empty(),
        "the returned cursor resumes without replaying observed messages"
    );

    let solo = coordinator.ok(&["channel", "create", "coordinator-notebook"]);
    coordinator.send(
        id(&solo),
        "personal-note",
        "A single participant can retain a conversation.",
    );
    assert_eq!(messages(&coordinator.read(id(&solo), false)).len(), 1);
    let name_hits = coordinator.ok(&["--no-fetch", "search", "agentui-build"]);
    assert!(
        name_hits
            .as_array()
            .unwrap()
            .iter()
            .any(|hit| hit["Kind"] == "channel" && hit["ID"] == channel)
    );
    let message_hits = coordinator.ok(&["--no-fetch", "search", "Please inspect"]);
    assert!(
        message_hits
            .as_array()
            .unwrap()
            .iter()
            .any(|hit| hit["Kind"] == "channel_message" && hit["ID"] == original_id)
    );
    let linked_issue = coordinator.ok(&["--no-fetch", "show", id(&epic)]);
    assert!(
        linked_issue["backlinks"]
            .as_array()
            .unwrap()
            .iter()
            .any(|link| link["from"] == channel),
        "the linked epic discovers its conversation through backlinks"
    );
    assert_eq!(
        coordinator.ok(&["--no-fetch", "doctor"])["ok"],
        true,
        "multiple channels and valid receipts leave doctor clean"
    );
    let bad_receipt = coordinator
        .hub()
        .join("projects/agentui/channels")
        .join(&channel)
        .join("acks")
        .join(beans::domain::channel::digest(&["Reviewer"]))
        .join("invalid.md");
    fs::create_dir_all(bad_receipt.parent().unwrap()).unwrap();
    fs::write(&bad_receipt, "---\ninvalid receipt\n---\n").unwrap();
    let doctor = coordinator.output(&["--no-fetch", "doctor"]);
    assert!(
        !doctor.status.success(),
        "doctor must diagnose malformed acknowledgement records"
    );
    let diagnostic: Value = serde_json::from_slice(&doctor.stdout).unwrap();
    assert_eq!(diagnostic["ok"], false);
    assert!(!diagnostic["warnings"].as_array().unwrap().is_empty());
    let healthy = coordinator.ok(&["--no-fetch", "channel", "list"]);
    assert_eq!(healthy["channels"].as_array().unwrap().len(), 1);
    assert_eq!(healthy["channels"][0]["id"], id(&solo));
    assert_eq!(healthy["warnings"].as_array().unwrap().len(), 1);
    fs::write(
        coordinator.hub().join("projects/agentui/channels/notes.md"),
        "incidental notes",
    )
    .unwrap();
    let continued = coordinator.send(
        id(&solo),
        "after-sibling-corruption",
        "Healthy conversations still deliver.",
    );
    assert_eq!(continued["pushed"], true);
    let healthy_read = coordinator.ok(&["--no-fetch", "channel", "read", id(&solo)]);
    assert!(contains(&healthy_read, id(&continued)));
    assert_eq!(healthy_read["warnings"].as_array().unwrap().len(), 1);
}

#[test]
fn concurrent_sends_and_interrupted_publication_keep_exactly_one_message_per_key() {
    let journey = Journey::new();
    let coordinator = journey.agent("linux-coordinator", "LinuxCoordinator");
    assert!(
        coordinator
            .output(&["init", journey.remote().to_str().unwrap()])
            .status
            .success()
    );
    let created = coordinator.ok(&[
        "channel",
        "create",
        "concurrent-build",
        "--participant",
        "MacAgent",
    ]);
    let channel = id(&created).to_owned();
    let mac = journey.clone_agent("mac-agent", "MacAgent");
    let gate = journey.0.join("release-pushes");
    for (agent, name) in [(&coordinator, "linux"), (&mac, "mac")] {
        hook(
            &agent.hub().join(".git/hooks/pre-push"),
            &format!(
                "touch '{}'\ni=0\nwhile test ! -f '{}'; do\n i=$((i+1))\n test \"$i\" -lt 400 || exit 97\n sleep 0.025\ndone\nexit 0",
                journey.0.join(format!("{name}.ready")).display(),
                gate.display()
            ),
        );
    }
    let linux_send = coordinator.spawn(&[
        "channel",
        "send",
        &channel,
        "--key",
        "linux-concurrent",
        "--description",
        "Linux concurrent message",
    ]);
    let mac_send = mac.spawn(&[
        "channel",
        "send",
        &channel,
        "--key",
        "mac-concurrent",
        "--description",
        "Mac concurrent message",
    ]);
    let deadline = Instant::now() + Duration::from_secs(15);
    while !(journey.0.join("linux.ready").exists() && journey.0.join("mac.ready").exists()) {
        assert!(
            Instant::now() < deadline,
            "both independent sends must reach publication"
        );
        thread::sleep(Duration::from_millis(10));
    }
    fs::write(&gate, "release").unwrap();
    let linux = parse_success(finish(linux_send));
    let mac_result = parse_success(finish(mac_send));
    let all = coordinator.read(&channel, false);
    assert!(contains(&all, id(&linux)));
    assert!(contains(&all, id(&mac_result)));
    assert_eq!(
        messages(&all).len(),
        2,
        "concurrent publications retain both messages"
    );
    fs::remove_file(coordinator.hub().join(".git/hooks/pre-push")).unwrap();
    fs::remove_file(mac.hub().join(".git/hooks/pre-push")).unwrap();
    let reject = journey.remote().join("hooks/pre-receive");
    hook(
        &reject,
        "echo 'simulated publication interruption' >&2\nexit 1",
    );
    let interrupted = coordinator.output(&[
        "channel",
        "send",
        &channel,
        "--key",
        "interrupted-key",
        "--description",
        "Retain this message across uncertain delivery.",
    ]);
    assert!(
        !interrupted.status.success(),
        "publication failure must not be reported as success"
    );
    let uncertain: Value = serde_json::from_slice(&interrupted.stdout).unwrap();
    assert_eq!(uncertain["delivery"], "uncertain");
    assert_eq!(uncertain["key"], "interrupted-key");
    let retained = coordinator.ok(&["--no-fetch", "channel", "read", &channel]);
    assert_eq!(
        messages(&retained).len(),
        3,
        "the local committed message survives a failed push"
    );
    fs::remove_file(reject).unwrap();
    let retried = coordinator.send(
        &channel,
        "interrupted-key",
        "Retain this message across uncertain delivery.",
    );
    assert_eq!(retried["pushed"], true);
    let published = mac.read(&channel, false);
    assert_eq!(
        messages(&published).len(),
        3,
        "retry must publish the original message without duplication"
    );
    assert!(contains(&published, id(&retried)));
    let repeated = coordinator.send(
        &channel,
        "interrupted-key",
        "Retain this message across uncertain delivery.",
    );
    assert_eq!(id(&retried), id(&repeated));
    let changed = coordinator.output(&[
        "channel",
        "send",
        &channel,
        "--key",
        "interrupted-key",
        "--description",
        "Different intent must be rejected.",
    ]);
    assert!(!changed.status.success());
    let rejected: Value = serde_json::from_slice(&changed.stdout).unwrap();
    assert_eq!(rejected["delivery"], "rejected");
    assert!(rejected.get("retry").is_none());
    assert_eq!(messages(&coordinator.read(&channel, false)).len(), 3);
}

#[test]
fn wait_fetches_remote_messages_and_distinguishes_timeout_from_network_failure() {
    let journey = Journey::new();
    let coordinator = journey.agent("linux-coordinator", "LinuxCoordinator");
    assert!(
        coordinator
            .output(&["init", journey.remote().to_str().unwrap()])
            .status
            .success()
    );
    let created = coordinator.ok(&[
        "channel",
        "create",
        "wait-build",
        "--participant",
        "MacAgent",
    ]);
    let channel = id(&created).to_owned();
    let mac = journey.clone_agent("mac-agent", "MacAgent");
    let before = mac.read(&channel, false);
    let cursor = before["cursor"].to_string();
    let waiting = mac.spawn(&[
        "channel",
        "wait",
        &channel,
        "--cursor",
        &cursor,
        "--timeout",
        "10",
        "--interval",
        "1",
    ]);
    let sent = coordinator.send(
        &channel,
        "remote-wait-arrival",
        "This message arrives through the remote, not a local file write.",
    );
    let arrived = parse_success(finish(waiting));
    assert_eq!(arrived["status"], "messages");
    assert!(contains(&arrived, id(&sent)));
    let after_cursor = arrived["cursor"].to_string();
    let timeout = mac.ok(&[
        "channel",
        "wait",
        &channel,
        "--cursor",
        &after_cursor,
        "--timeout",
        "1",
        "--interval",
        "1",
    ]);
    assert_eq!(timeout["status"], "timeout");
    assert!(messages(&timeout).is_empty());
    let tools = journey.0.join("stalled-tools");
    fs::create_dir_all(&tools).unwrap();
    let entered = journey.0.join("stalled-fetch-entered");
    hook(
        &tools.join("git"),
        &format!(
            "for arg in \"$@\"; do\n if test \"$arg\" = fetch; then touch '{}'; sleep 10; fi\ndone\nexec /usr/bin/git \"$@\"",
            entered.display()
        ),
    );
    let started = Instant::now();
    let stalled = finish(
        mac.command(&[
            "channel",
            "wait",
            &channel,
            "--cursor",
            &after_cursor,
            "--timeout",
            "1",
            "--interval",
            "1",
        ])
        .env("PATH", format!("{}:/usr/bin:/bin", tools.display()))
        .spawn()
        .unwrap(),
    );
    assert!(
        entered.exists(),
        "the wait test must exercise the stalled fetch"
    );
    assert!(
        started.elapsed() < Duration::from_secs(4),
        "the requested wait deadline bounds a stalled Git transport plus cleanup"
    );
    assert!(!stalled.status.success());
    let stalled_result: Value = serde_json::from_slice(&stalled.stdout).unwrap();
    assert_eq!(stalled_result["status"], "sync_error");
    assert_eq!(stalled_result["cursor"], arrived["cursor"]);
    let unseen = coordinator.send(
        &channel,
        "unseen-before-offline-wait",
        "An earlier fetch is not observation.",
    );
    mac.sync();
    git(
        &mac.hub(),
        &[
            "remote",
            "set-url",
            "origin",
            journey.0.join("missing.git").to_str().unwrap(),
        ],
    );
    let failed = mac.output(&[
        "channel",
        "wait",
        &channel,
        "--cursor",
        &after_cursor,
        "--timeout",
        "2",
        "--interval",
        "1",
    ]);
    assert!(
        !failed.status.success(),
        "network failure must have a nonzero exit"
    );
    let diagnostic: Value = serde_json::from_slice(&failed.stdout).unwrap();
    assert_eq!(diagnostic["status"], "sync_error");
    assert_eq!(
        diagnostic["cursor"], arrived["cursor"],
        "a failed refresh retains the resumable cursor"
    );
    assert!(messages(&diagnostic).is_empty());
    git(
        &mac.hub(),
        &[
            "remote",
            "set-url",
            "origin",
            journey.remote().to_str().unwrap(),
        ],
    );
    let resumed = mac.ok(&[
        "channel",
        "wait",
        &channel,
        "--cursor",
        &diagnostic["cursor"].to_string(),
        "--timeout",
        "2",
        "--interval",
        "1",
    ]);
    assert!(
        contains(&resumed, id(&unseen)),
        "a failed wait cannot consume a locally available unseen arrival"
    );
}

#[test]
fn stale_clone_can_send_by_name_after_remote_channel_creation() {
    let journey = Journey::new();
    let coordinator = journey.agent("linux-coordinator", "LinuxCoordinator");
    assert!(
        coordinator
            .output(&["init", journey.remote().to_str().unwrap()])
            .status
            .success()
    );
    let mac = journey.clone_agent("stale-mac", "MacAgent");
    coordinator.ok(&[
        "channel",
        "create",
        "new-remote-channel",
        "--participant",
        "MacAgent",
    ]);
    let sent = mac.send(
        "new-remote-channel",
        "stale-send",
        "Resolve under the mutation lock after refreshing Git.",
    );
    assert_eq!(sent["pushed"], true);
    assert!(contains(
        &coordinator.read("new-remote-channel", false),
        id(&sent)
    ));
}
