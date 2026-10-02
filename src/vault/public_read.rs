//! Descriptor-relative snapshot reads reject symlinks at every path component.
use crate::domain::frontmatter::Error;
use std::{
    fs::File,
    io::Read,
    os::fd::{AsRawFd, FromRawFd},
    os::unix::ffi::OsStrExt,
    path::{Component, Path},
};
pub fn valid_public_path(path: &str) -> bool {
    !path.is_empty()
        && !path.contains(['\\', '\0', '%'])
        && Path::new(path)
            .components()
            .all(|c| matches!(c, Component::Normal(v) if !v.as_bytes().starts_with(b".")))
        && !path
            .split('/')
            .any(|c| c.is_empty() || c == ".." || c == ".")
}
/// Open each component relative to a nofollow directory descriptor, eliminating symlink races.
pub fn read_public_file(root: &Path, path: &str) -> Result<Vec<u8>, Error> {
    if !valid_public_path(path) {
        return Err(Error::new("invalid path".into()));
    }
    read_snapshot_file(root, Path::new(path))
}
pub(crate) fn read_snapshot_file(root: &Path, path: &Path) -> Result<Vec<u8>, Error> {
    let parts: Vec<_> = path.components().collect();
    if parts.is_empty()
        || parts
            .iter()
            .any(|c| !matches!(c, Component::Normal(v) if !v.as_bytes().starts_with(b".")))
    {
        return Err(Error::new("invalid relative snapshot path".into()));
    }
    let root = std::ffi::CString::new(root.as_os_str().as_bytes())
        .map_err(|e| Error::new(e.to_string()))?;
    let fd = unsafe {
        libc::open(
            root.as_ptr(),
            libc::O_RDONLY | libc::O_DIRECTORY | libc::O_NOFOLLOW | libc::O_CLOEXEC,
        )
    };
    if fd < 0 {
        return Err(Error::new(std::io::Error::last_os_error().to_string()));
    }
    let mut file = unsafe { File::from_raw_fd(fd) };
    for (i, part) in parts.iter().enumerate() {
        let part = std::ffi::CString::new(part.as_os_str().as_bytes())
            .map_err(|e| Error::new(e.to_string()))?;
        let flags = libc::O_RDONLY
            | libc::O_NOFOLLOW
            | libc::O_CLOEXEC
            | libc::O_NONBLOCK
            | if i + 1 < parts.len() {
                libc::O_DIRECTORY
            } else {
                0
            };
        let fd = unsafe { libc::openat(file.as_raw_fd(), part.as_ptr(), flags) };
        if fd < 0 {
            return Err(Error::new("file unavailable".into()));
        }
        file = unsafe { File::from_raw_fd(fd) };
    }
    if !file
        .metadata()
        .map_err(|e| Error::new(e.to_string()))?
        .is_file()
    {
        return Err(Error::new("not a regular file".into()));
    }
    let mut bytes = Vec::new();
    file.read_to_end(&mut bytes)
        .map_err(|e| Error::new(e.to_string()))?;
    Ok(bytes)
}
