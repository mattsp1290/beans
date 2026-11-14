use serde_json::Value;
use std::{
    fs,
    io::Write,
    os::{fd::AsRawFd, unix::fs::PermissionsExt},
    path::PathBuf,
    process::{Command, Output, Stdio},
    time::{Duration, Instant},
};

struct Fixture {
    root: PathBuf,
    home: PathBuf,
    channel: String,
}
impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.root);
    }
}
impl Fixture {
    fn new() -> Self {
        let mut random = [0; 8];
        getrandom::fill(&mut random).unwrap();
        let root = std::env::temp_dir().join(format!(
            "bn-channel-wait-{}-{}",
            std::process::id(),
            u64::from_ne_bytes(random)
        ));
        fs::create_dir_all(&root).unwrap();
        let remote = root.join("remote.git");
        let output = Command::new("git")
            .args(["init", "--bare", "--initial-branch=main"])
            .arg(&remote)
            .output()
            .unwrap();
        assert!(output.status.success());
        let home = root.join("home");
        fs::create_dir(&home).unwrap();
        let mut fixture = Self {
            root,
            home,
            channel: String::new(),
        };
        assert!(
            fixture
                .command(&["init", remote.to_str().unwrap()])
                .output()
                .unwrap()
                .status
                .success()
        );
        fixture.channel = fixture.ok(&["channel", "create", "waiting"])["id"]
            .as_str()
            .unwrap()
            .into();
        fixture
    }
    fn hub(&self) -> PathBuf {
        self.home.join(".beans/hub")
    }
    fn command(&self, args: &[&str]) -> Command {
        let mut command = Command::new(env!("CARGO_BIN_EXE_bn"));
        command
            .args(["--json", "--project", "test", "--actor", "agent"])
            .args(args)
            .current_dir(&self.home)
            .env("HOME", &self.home)
            .env("BEANS_HOME", self.home.join(".beans"))
            .env("BN_NO_UPDATE_CHECK", "1")
            .env("GIT_CONFIG_GLOBAL", "/dev/null")
            .env("GIT_CONFIG_NOSYSTEM", "1")
            .env("GIT_TERMINAL_PROMPT", "0")
            .env_remove("BEANS_HUB")
            .env_remove("BEANS_PROJECT")
            .env_remove("BN_ACTOR")
            .env_remove("BN_CONFIG")
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        command
    }
    fn ok(&self, args: &[&str]) -> Value {
        success(self.command(args).output().unwrap())
    }
    fn git(&self, args: &[&str]) {
        let output = Command::new("git")
            .arg("-C")
            .arg(self.hub())
            .args(args)
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
    }
    fn tools(&self, body: &str) -> PathBuf {
        let tools = self.root.join("tools");
        fs::create_dir_all(&tools).unwrap();
        let git = tools.join("git");
        fs::write(
            &git,
            format!("#!/bin/sh\n{body}\nexec /usr/bin/git \"$@\"\n"),
        )
        .unwrap();
        fs::set_permissions(&git, fs::Permissions::from_mode(0o755)).unwrap();
        tools
    }
}
fn success(output: Output) -> Value {
    assert!(
        output.status.success(),
        "stdout={} stderr={}",
        String::from_utf8_lossy(&output.stdout),
        String::from_utf8_lossy(&output.stderr)
    );
    serde_json::from_slice(&output.stdout).unwrap()
}

#[test]
fn slow_quiet_polls_end_in_normal_timeout() {
    let f = Fixture::new();
    let tools = f.tools("for arg in \"$@\"; do if test \"$arg\" = fetch; then sleep 0.7; fi; done");
    let at = Instant::now();
    let result = success(
        f.command(&[
            "channel",
            "wait",
            &f.channel,
            "--timeout",
            "2",
            "--interval",
            "1",
        ])
        .env("PATH", format!("{}:/usr/bin:/bin", tools.display()))
        .output()
        .unwrap(),
    );
    assert_eq!(result["status"], "timeout");
    assert!(at.elapsed() < Duration::from_secs(4));
    assert_eq!(result["cursor"]["channel"], f.channel);
}

#[test]
fn busy_lock_is_retried_and_wait_timeout_preserves_cursor() {
    let f = Fixture::new();
    let read = f.ok(&["--no-fetch", "channel", "read", &f.channel]);
    let cursor = read["cursor"].to_string();
    let state = f.home.join(".beans/.beans-state/hub");
    let lock = fs::OpenOptions::new()
        .read(true)
        .write(true)
        .open(state.join("hub.lock"))
        .unwrap();
    assert_eq!(
        unsafe { libc::flock(lock.as_raw_fd(), libc::LOCK_EX | libc::LOCK_NB) },
        0
    );
    let result = f.ok(&[
        "channel",
        "wait",
        &f.channel,
        "--cursor",
        &cursor,
        "--timeout",
        "1",
        "--interval",
        "1",
    ]);
    assert_eq!(result["status"], "timeout");
    assert_eq!(result["freshness"]["refresh_reason"], "busy");
    assert_eq!(result["cursor"], read["cursor"]);
    let waiting = f
        .command(&[
            "channel",
            "wait",
            &f.channel,
            "--cursor",
            &cursor,
            "--timeout",
            "3",
            "--interval",
            "1",
        ])
        .spawn()
        .unwrap();
    std::thread::sleep(Duration::from_millis(300));
    assert_eq!(unsafe { libc::flock(lock.as_raw_fd(), libc::LOCK_UN) }, 0);
    let result = success(waiting.wait_with_output().unwrap());
    assert_eq!(result["status"], "timeout");
    assert_eq!(result["freshness"]["refresh_reason"], "refreshed");
}

#[test]
fn throttled_failed_refresh_reports_cached_local_state() {
    let f = Fixture::new();
    f.git(&[
        "remote",
        "set-url",
        "origin",
        f.root.join("missing.git").to_str().unwrap(),
    ]);
    let first = f.ok(&["channel", "read", &f.channel]);
    assert!(first["freshness"]["sync_error"].is_string());
    assert_eq!(first["freshness"]["refresh_reason"], "failed");
    let second = f.ok(&["channel", "read", &f.channel]);
    assert_eq!(second["freshness"]["refresh_skipped"], true);
    assert_eq!(second["freshness"]["refresh_reason"], "throttled");
    assert!(second["freshness"]["sync_error"].is_string());
    assert_eq!(
        first["freshness"]["last_fetch_attempt"],
        second["freshness"]["last_fetch_attempt"]
    );
}

#[test]
fn large_cursor_resumes_from_file_and_stdin_and_nonmembers_join_first() {
    let f = Fixture::new();
    let seen: Vec<_> = (0..4000).map(|n| format!("msg-{n:064x}")).collect();
    let cursor = serde_json::json!({"channel":f.channel,"seen":seen}).to_string();
    assert!(cursor.len() > 131072);
    let file = f.root.join("cursor.json");
    fs::write(&file, &cursor).unwrap();
    let read = f.ok(&[
        "--no-fetch",
        "channel",
        "read",
        &f.channel,
        "--cursor-file",
        file.to_str().unwrap(),
    ]);
    assert_eq!(read["cursor"]["seen"].as_array().unwrap().len(), 4000);
    let mut child = f
        .command(&[
            "--no-fetch",
            "channel",
            "read",
            &f.channel,
            "--cursor-file",
            "-",
        ])
        .stdin(Stdio::piped())
        .spawn()
        .unwrap();
    child
        .stdin
        .take()
        .unwrap()
        .write_all(cursor.as_bytes())
        .unwrap();
    let read = success(child.wait_with_output().unwrap());
    assert_eq!(read["cursor"]["seen"].as_array().unwrap().len(), 4000);
    let rejected = f
        .command(&[
            "channel",
            "read",
            &f.channel,
            "--cursor",
            "{}",
            "--cursor-file",
            file.to_str().unwrap(),
        ])
        .output()
        .unwrap();
    assert_eq!(rejected.status.code(), Some(2));
    let rejected = f
        .command(&["channel", "wait", &f.channel, "--timeout", "1"])
        .args(["--actor", "outsider"])
        .output()
        .unwrap();
    assert!(!rejected.status.success());
    assert!(String::from_utf8_lossy(&rejected.stderr).contains("join first"));
}
