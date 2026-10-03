use super::*;

#[test]
fn timed_transaction_phases_preserve_history_and_recovery() {
    for phase in ["fetch", "rebase", "add", "diff", "commit", "push"] {
        let s = Sandbox::new();
        let remote = s.remote(true);
        s.ok(&["init", remote.to_str().unwrap()]);
        s.ok(&["create", "establish project"]);
        let dir = s.path("home/hub");
        let before = git(&dir, &["rev-parse", "HEAD"]);
        let at = std::time::Instant::now();
        let output = timed_cli(
            &s,
            phase,
            &["--json", "create", "credential-sentinel-secret"],
            false,
        );
        assert!(at.elapsed() < Duration::from_secs(3), "{phase}");
        assert!(!output.status.success(), "{phase}");
        assert!(
            s.path("timed-entered").exists(),
            "fixture not exercised: {phase}"
        );
        let stderr = String::from_utf8(output.stderr).unwrap();
        assert!(stderr.contains("timed out"), "{phase}: {stderr}");
        git(&dir, &["merge-base", "--is-ancestor", &before, "HEAD"]);
        let journal = s.path("home/.beans-state/hub/op-journal.json");
        assert_eq!(
            journal.exists(),
            matches!(phase, "add" | "diff" | "commit" | "push"),
            "{phase}"
        );
        if phase == "push" {
            assert_ne!(git(&dir, &["rev-parse", "HEAD"]), before);
        } else {
            assert_eq!(git(&dir, &["rev-parse", "HEAD"]), before);
        }
        let records: Vec<serde_json::Value> = stderr
            .lines()
            .filter(|s| s.starts_with('{'))
            .map(|s| serde_json::from_str(s).unwrap())
            .collect();
        assert!(
            records
                .iter()
                .any(|r| r["event"] == "git_step" && r["result"] == "git_timeout"),
            "{phase}"
        );
        assert!(records.iter().any(|r| r["event"] == "lock_hold"));
        assert!(records.iter().any(|r| r["event"] == "operation"));
        let id = &records[0]["operation_id"];
        assert!(
            records
                .iter()
                .all(|r| &r["operation_id"] == id && r["duration_ms"].as_f64().unwrap() >= 0.0)
        );
        assert!(
            !records
                .iter()
                .any(|r| r.to_string().contains("credential-sentinel-secret"))
        );
        assert!(!String::from_utf8_lossy(&output.stdout).contains("bn.git"));
        // A fresh writer can acquire the same inode after cleanup and recover.
        s.ok(&["sync"]);
        assert!(!journal.exists());
    }
}
#[test]
fn push_response_timeout_converges_without_duplicate_authored_event() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    s.ok(&["init", remote.to_str().unwrap()]);
    s.ok(&["create", "establish project"]);
    let result = timed_cli(&s, "push", &["create", "published but lost response"], true);
    assert!(!result.status.success());
    let before = git(&s.path("home/hub"), &["rev-parse", "HEAD"]);
    assert_eq!(git(&remote, &["rev-parse", "main"]), before);
    s.ok(&["sync"]);
    assert_eq!(git(&s.path("home/hub"), &["rev-parse", "HEAD"]), before);
    let files = fs::read_dir(s.path("home/hub/projects/demo/issues")).unwrap();
    assert_eq!(
        files
            .filter_map(Result::ok)
            .filter(|e| fs::read_to_string(e.path())
                .unwrap()
                .contains("published but lost response"))
            .count(),
        1
    );
}
#[test]
fn rejecting_remote_hook_is_not_contention_in_mutate_or_sync() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    s.ok(&["init", remote.to_str().unwrap()]);
    let count = s.path("push-count");
    let hook = remote.join("hooks/pre-receive");
    fs::write(&hook, format!("#!/bin/sh\necho push >> '{}'\necho 'hook denies: [rejected] non-fast-forward fetch first failed to update ref incorrect old value provided' >&2\nexit 1\n", count.display())).unwrap();
    fs::set_permissions(hook, fs::Permissions::from_mode(0o755)).unwrap();
    let result = s.cli(&["create", "kept local"]);
    assert!(!result.status.success());
    assert!(String::from_utf8_lossy(&result.stderr).contains("hook denies"));
    assert_eq!(fs::read_to_string(&count).unwrap().lines().count(), 1);
    let before = git(&s.path("home/hub"), &["rev-parse", "HEAD"]);
    let result = s.cli(&["sync"]);
    assert!(!result.status.success());
    assert!(String::from_utf8_lossy(&result.stderr).contains("hook denies"));
    assert_eq!(fs::read_to_string(count).unwrap().lines().count(), 2);
    assert_eq!(git(&s.path("home/hub"), &["rev-parse", "HEAD"]), before);
}

#[test]
fn abort_timeout_preserves_conflict_and_never_discards_unowned_history() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    s.ok(&["init", remote.to_str().unwrap()]);
    let competitor = s.clone_hub(&remote, "competitor");
    fs::write(s.path("home/hub/data.txt"), b"unowned local edit\n").unwrap();
    fs::write(competitor.dir.join("data.txt"), b"competing remote edit\n").unwrap();
    git(&competitor.dir, &["add", "-A"]);
    git(&competitor.dir, &["commit", "-m", "unowned remote"]);
    git(&competitor.dir, &["push", "origin", "main"]);
    let before = git(&s.path("home/hub"), &["rev-parse", "HEAD"]);
    let result = timed_cli(&s, "--abort", &["sync"], false);
    assert!(!result.status.success());
    assert!(
        String::from_utf8_lossy(&result.stderr).contains("preserve files and resolve manually")
    );
    let dir = s.path("home/hub");
    assert!(dir.join(".git/rebase-merge").exists() || dir.join(".git/rebase-apply").exists());
    // Recover manually, then prove the independently authored local commit survived.
    git(&dir, &["rebase", "--abort"]);
    git(&dir, &["merge-base", "--is-ancestor", &before, "HEAD"]);
    assert_eq!(
        fs::read(dir.join("data.txt")).unwrap(),
        b"unowned local edit\n"
    );
    assert!(git(&dir, &["log", "-1", "--format=%s"]).contains("hand edits"));
}
