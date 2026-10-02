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

/// Existing regular files and directories are left alone. The complete batch
/// rejects symlink destinations and unsafe ancestors before any writes.
/// On failure earlier writes remain, but no staging paths are returned.
pub fn create_project_files(
    hub: &Path,
    name: &[u8],
    remote: &[u8],
) -> Result<Option<Vec<Vec<u8>>>, Error> {
    // Validate the complete batch before creating any project files.
    for tail in [
        "beans.toml",
        "issues/.gitkeep",
        "archive/.gitkeep",
        "docs/.gitkeep",
        "memories/.gitkeep",
        "requests/.gitkeep",
        "handoffs/.gitkeep",
        "handoffs/archive/.gitkeep",
    ] {
        let relative = PathBuf::from(OsString::from_vec(join(&[
            b"projects",
            name,
            tail.as_bytes(),
        ])));
        crate::gitops::check_hub_write_path(hub, &relative)?;
    }
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
        crate::gitops::write_hub_file(
            hub,
            &PathBuf::from(OsString::from_vec(relative.clone())),
            &bytes,
        )?;
        paths.push(relative);
    }
    Ok((!paths.is_empty()).then_some(paths))
}
