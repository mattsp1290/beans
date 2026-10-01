use crate::{
    domain::{file_io::path_error, frontmatter::Error, yaml_string::YamlString},
    vault::paths::{clean, join},
};
use std::{
    ffi::OsString,
    io,
    os::unix::ffi::{OsStrExt, OsStringExt},
    path::{Path, PathBuf},
};
fn path(bytes: Vec<u8>) -> PathBuf {
    OsString::from_vec(bytes).into()
}
fn parts(target: &Path) -> Result<(PathBuf, Vec<u8>), Error> {
    let raw = target.as_os_str().as_bytes();
    let mut base = raw;
    while base.ends_with(b"/") {
        base = &base[..base.len() - 1];
    }
    let base = if base.is_empty() {
        if raw.is_empty() {
            b".".as_slice()
        } else {
            b"/".as_slice()
        }
    } else {
        base.rsplit(|&b| b == b'/').next().unwrap()
    };
    if matches!(base, b"." | b"/" | b"") {
        return Err(Error::new(format!(
            "invalid tree target {}",
            YamlString::from_bytes(raw.into()).quoted()
        )));
    }
    let dir = raw
        .iter()
        .rposition(|&b| b == b'/')
        .map_or(b"".as_slice(), |i| &raw[..i + 1]);
    Ok((path(clean(dir)), base.into()))
}
fn remove_one(path: &Path) -> Result<(), io::Error> {
    match std::fs::remove_file(path) {
        Ok(()) => Ok(()),
        Err(unlink) => match std::fs::remove_dir(path) {
            Ok(()) => Ok(()),
            Err(dir) => Err(if dir.raw_os_error() == Some(libc::ENOTDIR) {
                unlink
            } else {
                dir
            }),
        },
    }
}
fn remove_all(path: &Path) -> Result<(), Error> {
    let first = match remove_one(path) {
        Ok(()) => return Ok(()),
        Err(e) if e.kind() == io::ErrorKind::NotFound => return Ok(()),
        Err(e) => e,
    };
    let info = match std::fs::symlink_metadata(path) {
        Ok(info) => info,
        Err(e) if e.kind() == io::ErrorKind::NotFound => return Ok(()),
        Err(e) => return Err(path_error("lstat", path, e)),
    };
    if !info.is_dir() {
        return Err(path_error("remove", path, first));
    }
    let entries = std::fs::read_dir(path).map_err(|e| path_error("open", path, e))?;
    let mut first = None;
    for entry in entries {
        let result = match entry {
            Ok(entry) => remove_all(&entry.path()),
            Err(e) => Err(path_error("readdirent", path, e)),
        };
        if let Err(e) = result
            && first.is_none()
        {
            first = Some(e);
        }
    }
    if let Err(e) = remove_one(path)
        && first.is_none()
    {
        first = Some(path_error("remove", path, e));
    }
    match first {
        Some(error) => Err(error),
        None => Ok(()),
    }
}
pub fn recover_tree(target: &Path) -> Result<(), Error> {
    let (parent, base) = parts(target)?;
    let backup = path(join(&[
        parent.as_os_str().as_bytes(),
        &[b".".as_slice(), &base, b".backup"].concat(),
    ]));
    match std::fs::symlink_metadata(&backup) {
        Ok(_) => match std::fs::symlink_metadata(target) {
            Err(e) if e.kind() == io::ErrorKind::NotFound => std::fs::rename(&backup, target)
                .map_err(|e| {
                    Error::new(format!(
                        "recover tree backup: rename {} {}: {}",
                        YamlString::from_bytes(backup.as_os_str().as_bytes().into()),
                        YamlString::from_bytes(target.as_os_str().as_bytes().into()),
                        reason(e)
                    ))
                }),
            Ok(_) => remove_all(&backup)
                .map_err(|e| Error::new(format!("clear completed tree backup: {e}"))),
            Err(e) => Err(path_error("lstat", target, e)),
        },
        Err(e) if e.kind() == io::ErrorKind::NotFound => Ok(()),
        Err(e) => Err(path_error("lstat", &backup, e)),
    }
}
fn reason(error: io::Error) -> String {
    let s = error.to_string();
    s.split(" (os error ").next().unwrap_or(&s).to_lowercase()
}
pub fn recover_trees(parent: &Path) -> Result<(), Error> {
    let entries = std::fs::read_dir(parent).map_err(|e| path_error("open", parent, e))?;
    let mut entries = entries
        .collect::<Result<Vec<_>, _>>()
        .map_err(|e| path_error("readdirent", parent, e))?;
    entries.sort_by(|a, b| a.file_name().as_bytes().cmp(b.file_name().as_bytes()));
    for entry in entries {
        let name = entry.file_name();
        let bytes = name.as_bytes();
        if !entry
            .file_type()
            .map_err(|e| path_error("lstat", &entry.path(), e))?
            .is_dir()
            || !bytes.starts_with(b".")
            || !bytes.ends_with(b".backup")
        {
            continue;
        }
        let base = &bytes[1..];
        let base = base.strip_suffix(b".backup").unwrap_or(base);
        if matches!(base, b"" | b"." | b"/") {
            continue;
        }
        recover_tree(&path(join(&[parent.as_os_str().as_bytes(), base])))?;
    }
    Ok(())
}
pub fn recover_plan_temp(plan: &Path) -> Result<(), Error> {
    let temp = path([plan.as_os_str().as_bytes(), b".tmp"].concat());
    match remove_one(&temp) {
        Ok(()) => Ok(()),
        Err(e) if e.kind() == io::ErrorKind::NotFound => Ok(()),
        Err(e) => Err(path_error("remove", &temp, e)),
    }
}
