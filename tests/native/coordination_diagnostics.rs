use super::*;

#[test]
fn diagnostic_backpressure_does_not_delay_commit_or_change_stdout() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    s.ok(&["init", remote.to_str().unwrap()]);
    fs::write(
        s.path("home/config.toml"),
        "[hub]\nbranch='main'\n[git]\ndiagnostics=true\n",
    )
    .unwrap();
    let at = std::time::Instant::now();
    let mut child = Command::new(env!("CARGO_BIN_EXE_bn"))
        .args([
            "--json",
            "--project",
            "demo",
            "create",
            "bounded diagnostics",
        ])
        .env("BEANS_HOME", s.path("home"))
        .env("BN_ACTOR", "Tester")
        .env_remove("BEANS_HUB")
        .env_remove("BN_CONFIG")
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .current_dir(&s.0)
        .spawn()
        .unwrap();
    // Hold the stderr pipe without consuming it while the transaction runs.
    assert!(finish(&mut child).success());
    assert!(at.elapsed() < Duration::from_secs(3));
    let mut stdout = String::new();
    std::io::Read::read_to_string(&mut child.stdout.take().unwrap(), &mut stdout).unwrap();
    let value: serde_json::Value = serde_json::from_str(&stdout).unwrap();
    assert!(value["pushed"].as_bool().unwrap());
    assert!(!stdout.contains("bn.git"));
    assert!(!s.path("home/.beans-state/hub/op-journal.json").exists());
    fs::write(s.path("home/config.toml"), "[hub]\nbranch='main'\n").unwrap();
    let output = s.cli(&["create", "default diagnostics off"]);
    assert!(output.status.success());
    assert!(!String::from_utf8_lossy(&output.stderr).contains("bn.git"));
}

#[test]
fn setup_clone_queries_and_orphan_checks_use_finite_policy() {
    for phase in ["clone", "ls-remote", "config", "rev-parse", "ls-files"] {
        let s = Sandbox::new();
        let remote = s.remote(true);
        s.ok(&["init", remote.to_str().unwrap()]);
        s.ok(&["create", "established project"]);
        if phase == "ls-files" {
            let prepared = beans::gitops::PreparedHubWrite::prepare(
                &s.path("home/hub"),
                Path::new("orphan.md"),
                b"preserved orphan",
            )
            .unwrap();
            std::mem::forget(prepared);
        }
        // Build private PATH/config, then run a fresh invocation with setup lookup enabled.
        let _ = timed_cli(&s, "unused-phase", &["--no-fetch", "list"], false);
        let tools = s.path("timed-tools");
        let script = format!(
            "#!/bin/sh\nfor arg in \"$@\"; do if test \"$arg\" = '{}'; then touch '{}'; sleep 10; fi; done\nexec /usr/bin/git \"$@\"\n",
            phase,
            s.path("setup-entered").display()
        );
        fs::write(tools.join("git"), script).unwrap();
        let target = s.path("new-clone");
        let mut command = Command::new(env!("CARGO_BIN_EXE_bn"));
        if matches!(phase, "clone" | "ls-remote") {
            command.arg("--hub").arg(&target);
            if phase == "clone" {
                command.args(["--branch", "main"]);
            }
            command.arg("init").arg(&remote);
        } else if phase == "rev-parse" {
            command.args([
                "--branch",
                "main",
                "--project",
                "demo",
                "--no-fetch",
                "list",
            ]);
        } else {
            command.args([
                "--branch",
                "main",
                "--project",
                "demo",
                "--no-sync",
                "create",
                "bounded setup",
            ]);
        }
        let at = std::time::Instant::now();
        let output = command
            .env("BEANS_HOME", s.path("home"))
            .env_remove("BN_ACTOR")
            .env_remove("BN_CONFIG")
            .env_remove("BEANS_HUB")
            .env("PATH", format!("{}:/usr/bin:/bin", tools.display()))
            .current_dir(&s.0)
            .output()
            .unwrap();
        assert!(at.elapsed() < Duration::from_secs(3), "{phase}");
        assert!(s.path("setup-entered").exists(), "{phase} wasn't exercised");
        let text = String::from_utf8_lossy(&output.stderr);
        let records: Vec<serde_json::Value> = text
            .lines()
            .filter(|line| line.starts_with('{'))
            .map(|line| serde_json::from_str(line).unwrap())
            .collect();
        assert!(
            records
                .iter()
                .any(|r| r["event"] == "git_step" && r["result"] == "git_timeout"),
            "{phase}: {text}"
        );
        if matches!(phase, "clone" | "ls-remote" | "ls-files") {
            assert!(!output.status.success(), "{phase}");
            assert!(!target.exists());
        } else {
            assert!(output.status.success(), "best effort {phase}: {text}");
        }
    }
}

#[test]
fn refresh_reports_fresh_and_busy_without_waiting_or_recovery() {
    let s = Sandbox::new();
    let remote = s.remote(true);
    s.ok(&["init", remote.to_str().unwrap()]);
    s.ok(&["create", "visible local issue"]);
    fs::write(
        s.path("home/config.toml"),
        "[hub]\nbranch='main'\n[git]\ndiagnostics=true\nlock_timeout='2s'\n",
    )
    .unwrap();
    let state = s.path("home/.beans-state/hub");
    let now = time::OffsetDateTime::now_utc()
        .format(&time::format_description::well_known::Rfc3339)
        .unwrap();
    fs::write(state.join("last-fetch-attempt"), now).unwrap();
    let fresh = s.cli(&["list"]);
    assert!(fresh.status.success());
    assert!(String::from_utf8_lossy(&fresh.stderr).contains("skipped-fresh"));
    fs::remove_file(state.join("last-fetch-attempt")).unwrap();
    let lock = fs::File::create(state.join("hub.lock")).unwrap();
    assert_eq!(
        unsafe { libc::flock(lock.as_raw_fd(), libc::LOCK_EX | libc::LOCK_NB) },
        0
    );
    fs::write(state.join("op-journal.json"), b"retained evidence").unwrap();
    let at = std::time::Instant::now();
    let busy = s.cli(&["list"]);
    assert!(busy.status.success());
    assert!(at.elapsed() < Duration::from_secs(1));
    assert!(String::from_utf8_lossy(&busy.stderr).contains("skipped-busy"));
    assert!(String::from_utf8_lossy(&busy.stdout).contains("visible local issue"));
    assert_eq!(
        fs::read(state.join("op-journal.json")).unwrap(),
        b"retained evidence"
    );
}
