//! `bn upgrade`: replace the running binary with a published release.
//!
//! The command never touches the hub. It fetches the release manifest and the
//! binary through a `curl` subprocess, verifies the binary beside the current
//! executable and only then renames it into place.
use crate::domain::frontmatter::Error;
use sha2::{Digest, Sha256};
use std::{
    collections::BTreeMap,
    ffi::OsStr,
    fs,
    io::Read,
    os::unix::fs::PermissionsExt,
    path::{Component, Path, PathBuf},
    process::{Command, Stdio},
    time::{Duration, Instant},
};

const BASE_URL_ENV: &str = "BN_RELEASE_BASE_URL";
const DEFAULT_BASE_URL: &str = "https://github.com/mattsp1290/beans/releases";
const MANIFEST_LIMIT: u64 = 64 * 1024;
const TEMP_PREFIX: &str = ".bn-upgrade-";
const STALE_AFTER: Duration = Duration::from_secs(60 * 60);
const SMOKE_TIMEOUT: Duration = Duration::from_secs(10);
const SPAWN_ATTEMPTS: usize = 3;

type Version = (u64, u64, u64);

fn err(message: impl Into<String>) -> Error {
    Error::new(message.into())
}

fn parse_version(text: &str) -> Option<Version> {
    let mut parts = text.split('.').map(|part| {
        let decimal = !part.is_empty() && part.bytes().all(|b| b.is_ascii_digit());
        decimal.then(|| part.parse::<u64>().ok()).flatten()
    });
    let version = (parts.next()??, parts.next()??, parts.next()??);
    parts.next().is_none().then_some(version)
}

fn parse_tag(tag: &str) -> Option<Version> {
    parse_version(tag.strip_prefix('v')?)
}

/// The release a `git describe` version was built from: `vA.B.C`, optionally
/// followed by `-N-g<sha>` and then `-dirty`.
fn build_base(current: &str) -> Option<Version> {
    let described = current.strip_suffix("-dirty").unwrap_or(current);
    let mut parts = described.splitn(3, '-');
    let base = parse_tag(parts.next()?)?;
    match (parts.next(), parts.next()) {
        (None, _) => Some(base),
        (Some(count), Some(sha)) => {
            let hex = sha.strip_prefix('g')?;
            let described = !count.is_empty()
                && count.bytes().all(|b| b.is_ascii_digit())
                && !hex.is_empty()
                && hex.bytes().all(|b| b.is_ascii_hexdigit());
            described.then_some(base)
        }
        (Some(_), None) => None,
    }
}

#[derive(Clone, Copy, Debug, PartialEq)]
enum Decision {
    Upgrade,
    UpToDate,
    /// The running build has no release version to compare with.
    Unknown,
}

fn decide(current: &str, latest: Version) -> Decision {
    match build_base(current) {
        Some(base) if latest > base => Decision::Upgrade,
        Some(_) => Decision::UpToDate,
        None => Decision::Unknown,
    }
}

fn status_line(decision: Decision, current: &str, latest: &str) -> String {
    match decision {
        Decision::Upgrade => format!("bn {latest} is available (current {current})"),
        Decision::UpToDate => format!("bn {current} is up to date"),
        Decision::Unknown => {
            format!("cannot compare bn {current} with {latest}; re-run with --force")
        }
    }
}

/// True for binaries owned by Cargo or sitting in a build tree: under one of
/// `cargo_bins`, or below `target/{debug,release}` with an optional target
/// triple in between.
fn is_managed(exe: &Path, cargo_bins: &[PathBuf]) -> bool {
    if cargo_bins.iter().any(|bin| exe.starts_with(bin)) {
        return true;
    }
    let names: Vec<&OsStr> = exe
        .components()
        .filter_map(|component| match component {
            Component::Normal(name) => Some(name),
            _ => None,
        })
        .collect();
    let profile = |name: Option<&&OsStr>| name.is_some_and(|n| *n == "debug" || *n == "release");
    names.iter().enumerate().any(|(at, name)| {
        *name == "target" && (profile(names.get(at + 1)) || profile(names.get(at + 2)))
    })
}

fn cargo_bins() -> Vec<PathBuf> {
    let from = |name: &str, suffix: &str| {
        let root = std::env::var_os(name).filter(|value| !value.is_empty())?;
        let bin = PathBuf::from(root).join(suffix);
        Some(fs::canonicalize(&bin).unwrap_or(bin))
    };
    [from("CARGO_HOME", "bin"), from("HOME", ".cargo/bin")]
        .into_iter()
        .flatten()
        .collect()
}

fn target() -> Result<String, Error> {
    use std::env::consts::{ARCH, OS};
    if matches!(OS, "linux" | "macos") && matches!(ARCH, "x86_64" | "aarch64") {
        Ok(format!("{OS}-{ARCH}"))
    } else {
        Err(err(format!("no release binary for {OS}-{ARCH}")))
    }
}

fn release_base(raw: &str) -> Result<&str, Error> {
    let base = raw.trim_end_matches('/');
    if base.starts_with("https://") || base.starts_with("file://") {
        Ok(base)
    } else {
        Err(err(format!(
            "{BASE_URL_ENV} must start with https:// or file://"
        )))
    }
}

#[derive(serde::Deserialize)]
struct Manifest {
    version: String,
    tag: String,
    assets: BTreeMap<String, String>,
    sha256: BTreeMap<String, String>,
}

#[derive(Debug)]
struct Release {
    tag: String,
    version: Version,
    url: String,
    sha256: String,
}

/// Checks a schema 1 manifest and selects `target`. `requested` is the tag
/// given with `--version`.
fn validate(
    raw: &[u8],
    base: &str,
    target: &str,
    requested: Option<&str>,
) -> Result<Release, Error> {
    let invalid = |reason: &str| err(format!("invalid release manifest: {reason}"));
    let json: serde_json::Value =
        serde_json::from_slice(raw).map_err(|e| invalid(&e.to_string()))?;
    if json["schema"] != 1 {
        return Err(err("unsupported release manifest schema"));
    }
    let manifest: Manifest = serde_json::from_value(json).map_err(|e| invalid(&e.to_string()))?;
    let version = parse_version(&manifest.version).ok_or_else(|| invalid("version"))?;
    let tag = manifest.tag;
    if tag != format!("v{}", manifest.version) {
        return Err(invalid("tag does not match version"));
    }
    if requested.is_some_and(|requested| requested != tag) {
        return Err(invalid(&format!("it describes {tag}")));
    }
    if !manifest.assets.keys().eq(manifest.sha256.keys()) {
        return Err(invalid("assets and sha256 name different targets"));
    }
    let (Some(url), Some(sha256)) = (manifest.assets.get(target), manifest.sha256.get(target))
    else {
        return Err(err(format!("no release binary for {target} in {tag}")));
    };
    if *url != format!("{base}/download/{tag}/bn-{target}") {
        return Err(invalid("asset URL is outside the release"));
    }
    let hex = |b: u8| b.is_ascii_digit() || (b'a'..=b'f').contains(&b);
    if sha256.len() != 64 || !sha256.bytes().all(hex) {
        return Err(invalid("sha256"));
    }
    Ok(Release {
        tag,
        version,
        url: url.clone(),
        sha256: sha256.clone(),
    })
}

fn curl(base: &str, max_time: &str) -> Command {
    let mut curl = Command::new("curl");
    curl.args(["-fsSL", "--retry", "3", "--connect-timeout", "10"])
        .args(["--max-time", max_time])
        .stdin(Stdio::null());
    if base.starts_with("https://") {
        curl.args(["--proto", "=https", "--proto-redir", "=https"]);
    }
    curl
}

fn curl_error(error: std::io::Error) -> Error {
    if error.kind() == std::io::ErrorKind::NotFound {
        err("bn upgrade requires curl")
    } else {
        err(format!("curl: {error}"))
    }
}

fn curl_failure(what: &str, url: &str, stderr: &[u8]) -> Error {
    let detail = String::from_utf8_lossy(stderr);
    err(format!("could not fetch {what} {url}: {}", detail.trim()))
}

fn fetch_manifest(base: &str, url: &str) -> Result<Vec<u8>, Error> {
    let mut child = curl(base, "20")
        .args(["--url", url])
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .map_err(curl_error)?;
    let mut body = Vec::new();
    let read = child
        .stdout
        .take()
        .unwrap()
        .take(MANIFEST_LIMIT + 1)
        .read_to_end(&mut body);
    if body.len() as u64 > MANIFEST_LIMIT {
        let _ = child.kill();
        let _ = child.wait();
        return Err(err("release manifest too large"));
    }
    let output = child.wait_with_output().map_err(curl_error)?;
    read.map_err(curl_error)?;
    if !output.status.success() {
        return Err(curl_failure("release manifest", url, &output.stderr));
    }
    Ok(body)
}

fn download(base: &str, url: &str, destination: &Path) -> Result<(), Error> {
    let output = curl(base, "300")
        .arg("-o")
        .arg(destination)
        .args(["--url", url])
        .output()
        .map_err(curl_error)?;
    if !output.status.success() {
        return Err(curl_failure("release binary", url, &output.stderr));
    }
    Ok(())
}

fn sha256_hex(path: &Path) -> Result<String, Error> {
    let mut file = fs::File::open(path).map_err(|e| err(e.to_string()))?;
    let mut hasher = Sha256::new();
    let mut buffer = [0; 64 * 1024];
    loop {
        let read = file.read(&mut buffer).map_err(|e| err(e.to_string()))?;
        if read == 0 {
            break;
        }
        hasher.update(&buffer[..read]);
    }
    Ok(hasher
        .finalize()
        .iter()
        .map(|byte| format!("{byte:02x}"))
        .collect())
}

/// A same-directory download target, removed on every exit path. After the
/// rename into place the name no longer exists and removal is a no-op.
struct TempFile(PathBuf);

impl TempFile {
    /// Creates the file and closes it at once: no descriptor to it may be
    /// open in this process when it is executed.
    fn create(dir: &Path) -> Result<Self, Error> {
        let mut random = [0; 8];
        getrandom::fill(&mut random).map_err(|e| err(e.to_string()))?;
        let name = format!(
            "{TEMP_PREFIX}{}-{:016x}",
            std::process::id(),
            u64::from_ne_bytes(random)
        );
        let path = dir.join(name);
        fs::OpenOptions::new()
            .write(true)
            .create_new(true)
            .open(&path)
            .map_err(|e| {
                err(format!(
                    "install directory not writable: {} ({e})",
                    dir.display()
                ))
            })?;
        Ok(Self(path))
    }
}

impl Drop for TempFile {
    fn drop(&mut self) {
        let _ = fs::remove_file(&self.0);
    }
}

/// Best effort: an interrupted upgrade can leave its download behind.
fn remove_stale_temps(dir: &Path) {
    let Ok(entries) = fs::read_dir(dir) else {
        return;
    };
    for entry in entries.flatten() {
        let stale = entry.file_name().to_string_lossy().starts_with(TEMP_PREFIX)
            && entry.metadata().is_ok_and(|meta| {
                meta.is_file()
                    && meta
                        .modified()
                        .ok()
                        .and_then(|modified| modified.elapsed().ok())
                        .is_some_and(|age| age > STALE_AFTER)
            });
        if stale {
            let _ = fs::remove_file(entry.path());
        }
    }
}

/// Runs `<binary> --version` under `timeout` and returns its trimmed stdout.
fn reported_version(binary: &Path, timeout: Duration) -> Result<String, Error> {
    let failed = |reason: String| err(format!("downloaded bn did not run: {reason}"));
    let mut attempt = 0;
    let mut child = loop {
        attempt += 1;
        let spawned = Command::new(binary)
            .arg("--version")
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .spawn();
        match spawned {
            Ok(child) => break child,
            // A concurrent fork can briefly hold the write descriptor.
            Err(e) if e.raw_os_error() == Some(libc::ETXTBSY) && attempt < SPAWN_ATTEMPTS => {
                std::thread::sleep(Duration::from_millis(100));
            }
            Err(e) => return Err(failed(e.to_string())),
        }
    };
    let deadline = Instant::now() + timeout;
    let status = loop {
        match child.try_wait() {
            Ok(Some(status)) => break status,
            Ok(None) if Instant::now() < deadline => {
                std::thread::sleep(Duration::from_millis(20));
            }
            outcome => {
                let _ = child.kill();
                let _ = child.wait();
                return Err(failed(match outcome {
                    Err(e) => e.to_string(),
                    _ => format!("no version after {} seconds", timeout.as_secs_f32()),
                }));
            }
        }
    };
    if !status.success() {
        return Err(failed(status.to_string()));
    }
    let mut reported = String::new();
    child
        .stdout
        .take()
        .unwrap()
        .take(4096)
        .read_to_string(&mut reported)
        .map_err(|e| failed(e.to_string()))?;
    Ok(reported.trim().to_owned())
}

/// Downloads and verifies `release` beside `exe`, then renames it over `exe`.
/// Nothing is written to `exe` unless the checksum and the smoke run pass.
fn install(
    release: &Release,
    exe: &Path,
    base: &str,
    smoke_timeout: Duration,
) -> Result<(), Error> {
    let dir = exe
        .parent()
        .ok_or_else(|| err("bn executable has no parent directory"))?;
    let temp = TempFile::create(dir)?;
    download(base, &release.url, &temp.0)?;
    if sha256_hex(&temp.0)? != release.sha256 {
        return Err(err("downloaded bn checksum did not match"));
    }
    fs::set_permissions(&temp.0, fs::Permissions::from_mode(0o755))
        .map_err(|e| err(e.to_string()))?;
    let reported = reported_version(&temp.0, smoke_timeout)?;
    let expected = format!("bn {}", release.tag);
    if reported != expected {
        return Err(err(format!(
            "downloaded bn reported `{reported}`, expected `{expected}`"
        )));
    }
    fs::rename(&temp.0, exe).map_err(|e| err(format!("cannot replace {}: {e}", exe.display())))
}

struct Request<'a> {
    /// The running build's `BN_VERSION`.
    current: &'a str,
    /// Canonical path of the binary to replace.
    exe: &'a Path,
    base_url: &'a str,
    cargo_bins: &'a [PathBuf],
    check: bool,
    force: bool,
    tag: Option<&'a str>,
    smoke_timeout: Duration,
}

#[derive(Debug)]
struct Outcome {
    latest: String,
    target: String,
    update_available: bool,
    upgraded: bool,
}

/// The whole command. `progress` receives each plain-output line as it happens.
fn run(request: &Request, progress: &dyn Fn(&str)) -> Result<Outcome, Error> {
    let Request { current, exe, .. } = *request;
    if let Some(dir) = exe.parent() {
        remove_stale_temps(dir);
    }
    if !request.check && !request.force && is_managed(exe, request.cargo_bins) {
        return Err(err(format!(
            "{} was installed by Cargo or is a build tree binary; use `make install`, install with distribution/install.sh, or re-run with --force",
            exe.display()
        )));
    }
    let target = target()?;
    let base = release_base(request.base_url)?;
    let manifest_url = match request.tag {
        Some(tag) if parse_tag(tag).is_none() => {
            return Err(err(format!("invalid release tag {tag}: expected vX.Y.Z")));
        }
        Some(tag) => format!("{base}/download/{tag}/bn-manifest.json"),
        None => format!("{base}/latest/download/bn-manifest.json"),
    };
    let manifest = fetch_manifest(base, &manifest_url)?;
    let release = validate(&manifest, base, &target, request.tag)?;
    let decision = decide(current, release.version);
    let outcome = |upgraded| Outcome {
        latest: release.tag.clone(),
        target: target.clone(),
        update_available: decision != Decision::UpToDate,
        upgraded,
    };
    let status = status_line(decision, current, &release.tag);
    if request.check {
        progress(&status);
        return Ok(outcome(false));
    }
    if !request.force && request.tag.is_none() {
        match decision {
            Decision::Upgrade => {}
            Decision::UpToDate => {
                progress(&status);
                return Ok(outcome(false));
            }
            Decision::Unknown => return Err(err(status)),
        }
    }
    progress(&format!("upgrading bn {current} -> {}", release.tag));
    install(&release, exe, base, request.smoke_timeout)?;
    progress(&format!(
        "installed bn {} at {}",
        release.tag,
        exe.display()
    ));
    progress("restart any running 'bn serve' to use the new version");
    Ok(outcome(true))
}

pub(super) fn execute(matches: &clap::ArgMatches, json: bool) -> Result<(), Error> {
    let exe = std::env::current_exe()
        .and_then(fs::canonicalize)
        .map_err(|e| err(format!("cannot locate the running bn executable: {e}")))?;
    let base_url = match std::env::var(BASE_URL_ENV) {
        Ok(value) if !value.is_empty() => value,
        Ok(_) | Err(std::env::VarError::NotPresent) => DEFAULT_BASE_URL.to_owned(),
        Err(e) => return Err(err(format!("{BASE_URL_ENV}: {e}"))),
    };
    let current = env!("BN_VERSION");
    let outcome = run(
        &Request {
            current,
            exe: &exe,
            base_url: &base_url,
            cargo_bins: &cargo_bins(),
            check: matches.get_flag("check"),
            force: matches.get_flag("force"),
            tag: matches.get_one::<String>("tag").map(String::as_str),
            smoke_timeout: SMOKE_TIMEOUT,
        },
        &|line| {
            if !json {
                println!("{line}");
            }
        },
    )?;
    if json {
        println!(
            "{}",
            serde_json::json!({
                "current": current,
                "latest": outcome.latest,
                "target": outcome.target,
                "path": exe,
                "update_available": outcome.update_available,
                "upgraded": outcome.upgraded,
            })
        );
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    const BASE: &str = "https://example.org/releases";
    const DIGEST: &str = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";

    fn manifest(base: &str, tag: &str, target: &str, digest: &str) -> serde_json::Value {
        serde_json::json!({
            "schema": 1,
            "version": tag.trim_start_matches('v'),
            "tag": tag,
            "assets": {target: format!("{base}/download/{tag}/bn-{target}")},
            "sha256": {target: digest},
        })
    }

    fn checked(manifest: &serde_json::Value, requested: Option<&str>) -> Result<Release, Error> {
        validate(
            manifest.to_string().as_bytes(),
            BASE,
            "linux-x86_64",
            requested,
        )
    }

    #[test]
    fn versions_compare_numerically_and_reject_other_shapes() {
        assert_eq!(parse_tag("v0.10.2"), Some((0, 10, 2)));
        assert!(parse_tag("v0.10.2") > parse_tag("v0.9.9"));
        for bad in [
            "0.1.2",
            "v1.2",
            "v1.2.3.4",
            "v1.2.x",
            "v1..3",
            "v1.2.3-rc1",
            "v+1.2.3",
            "",
        ] {
            assert_eq!(parse_tag(bad), None, "{bad}");
        }
    }

    #[test]
    fn decision_table_follows_the_running_build() {
        use Decision::*;
        let latest = (1, 2, 3);
        for (current, expected) in [
            ("v1.2.2", Upgrade),
            ("v1.2.3", UpToDate),
            ("v1.10.0", UpToDate),
            ("v1.2.2-dirty", Upgrade),
            ("v1.2.3-dirty", UpToDate),
            ("v1.2.2-4-gabc1234", Upgrade),
            ("v1.2.2-4-gabc1234-dirty", Upgrade),
            ("v1.2.3-4-gabc1234", UpToDate),
            ("v1.3.0-1-gabc1234-dirty", UpToDate),
            ("dev", Unknown),
            ("abc1234", Unknown),
            ("abc1234-dirty", Unknown),
            ("v1.2.2-rc1", Unknown),
            ("v1.2.2-4-abc1234", Unknown),
            ("v1.2.2-x-gabc1234", Unknown),
        ] {
            assert_eq!(decide(current, latest), expected, "{current}");
        }
    }

    #[test]
    fn manifest_validation_fails_closed() {
        let good = manifest(BASE, "v1.2.3", "linux-x86_64", DIGEST);
        let release = checked(&good, Some("v1.2.3")).unwrap();
        assert_eq!(release.version, (1, 2, 3));
        assert_eq!(
            release.url,
            format!("{BASE}/download/v1.2.3/bn-linux-x86_64")
        );
        let broken = |edit: &dyn Fn(&mut serde_json::Value)| {
            let mut manifest = good.clone();
            edit(&mut manifest);
            checked(&manifest, None).unwrap_err().to_string()
        };
        assert!(broken(&|m| m["schema"] = 2.into()).contains("schema"));
        assert!(broken(&|m| m["schema"] = "1".into()).contains("schema"));
        assert!(broken(&|m| m["version"] = "1.2".into()).contains("version"));
        assert!(broken(&|m| m["tag"] = "v1.2.4".into()).contains("tag"));
        assert!(
            broken(&|m| m["sha256"]["macos-aarch64"] = DIGEST.into()).contains("different targets")
        );
        assert!(broken(&|m| m["sha256"]["linux-x86_64"] = "abc".into()).contains("sha256"));
        assert!(
            broken(&|m| m["sha256"]["linux-x86_64"] = DIGEST.to_uppercase().into())
                .contains("sha256")
        );
        for url in [
            "https://example.com/releases/download/v1.2.3/bn-linux-x86_64".to_owned(),
            format!("{BASE}/download/../x/bn-linux-x86_64"),
            format!("{BASE}/download/v1.2.3/bn-linux-x86_64?x"),
        ] {
            assert!(
                broken(&|m| m["assets"]["linux-x86_64"] = url.clone().into()).contains("outside"),
                "{url}"
            );
        }
        assert!(
            checked(&good, Some("v1.2.4"))
                .unwrap_err()
                .to_string()
                .contains("v1.2.3")
        );
        let missing = validate(good.to_string().as_bytes(), BASE, "macos-aarch64", None);
        assert!(missing.unwrap_err().to_string().contains("macos-aarch64"));
        assert!(validate(b"not json", BASE, "linux-x86_64", None).is_err());
    }

    #[test]
    fn base_url_needs_https_or_file_and_loses_trailing_slashes() {
        assert_eq!(release_base("https://example.org/r//").unwrap(), {
            "https://example.org/r"
        });
        assert_eq!(release_base("file:///tmp/r/").unwrap(), "file:///tmp/r");
        for bad in ["http://example.org/r", "example.org", "", "file://"] {
            assert!(release_base(bad).is_err(), "{bad}");
        }
    }

    #[test]
    fn managed_paths_cover_cargo_bins_and_build_trees() {
        let bins = [PathBuf::from("/home/u/.cargo/bin")];
        for managed in [
            "/home/u/.cargo/bin/bn",
            "/src/beans/target/release/bn",
            "/src/beans/target/debug/bn",
            "/src/beans/target/x86_64-unknown-linux-musl/release/bn",
            "/src/beans/target/debug/deps/bn-0123",
        ] {
            assert!(is_managed(Path::new(managed), &bins), "{managed}");
        }
        for free in [
            "/home/u/.local/bin/bn",
            "/src/beans/bin/bn",
            "/home/u/.cargo/bn",
            "/opt/target/bn",
            "/opt/target/a/b/release/bn",
            "/opt/release/bn",
        ] {
            assert!(!is_managed(Path::new(free), &bins), "{free}");
        }
    }

    /// A `file://` release tree with one published release and an installed
    /// stand-in binary. The released "binary" is a shell script.
    struct Tree(PathBuf);

    impl Tree {
        const INSTALLED: &str = "installed stand-in\n";

        fn new(tag: &str, script: &str) -> Option<Self> {
            if Command::new("curl").arg("--version").output().is_err() {
                eprintln!("skipping: curl is not on PATH");
                return None;
            }
            let mut random = [0; 8];
            getrandom::fill(&mut random).unwrap();
            let name = format!(
                "bn-upgrade-{}-{:016x}",
                std::process::id(),
                u64::from_ne_bytes(random)
            );
            // Canonical like the executable path the command receives.
            let tree = Self(fs::canonicalize(std::env::temp_dir()).unwrap().join(name));
            let target = target().unwrap();
            let release = tree.0.join("releases/download").join(tag);
            let latest = tree.0.join("releases/latest/download");
            for dir in [&release, &latest, &tree.0.join("install")] {
                fs::create_dir_all(dir).unwrap();
            }
            let asset = release.join(format!("bn-{target}"));
            fs::write(&asset, script).unwrap();
            let manifest =
                manifest(&tree.base(), tag, &target, &sha256_hex(&asset).unwrap()).to_string();
            fs::write(release.join("bn-manifest.json"), &manifest).unwrap();
            fs::write(latest.join("bn-manifest.json"), &manifest).unwrap();
            fs::write(tree.exe(), Self::INSTALLED).unwrap();
            Some(tree)
        }
        fn base(&self) -> String {
            format!("file://{}/releases", self.0.display())
        }
        fn exe(&self) -> PathBuf {
            self.0.join("install/bn")
        }
        fn run(&self, current: &str, timeout: Duration) -> Result<Outcome, Error> {
            run(
                &Request {
                    current,
                    exe: &self.exe(),
                    base_url: &self.base(),
                    cargo_bins: &[],
                    check: false,
                    force: false,
                    tag: None,
                    smoke_timeout: timeout,
                },
                &|_| {},
            )
        }
        fn unchanged(&self) -> bool {
            fs::read_to_string(self.exe()).unwrap() == Self::INSTALLED && self.temps().is_empty()
        }
        fn temps(&self) -> Vec<String> {
            fs::read_dir(self.0.join("install"))
                .unwrap()
                .map(|entry| entry.unwrap().file_name().to_string_lossy().into_owned())
                .filter(|name| name.starts_with(TEMP_PREFIX))
                .collect()
        }
    }

    impl Drop for Tree {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.0);
        }
    }

    const RELEASED: &str = "#!/bin/sh\necho 'bn v9.9.9'\n";

    #[test]
    fn older_build_is_replaced_without_flags() {
        let Some(tree) = Tree::new("v9.9.9", RELEASED) else {
            return;
        };
        let outcome = tree.run("v0.0.1", SMOKE_TIMEOUT).unwrap();
        assert!(outcome.upgraded && outcome.update_available);
        assert_eq!(outcome.latest, "v9.9.9");
        assert_eq!(fs::read_to_string(tree.exe()).unwrap(), RELEASED);
        let mode = fs::metadata(tree.exe()).unwrap().permissions().mode();
        assert_eq!(mode & 0o777, 0o755);
        assert!(tree.temps().is_empty());
    }

    #[test]
    fn current_and_development_builds_stay_in_place() {
        let Some(tree) = Tree::new("v9.9.9", RELEASED) else {
            return;
        };
        for current in ["v9.9.9", "v9.9.9-3-gabc1234"] {
            let outcome = tree.run(current, SMOKE_TIMEOUT).unwrap();
            assert!(!outcome.upgraded && !outcome.update_available, "{current}");
            assert!(tree.unchanged(), "{current}");
        }
        let error = tree.run("dev", SMOKE_TIMEOUT).unwrap_err().to_string();
        assert!(error.contains("--force"), "{error}");
        assert!(tree.unchanged());
    }

    #[test]
    fn hung_download_is_killed_and_leaves_the_binary_alone() {
        let Some(tree) = Tree::new("v9.9.9", "#!/bin/sh\nexec sleep 60\n") else {
            return;
        };
        let started = Instant::now();
        let error = tree.run("v0.0.1", Duration::from_millis(300)).unwrap_err();
        assert!(started.elapsed() < Duration::from_secs(20));
        assert!(error.to_string().contains("did not run"), "{error}");
        assert!(tree.unchanged());
    }

    #[test]
    fn stale_downloads_are_removed_and_fresh_ones_kept() {
        let Some(tree) = Tree::new("v9.9.9", RELEASED) else {
            return;
        };
        let stale = tree.0.join("install/.bn-upgrade-1-stale");
        let fresh = tree.0.join("install/.bn-upgrade-1-fresh");
        for path in [&stale, &fresh] {
            fs::write(path, "partial").unwrap();
        }
        let old = std::time::SystemTime::now() - 2 * STALE_AFTER;
        let file = fs::File::options().write(true).open(&stale).unwrap();
        file.set_modified(old).unwrap();
        drop(file);
        tree.run("v9.9.9", SMOKE_TIMEOUT).unwrap();
        assert!(!stale.exists());
        assert!(fresh.exists());
    }
}
