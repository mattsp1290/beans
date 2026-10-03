use super::*;

#[test]
fn distinct_homes_and_alias_share_native_write_lock() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    s.ok(&["init", remote.to_str().unwrap()]);
    let dir = s.path("home/hub");
    let alias = s.path("alias");
    std::os::unix::fs::symlink(&dir, &alias).unwrap();
    let entered = s.path("entered");
    let release = s.path("release");
    let overlap = s.path("overlap");
    let script = format!(
        "#!/bin/sh\nif mkdir '{}' 2>/dev/null; then\n touch '{}'\n while test ! -f '{}'; do sleep 0.01; done\nelse\n touch '{}'\nfi\n",
        s.path("barrier").display(),
        entered.display(),
        release.display(),
        overlap.display()
    );
    let hook = dir.join(".git/hooks/pre-commit");
    fs::write(&hook, script).unwrap();
    fs::set_permissions(&hook, fs::Permissions::from_mode(0o755)).unwrap();
    for home in ["home", "different-home"] {
        fs::create_dir_all(s.path(home)).unwrap();
        fs::write(
            s.path(home).join("config.toml"),
            "[hub]\nbranch='main'\n[git]\ndiagnostics=true\n",
        )
        .unwrap();
    }
    let spawn = |home: &str, hub: &Path, title: &str| {
        Command::new(env!("CARGO_BIN_EXE_bn"))
            .args(["--no-sync", "--hub"])
            .arg(hub)
            .args(["--project", "demo", "create", title])
            .env("BEANS_HOME", s.path(home))
            .env("BN_ACTOR", "Tester")
            .env_remove("BN_CONFIG")
            .env_remove("BEANS_HUB")
            .current_dir(&s.0)
            .stdout(Stdio::null())
            .stderr(Stdio::piped())
            .spawn()
            .unwrap()
    };
    let mut first = spawn("home", &dir, "first writer");
    let start = std::time::Instant::now();
    while !entered.exists() {
        assert!(start.elapsed() < Duration::from_secs(10));
        std::thread::sleep(Duration::from_millis(10));
    }
    let mut second = spawn("different-home", &alias, "second writer");
    std::thread::sleep(Duration::from_millis(400));
    let excluded = !overlap.exists() && second.try_wait().unwrap().is_none();
    fs::write(&release, b"release").unwrap();
    let a = finish(&mut first);
    let b = finish(&mut second);
    use std::io::Read;
    let mut ids = Vec::new();
    for child in [&mut first, &mut second] {
        let mut stderr = String::new();
        child
            .stderr
            .take()
            .unwrap()
            .read_to_string(&mut stderr)
            .unwrap();
        let records: Vec<serde_json::Value> = stderr
            .lines()
            .filter(|line| line.starts_with('{'))
            .map(|line| serde_json::from_str(line).unwrap())
            .collect();
        assert!(records.iter().any(|r| r["event"] == "lock_hold"));
        assert!(records.iter().any(|r| r["event"] == "operation"));
        let id = records[0]["operation_id"].clone();
        assert!(
            records
                .iter()
                .all(|r| r["operation_id"] == id && r["duration_ms"].as_f64().unwrap() >= 0.0)
        );
        if !ids.is_empty() {
            assert!(
                records
                    .iter()
                    .any(|r| r["event"] == "lock_wait"
                        && r["duration_ms"].as_f64().unwrap() >= 100.0)
            );
        }
        ids.push(id);
    }
    assert_ne!(ids[0], ids[1]);
    assert!(
        excluded,
        "second home entered the first writer's commit pipeline"
    );
    assert!(a.success() && b.success());
    assert_eq!(
        fs::read_dir(dir.join("projects/demo/issues"))
            .unwrap()
            .filter(|e| e
                .as_ref()
                .unwrap()
                .path()
                .extension()
                .is_some_and(|x| x == "md"))
            .count(),
        2
    );
}

#[test]
fn clone_state_is_independent_and_cache_clear_preserves_evidence_and_inode() {
    use std::os::unix::fs::MetadataExt;
    let s = Sandbox::new();
    let remote = s.remote(true);
    let mut a = s.clone_hub(&remote, "a");
    let b = s.clone_hub(&remote, "b");
    a.lock_timeout = Duration::from_millis(80);
    for hub in [&a, &b] {
        fs::write(hub.state_path().join("last-fetch"), b"derived").unwrap();
        fs::write(hub.state_path().join("last-fetch-attempt"), b"derived").unwrap();
        fs::write(hub.journal_path(), b"recovery bytes").unwrap();
        fs::write(hub.state_path().join("unknown"), b"authored bytes").unwrap();
        fs::create_dir(hub.state_path().join("unknown-directory")).unwrap();
    }
    let lock = fs::File::create(a.lock_path()).unwrap();
    assert_eq!(
        unsafe { libc::flock(lock.as_raw_fd(), libc::LOCK_EX | libc::LOCK_NB) },
        0
    );
    let original = lock.metadata().unwrap();
    assert!(a.clear_cache().unwrap_err().to_string().contains("lock"));
    assert!(a.state_path().join("last-fetch").exists());
    // Another clone does not wait for A, despite sharing its parent/home.
    b.clear_cache().unwrap();
    assert!(a.state_path().join("last-fetch-attempt").exists());
    drop(lock);
    a.clear_cache().unwrap();
    let now = fs::metadata(a.lock_path()).unwrap();
    assert_eq!((original.dev(), original.ino()), (now.dev(), now.ino()));
    for hub in [&a, &b] {
        assert_eq!(fs::read(hub.journal_path()).unwrap(), b"recovery bytes");
        assert_eq!(
            fs::read(hub.state_path().join("unknown")).unwrap(),
            b"authored bytes"
        );
        assert!(hub.state_path().join("unknown-directory").is_dir());
        assert!(!hub.state_path().join("last-fetch").exists());
        assert!(!hub.state_path().join("last-fetch-attempt").exists());
        assert!(!hub.dir.join(".beans-state").exists());
    }
}
