//! Linux path diagnostics shared by configuration reads and bundle effects.
use super::{frontmatter::Error, yaml_string::YamlString};
use std::{io, os::unix::ffi::OsStrExt, path::Path};

pub(crate) fn path_name(path: &Path) -> YamlString {
    YamlString::from_bytes(path.as_os_str().as_bytes().into())
}
fn reason(error: io::Error, has_nul: bool) -> String {
    if error.kind() == io::ErrorKind::InvalidInput && has_nul {
        "invalid argument".into()
    } else {
        let text = error.to_string();
        text.split(" (os error ")
            .next()
            .unwrap_or(&text)
            .to_lowercase()
    }
}

pub(crate) fn path_error(operation: &str, path: &Path, error: io::Error) -> Error {
    let message = reason(error, path.as_os_str().as_bytes().contains(&0));
    let mut bytes = operation.as_bytes().to_vec();
    bytes.push(b' ');
    bytes.extend_from_slice(path.as_os_str().as_bytes());
    bytes.extend_from_slice(b": ");
    bytes.extend_from_slice(message.as_bytes());
    Error::from_bytes(bytes)
}

pub(crate) fn rename_error(from: &Path, to: &Path, error: io::Error) -> Error {
    let mut bytes = b"rename ".to_vec();
    bytes.extend_from_slice(from.as_os_str().as_bytes());
    bytes.push(b' ');
    bytes.extend_from_slice(to.as_os_str().as_bytes());
    bytes.extend_from_slice(b": ");
    bytes.extend_from_slice(
        reason(
            error,
            from.as_os_str().as_bytes().contains(&0) || to.as_os_str().as_bytes().contains(&0),
        )
        .as_bytes(),
    );
    Error::from_bytes(bytes)
}

/// Directory destinations are reported as EEXIST,
/// prioritizing a bad source path before that error. Linux rename alone reports
/// EISDIR for a regular file onto a directory.
pub(crate) fn rename(from: &Path, to: &Path) -> Result<(), Error> {
    use std::os::unix::fs::MetadataExt;
    if let Ok(destination) = std::fs::symlink_metadata(to)
        && destination.is_dir()
    {
        let source = std::fs::symlink_metadata(from).map_err(|e| rename_error(from, to, e))?;
        if from == to || source.dev() != destination.dev() || source.ino() != destination.ino() {
            return Err(rename_error(
                from,
                to,
                io::Error::from_raw_os_error(libc::EEXIST),
            ));
        }
    }
    loop {
        match std::fs::rename(from, to) {
            Err(e) if e.kind() == io::ErrorKind::Interrupted => continue,
            result => return result.map_err(|e| rename_error(from, to, e)),
        }
    }
}
