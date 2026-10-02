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
