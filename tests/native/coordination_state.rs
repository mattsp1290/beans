use super::*;

#[test]
fn state_rejects_reserved_names_symlinks_and_handles_native_names() {
    use std::os::unix::ffi::OsStringExt;
    let s = Sandbox::new();
    let make = |path| {
        Hub::new(
            path,
            s.path("home/cache"),
            "main".into(),
            "Tester".into(),
            true,
            Duration::ZERO,
        )
    };
    let reserved = s.path("missing-parent/.beans-state");
    assert!(make(reserved).is_err());
    assert!(!s.path("missing-parent").exists());
    fs::create_dir(s.path(".beans-state")).unwrap();
    assert!(make(s.path(".beans-state")).is_err());
    let target = s.path("unsafe-parent/hub");
    fs::create_dir_all(target.parent().unwrap()).unwrap();
    let outside = s.path("outside");
    fs::create_dir(&outside).unwrap();
    std::os::unix::fs::symlink(&outside, target.parent().unwrap().join(".beans-state")).unwrap();
    assert!(make(target).is_err());
    assert_eq!(fs::read_dir(&outside).unwrap().count(), 0);
    let native = s.0.join(std::ffi::OsString::from_vec(b"hub-\xff".to_vec()));
    let hub = make(native.clone()).unwrap();
    assert_eq!(hub.state_path().file_name(), native.file_name());
    fs::write(outside.join("sentinel"), b"unchanged").unwrap();
    std::os::unix::fs::symlink(outside.join("sentinel"), hub.lock_path()).unwrap();
    assert!(hub.clear_cache().is_err());
    assert_eq!(fs::read(outside.join("sentinel")).unwrap(), b"unchanged");
}

#[test]
fn legacy_evidence_blocks_writes_but_clear_and_doctor_preserve_it() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    s.ok(&["init", remote.to_str().unwrap()]);
    s.ok(&["create", "existing project"]);
    let legacy = s.path("home/cache/op-journal.json");
    fs::create_dir_all(legacy.parent().unwrap()).unwrap();
    fs::write(&legacy, b"unassigned legacy journal").unwrap();
    let before = git(&s.path("home/hub"), &["rev-parse", "HEAD"]);
    let result = s.cli(&["create", "must refuse"]);
    assert!(!result.status.success());
    assert!(String::from_utf8_lossy(&result.stderr).contains("legacy recovery evidence"));
    s.ok(&["cache", "clear"]);
    s.ok(&["doctor"]);
    assert_eq!(fs::read(legacy).unwrap(), b"unassigned legacy journal");
    assert_eq!(git(&s.path("home/hub"), &["rev-parse", "HEAD"]), before);
}

#[test]
fn initialization_excludes_other_home_and_partial_clone_writer() {
    let s = Sandbox::new();
    let remote = s.remote(false);
    let tools = s.path("tools");
    fs::create_dir(&tools).unwrap();
    let actual_git = Command::new("sh")
        .args(["-c", "command -v git"])
        .output()
        .unwrap();
    let actual_git = String::from_utf8(actual_git.stdout).unwrap();
    let entered = s.path("clone-entered");
    let release = s.path("clone-release");
    let script = format!(
        "#!/bin/sh\nif test \"$1\" = clone; then\n '{}' \"$@\" || exit $?\n touch '{}'\n while test ! -f '{}'; do sleep 0.01; done\n exit 0\nfi\nexec '{}' \"$@\"\n",
        actual_git.trim(),
        entered.display(),
        release.display(),
        actual_git.trim()
    );
    fs::write(tools.join("git"), script).unwrap();
    fs::set_permissions(tools.join("git"), fs::Permissions::from_mode(0o755)).unwrap();
    let spawn = |home: &str, args: &[&str]| {
        Command::new(env!("CARGO_BIN_EXE_bn"))
            .args(["--branch", "main", "--hub"])
            .arg(s.path("target"))
            .args(args)
            .env("BEANS_HOME", s.path(home))
            .env("BN_ACTOR", "Tester")
            .env(
                "PATH",
                format!("{}:{}", tools.display(), std::env::var("PATH").unwrap()),
            )
            .env_remove("BN_CONFIG")
            .env_remove("BEANS_HUB")
            .stdout(Stdio::null())
            .stderr(Stdio::inherit())
            .current_dir(&s.0)
            .spawn()
            .unwrap()
    };
    let mut first = spawn("first-home", &["init", remote.to_str().unwrap()]);
    let at = std::time::Instant::now();
    while !entered.exists() {
        assert!(at.elapsed() < Duration::from_secs(10));
        std::thread::sleep(Duration::from_millis(10));
    }
    let mut second = spawn("second-home", &["init", remote.to_str().unwrap()]);
    let mut writer = spawn("third-home", &["sync"]);
    std::thread::sleep(Duration::from_millis(150));
    let excluded = second.try_wait().unwrap().is_none() && writer.try_wait().unwrap().is_none();
    fs::write(release, b"release").unwrap();
    assert!(finish(&mut first).success());
    assert!(!finish(&mut second).success());
    assert!(finish(&mut writer).success());
    assert!(excluded, "another pipeline acted on a partial clone");
    assert_eq!(
        git(&s.path("target"), &["rev-list", "--count", "HEAD"]),
        "1"
    );
}

#[test]
fn unquoted_native_filename_survives_mutation_sync_refresh_and_status() {
    use std::os::unix::ffi::OsStringExt;
    let s = Sandbox::new();
    let remote = s.remote(true);
    s.ok(&["init", remote.to_str().unwrap()]);
    let dir = s.path("home/hub");
    git(&dir, &["config", "core.quotePath", "false"]);
    let relative = PathBuf::from(std::ffi::OsString::from_vec(b"authored-\xff.txt".to_vec()));
    fs::write(dir.join(&relative), b"authored bytes\xff\n").unwrap();
    let status: serde_json::Value = serde_json::from_str(&s.ok(&["status"])).unwrap();
    assert!(status["dirty"].as_str().unwrap().contains("\\377"));
    s.ok(&["--no-sync", "create", "native hand edit"]);
    s.ok(&["sync"]);
    s.ok(&["list"]);
    assert_eq!(
        fs::read(dir.join(&relative)).unwrap(),
        b"authored bytes\xff\n"
    );
    let output = Command::new("git")
        .arg("-C")
        .arg(&dir)
        .args(["ls-files", "-z", "--"])
        .arg(&relative)
        .output()
        .unwrap();
    assert!(output.status.success());
    assert_eq!(output.stdout, b"authored-\xff.txt\0");
    assert!(git(&dir, &["log", "--format=%s"]).contains("bn: hand edits"));
    assert_eq!(
        git(&dir, &["rev-list", "--count", "origin/main..HEAD"]),
        "0"
    );
}
