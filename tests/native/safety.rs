use super::*;

#[test]
fn plan_export_never_replaces_authored_destination_and_escaping_bundle_is_rejected() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    s.ok(&["init", remote.to_str().unwrap()]);
    s.ok(&["project", "create", "demo"]);
    let draft = s.path("draft");
    let result: serde_json::Value = serde_json::from_str(&s.ok(&[
        "--json",
        "plan",
        "init",
        "Qualified plan",
        "--output",
        draft.to_str().unwrap(),
    ]))
    .unwrap();
    let id = result["id"].as_str().unwrap();
    s.ok(&["plan", "put", draft.to_str().unwrap()]);
    let destination = s.path("authored");
    fs::create_dir_all(destination.join("sections")).unwrap();
    fs::write(destination.join("old.md"), b"authored old\xff\n").unwrap();
    fs::write(destination.join("sections/keep.md"), b"authored section\n").unwrap();
    let before = contents(&destination);
    let head = git(&s.path("home/hub"), &["rev-parse", "HEAD"]);
    assert!(
        !s.cli(&["plan", "get", id, "--output", destination.to_str().unwrap()])
            .status
            .success()
    );
    assert_eq!(contents(&destination), before);
    assert_eq!(git(&s.path("home/hub"), &["rev-parse", "HEAD"]), head);
    let clean = s.path("clean");
    s.ok(&["plan", "get", id, "--output", clean.to_str().unwrap()]);
    assert!(clean.join("plan.md").is_file());
    assert!(!clean.join("old.md").exists());
    let manifest = draft.join("plan.md");
    let raw = fs::read_to_string(&manifest).unwrap();
    fs::write(
        &manifest,
        raw.replacen("\n---\n", "\nsections: [../escape.md]\n---\n", 1),
    )
    .unwrap();
    fs::write(s.path("escape.md"), b"external authored section\n").unwrap();
    let hub_before = contents(&s.path("home/hub/projects"));
    assert!(
        !s.cli(&["plan", "put", draft.to_str().unwrap()])
            .status
            .success()
    );
    assert_eq!(contents(&s.path("home/hub/projects")), hub_before);
    assert_eq!(
        fs::read(s.path("escape.md")).unwrap(),
        b"external authored section\n"
    );
    assert_eq!(git(&s.path("home/hub"), &["rev-parse", "HEAD"]), head);
}

#[test]
fn legacy_interrupted_tree_backup_recovers_bytes_before_subsequent_native_write() {
    let s = Sandbox::new();
    let parent = s.path("plans");
    let backup = parent.join(".bundle.backup");
    fs::create_dir_all(backup.join("sections")).unwrap();
    fs::write(backup.join("plan.md"), b"original manifest\xff\n").unwrap();
    fs::write(backup.join("sections/user.md"), b"user section\n").unwrap();
    beans::gitops::recover_tree(&parent.join("bundle")).unwrap();
    assert!(!backup.exists());
    assert_eq!(
        fs::read(parent.join("bundle/plan.md")).unwrap(),
        b"original manifest\xff\n"
    );
    assert_eq!(
        fs::read(parent.join("bundle/sections/user.md")).unwrap(),
        b"user section\n"
    );
    write_file(&parent.join("bundle/plan.md"), b"new manifest\n").unwrap();
    assert_eq!(
        fs::read(parent.join("bundle/plan.md")).unwrap(),
        b"new manifest\n"
    );
    assert_eq!(
        fs::read(parent.join("bundle/sections/user.md")).unwrap(),
        b"user section\n"
    );
    let snapshot = contents(&parent);
    beans::gitops::recover_trees(&parent).unwrap();
    assert_eq!(contents(&parent), snapshot);
}

#[test]
fn bounded_snapshot_descriptors_index_every_project_under_low_process_limit() {
    use std::os::unix::process::CommandExt;
    let s = Sandbox::new();
    let remote = s.remote(true);
    s.ok(&["init", remote.to_str().unwrap()]);
    let hub = s.path("home/hub");
    for i in 0..100 {
        let project = format!("p{i:03}");
        let dir = hub.join("projects").join(&project);
        fs::create_dir_all(dir.join("issues")).unwrap();
        fs::write(
            dir.join("beans.toml"),
            format!("name = '{project}'\nprefix = '{project}'\n"),
        )
        .unwrap();
        fs::write(dir.join("issues").join(format!("{project}-abcd-note.md")), format!("---\nid: {project}-abcd\ntitle: Note {i}\ntype: task\nstatus: open\npriority: 2\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\nOriginal body {i}\n")).unwrap();
    }
    let mut command = Command::new(env!("CARGO_BIN_EXE_bn"));
    command
        .args([
            "--json",
            "--no-fetch",
            "list",
            "--all-projects",
            "--limit",
            "0",
        ])
        .env("BEANS_HOME", s.path("home"))
        .env_remove("BEANS_HUB")
        .env_remove("BN_CONFIG")
        .env_remove("BEANS_PROJECT")
        .current_dir(&s.0);
    // SAFETY: the child hook calls only setrlimit, which is async-signal-safe;
    // no locks or allocations occur between fork and exec.
    unsafe {
        command.pre_exec(|| {
            let limit = libc::rlimit {
                rlim_cur: 64,
                rlim_max: 64,
            };
            if libc::setrlimit(libc::RLIMIT_NOFILE, &limit) == 0 {
                Ok(())
            } else {
                Err(std::io::Error::last_os_error())
            }
        });
    }
    let output = command.output().unwrap();
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
    assert!(
        output.stderr.is_empty(),
        "unexpected dropped-note warning: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    let notes: serde_json::Value = serde_json::from_slice(&output.stdout).unwrap();
    assert_eq!(notes.as_array().unwrap().len(), 100);
    for i in 0..100 {
        assert!(
            notes
                .as_array()
                .unwrap()
                .iter()
                .any(|n| n["id"] == format!("p{i:03}-abcd"))
        );
    }
}

#[test]
fn real_push_races_recover_with_new_git_compare_and_swap_rejection_diagnostics() {
    let s = Sandbox::new();
    let tools = s.path("git-tools");
    fs::create_dir(&tools).unwrap();
    let observed = s.path("observed-ref-races");
    // Preserve real Git effects and exit codes. Git 2.43 calls this receiver
    // race "failed to update ref"; Git 2.55 calls it "incorrect old value
    // provided". Normalize only that status for deterministic native coverage.
    let wrapper = format!(
        "#!/bin/sh\nerr=$(mktemp) || exit 99\n/usr/bin/git \"$@\" 2>\"$err\"\nresult=$?\nsed 's/(failed to update ref)/(incorrect old value provided)/g' \"$err\" >\"$err.new\"\nif grep -q '\\[remote rejected\\].*(incorrect old value provided)' \"$err.new\"; then printf 'ref race\\n' >> '{}'; fi\ncat \"$err.new\" >&2\nrm -f \"$err\" \"$err.new\"\nexit \"$result\"\n",
        observed.display()
    );
    let shim = tools.join("git");
    fs::write(&shim, wrapper).unwrap();
    fs::set_permissions(&shim, fs::Permissions::from_mode(0o755)).unwrap();
    for selector in [
        "native_issue_note_replay_keeps_both_writers_and_frozen_primary_log",
        "push_conflict_replays_owned_head_without_duplicating_notes",
        "repeated_races_exhaust_three_pushes_and_preserve_last_commit",
        "review_regressions::age_archive_rechecks_eligibility_after_push_race_and_rename_merge",
        "successful_rebase_and_dropped_operation_are_rederived_without_duplication",
    ] {
        let before = fs::read_to_string(&observed)
            .unwrap_or_default()
            .lines()
            .count();
        let output = Command::new(std::env::current_exe().unwrap())
            .args([selector, "--exact"])
            .env("PATH", format!("{}:/usr/bin:/bin", tools.display()))
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "{selector}: {}\n{}",
            String::from_utf8_lossy(&output.stdout),
            String::from_utf8_lossy(&output.stderr)
        );
        assert!(String::from_utf8_lossy(&output.stdout).contains("1 passed; 0 failed"));
        let races = fs::read_to_string(&observed).unwrap().lines().count() - before;
        assert!(
            races > 0,
            "{selector} must exercise a real receiver ref race"
        );
        println!("{selector}: passed after {races} real receiver ref race(s)");
    }
}

#[test]
fn status_replay_records_latest_transition_and_adjacent_note_without_forging_old_approval() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    s.ok(&["init", remote.to_str().unwrap()]);
    let created: serde_json::Value =
        serde_json::from_str(&s.ok(&["--json", "create", "Status race"])).unwrap();
    let id = created["id"].as_str().unwrap();
    s.ok(&["update", id, "--status", "ready_for_review"]);
    let hub = Hub {
        dir: s.path("home/hub"),
        cache: s.path("home/cache"),
        branch: "main".into(),
        actor: "Native Tester".into(),
        no_sync: false,
        throttle: Duration::ZERO,
    };
    let competitor = s.clone_hub(&remote, "status-competitor");
    let raced = s.path("raced-status");
    hook(
        &hub,
        "pre-push",
        &format!(
            "test ! -f '{}' || exit 0\ntouch '{}'\nBEANS_HOME='{}' BN_ACTOR='Other Writer' '{}' --hub '{}' --project demo update '{}' --status ready_for_merge",
            raced.display(),
            raced.display(),
            s.path("other-home").display(),
            env!("CARGO_BIN_EXE_bn"),
            competitor.dir.display(),
            id
        ),
    );
    s.ok(&[
        "update",
        id,
        "--status",
        "ready_for_validation",
        "--note",
        "exact native approval marker",
    ]);
    let detail: serde_json::Value =
        serde_json::from_str(&s.ok(&["show", id, "--json", "--no-fetch"])).unwrap();
    assert_eq!(detail["status"], "ready_for_validation");
    let logs = detail["log"].as_array().unwrap();
    let notes: Vec<_> = logs
        .iter()
        .enumerate()
        .filter(|(_, row)| row["event"] == "exact native approval marker")
        .collect();
    assert_eq!(notes.len(), 1);
    let (position, note) = notes[0];
    let transition = &logs[position - 1];
    assert_eq!(
        transition["event"],
        "status ready_for_merge → ready_for_validation"
    );
    assert_ne!(
        transition["event"],
        "status ready_for_review → ready_for_validation"
    );
    assert_eq!(transition["actor"], "Native-Tester");
    for field in ["actor", "at", "repo", "sha", "branch"] {
        assert_eq!(transition[field], note[field]);
    }
    assert!(logs.iter().any(
        |row| row["event"] == "status ready_for_review → ready_for_merge"
            && row["actor"] == "Other-Writer"
    ));
    assert_eq!(
        hub.git(&["rev-parse", "HEAD"]).unwrap(),
        hub.git(&["rev-parse", "origin/main"]).unwrap()
    );
    assert_eq!(
        hub.git(&["log", "-1", "--format=%(trailers:key=Bn-Run,valueonly)"])
            .unwrap()
            .len(),
        32
    );
}

#[test]
fn status_reports_read_only_resolved_identity_and_explicit_project_precedence() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    s.ok(&["init", remote.to_str().unwrap()]);
    let initial: serde_json::Value = serde_json::from_str(&s.ok(&["--json", "status"])).unwrap();
    assert!(initial.get("project").is_none());
    assert!(
        initial["resolution"]
            .as_str()
            .unwrap()
            .contains("does not exist")
    );
    assert!(!s.path("home/hub/projects/demo").exists());
    s.ok(&["project", "create", "demo"]);
    s.ok(&["project", "create", "other"]);
    let head = git(&s.path("home/hub"), &["rev-parse", "HEAD"]);
    let synchronized: serde_json::Value = serde_json::from_str(&s.ok(&["--json", "sync"])).unwrap();
    assert_eq!(synchronized["synced"], true);
    assert_eq!(synchronized["status"]["ahead"], 0);
    assert_eq!(synchronized["status"]["behind"], 0);
    for (args, expected) in [
        (vec!["--json", "status"], "demo"),
        (vec!["--json", "--project", "other", "status"], "other"),
    ] {
        let status: serde_json::Value = serde_json::from_str(&s.ok(&args)).unwrap();
        assert_eq!(status["project"], expected);
        assert_eq!(status["resolution"], "resolved");
        assert_eq!(
            Path::new(status["project_dir"].as_str().unwrap()),
            s.path(&format!("home/hub/projects/{expected}"))
        );
    }
    let unscoped = Command::new(env!("CARGO_BIN_EXE_bn"))
        .args(["--json", "status"])
        .env("BEANS_HOME", s.path("home"))
        .env_remove("BEANS_PROJECT")
        .env_remove("BEANS_HUB")
        .env_remove("BN_CONFIG")
        .current_dir(&s.0)
        .output()
        .unwrap();
    assert!(unscoped.status.success());
    let unscoped: serde_json::Value = serde_json::from_slice(&unscoped.stdout).unwrap();
    assert!(unscoped.get("project").is_none());
    assert!(
        unscoped["resolution"]
            .as_str()
            .unwrap()
            .contains("not inside a git repository")
    );
    assert_eq!(git(&s.path("home/hub"), &["rev-parse", "HEAD"]), head);
    assert!(git(&s.path("home/hub"), &["status", "--porcelain"]).is_empty());
}
