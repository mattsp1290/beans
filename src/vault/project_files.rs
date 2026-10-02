//! Operation-owned project scaffolding; resolution itself never writes files.
use super::paths::join;
use crate::domain::{
    config::{ProjectConfig, encode_project_config},
    frontmatter::Error,
    yaml_string::YamlString,
};
use std::{
    ffi::OsString,
    os::unix::ffi::{OsStrExt, OsStringExt},
    path::{Path, PathBuf},
};

/// Existing files (including directories and valid symlinks) are left alone.
/// Stat errors other than ENOENT are skipped as in the Go implementation.
/// On failure earlier writes remain, but no staging paths are returned.
pub fn create_project_files(
    hub: &Path,
    name: &[u8],
    remote: &[u8],
) -> Result<Option<Vec<Vec<u8>>>, Error> {
    let mut paths = Vec::new();
    for tail in [
        b"beans.toml".as_slice(),
        b"issues/.gitkeep",
        b"archive/.gitkeep",
        b"docs/.gitkeep",
        b"memories/.gitkeep",
        b"requests/.gitkeep",
        b"handoffs/.gitkeep",
        b"handoffs/archive/.gitkeep",
    ] {
        let relative = join(&[b"projects", name, tail]);
        let full = PathBuf::from(OsString::from_vec(join(&[
            hub.as_os_str().as_bytes(),
            &relative,
        ])));
        if !std::fs::metadata(&full).is_err_and(|e| e.kind() == std::io::ErrorKind::NotFound) {
            continue;
        }
        let bytes = if tail == b"beans.toml" {
            encode_project_config(&ProjectConfig {
                name: YamlString::from_bytes(name.into()),
                prefix: YamlString::from_bytes(name.into()),
                remotes: (!remote.is_empty()).then(|| vec![YamlString::from_bytes(remote.into())]),
                ..ProjectConfig::default()
            })
        } else {
            Vec::new()
        };
        crate::gitops::write_file(&full, &bytes)?;
        paths.push(relative);
    }
    Ok((!paths.is_empty()).then_some(paths))
}
