//! Release downloads through a `curl` subprocess, so no TLS stack is linked.
use crate::domain::frontmatter::Error;
use std::{
    io::Read,
    path::Path,
    process::{Command, Stdio},
};

const MANIFEST_LIMIT: u64 = 64 * 1024;

fn curl(base: &str, max_time: &str) -> Command {
    let mut curl = Command::new("curl");
    // `-q` must come first: a user's ~/.curlrc could otherwise change the
    // output. `--globoff` keeps `{}` and `[]` in a URL literal. The time
    // bound covers every retry, not each attempt.
    curl.args(["-q", "-fsSL", "--globoff", "--connect-timeout", "10"])
        .args(["--retry", "3", "--retry-max-time", max_time])
        .args(["--max-time", max_time])
        .stdin(Stdio::null());
    if base.starts_with("https://") {
        curl.args(["--proto", "=https", "--proto-redir", "=https"]);
    }
    curl
}

fn curl_error(error: std::io::Error) -> Error {
    if error.kind() == std::io::ErrorKind::NotFound {
        Error::from("bn upgrade requires curl")
    } else {
        Error::from(format!("curl: {error}"))
    }
}

fn curl_failure(what: &str, url: &str, stderr: &[u8]) -> Error {
    let detail = String::from_utf8_lossy(stderr);
    Error::from(format!("could not fetch {what} {url}: {}", detail.trim()))
}

pub(super) fn fetch_manifest(base: &str, url: &str) -> Result<Vec<u8>, Error> {
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
        return Err(Error::from("release manifest too large"));
    }
    let output = child.wait_with_output().map_err(curl_error)?;
    read.map_err(curl_error)?;
    if !output.status.success() {
        return Err(curl_failure("release manifest", url, &output.stderr));
    }
    Ok(body)
}

pub(super) fn download(base: &str, url: &str, destination: &Path) -> Result<(), Error> {
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

pub(super) fn fetch_notice_manifest(
    base: &str,
    url: &str,
    directory: &Path,
) -> Result<Vec<u8>, Error> {
    use std::{
        io::{Seek, SeekFrom},
        os::unix::process::CommandExt,
        time::{Duration, Instant},
    };
    let (temporary, mut file) = crate::gitops::temporary(directory, ".bn-update-check-")?;
    drop(temporary);
    let mut child = curl(base, "1")
        .args([
            "--retry",
            "0",
            "--connect-timeout",
            "1",
            "--max-filesize",
            "65536",
            "--url",
            url,
        ])
        .process_group(0)
        .stdout(file.try_clone().map_err(curl_error)?)
        .stderr(Stdio::null())
        .spawn()
        .map_err(curl_error)?;
    let deadline = Instant::now() + Duration::from_secs(1);
    let status = loop {
        match child.try_wait() {
            Ok(Some(status)) => break status,
            Ok(None) if Instant::now() < deadline => std::thread::sleep(Duration::from_millis(10)),
            _ => {
                unsafe { libc::kill(-(child.id() as i32), libc::SIGKILL) };
                let _ = child.wait();
                return Err(Error::from("automatic update check timed out"));
            }
        }
    };
    if !status.success() {
        return Err(Error::from("automatic update check failed"));
    }
    file.seek(SeekFrom::Start(0)).map_err(curl_error)?;
    let mut raw = Vec::new();
    file.take(MANIFEST_LIMIT + 1)
        .read_to_end(&mut raw)
        .map_err(curl_error)?;
    if raw.len() as u64 > MANIFEST_LIMIT {
        return Err(Error::from("release manifest too large"));
    }
    Ok(raw)
}
