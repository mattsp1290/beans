//! Linux path diagnostics shared by configuration reads and bundle effects.
use super::{frontmatter::Error, yaml_string::YamlString};
use std::{io, os::unix::ffi::OsStrExt, path::Path};

pub(crate) fn path_name(path: &Path) -> YamlString {
    YamlString::from_bytes(path.as_os_str().as_bytes().into())
}
pub(crate) fn path_error(operation: &str, path: &Path, error: io::Error) -> Error {
    let message = if error.kind() == io::ErrorKind::InvalidInput
        && path.as_os_str().as_bytes().contains(&0)
    {
        "invalid argument".into()
    } else {
        let text = error.to_string();
        text.split(" (os error ")
            .next()
            .unwrap_or(&text)
            .to_lowercase()
    };
    Error(format!("{operation} {}: {message}", path_name(path)))
}
