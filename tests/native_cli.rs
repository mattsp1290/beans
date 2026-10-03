//! Real-Git native journey and transaction regression gates. No delegate binary.
use beans::{
    domain::frontmatter::Error,
    gitops::{Hub, Operation, write_file},
};
use std::{
    fs,
    os::{fd::AsRawFd, unix::fs::PermissionsExt},
    path::{Path, PathBuf},
    process::{Command, Output},
    time::Duration,
};
struct Sandbox(PathBuf);
impl Sandbox {
    fn new() -> Self {
        let mut random = [0; 8];
        getrandom::fill(&mut random).unwrap();
        let p = std::env::temp_dir().join(format!(
            "bn-native-{}-{}",
            std::process::id(),
            u64::from_ne_bytes(random)
        ));
        fs::create_dir_all(&p).unwrap();
        Self(p)
    }
    fn path(&self, name: &str) -> PathBuf {
        self.0.join(name)
    }
    fn git(&self, dir: &Path, args: &[&str]) -> String {
        git(dir, args)
    }
    fn remote(&self, seeded: bool) -> PathBuf {
        let remote = self.path("remote.git");
        fs::create_dir_all(&remote).unwrap();
        git(&remote, &["init", "--bare", "--initial-branch=main"]);
        if seeded {
            let seed = self.path("seed");
            fs::create_dir_all(&seed).unwrap();
            git(&seed, &["init", "--initial-branch=main"]);
            fs::write(seed.join("data.txt"), "base\n").unwrap();
            git(&seed, &["add", "-A"]);
            git(&seed, &["commit", "-m", "seed"]);
            git(
                &seed,
                &["remote", "add", "origin", remote.to_str().unwrap()],
            );
            git(&seed, &["push", "origin", "main"]);
        }
        remote
    }
    fn clone_hub(&self, remote: &Path, name: &str) -> Hub {
        let dir = self.path(name);
        git(
            &self.0,
            &["clone", remote.to_str().unwrap(), dir.to_str().unwrap()],
        );
        Hub::new(
            dir,
            self.path(&format!("{name}-cache")),
            "main".into(),
            "Native Tester".into(),
            false,
            Duration::ZERO,
        )
        .unwrap()
    }
    fn cli(&self, args: &[&str]) -> Output {
        Command::new(env!("CARGO_BIN_EXE_bn"))
            .args(args)
            .env("BEANS_HOME", self.path("home"))
            .env("BEANS_PROJECT", "demo")
            .env("BN_ACTOR", "Native Tester")
            .env_remove("BEANS_HUB")
            .env_remove("BN_CONFIG")
            .current_dir(&self.0)
            .output()
            .unwrap()
    }
    fn ok(&self, args: &[&str]) -> String {
        let o = self.cli(args);
        assert!(
            o.status.success(),
            "{args:?}: {}",
            String::from_utf8_lossy(&o.stderr)
        );
        String::from_utf8(o.stdout).unwrap()
    }
}
impl Drop for Sandbox {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}
fn git(dir: &Path, args: &[&str]) -> String {
    let out = Command::new("git")
        .arg("-C")
        .arg(dir)
        .args(args)
        .env("GIT_TERMINAL_PROMPT", "0")
        .env("GIT_AUTHOR_NAME", "Tester")
        .env("GIT_AUTHOR_EMAIL", "test@example.com")
        .env("GIT_COMMITTER_NAME", "Tester")
        .env("GIT_COMMITTER_EMAIL", "test@example.com")
        .output()
        .unwrap();
    assert!(
        out.status.success(),
        "git {args:?}: {}",
        String::from_utf8_lossy(&out.stderr)
    );
    String::from_utf8_lossy(&out.stdout).trim().into()
}
fn hook(hub: &Hub, name: &str, text: &str) {
    let p = hub.dir.join(".git/hooks").join(name);
    fs::write(&p, format!("#!/bin/sh\nset -eu\n{text}\n")).unwrap();
    fs::set_permissions(p, fs::Permissions::from_mode(0o755)).unwrap();
}
struct Append {
    line: &'static str,
    calls: usize,
    fail: bool,
}
impl Append {
    fn new(line: &'static str) -> Self {
        Self {
            line,
            calls: 0,
            fail: false,
        }
    }
}
impl Operation for Append {
    fn subject(&self) -> String {
        "bn: note same subject".into()
    }
    fn apply(&mut self, hub: &Path) -> Result<Vec<PathBuf>, Error> {
        self.calls += 1;
        let path = hub.join("data.txt");
        let mut text = fs::read_to_string(&path).map_err(|e| Error::new(e.to_string()))?;
        if !text.lines().any(|l| l == self.line) {
            text.push_str(self.line);
            text.push('\n');
            write_file(&path, text.as_bytes())?;
        }
        if self.fail {
            return Err(Error::new("injected after partial write".into()));
        }
        Ok(vec!["data.txt".into()])
    }
}
#[test]
fn native_first_cli_journey_second_clone_sees_closed_full_history() {
    for seeded in [false, true] {
        let s = Sandbox::new();
        let remote = s.remote(seeded);
        s.ok(&["init", remote.to_str().unwrap()]);
        let out = s.ok(&[
            "--json",
            "create",
            "Native journey",
            "-d",
            "Preserve this description",
        ]);
        let created: serde_json::Value = serde_json::from_str(&out).unwrap();
        let id = created["id"].as_str().unwrap();
        assert!(s.ok(&["ready"]).contains(id));
        assert!(s.ok(&["list"]).contains(id));
        s.ok(&["update", id, "--claim"]);
        assert!(s.ok(&["show", id]).contains("in_progress"));
        s.ok(&["note", id, "native note"]);
        s.ok(&["close", id, "-r", "native completion"]);
        s.ok(&["sync"]);
        let status: serde_json::Value = serde_json::from_str(&s.ok(&["status"])).unwrap();
        assert_eq!(status["ahead"], 0);
        assert!(!s.ok(&["ready"]).contains(id));
        let other = s.clone_hub(&remote, "second");
        let index = beans::vault::Index::load(&other.dir).unwrap();
        let issue = index.issue_by_id(id.as_bytes()).unwrap();
        assert_eq!(issue.metadata.status, "closed");
        assert_eq!(issue.log.len(), 4);
        assert!(
            issue
                .description
                .as_bytes()
                .starts_with(b"Preserve this description")
        );
        let log = s.git(&other.dir, &["log", "--format=%s"]);
        for verb in ["create", "update", "note", "close"] {
            assert!(log.contains(&format!("bn: {verb} {id}")));
        }
    }
}
#[test]
fn hand_edits_offline_no_sync_and_owned_temps_are_preserved_or_recovered() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    let mut hub = s.clone_hub(&remote, "hub");
    hub.no_sync = true;
    fs::write(hub.dir.join("user.tmp"), "user content").unwrap();
    fs::write(hub.dir.join(".bn-write-orphan"), "orphan").unwrap();
    fs::write(hub.dir.join("plan.md.tmp"), "orphan").unwrap();
    fs::write(hub.dir.join("data.txt"), "base\nhand edit\n").unwrap();
    hub.mutate(&mut Append::new("note")).unwrap();
    assert_eq!(
        hub.git(&["rev-list", "--count", "origin/main..HEAD"])
            .unwrap(),
        "2"
    );
    assert!(hub.dir.join("user.tmp").exists());
    assert!(hub.dir.join(".bn-write-orphan").exists());
    assert!(hub.dir.join("plan.md.tmp").exists());
    hub.no_sync = false;
    hub.sync().unwrap();
    assert_eq!(
        hub.git(&["rev-list", "--count", "origin/main..HEAD"])
            .unwrap(),
        "0"
    );
    hub.git(&["remote", "set-url", "origin", "/nonexistent/bn-test-remote"])
        .unwrap();
    let out = hub.mutate(&mut Append::new("offline note")).unwrap_err();
    assert!(out.to_string().contains("committed locally"));
    assert!(hub.state_path().join("op-journal.json").exists());
    hub.git(&["remote", "set-url", "origin", remote.to_str().unwrap()])
        .unwrap();
    hub.sync().unwrap();
    assert!(!hub.state_path().join("op-journal.json").exists());
    assert!(
        hub.git(&["log", "--format=%s"])
            .unwrap()
            .contains("bn: hand edits")
    );
}
#[test]
fn partial_apply_stage_and_commit_errors_keep_recovery_and_sync_recovers() {
    for failure in ["apply", "stage", "commit"] {
        let s = Sandbox::new();
        let remote = s.remote(true);
        let hub = s.clone_hub(&remote, "hub");
        let mut hub = hub;
        hub.no_sync = failure == "apply";
        let mut op = Append::new("recoverable note");
        match failure {
            "apply" => op.fail = true,
            "stage" => {
                fs::write(hub.dir.join(".git/index.lock"), "busy").unwrap();
            }
            "commit" => hook(&hub, "pre-commit", "exit 1"),
            _ => unreachable!(),
        }
        let result = hub.mutate(&mut op);
        assert!(result.is_err(), "{failure}");
        assert!(hub.state_path().join("op-journal.json").exists());
        assert!(
            fs::read_to_string(hub.dir.join("data.txt"))
                .unwrap()
                .contains("recoverable note")
        );
        let _ = fs::remove_file(hub.dir.join(".git/index.lock"));
        let _ = fs::remove_file(hub.dir.join(".git/hooks/pre-commit"));
        hub.sync().unwrap();
        assert!(!hub.state_path().join("op-journal.json").exists());
        assert!(
            hub.git(&["log", "--format=%s"])
                .unwrap()
                .contains("bn: recovered partial operation")
        );
    }
}
#[test]
fn lock_and_interrupted_or_detached_checkout_do_not_mutate_or_fetch() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    let mut hub = s.clone_hub(&remote, "hub");
    hub.executor.policy.lock_timeout = Duration::from_millis(80);
    fs::create_dir_all(hub.state_path()).unwrap();
    let file = fs::File::create(hub.state_path().join("hub.lock")).unwrap();
    assert_eq!(
        unsafe { libc::flock(file.as_raw_fd(), libc::LOCK_EX | libc::LOCK_NB) },
        0
    );
    let mut op = Append::new("forbidden");
    assert!(
        hub.mutate(&mut op)
            .unwrap_err()
            .to_string()
            .contains("lock")
    );
    assert_eq!(op.calls, 0);
    assert!(hub.refresh().is_err());
    assert!(!hub.state_path().join("last-fetch-attempt").exists());
    drop(file);
    for marker in ["rebase-merge", "rebase-apply", "MERGE_HEAD"] {
        let p = hub.dir.join(".git").join(marker);
        fs::write(&p, "interrupted").unwrap();
        assert!(hub.mutate(&mut op).is_err());
        fs::remove_file(p).unwrap();
    }
    hub.git(&["checkout", "--detach"]).unwrap();
    assert!(
        hub.mutate(&mut op)
            .unwrap_err()
            .to_string()
            .contains("detached")
    );
    assert_eq!(op.calls, 0);
}
fn race_hook(s: &Sandbox, hub: &Hub, competitor: &Hub, conflicting: bool, repeat: bool) {
    // All paths are generated under temp_dir and contain no shell metacharacters.
    let text = format!(
        "{}\nprintf '{}\\n' >> '{}/data.txt'\ngit -C '{}' add -A\ngit -C '{}' -c user.name=Race -c user.email=race@example.com commit -qm race\ngit -C '{}' push -q origin main",
        if repeat {
            String::new()
        } else {
            format!(
                "test ! -f '{}' || exit 0\ntouch '{}'",
                s.path("raced").display(),
                s.path("raced").display()
            )
        },
        if conflicting { "competing note" } else { "" },
        competitor.dir.display(),
        competitor.dir.display(),
        competitor.dir.display(),
        competitor.dir.display()
    );
    hook(hub, "pre-push", &text);
}
#[test]
fn push_conflict_replays_owned_head_without_duplicating_notes() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    let hub = s.clone_hub(&remote, "hub");
    let competitor = s.clone_hub(&remote, "competitor");
    race_hook(&s, &hub, &competitor, true, false);
    let mut op = Append::new("one frozen note");
    let result = hub.mutate(&mut op).unwrap();
    assert!(result.pushed);
    assert_eq!(op.calls, 2);
    let text = fs::read_to_string(hub.dir.join("data.txt")).unwrap();
    assert!(text.contains("competing note"));
    assert_eq!(text.matches("one frozen note").count(), 1);
    assert!(
        hub.git(&["log", "-1", "--format=%(trailers:key=Bn-Run,valueonly)"])
            .unwrap()
            .len()
            == 32
    );
}
#[test]
fn conflict_preserves_unowned_history_even_with_matching_subject() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    let hub = s.clone_hub(&remote, "hub");
    let competitor = s.clone_hub(&remote, "competitor");
    fs::write(
        hub.dir.join("data.txt"),
        "base\nunowned subject collision\n",
    )
    .unwrap();
    hub.git(&["add", "-A"]).unwrap();
    git(
        &hub.dir,
        &[
            "commit",
            "-m",
            "bn: note same subject",
            "-m",
            "Bn-Run: forged",
        ],
    );
    let unowned = hub.git(&["rev-parse", "HEAD"]).unwrap();
    race_hook(&s, &hub, &competitor, true, false);
    let mut op = Append::new("owned new note");
    assert!(hub.mutate(&mut op).is_err());
    assert!(
        hub.git(&["merge-base", "--is-ancestor", &unowned, "HEAD"])
            .is_ok()
    );
    assert!(!hub.dir.join(".git/rebase-merge").exists());
    assert_eq!(op.calls, 1);
    assert!(hub.state_path().join("op-journal.json").exists());
}
#[test]
fn repeated_races_exhaust_three_pushes_and_preserve_last_commit() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    let hub = s.clone_hub(&remote, "hub");
    let competitor = s.clone_hub(&remote, "competitor");
    race_hook(&s, &hub, &competitor, true, true);
    let mut op = Append::new("retained note");
    let error = hub.mutate(&mut op).unwrap_err();
    assert!(error.to_string().contains("three"));
    assert_eq!(op.calls, 3);
    assert!(
        fs::read_to_string(hub.dir.join("data.txt"))
            .unwrap()
            .contains("retained note")
    );
    assert!(hub.state_path().join("op-journal.json").exists());
}
#[test]
fn sync_conflict_aborts_without_discarding_local_commit() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    let mut hub = s.clone_hub(&remote, "hub");
    let competitor = s.clone_hub(&remote, "competitor");
    hub.no_sync = true;
    hub.mutate(&mut Append::new("local note")).unwrap();
    let head = hub.git(&["rev-parse", "HEAD"]).unwrap();
    competitor.mutate(&mut Append::new("remote note")).unwrap();
    assert!(hub.sync().is_err());
    assert_eq!(hub.git(&["rev-parse", "HEAD"]).unwrap(), head);
    assert!(!hub.dir.join(".git/rebase-merge").exists());
}
#[test]
fn fetch_throttle_keeps_locked_reads_nonblocking_and_local_view_available() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    let mut hub = s.clone_hub(&remote, "hub");
    hub.throttle = Duration::from_secs(3600);
    hub.refresh().unwrap();
    let first = fs::read(hub.state_path().join("last-fetch-attempt")).unwrap();
    hub.git(&["remote", "set-url", "origin", "/nonexistent/bn-test-remote"])
        .unwrap();
    hub.refresh().unwrap();
    assert_eq!(
        fs::read(hub.state_path().join("last-fetch-attempt")).unwrap(),
        first
    );
}
#[test]
fn malformed_docs_fail_with_recoverable_state_and_existing_bytes_untouched() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    s.ok(&["init", remote.to_str().unwrap()]);
    let created: serde_json::Value =
        serde_json::from_str(&s.ok(&["--json", "create", "Malformed test"])).unwrap();
    let id = created["id"].as_str().unwrap();
    let hub = s.path("home/hub");
    let index = beans::vault::Index::load(&hub).unwrap();
    let note = index.resolve_issue_ref(id.as_bytes()).1.unwrap();
    let path = hub.join(String::from_utf8_lossy(&note.graph.path).as_ref());
    let malformed = b"---\nid: broken\n---\nkeep authored body\n";
    fs::write(&path, malformed).unwrap();
    let out = s.cli(&["note", id, "must fail"]);
    assert!(!out.status.success());
    assert!(String::from_utf8_lossy(&out.stderr).contains("malformed"));
    assert_eq!(fs::read(path).unwrap(), malformed);
    assert!(s.path("home/.beans-state/hub/op-journal.json").exists());
}
#[test]
fn successful_rebase_and_dropped_operation_are_rederived_without_duplication() {
    for identical in [false, true] {
        let s = Sandbox::new();
        let remote = s.remote(true);
        let hub = s.clone_hub(&remote, "hub");
        let competitor = s.clone_hub(&remote, "competitor");
        let marker = s.path("raced");
        let file = if identical { "data.txt" } else { "other.txt" };
        let text = format!(
            "test ! -f '{}' || exit 0\ntouch '{}'\nprintf '{}\\n' {} '{}/{}'\ngit -C '{}' add -A\ngit -C '{}' -c user.name=Race -c user.email=race@example.com commit -qm race\ngit -C '{}' push -q origin main",
            marker.display(),
            marker.display(),
            if identical {
                "same frozen note"
            } else {
                "unrelated remote change"
            },
            if identical { ">>" } else { ">" },
            competitor.dir.display(),
            file,
            competitor.dir.display(),
            competitor.dir.display(),
            competitor.dir.display()
        );
        fs::write(hub.dir.join("user-hand-edit.txt"), "authored before race").unwrap();
        hook(&hub, "pre-push", &text);
        let mut op = Append::new("same frozen note");
        assert!(hub.mutate(&mut op).unwrap().pushed);
        assert_eq!(
            fs::read_to_string(hub.dir.join("user-hand-edit.txt")).unwrap(),
            "authored before race"
        );
        assert!(
            hub.git(&["log", "--format=%s"])
                .unwrap()
                .contains("bn: hand edits")
        );
        assert_eq!(op.calls, 2);
        assert_eq!(
            fs::read_to_string(hub.dir.join("data.txt"))
                .unwrap()
                .matches("same frozen note")
                .count(),
            1
        );
        assert_eq!(
            hub.git(&["rev-list", "--count", "origin/main..HEAD"])
                .unwrap(),
            "0"
        );
    }
}
#[test]
fn stranded_previous_nonce_never_authorizes_discard_in_new_invocation() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    let mut hub = s.clone_hub(&remote, "hub");
    let competitor = s.clone_hub(&remote, "competitor");
    hub.no_sync = true;
    hub.mutate(&mut Append::new("previous invocation")).unwrap();
    let previous = hub.git(&["rev-parse", "HEAD"]).unwrap();
    hub.no_sync = false;
    race_hook(&s, &hub, &competitor, true, false);
    // Apply is unchanged: the only local commit is an old, genuine bn operation.
    assert!(hub.mutate(&mut Append::new("previous invocation")).is_err());
    assert_eq!(hub.git(&["rev-parse", "HEAD"]).unwrap(), previous);
}
#[test]
fn unchanged_close_update_and_identical_notes_have_distinct_invocation_semantics() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    s.ok(&["init", remote.to_str().unwrap()]);
    let created: serde_json::Value =
        serde_json::from_str(&s.ok(&["--json", "create", "Semantic test"])).unwrap();
    let id = created["id"].as_str().unwrap();
    s.ok(&["note", id, "repeat intentionally"]);
    s.ok(&["note", id, "repeat intentionally"]);
    let hub = s.path("home/hub");
    let index = beans::vault::Index::load(&hub).unwrap();
    assert_eq!(index.issue_by_id(id.as_bytes()).unwrap().log.len(), 3);
    let head = git(&hub, &["rev-parse", "HEAD"]);
    s.ok(&["update", id, "--title", "Semantic test"]);
    assert_eq!(git(&hub, &["rev-parse", "HEAD"]), head);
    for args in [
        vec!["update", id, "--title", ""],
        vec!["update", id, "--priority", "9"],
    ] {
        assert!(!s.cli(&args).status.success());
    }
    assert_eq!(git(&hub, &["rev-parse", "HEAD"]), head);
    s.ok(&["close", id, "-r", "completed"]);
    let head = git(&hub, &["rev-parse", "HEAD"]);
    s.ok(&["close", id, "-r", "completed"]);
    assert_eq!(git(&hub, &["rev-parse", "HEAD"]), head);
}
#[test]
fn native_branch_discovery_config_unknowns_and_overrides_survive_init() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    git(&s.path("seed"), &["branch", "-m", "trunk"]);
    git(&s.path("seed"), &["push", "origin", "trunk"]);
    git(&remote, &["symbolic-ref", "HEAD", "refs/heads/trunk"]);
    fs::create_dir_all(s.path("home")).unwrap();
    fs::write(
        s.path("home/config.toml"),
        "actor = 'Configured Actor'\ncustom = 'keep'\n[hub]\nextra = 42\n",
    )
    .unwrap();
    s.ok(&["init", remote.to_str().unwrap()]);
    let config: toml::Value =
        toml::from_str(&fs::read_to_string(s.path("home/config.toml")).unwrap()).unwrap();
    assert_eq!(config["custom"].as_str(), Some("keep"));
    assert_eq!(config["hub"]["extra"].as_integer(), Some(42));
    assert_eq!(config["hub"]["branch"].as_str(), Some("trunk"));
    let workflow = s.path("workflow.yaml");
    fs::write(&workflow, "workflow:\n  statuses: [queued, working, completed]\n  default: queued\n  active: [queued]\n  terminal: [completed]\n").unwrap();
    let out = Command::new(env!("CARGO_BIN_EXE_bn"))
        .args([
            "--hub",
            s.path("home/hub").to_str().unwrap(),
            "--project",
            "custom",
            "--json",
            "create",
            "Override test",
        ])
        .env("BEANS_HOME", s.path("home"))
        .env("BN_CONFIG", &workflow)
        .env_remove("BN_ACTOR")
        .current_dir(&s.0)
        .output()
        .unwrap();
    assert!(
        out.status.success(),
        "{}",
        String::from_utf8_lossy(&out.stderr)
    );
    let created: serde_json::Value = serde_json::from_slice(&out.stdout).unwrap();
    let index = beans::vault::Index::load_with_options(
        &s.path("home/hub"),
        beans::vault::LoadOptions {
            explicit_workflow: Some(workflow),
        },
    )
    .unwrap();
    let issue = index
        .issue_by_id(created["id"].as_str().unwrap().as_bytes())
        .unwrap();
    assert_eq!(issue.metadata.status, "queued");
    assert_eq!(issue.log[0].actor.to_string(), "Configured-Actor");
}
#[test]
fn failed_create_retains_partial_scaffolding_and_locked_cli_reads_local_snapshot() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    s.ok(&["init", remote.to_str().unwrap()]);
    let hub = s.path("home/hub");
    fs::create_dir_all(hub.join("projects/demo")).unwrap();
    fs::write(hub.join("projects/demo/issues"), "hand-written obstruction").unwrap();
    let out = s.cli(&["create", "Write must fail"]);
    assert!(!out.status.success());
    assert!(s.path("home/.beans-state/hub/op-journal.json").exists());
    assert!(!hub.join("projects/demo/beans.toml").exists());
    assert_eq!(
        fs::read_to_string(hub.join("projects/demo/issues")).unwrap(),
        "hand-written obstruction"
    );
    fs::remove_file(hub.join("projects/demo/issues")).unwrap();
    s.ok(&["sync"]);
    let created: serde_json::Value =
        serde_json::from_str(&s.ok(&["--json", "create", "Read while locked"])).unwrap();
    let file = fs::File::create(s.path("home/.beans-state/hub/hub.lock")).unwrap();
    assert_eq!(
        unsafe { libc::flock(file.as_raw_fd(), libc::LOCK_EX | libc::LOCK_NB) },
        0
    );
    let start = std::time::Instant::now();
    let text = s.ok(&["list"]);
    assert!(text.contains(created["id"].as_str().unwrap()));
    assert!(start.elapsed() < Duration::from_secs(3));
}
#[test]
fn snapshot_loader_never_recovers_or_removes_plan_artifacts() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    let hub = s.clone_hub(&remote, "hub");
    fs::create_dir_all(hub.dir.join("projects/demo/plans/.plan.backup")).unwrap();
    fs::write(
        hub.dir.join("projects/demo/plans/.plan.backup/user.md"),
        "recovery evidence",
    )
    .unwrap();
    beans::vault::Index::load_snapshot(&hub.dir, Default::default()).unwrap();
    assert!(
        hub.dir
            .join("projects/demo/plans/.plan.backup/user.md")
            .exists()
    );
    assert!(!hub.dir.join("projects/demo/plans/plan").exists());
}
#[test]
fn next_idempotent_mutation_pushes_stranded_commits_with_no_new_log() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    let mut hub = s.clone_hub(&remote, "hub");
    hub.no_sync = true;
    hub.mutate(&mut Append::new("stranded note")).unwrap();
    let before = hub.git(&["rev-parse", "HEAD"]).unwrap();
    hub.no_sync = false;
    assert!(
        hub.mutate(&mut Append::new("stranded note"))
            .unwrap()
            .pushed
    );
    assert_eq!(hub.git(&["rev-parse", "HEAD"]).unwrap(), before);
    assert_eq!(
        hub.git(&["rev-list", "--count", "origin/main..HEAD"])
            .unwrap(),
        "0"
    );
}
#[test]
fn native_issue_note_replay_keeps_both_writers_and_frozen_primary_log() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    s.ok(&["init", remote.to_str().unwrap()]);
    let created: serde_json::Value =
        serde_json::from_str(&s.ok(&["--json", "create", "Native race"])).unwrap();
    let id = created["id"].as_str().unwrap();
    let hub = Hub::new(
        s.path("home/hub"),
        s.path("home/cache"),
        "main".into(),
        "Native Tester".into(),
        false,
        Duration::ZERO,
    )
    .unwrap();
    let competitor = s.clone_hub(&remote, "competitor");
    let marker = s.path("raced");
    let text = format!(
        "test ! -f '{}' || exit 0\ntouch '{}'\nBEANS_HOME='{}' BN_ACTOR='Other Writer' '{}' --hub '{}' --project demo note '{}' 'competing native note'",
        marker.display(),
        marker.display(),
        s.path("other-home").display(),
        env!("CARGO_BIN_EXE_bn"),
        competitor.dir.display(),
        id
    );
    hook(&hub, "pre-push", &text);
    let resolved = beans::vault::resolve(
        &hub.dir,
        beans::vault::ResolveOptions {
            flag_project: b"demo",
            cwd: Some(&s.0),
            write: true,
            ..Default::default()
        },
    )
    .unwrap();
    let mut op = beans::ops::IssueMutation::new(
        resolved,
        id.into(),
        beans::ops::IssueChange::Note("primary frozen native note".into()),
        "Native Tester".into(),
        None,
    );
    assert!(hub.mutate(&mut op).unwrap().pushed);
    let index = beans::vault::Index::load(&hub.dir).unwrap();
    let issue = index.issue_by_id(id.as_bytes()).unwrap();
    assert_eq!(issue.log.len(), 3);
    assert_eq!(
        issue
            .log
            .iter()
            .filter(|log| log.event.as_bytes() == b"primary frozen native note")
            .count(),
        1
    );
    assert_eq!(
        issue
            .log
            .iter()
            .filter(|log| log.event.as_bytes() == b"competing native note")
            .count(),
        1
    );
}
#[test]
fn cross_kind_id_collision_never_overwrites_request_or_creates_issue() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    s.ok(&["init", remote.to_str().unwrap()]);
    s.ok(&["create", "Create project"]);
    let dir = s.path("home/hub");
    let path = dir.join("projects/demo/requests/demo-r-c3d4.md");
    let request =
        include_str!("fixtures/hub/projects/alpha/requests/alpha-r-c3d4-contract-request.md")
            .replace("alpha", "demo");
    fs::write(&path, &request).unwrap();
    let hub = Hub::new(
        dir.clone(),
        s.path("home/cache"),
        "main".into(),
        "Tester".into(),
        true,
        Duration::ZERO,
    )
    .unwrap();
    let resolved = beans::vault::resolve(
        &dir,
        beans::vault::ResolveOptions {
            flag_project: b"demo",
            cwd: Some(&s.0),
            write: true,
            ..Default::default()
        },
    )
    .unwrap();
    let mut op = beans::ops::IssueMutation::new(
        resolved,
        String::new(),
        beans::ops::IssueChange::Create {
            title: "Collision".into(),
            kind: "task".into(),
            priority: 2,
            description: None,
        },
        "Tester".into(),
        None,
    );
    op.id = "demo-r-c3d4".into();
    assert!(hub.mutate(&mut op).is_err());
    assert_eq!(fs::read_to_string(path).unwrap(), request);
    assert!(
        beans::vault::Index::load(&dir)
            .unwrap()
            .issue_by_id(b"demo-r-c3d4")
            .is_none()
    );
}
struct IdenticalNoteRace {
    primary: beans::ops::IssueMutation,
    other: beans::ops::IssueMutation,
    competitor: Hub,
    survived: Vec<bool>,
    fired: bool,
}
impl Operation for IdenticalNoteRace {
    fn subject(&self) -> String {
        self.primary.subject()
    }
    fn after_rebase(&mut self, present: bool) {
        self.survived.push(present);
        self.primary.after_rebase(present);
    }
    fn apply(&mut self, hub: &Path) -> Result<Vec<PathBuf>, Error> {
        let paths = self.primary.apply(hub)?;
        if !self.fired {
            self.fired = true;
            assert!(self.competitor.mutate(&mut self.other)?.pushed);
        }
        Ok(paths)
    }
}
#[test]
fn identical_same_second_native_notes_both_survive_dropped_patch_with_hand_edits() {
    for hand_edit in [false, true] {
        let s = Sandbox::new();
        let remote = s.remote(true);
        s.ok(&["init", remote.to_str().unwrap()]);
        let created: serde_json::Value =
            serde_json::from_str(&s.ok(&["--json", "create", "Exact identical race"])).unwrap();
        let id = created["id"].as_str().unwrap();
        let hub = Hub::new(
            s.path("home/hub"),
            s.path("home/cache"),
            "main".into(),
            "Same Writer".into(),
            false,
            Duration::ZERO,
        )
        .unwrap();
        let competitor = s.clone_hub(&remote, "competitor");
        let resolved = |dir: &Path| {
            beans::vault::resolve(
                dir,
                beans::vault::ResolveOptions {
                    flag_project: b"demo",
                    cwd: Some(&s.0),
                    write: true,
                    ..Default::default()
                },
            )
            .unwrap()
        };
        let at = time::OffsetDateTime::now_utc();
        let note = |dir: &Path| {
            beans::ops::IssueMutation::new_at(
                resolved(dir),
                id.into(),
                beans::ops::IssueChange::Note("identical same-second note".into()),
                "Same Writer".into(),
                None,
                at,
            )
        };
        let primary = note(&hub.dir);
        let other = note(&competitor.dir);
        if hand_edit {
            fs::write(hub.dir.join("authored.txt"), "retain this unowned edit").unwrap();
        }
        let mut race = IdenticalNoteRace {
            primary,
            other,
            competitor,
            fired: false,
            survived: Vec::new(),
        };
        assert!(hub.mutate(&mut race).unwrap().pushed);
        assert_eq!(
            race.survived,
            [false],
            "identical competing patch must drop this run and trigger fresh append"
        );
        let index = beans::vault::Index::load_snapshot(&hub.dir, Default::default()).unwrap();
        let issue = index.issue_by_id(id.as_bytes()).unwrap();
        let notes: Vec<_> = issue
            .log
            .iter()
            .filter(|e| e.event.as_bytes() == b"identical same-second note")
            .collect();
        assert_eq!(notes.len(), 2);
        assert_eq!(
            notes[0].format_bytes().unwrap(),
            notes[1].format_bytes().unwrap()
        );
        if hand_edit {
            assert_eq!(
                fs::read_to_string(hub.dir.join("authored.txt")).unwrap(),
                "retain this unowned edit"
            );
        }
        assert_eq!(
            hub.git(&["rev-list", "--count", "origin/main..HEAD"])
                .unwrap(),
            "0"
        );
    }
}
fn contents(dir: &Path) -> Vec<(PathBuf, Vec<u8>)> {
    fn walk(root: &Path, dir: &Path, out: &mut Vec<(PathBuf, Vec<u8>)>) {
        for entry in fs::read_dir(dir).unwrap() {
            let e = entry.unwrap();
            if e.file_type().unwrap().is_dir() {
                walk(root, &e.path(), out);
            } else {
                out.push((
                    e.path().strip_prefix(root).unwrap().to_owned(),
                    fs::read(e.path()).unwrap(),
                ));
            }
        }
    }
    let mut out = Vec::new();
    walk(dir, dir, &mut out);
    out.sort();
    out
}
#[test]
fn symlinked_projects_project_and_issues_fail_before_any_external_write() {
    for ancestor in ["projects", "projects/demo", "projects/demo/issues"] {
        let s = Sandbox::new();
        let remote = s.remote(true);
        s.ok(&["init", remote.to_str().unwrap()]);
        let hub = s.path("home/hub");
        if ancestor.ends_with("issues") {
            s.ok(&["create", "Existing project"]);
            fs::remove_dir_all(hub.join(ancestor)).unwrap();
        }
        let external = s.path("external");
        fs::create_dir_all(external.join("nested")).unwrap();
        fs::write(
            external.join("sentinel.txt"),
            "external bytes must not change",
        )
        .unwrap();
        fs::write(external.join("nested/authored.md"), "nested authored bytes").unwrap();
        for base in [external.join("plans"), external.join("demo/plans")] {
            fs::create_dir_all(base.join(".draft.backup")).unwrap();
            fs::write(
                base.join(".draft.backup/authored.md"),
                "external recovery backup must survive",
            )
            .unwrap();
            fs::create_dir_all(base.join("live")).unwrap();
            fs::write(
                base.join("live/plan.md.tmp"),
                "external legacy temp must survive",
            )
            .unwrap();
        }
        let before = contents(&external);
        fs::create_dir_all(hub.join(ancestor).parent().unwrap()).unwrap();
        std::os::unix::fs::symlink(&external, hub.join(ancestor)).unwrap();
        let out = s.cli(&["--no-sync", "create", "Must stay contained"]);
        assert!(!out.status.success(), "{ancestor}");
        assert_eq!(contents(&external), before, "{ancestor}");
        assert!(
            fs::symlink_metadata(hub.join(ancestor))
                .unwrap()
                .file_type()
                .is_symlink()
        );
        assert!(s.path("home/.beans-state/hub/op-journal.json").exists());
    }
}
#[test]
fn hub_writer_rejects_symlink_file_and_validated_ancestor_replacement() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    let hub = s.clone_hub(&remote, "hub");
    let external = s.path("external");
    fs::create_dir_all(&external).unwrap();
    fs::write(external.join("sentinel"), "safe").unwrap();
    std::os::unix::fs::symlink(external.join("sentinel"), hub.dir.join("target.md")).unwrap();
    assert!(beans::gitops::write_hub_file(&hub.dir, Path::new("target.md"), b"overwrite").is_err());
    assert_eq!(fs::read(external.join("sentinel")).unwrap(), b"safe");
    beans::gitops::check_hub_write_path(&hub.dir, Path::new("new-parent/target.md")).unwrap();
    std::os::unix::fs::symlink(&external, hub.dir.join("new-parent")).unwrap();
    assert!(
        beans::gitops::write_hub_file(&hub.dir, Path::new("new-parent/target.md"), b"escape")
            .is_err()
    );
    assert!(!external.join("target.md").exists());
}
#[test]
fn cleanup_requires_generated_evidence_and_untracked_state_preserving_authored_drafts() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    let hub = s.clone_hub(&remote, "hub");
    fs::create_dir_all(hub.dir.join("docs")).unwrap();
    for file in [
        "docs/plan.md.tmp",
        "docs/.bn-write-authored",
        "docs/.bn-plan-authored",
    ] {
        fs::write(hub.dir.join(file), "tracked authored draft").unwrap();
    }
    git(&hub.dir, &["add", "-A"]);
    git(&hub.dir, &["commit", "-m", "authored drafts"]);
    fs::create_dir_all(hub.dir.join("other-docs")).unwrap();
    for file in [
        "other-docs/plan.md.tmp",
        "other-docs/.bn-write-authored",
        "other-docs/.bn-plan-authored",
    ] {
        fs::write(hub.dir.join(file), "untracked authored draft").unwrap();
    }
    fs::create_dir_all(hub.dir.join("projects/demo/issues")).unwrap();
    let prepared = beans::gitops::PreparedHubWrite::prepare(
        &hub.dir,
        Path::new("projects/demo/issues/never-installed.md"),
        b"genuine generated orphan",
    )
    .unwrap();
    std::mem::forget(prepared);
    let orphan = fs::read_dir(hub.dir.join("projects/demo/issues"))
        .unwrap()
        .next()
        .unwrap()
        .unwrap()
        .path();
    assert!(
        orphan
            .file_name()
            .unwrap()
            .to_string_lossy()
            .starts_with(".bn-write-")
    );
    let tracked = beans::gitops::PreparedHubWrite::prepare(
        &hub.dir,
        Path::new("projects/demo/issues/also-never-installed.md"),
        b"registered but user explicitly tracked",
    )
    .unwrap();
    std::mem::forget(tracked);
    git(&hub.dir, &["add", "-A"]);
    git(
        &hub.dir,
        &["commit", "-m", "user tracks generated candidates"],
    );
    // The first generated orphan became tracked too; make only that one untracked.
    git(
        &hub.dir,
        &[
            "rm",
            "--cached",
            orphan.strip_prefix(&hub.dir).unwrap().to_str().unwrap(),
        ],
    );
    git(
        &hub.dir,
        &["commit", "-m", "release first candidate from index"],
    );
    let before_mutation = hub.git(&["rev-parse", "HEAD"]).unwrap();
    hub.mutate(&mut Append::new("safe mutation")).unwrap();
    assert!(!orphan.exists());
    hub.sync().unwrap();
    for dir in ["docs", "other-docs"] {
        for file in ["plan.md.tmp", ".bn-write-authored", ".bn-plan-authored"] {
            assert!(hub.dir.join(dir).join(file).exists());
        }
    }
    let entries: Vec<_> = fs::read_dir(hub.dir.join("projects/demo/issues"))
        .unwrap()
        .collect();
    assert_eq!(entries.len(), 1);
    assert!(
        fs::read(entries[0].as_ref().unwrap().path())
            .unwrap()
            .starts_with(b"registered but user")
    );
    let changes = hub
        .git(&[
            "log",
            &format!("{before_mutation}..HEAD"),
            "--format=",
            "--name-status",
        ])
        .unwrap();
    assert!(!changes.lines().any(|l| l.starts_with("D\t")));
}
struct UnrelatedNativeRace {
    primary: beans::ops::IssueMutation,
    competitor: Hub,
    fired: bool,
    survived: Vec<bool>,
}
impl Operation for UnrelatedNativeRace {
    fn subject(&self) -> String {
        self.primary.subject()
    }
    fn after_rebase(&mut self, present: bool) {
        self.survived.push(present);
        self.primary.after_rebase(present);
    }
    fn apply(&mut self, hub: &Path) -> Result<Vec<PathBuf>, Error> {
        let paths = self.primary.apply(hub)?;
        if !self.fired {
            self.fired = true;
            assert!(
                self.competitor
                    .mutate(&mut Append::new("unrelated remote file edit"))?
                    .pushed
            );
        }
        Ok(paths)
    }
}
#[test]
fn surviving_native_note_commit_is_recognized_after_successful_rebase() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    s.ok(&["init", remote.to_str().unwrap()]);
    let created: serde_json::Value =
        serde_json::from_str(&s.ok(&["--json", "create", "Surviving native rebase"])).unwrap();
    let id = created["id"].as_str().unwrap();
    let hub = Hub::new(
        s.path("home/hub"),
        s.path("home/cache"),
        "main".into(),
        "Native Tester".into(),
        false,
        Duration::ZERO,
    )
    .unwrap();
    let competitor = s.clone_hub(&remote, "competitor");
    let resolved = beans::vault::resolve(
        &hub.dir,
        beans::vault::ResolveOptions {
            flag_project: b"demo",
            cwd: Some(&s.0),
            write: true,
            ..Default::default()
        },
    )
    .unwrap();
    let primary = beans::ops::IssueMutation::new(
        resolved,
        id.into(),
        beans::ops::IssueChange::Note("surviving native event".into()),
        "Native Tester".into(),
        None,
    );
    let mut race = UnrelatedNativeRace {
        primary,
        competitor,
        fired: false,
        survived: Vec::new(),
    };
    assert!(hub.mutate(&mut race).unwrap().pushed);
    assert_eq!(race.survived, [true]);
    let index = beans::vault::Index::load_snapshot(&hub.dir, Default::default()).unwrap();
    assert_eq!(
        index
            .issue_by_id(id.as_bytes())
            .unwrap()
            .log
            .iter()
            .filter(|e| e.event.as_bytes() == b"surviving native event")
            .count(),
        1
    );
}
#[path = "native/git_coordination.rs"]
mod git_coordination;
#[path = "native/git_resolver.rs"]
mod git_resolver;
#[path = "native/commands.rs"]
mod retained_commands;
#[path = "native/review_regressions.rs"]
mod review_regressions;
#[path = "native/safety.rs"]
mod safety;
