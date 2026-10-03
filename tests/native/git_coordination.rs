use super::*;
use std::process::{Child, Stdio};

fn finish(child: &mut Child) -> std::process::ExitStatus {
    let start = std::time::Instant::now();
    loop {
        if let Some(status) = child.try_wait().unwrap() {
            return status;
        }
        if start.elapsed() > Duration::from_secs(15) {
            child.kill().unwrap();
            child.wait().unwrap();
            panic!("native child watchdog expired");
        }
        std::thread::sleep(Duration::from_millis(10));
    }
}

fn timed_policy(s: &Sandbox) {
    fs::write(s.path("home/config.toml"), "[git]\ncommand_timeout='500ms'\nnetwork_timeout='500ms'\ncleanup_timeout='100ms'\nlock_timeout='100ms'\ndiagnostics=true\n").unwrap();
}

fn timed_cli(s: &Sandbox, phase: &str, args: &[&str], after: bool) -> Output {
    let tools = s.path("timed-tools");
    fs::create_dir_all(&tools).unwrap();
    let script = format!(
        r#"#!/bin/sh
phase='{}'
for arg in "$@"; do
 if test "$arg" = "$phase"; then
  {}
  echo entered > '{}'
  sleep 10
 fi
done
exec /usr/bin/git "$@"
"#,
        phase,
        if after {
            "/usr/bin/git \"$@\" || exit $?"
        } else {
            ":"
        },
        s.path("timed-entered").display()
    );
    fs::write(tools.join("git"), script).unwrap();
    fs::set_permissions(tools.join("git"), fs::Permissions::from_mode(0o755)).unwrap();
    let config = s.path("home/config.toml");
    let original_config = fs::read(&config).ok();
    timed_policy(s);
    let output = Command::new(env!("CARGO_BIN_EXE_bn"))
        .args(["--branch", "main", "--project", "demo"])
        .args(args)
        .env("BEANS_HOME", s.path("home"))
        .env("BN_ACTOR", "Tester")
        .env("PATH", format!("{}:/usr/bin:/bin", tools.display()))
        .env_remove("BN_CONFIG")
        .env_remove("BEANS_HUB")
        .current_dir(&s.0)
        .output()
        .unwrap();
    if let Some(bytes) = original_config {
        fs::write(config, bytes).unwrap();
    } else {
        fs::remove_file(config).unwrap();
    }
    output
}
#[path = "coordination_diagnostics.rs"]
mod coordination_diagnostics;
#[path = "coordination_failures.rs"]
mod coordination_failures;
#[path = "coordination_identity.rs"]
mod coordination_identity;
#[path = "coordination_state.rs"]
mod coordination_state;
