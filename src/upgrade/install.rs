//! Verified replacement of the running binary, and the installs it refuses.
use super::{fetch::download, manifest::Release};
use crate::{
    domain::{file_io::path_error, frontmatter::Error},
    gitops::{Temporary, temporary},
};
use sha2::{Digest, Sha256};
use std::{
    ffi::OsStr,
    fs, io,
    io::Read,
    os::unix::fs::PermissionsExt,
    path::{Component, Path, PathBuf},
    process::{Child, Command, ExitStatus, Stdio},
    time::{Duration, Instant},
};

pub(super) const TEMP_PREFIX: &str = ".bn-upgrade-";
pub(super) const STALE_AFTER: Duration = Duration::from_secs(60 * 60);
const SPAWN_ATTEMPTS: usize = 3;

/// True for binaries owned by Cargo or sitting in a build tree: under one of
/// `cargo_bins`, or below `target/{debug,release}` with an optional target
/// triple in between.
pub(super) fn is_managed(exe: &Path, cargo_bins: &[PathBuf]) -> bool {
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

pub(crate) fn cargo_bins() -> Vec<PathBuf> {
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

pub(super) fn sha256_hex(path: &Path) -> Result<String, Error> {
    let mut file = fs::File::open(path).map_err(|e| path_error("open", path, e))?;
    let mut hasher = Sha256::new();
    let mut buffer = [0; 64 * 1024];
    loop {
        let read = file
            .read(&mut buffer)
            .map_err(|e| path_error("read", path, e))?;
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

/// A file beside the binary, removed on every exit path; after the rename
/// into place the name no longer exists and removal is a no-op. It is closed
/// at once: no descriptor to it may be open in this process when it is
/// executed.
fn temp_in(dir: &Path) -> Result<Temporary, Error> {
    let (temp, _file) = temporary(dir, TEMP_PREFIX).map_err(|e| {
        Error::from(format!(
            "install directory not writable: {} ({e})",
            dir.display()
        ))
    })?;
    Ok(temp)
}

/// Best effort: an interrupted upgrade can leave its download behind.
pub(super) fn remove_stale_temps(dir: &Path) {
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

/// Spawns `<binary> --version` with its stdout in `capture`. A fork elsewhere
/// in this process can briefly hold a write descriptor to `binary`, which
/// fails the exec with ETXTBSY. That needs a second thread, so it happens
/// under the test harness and not in the single-threaded command.
fn spawn_version(binary: &Path, capture: &Path) -> io::Result<Child> {
    let mut attempt = 0;
    loop {
        attempt += 1;
        let stdout = fs::File::options().write(true).open(capture)?;
        let spawned = Command::new(binary)
            .arg("--version")
            .stdin(Stdio::null())
            .stdout(stdout)
            .stderr(Stdio::null())
            .spawn();
        match spawned {
            Err(e) if e.raw_os_error() == Some(libc::ETXTBSY) && attempt < SPAWN_ATTEMPTS => {
                std::thread::sleep(Duration::from_millis(100));
            }
            spawned => return spawned,
        }
    }
}

/// Waits for `child` until `deadline`. `None` means it overran the deadline;
/// in that case, and on a wait error, it has been killed and reaped.
fn wait_until(child: &mut Child, deadline: Instant) -> io::Result<Option<ExitStatus>> {
    loop {
        match child.try_wait() {
            Ok(None) if Instant::now() < deadline => {
                std::thread::sleep(Duration::from_millis(20));
            }
            Ok(Some(status)) => return Ok(Some(status)),
            overrun_or_error => {
                let _ = child.kill();
                let _ = child.wait();
                return overrun_or_error;
            }
        }
    }
}

/// Runs `<binary> --version` under `timeout` and returns its trimmed stdout.
/// Output goes to a file in `dir`, not a pipe: a descendant that keeps a pipe
/// open would hold a reader past the timeout.
fn reported_version(binary: &Path, dir: &Path, timeout: Duration) -> Result<String, Error> {
    let failed = |reason: String| Error::from(format!("downloaded bn did not run: {reason}"));
    let capture = temp_in(dir)?;
    let mut child = spawn_version(binary, &capture.0).map_err(|e| failed(e.to_string()))?;
    let waited = wait_until(&mut child, Instant::now() + timeout);
    match waited.map_err(|e| failed(e.to_string()))? {
        Some(status) if status.success() => {}
        Some(status) => return Err(failed(status.to_string())),
        None => {
            let seconds = timeout.as_secs_f32();
            return Err(failed(format!("no version after {seconds} seconds")));
        }
    }
    let reported = fs::read(&capture.0).map_err(|e| failed(e.to_string()))?;
    Ok(String::from_utf8_lossy(&reported).trim().to_owned())
}

/// Downloads and verifies `release` beside `exe`, then renames it over `exe`.
/// Nothing is written to `exe` unless the checksum and the smoke run pass.
pub(super) fn install(
    release: &Release,
    exe: &Path,
    base: &str,
    smoke_timeout: Duration,
) -> Result<(), Error> {
    let dir = exe
        .parent()
        .ok_or_else(|| Error::from("bn executable has no parent directory"))?;
    let temp = temp_in(dir)?;
    download(base, &release.url, &temp.0)?;
    if sha256_hex(&temp.0)? != release.sha256 {
        return Err(Error::from("downloaded bn checksum did not match"));
    }
    fs::set_permissions(&temp.0, fs::Permissions::from_mode(0o755))
        .map_err(|e| path_error("chmod", &temp.0, e))?;
    let reported = reported_version(&temp.0, dir, smoke_timeout)?;
    let expected = format!("bn {}", release.tag);
    if reported != expected {
        return Err(Error::from(format!(
            "downloaded bn reported `{reported}`, expected `{expected}`"
        )));
    }
    // The rename must not outlive the bytes it names across a power loss.
    fs::File::open(&temp.0)
        .and_then(|file| file.sync_all())
        .and_then(|()| fs::rename(&temp.0, exe))
        .map_err(|e| Error::from(format!("cannot replace {}: {e}", exe.display())))
}

#[cfg(test)]
mod tests {
    use super::*;

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
}
