struct Temp(std::path::PathBuf);
impl Temp {
    fn new() -> Self {
        let mut random = [0; 8];
        getrandom::fill(&mut random).unwrap();
        let path = std::env::temp_dir().join(format!(
            "bn-update-check-{}-{}",
            std::process::id(),
            u64::from_ne_bytes(random)
        ));
        std::fs::create_dir(&path).unwrap();
        Self(path)
    }
    fn path(&self) -> &std::path::Path {
        &self.0
    }
}
impl Drop for Temp {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}
use std::{fs, process::Command};

fn command(home: &std::path::Path, base: &str, args: &[&str]) -> std::process::Output {
    Command::new(env!("CARGO_BIN_EXE_bn"))
        .args(args)
        .env("BEANS_HOME", home)
        .env("BN_RELEASE_BASE_URL", base)
        .env_remove("BN_NO_UPDATE_CHECK")
        .output()
        .unwrap()
}

#[test]
fn every_invocation_uses_an_hourly_cache_without_changing_stdout_or_exit() {
    let tree = Temp::new();
    let base = format!("file://{}/releases", tree.path().display());
    let latest = tree.path().join("releases/latest/download");
    fs::create_dir_all(&latest).unwrap();
    let target = format!("{}-{}", std::env::consts::OS, std::env::consts::ARCH);
    let tag = "v999999.0.0";
    let manifest = serde_json::json!({"schema":1,"version":"999999.0.0","tag":tag,
        "assets":{&target: format!("{base}/download/{tag}/bn-{target}")},
        "sha256":{&target:"0".repeat(64)}});
    fs::write(latest.join("bn-manifest.json"), manifest.to_string()).unwrap();
    let home = tree.path().join("home");
    let first = command(&home, &base, &["--version"]);
    assert!(first.status.success());
    assert!(String::from_utf8_lossy(&first.stdout).starts_with("bn "));
    let cache = home.join("cache/update-check.json");
    let original = fs::read(&cache).unwrap();
    let saved: serde_json::Value = serde_json::from_slice(&original).unwrap();
    assert_eq!(saved["latest"], tag);
    let explicit = command(&home, &base, &["upgrade", "--check", "--json"]);
    assert!(
        explicit.status.success(),
        "{}",
        String::from_utf8_lossy(&explicit.stderr)
    );
    let json: serde_json::Value = serde_json::from_slice(&explicit.stdout).unwrap();
    assert_eq!(json["latest"], tag);
    assert_eq!(fs::read(&cache).unwrap(), original);
    if env!("BN_VERSION").starts_with('v') {
        assert!(String::from_utf8_lossy(&first.stderr).contains("Run `bn upgrade`"));
    }
    fs::remove_file(latest.join("bn-manifest.json")).unwrap();
    let second = command(&home, &base, &["--version"]);
    assert!(second.status.success());
    assert_eq!(first.stdout, second.stdout);
    assert_eq!(fs::read(&cache).unwrap(), original);
    let invalid = command(&home, &base, &["missing-command"]);
    assert_eq!(invalid.status.code(), Some(2));
    assert_eq!(fs::read(&cache).unwrap(), original);
    let mut expired = saved;
    expired["attempted_at"] = serde_json::json!(1);
    fs::write(&cache, expired.to_string()).unwrap();
    let offline = command(&home, &base, &["--version"]);
    assert!(offline.status.success());
    let refreshed: serde_json::Value = serde_json::from_slice(&fs::read(&cache).unwrap()).unwrap();
    assert!(refreshed["attempted_at"].as_u64().unwrap() > 1);
    assert_eq!(refreshed["latest"], tag);
    let disabled = Command::new(env!("CARGO_BIN_EXE_bn"))
        .args(["--version"])
        .env("BEANS_HOME", tree.path().join("disabled"))
        .env("BN_RELEASE_BASE_URL", &base)
        .env("BN_NO_UPDATE_CHECK", "1")
        .output()
        .unwrap();
    assert!(disabled.status.success());
    assert!(!tree.path().join("disabled").exists());
}

#[test]
fn stalled_curl_is_killed_and_failure_has_the_same_hourly_ttl() {
    use std::{
        os::unix::fs::PermissionsExt,
        time::{Duration, Instant},
    };
    let tree = Temp::new();
    let tools = tree.path().join("tools");
    fs::create_dir(&tools).unwrap();
    let curl = tools.join("curl");
    fs::write(&curl, "#!/bin/sh\nexec /bin/sleep 60\n").unwrap();
    fs::set_permissions(&curl, fs::Permissions::from_mode(0o755)).unwrap();
    let home = tree.path().join("home");
    let run = || {
        Command::new(env!("CARGO_BIN_EXE_bn"))
            .args(["--version"])
            .env("BEANS_HOME", &home)
            .env("BN_RELEASE_BASE_URL", "https://example.invalid/releases")
            .env("PATH", format!("{}:/usr/bin:/bin", tools.display()))
            .env_remove("BN_NO_UPDATE_CHECK")
            .output()
            .unwrap()
    };
    let started = Instant::now();
    assert!(run().status.success());
    assert!(started.elapsed() < Duration::from_secs(3));
    fs::remove_file(&curl).unwrap();
    let started = Instant::now();
    assert!(run().status.success());
    assert!(started.elapsed() < Duration::from_millis(800));
}
