//! Executor lifecycle tests run private PATH fixtures in isolated subprocesses.
use beans::gitops::{ExecutionPolicy, GitExecutor};
use std::{
    fs,
    os::unix::fs::PermissionsExt,
    process::{Command, Stdio},
    time::{Duration, Instant},
};

struct TempDir(std::path::PathBuf);
impl TempDir {
    fn new() -> Self {
        let mut random = [0u8; 8];
        getrandom::fill(&mut random).unwrap();
        let p = std::env::temp_dir().join(format!(
            "bn-runner-{}-{}",
            std::process::id(),
            u64::from_ne_bytes(random)
        ));
        fs::create_dir(&p).unwrap();
        Self(p)
    }
    fn path(&self) -> &std::path::Path {
        &self.0
    }
}
impl Drop for TempDir {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

#[test]
fn runner_child() {
    let Ok(path) = std::env::var("BN_RUNNER_RESULT") else {
        return;
    };
    let executor = GitExecutor {
        policy: ExecutionPolicy {
            command_timeout: Duration::from_millis(150),
            ..Default::default()
        },
    };
    let at = Instant::now();
    let result = match executor.run(None, ["fixture"], "fixture") {
        Ok(out) => {
            serde_json::json!({"code":out.status.code(), "stdout":out.stdout, "stderr":out.stderr})
        }
        Err(e) => {
            serde_json::json!({"category":format!("{:?}",e.category()), "error":e.to_string()})
        }
    };
    assert!(at.elapsed() < Duration::from_secs(3));
    fs::write(path, serde_json::to_vec(&result).unwrap()).unwrap();
}
fn fixture(script: &str) -> (serde_json::Value, TempDir) {
    let dir = TempDir::new();
    fs::write(dir.path().join("git"), format!("#!/bin/sh\n{script}\n")).unwrap();
    fs::set_permissions(dir.path().join("git"), fs::Permissions::from_mode(0o755)).unwrap();
    let result = dir.path().join("result.json");
    let mut child = Command::new(std::env::current_exe().unwrap())
        .args(["--exact", "runner_child", "--nocapture"])
        .env("BN_RUNNER_RESULT", &result)
        .env("BN_RUNNER_DIR", dir.path())
        .env("PATH", format!("{}:/usr/bin:/bin", dir.path().display()))
        .stdout(Stdio::null())
        .stderr(Stdio::inherit())
        .spawn()
        .unwrap();
    let at = Instant::now();
    loop {
        if let Some(status) = child.try_wait().unwrap() {
            assert!(status.success());
            break;
        }
        if at.elapsed() > Duration::from_secs(5) {
            child.kill().unwrap();
            child.wait().unwrap();
            panic!("runner subprocess watchdog");
        }
        std::thread::sleep(Duration::from_millis(5));
    }
    (
        serde_json::from_slice(&fs::read(result).unwrap()).unwrap(),
        dir,
    )
}
#[test]
fn raw_output_and_ordinary_nonzero_are_preserved() {
    let (r, _) = fixture("printf '\\377raw'; printf 'cause' >&2; exit 1");
    assert_eq!(r["code"], 1);
    assert_eq!(r["stdout"], serde_json::json!([255, 114, 97, 119]));
    assert_eq!(r["stderr"], serde_json::json!(b"cause".to_vec()));
}
#[test]
fn both_pipes_drain_without_deadlock() {
    let (r, _) = fixture("head -c 100000 /dev/zero; head -c 100000 /dev/zero >&2");
    assert_eq!(r["code"], 0);
    assert_eq!(r["stdout"].as_array().unwrap().len(), 100000);
    assert_eq!(r["stderr"].as_array().unwrap().len(), 100000);
}
#[test]
fn stalled_leader_and_inherited_pipe_descendant_are_terminated() {
    for script in [
        "echo $$ > \"$BN_RUNNER_DIR/leader\"; sleep 10",
        "(sleep 1; echo leaked > \"$BN_RUNNER_DIR/leaked\") & echo $! > \"$BN_RUNNER_DIR/descendant\"; exit 0",
    ] {
        let (r, dir) = fixture(script);
        assert_eq!(r["category"], "Some(GitTimeout)");
        if let Ok(pid) = fs::read_to_string(dir.path().join("leader")) {
            let pid: i32 = pid.trim().parse().unwrap();
            assert_eq!(
                unsafe { libc::kill(pid, 0) },
                -1,
                "direct child was not reaped"
            );
        }
        std::thread::sleep(Duration::from_millis(1100));
        assert!(!dir.path().join("leaked").exists());
    }
}
#[test]
fn closed_pipe_background_helper_cannot_outlive_return() {
    let (r, dir) = fixture(
        "(exec >/dev/null 2>&1; sleep 1; echo leaked > \"$BN_RUNNER_DIR/leaked\") & exit 0",
    );
    assert_eq!(r["code"], 0);
    std::thread::sleep(Duration::from_millis(1100));
    assert!(!dir.path().join("leaked").exists());
}

#[test]
fn signal_exit_remains_distinct_from_ordinary_exit_status() {
    let (r, _) = fixture("kill -TERM $$");
    assert!(r["code"].is_null());
    assert!(r.get("error").is_none());
}
