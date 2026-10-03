//! `bn upgrade` control flow against `file://` release trees. The released
//! "binary" is a shell script; real binaries and HTTPS are covered by the
//! release workflow's smoke job.
use super::*;

const TEMP_PREFIX: &str = ".bn-upgrade-";

fn target() -> String {
    format!("{}-{}", std::env::consts::OS, std::env::consts::ARCH)
}
fn script(version: &str) -> String {
    format!("#!/bin/sh\necho 'bn {version}'\n")
}
fn sha256(path: &Path) -> String {
    let tool = |name: &str, args: &[&str]| {
        let out = Command::new(name).args(args).arg(path).output().ok()?;
        let text = String::from_utf8(out.stdout).ok()?;
        out.status
            .success()
            .then(|| text.split_whitespace().next().map(str::to_owned))?
    };
    tool("sha256sum", &[])
        .or_else(|| tool("shasum", &["-a", "256"]))
        .expect("sha256sum or shasum")
}

/// One sandbox holding a release tree and a private copy of the built `bn`.
/// `upgrade` never runs on the Cargo-built binary itself.
struct Releases {
    /// Removes the tree on drop.
    _sandbox: Sandbox,
    /// Canonical, because `bn` reports its own canonical path.
    root: PathBuf,
    bn: PathBuf,
    original: Vec<u8>,
}
impl Releases {
    /// `None` when `curl` is absent. Publishes `latest` as the latest release.
    fn new(latest: &str) -> Option<Self> {
        Self::installed_at("install/bn", latest)
    }
    fn installed_at(relative: &str, latest: &str) -> Option<Self> {
        if Command::new("curl").arg("--version").output().is_err() {
            eprintln!("skipping: curl is not on PATH");
            return None;
        }
        let sandbox = Sandbox::new();
        let root = fs::canonicalize(&sandbox.0).unwrap();
        let bn = root.join(relative);
        fs::create_dir_all(bn.parent().unwrap()).unwrap();
        fs::copy(env!("CARGO_BIN_EXE_bn"), &bn).unwrap();
        let releases = Self {
            original: fs::read(&bn).unwrap(),
            _sandbox: sandbox,
            root,
            bn,
        };
        let manifest = releases.publish(latest, &script(latest));
        let dir = releases.dir("latest/download");
        fs::create_dir_all(&dir).unwrap();
        fs::copy(manifest, dir.join("bn-manifest.json")).unwrap();
        Some(releases)
    }
    fn dir(&self, relative: &str) -> PathBuf {
        self.root.join("releases").join(relative)
    }
    fn base(&self) -> String {
        format!("file://{}/releases", self.root.display())
    }
    fn asset(&self, tag: &str) -> PathBuf {
        self.dir(&format!("download/{tag}/bn-{}", target()))
    }
    /// Writes the pinned tree for `tag` and returns its manifest path.
    fn publish(&self, tag: &str, binary: &str) -> PathBuf {
        let asset = self.asset(tag);
        fs::create_dir_all(asset.parent().unwrap()).unwrap();
        fs::write(&asset, binary).unwrap();
        let manifest = serde_json::json!({
            "schema": 1,
            "version": tag.trim_start_matches('v'),
            "tag": tag,
            "assets": {target(): format!("{}/download/{tag}/bn-{}", self.base(), target())},
            "sha256": {target(): sha256(&asset)},
        });
        let path = asset.with_file_name("bn-manifest.json");
        fs::write(&path, serde_json::to_string_pretty(&manifest).unwrap()).unwrap();
        path
    }
    fn latest_manifest(&self) -> PathBuf {
        self.dir("latest/download/bn-manifest.json")
    }
    fn command(&self, base: &str, args: &[&str]) -> Command {
        let mut command = Command::new(&self.bn);
        command
            .args(args)
            .env("BN_RELEASE_BASE_URL", base)
            .env_remove("BEANS_HOME")
            .env_remove("BEANS_HUB")
            .env_remove("BN_CONFIG")
            .env_remove("CARGO_HOME")
            .current_dir(&self.root);
        command
    }
    fn run(&self, args: &[&str]) -> Output {
        output(&mut self.command(&self.base(), args))
    }
    fn unchanged(&self) -> bool {
        fs::read(&self.bn).unwrap() == self.original && self.temps().is_empty()
    }
    fn temps(&self) -> Vec<String> {
        fs::read_dir(self.bn.parent().unwrap())
            .unwrap()
            .map(|entry| entry.unwrap().file_name().to_string_lossy().into_owned())
            .filter(|name| name.starts_with(TEMP_PREFIX))
            .collect()
    }
}
/// A sibling test forking between this copy's write and close can hold the
/// descriptor briefly, which makes the first exec fail with ETXTBSY.
fn output(command: &mut Command) -> Output {
    for _ in 0..50 {
        match command.output() {
            Err(e) if e.raw_os_error() == Some(libc::ETXTBSY) => {
                std::thread::sleep(Duration::from_millis(20));
            }
            result => return result.unwrap(),
        }
    }
    panic!("copied bn stayed busy");
}
fn stderr(output: &Output) -> String {
    String::from_utf8_lossy(&output.stderr).into_owned()
}
fn failed(output: &Output) -> String {
    assert_eq!(output.status.code(), Some(1), "{}", stderr(output));
    stderr(output)
}

#[test]
fn upgrade_force_replaces_the_binary_in_place() {
    let Some(r) = Releases::new("v9.9.9") else {
        return;
    };
    let out = r.run(&["upgrade", "--force"]);
    assert!(out.status.success(), "{}", stderr(&out));
    let stdout = String::from_utf8_lossy(&out.stdout);
    assert!(stdout.contains("installed bn v9.9.9 at"), "{stdout}");
    assert!(stdout.contains("restart any running 'bn serve'"));
    assert_eq!(fs::read_to_string(&r.bn).unwrap(), script("v9.9.9"));
    let mode = fs::metadata(&r.bn).unwrap().permissions().mode();
    assert_eq!(mode & 0o777, 0o755);
    assert!(r.temps().is_empty());
}

#[test]
fn upgrade_version_installs_the_pinned_release() {
    let Some(r) = Releases::new("v9.9.9") else {
        return;
    };
    r.publish("v9.9.8", &script("v9.9.8"));
    let out = r.run(&["upgrade", "--version", "v9.9.8"]);
    assert!(out.status.success(), "{}", stderr(&out));
    // The root `--version` banner would print `bn <BN_VERSION>` and install nothing.
    let stdout = String::from_utf8_lossy(&out.stdout);
    assert!(!stdout.starts_with("bn "), "{stdout}");
    assert_eq!(fs::read_to_string(&r.bn).unwrap(), script("v9.9.8"));
    let Some(r) = Releases::new("v9.9.9") else {
        return;
    };
    assert!(failed(&r.run(&["upgrade", "--version", "9.9.9"])).contains("vX.Y.Z"));
    // A pinned manifest that describes another release is refused.
    fs::create_dir_all(r.dir("download/v9.9.7")).unwrap();
    fs::copy(
        r.latest_manifest(),
        r.dir("download/v9.9.7/bn-manifest.json"),
    )
    .unwrap();
    assert!(failed(&r.run(&["upgrade", "--version", "v9.9.7"])).contains("v9.9.9"));
    assert!(r.unchanged());
}

#[test]
fn upgrade_check_reports_json_without_a_hub_or_home() {
    let Some(r) = Releases::new("v9.9.9") else {
        return;
    };
    let out = output(
        r.command(&r.base(), &["upgrade", "--check", "--json"])
            .env_remove("HOME"),
    );
    assert!(out.status.success(), "{}", stderr(&out));
    let report: serde_json::Value = serde_json::from_slice(&out.stdout).unwrap();
    let mut keys: Vec<_> = report.as_object().unwrap().keys().cloned().collect();
    keys.sort();
    assert_eq!(
        keys,
        [
            "current",
            "latest",
            "path",
            "target",
            "update_available",
            "upgraded"
        ]
    );
    assert_eq!(report["latest"], "v9.9.9");
    assert_eq!(report["target"], target());
    assert_eq!(report["path"], r.bn.to_str().unwrap());
    assert_eq!(report["upgraded"], false);
    assert!(report["update_available"].is_boolean());
    assert!(r.unchanged());
    // A trailing slash on the base names the same release tree.
    let slashed =
        output(&mut r.command(&format!("{}/", r.base()), &["upgrade", "--check", "--json"]));
    assert!(slashed.status.success(), "{}", stderr(&slashed));
    assert_eq!(
        serde_json::from_slice::<serde_json::Value>(&slashed.stdout).unwrap(),
        report
    );
}

#[test]
fn upgrade_failures_leave_the_binary_and_directory_untouched() {
    let Some(r) = Releases::new("v9.9.9") else {
        return;
    };
    fs::write(r.asset("v9.9.9"), "tampered\n").unwrap();
    assert!(failed(&r.run(&["upgrade", "--force"])).contains("checksum"));
    assert!(r.unchanged());

    // The checksum matches but the binary reports another version.
    let manifest = r.publish("v9.9.9", &script("v1.1.1"));
    fs::copy(manifest, r.latest_manifest()).unwrap();
    assert!(failed(&r.run(&["upgrade", "--force"])).contains("bn v1.1.1"));
    assert!(r.unchanged());

    let text = fs::read_to_string(r.latest_manifest()).unwrap();
    fs::write(r.latest_manifest(), text.replace(&target(), "plan9-mips")).unwrap();
    assert!(failed(&r.run(&["upgrade", "--force"])).contains(&target()));
    assert!(r.unchanged());

    fs::write(r.latest_manifest(), " ".repeat(64 * 1024 + 1)).unwrap();
    assert!(failed(&r.run(&["upgrade", "--force"])).contains("too large"));
    assert!(r.unchanged());

    let unreachable = output(&mut r.command("file:///nonexistent", &["upgrade", "--force"]));
    failed(&unreachable);
    let insecure = output(&mut r.command("http://example.invalid/r", &["upgrade", "--force"]));
    assert!(failed(&insecure).contains("https://"));
    assert!(r.unchanged());
}

#[test]
fn upgrade_reports_a_read_only_install_directory() {
    let Some(r) = Releases::new("v9.9.9") else {
        return;
    };
    if unsafe { libc::geteuid() } == 0 {
        eprintln!("skipping: root ignores directory permissions");
        return;
    }
    let dir = r.bn.parent().unwrap();
    fs::set_permissions(dir, fs::Permissions::from_mode(0o555)).unwrap();
    let out = r.run(&["upgrade", "--force"]);
    fs::set_permissions(dir, fs::Permissions::from_mode(0o755)).unwrap();
    assert!(failed(&out).contains("install directory not writable"));
    assert!(r.unchanged());
}

#[test]
fn upgrade_refuses_build_tree_binaries_unless_forced() {
    let Some(r) = Releases::installed_at("target/release/bn", "v9.9.9") else {
        return;
    };
    assert!(failed(&r.run(&["upgrade"])).contains("--force"));
    assert!(r.unchanged());
    // Reporting is always allowed.
    assert!(r.run(&["upgrade", "--check"]).status.success());
    let out = r.run(&["upgrade", "--force"]);
    assert!(out.status.success(), "{}", stderr(&out));
    assert_eq!(fs::read_to_string(&r.bn).unwrap(), script("v9.9.9"));
}

#[test]
fn upgrade_is_documented_and_update_still_edits_issues() {
    let s = Sandbox::new();
    assert!(s.ok(&["man"]).contains("bn upgrade"));
    let update = s.ok(&["update", "--help"]);
    assert!(
        update.contains("<id>") || update.contains("[id]"),
        "{update}"
    );
    assert!(!update.contains("release"), "{update}");
    assert!(s.ok(&["upgrade", "--help"]).contains("--version <tag>"));
}
