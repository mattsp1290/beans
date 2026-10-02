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
