//! Same-directory temporary writes used by operation-owned project creation.
use crate::domain::{file_io::path_error, frontmatter::Error};
use std::{
    fs::{self, DirBuilder, File, OpenOptions},
    io::{self, Write},
    os::{
        fd::IntoRawFd,
        unix::fs::{DirBuilderExt, OpenOptionsExt},
    },
    path::{Path, PathBuf},
};

fn mkdir_all(path: &Path) -> Result<(), Error> {
    match fs::metadata(path) {
        Ok(meta) if meta.is_dir() => return Ok(()),
        Ok(_) => {
            return Err(path_error(
                "mkdir",
                path,
                io::Error::from_raw_os_error(libc::ENOTDIR),
            ));
        }
        Err(_) => {}
    }
    if let Some(parent) = path
        .parent()
        .filter(|p| !p.as_os_str().is_empty() && *p != path)
    {
        mkdir_all(parent)?;
    }
    match DirBuilder::new().mode(0o755).create(path) {
        Ok(()) => Ok(()),
        // Go also accepts a directory created by another writer in this gap.
        Err(_) if fs::symlink_metadata(path).is_ok_and(|m| m.is_dir()) => Ok(()),
        Err(e) => Err(path_error("mkdir", path, e)),
    }
}
struct Temporary(PathBuf);
impl Drop for Temporary {
    fn drop(&mut self) {
        let _ = fs::remove_file(&self.0);
    }
}
fn temporary(parent: &Path) -> Result<(Temporary, File), Error> {
    for _ in 0..10000 {
        let mut bytes = [0; 4];
        getrandom::fill(&mut bytes)
            .map_err(|e| Error::new(format!("random temporary filename: {e}")))?;
        let path = parent.join(format!(".bn-write-{}", u32::from_ne_bytes(bytes)));
        match OpenOptions::new()
            .read(true)
            .write(true)
            .create_new(true)
            .mode(0o600)
            .open(&path)
        {
            Ok(file) => return Ok((Temporary(path), file)),
            Err(e) if e.kind() == io::ErrorKind::AlreadyExists => continue,
            Err(e) => return Err(path_error("open", &path, e)),
        }
    }
    Err(Error::new(format!(
        "createtemp {}: file exists",
        parent.display()
    )))
}
/// Preserve Go WriteFile's mkdir/temp/write/close/rename order and 0600 mode.
/// No fsync or stronger crash durability is implied.
pub fn write_file(path: &Path, bytes: &[u8]) -> Result<(), Error> {
    let parent = path
        .parent()
        .filter(|p| !p.as_os_str().is_empty())
        .unwrap_or(Path::new("."));
    mkdir_all(parent)?;
    let (temp, mut file) = temporary(parent)?;
    let result = loop {
        match file.write(bytes) {
            Err(e) if e.kind() == io::ErrorKind::Interrupted => continue,
            Err(e) => break Err(path_error("write", &temp.0, e)),
            Ok(written) if written != bytes.len() => break Err(Error::new("short write".into())),
            Ok(_) => break Ok(()),
        }
    };
    let descriptor = file.into_raw_fd();
    // SAFETY: the consumed File owns this descriptor; close it exactly once.
    let closed = unsafe { libc::close(descriptor) };
    let close_error = (closed < 0).then(io::Error::last_os_error);
    result?;
    if let Some(e) = close_error {
        return Err(path_error("close", &temp.0, e));
    }
    crate::domain::file_io::rename(&temp.0, path)
}
