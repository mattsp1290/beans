//! `bn upgrade`: replace the running binary with a published release.
//!
//! The command never touches the hub. It fetches the release manifest and the
//! binary through a `curl` subprocess, verifies the binary beside the current
//! executable and only then renames it into place.
use crate::domain::frontmatter::Error;
use std::{
    path::{Path, PathBuf},
    time::Duration,
};

mod fetch;
mod install;
mod manifest;
mod version;

pub(crate) use install::cargo_bins;
use version::Decision;

pub(crate) const BASE_URL_ENV: &str = "BN_RELEASE_BASE_URL";
pub(crate) const DEFAULT_BASE_URL: &str = "https://github.com/mattsp1290/beans/releases";
pub(crate) const SMOKE_TIMEOUT: Duration = Duration::from_secs(10);

fn status_line(decision: Decision, current: &str, latest: &str) -> String {
    match decision {
        Decision::Upgrade => format!("bn {latest} is available (current {current})"),
        Decision::UpToDate => format!("bn {current} is up to date"),
        Decision::Unknown => {
            format!("cannot compare bn {current} with {latest}; run `bn upgrade --force`")
        }
    }
}

pub(crate) struct Request<'a> {
    /// The running build's `BN_VERSION`.
    pub(crate) current: &'a str,
    /// Canonical path of the binary to replace.
    pub(crate) exe: &'a Path,
    pub(crate) base_url: &'a str,
    pub(crate) cargo_bins: &'a [PathBuf],
    pub(crate) check: bool,
    pub(crate) force: bool,
    pub(crate) tag: Option<&'a str>,
    pub(crate) smoke_timeout: Duration,
}

#[derive(Debug)]
pub(crate) struct Outcome {
    pub(crate) latest: String,
    pub(crate) target: String,
    pub(crate) update_available: bool,
    pub(crate) upgraded: bool,
}

/// The whole command. `progress` receives each plain-output line as it happens.
pub(crate) fn run(request: &Request, progress: &dyn Fn(&str)) -> Result<Outcome, Error> {
    let Request { current, exe, .. } = *request;
    if let Some(dir) = exe.parent() {
        install::remove_stale_temps(dir);
    }
    if !request.check && !request.force && install::is_managed(exe, request.cargo_bins) {
        return Err(Error::from(format!(
            "{} was installed by Cargo or is a build tree binary; use `make install`, install with distribution/install.sh, or re-run with --force",
            exe.display()
        )));
    }
    let target = manifest::target()?;
    let base = manifest::release_base(request.base_url)?;
    let manifest_url = match request.tag {
        Some(tag) if version::parse_tag(tag).is_none() => {
            return Err(Error::from(format!(
                "invalid release tag {tag}: expected vX.Y.Z"
            )));
        }
        Some(tag) => format!("{base}/download/{tag}/bn-manifest.json"),
        None => format!("{base}/latest/download/bn-manifest.json"),
    };
    let manifest = fetch::fetch_manifest(base, &manifest_url)?;
    let release = manifest::validate(&manifest, base, &target, request.tag)?;
    let decision = version::decide(current, release.version);
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
            Decision::Unknown => return Err(Error::from(status)),
        }
    }
    progress(&format!("upgrading bn {current} -> {}", release.tag));
    install::install(&release, exe, base, request.smoke_timeout)?;
    progress(&format!(
        "installed bn {} at {}",
        release.tag,
        exe.display()
    ));
    progress("restart any running 'bn serve' to use the new version");
    Ok(outcome(true))
}

#[cfg(test)]
mod tests {
    use super::{
        install::{STALE_AFTER, TEMP_PREFIX, sha256_hex},
        manifest::{sample as manifest, target},
        *,
    };
    use std::{
        fs,
        os::unix::fs::PermissionsExt,
        process::Command,
        time::{Duration, Instant},
    };

    /// A `file://` release tree with one published release and an installed
    /// stand-in binary. The released "binary" is a shell script.
    struct Tree(PathBuf);

    impl Tree {
        const INSTALLED: &str = "installed stand-in\n";

        fn new(tag: &str, script: &str) -> Option<Self> {
            if Command::new("curl").arg("--version").output().is_err() {
                assert!(std::env::var_os("CI").is_none(), "CI must provide curl");
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
            self.request(current, false, timeout)
        }
        fn check(&self, current: &str) -> Outcome {
            self.request(current, true, SMOKE_TIMEOUT).unwrap()
        }
        fn request(&self, current: &str, check: bool, timeout: Duration) -> Result<Outcome, Error> {
            run(
                &Request {
                    current,
                    exe: &self.exe(),
                    base_url: &self.base(),
                    cargo_bins: &[],
                    check,
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
        // Reporting never fails on an incomparable build, and never installs.
        for (current, available) in [("dev", true), ("v0.0.1", true), ("v9.9.9", false)] {
            let outcome = tree.check(current);
            assert_eq!(outcome.update_available, available, "{current}");
            assert!(!outcome.upgraded && tree.unchanged(), "{current}");
        }
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
    fn lingering_descendant_cannot_outlast_the_version_check() {
        // The script exits at once but leaves a child holding its stdout.
        let script = "#!/bin/sh\necho 'bn v9.9.9'\nsleep 60 &\n";
        let Some(tree) = Tree::new("v9.9.9", script) else {
            return;
        };
        let started = Instant::now();
        assert!(tree.run("v0.0.1", SMOKE_TIMEOUT).unwrap().upgraded);
        assert!(started.elapsed() < SMOKE_TIMEOUT);
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
