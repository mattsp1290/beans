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
    SnapshotReader::new(root)?.read(path)
}
/// Directory descriptors live only for one index walk. Reusing an already opened
/// nofollow directory pins that directory, even if its path is replaced mid-walk.
/// The next snapshot opens a fresh set; no stale descriptors survive reloads.
pub(crate) struct SnapshotReader {
    directories: std::collections::BTreeMap<std::path::PathBuf, File>,
}
impl SnapshotReader {
    pub(crate) fn new(root: &Path) -> Result<Self, Error> {
        let root = std::ffi::CString::new(root.as_os_str().as_bytes())
            .map_err(|e| Error::new(e.to_string()))?;
        // SAFETY: root is NUL terminated; successful open gives a new owned fd.
        let fd = unsafe {
            libc::open(
                root.as_ptr(),
                libc::O_RDONLY | libc::O_DIRECTORY | libc::O_NOFOLLOW | libc::O_CLOEXEC,
            )
        };
        if fd < 0 {
            return Err(Error::new(std::io::Error::last_os_error().to_string()));
        }
        // SAFETY: fd is newly opened and owned exactly once by this File.
        let root = unsafe { File::from_raw_fd(fd) };
        Ok(Self {
            directories: [(std::path::PathBuf::new(), root)].into(),
        })
    }
    pub(crate) fn read(&mut self, path: &Path) -> Result<Vec<u8>, Error> {
        let parts: Vec<_> = path.components().collect();
        if parts.is_empty()
            || parts
                .iter()
                .any(|c| !matches!(c, Component::Normal(v) if !v.as_bytes().starts_with(b".")))
        {
            return Err(Error::new("invalid relative snapshot path".into()));
        }
        let mut parent = std::path::PathBuf::new();
        for (i, component) in parts.iter().enumerate() {
            let child = parent.join(component.as_os_str());
            let directory = i + 1 < parts.len();
            if directory && self.directories.contains_key(&child) {
                parent = child;
                continue;
            }
            let name = std::ffi::CString::new(component.as_os_str().as_bytes())
                .map_err(|e| Error::new(e.to_string()))?;
            let flags = libc::O_RDONLY
                | libc::O_NOFOLLOW
                | libc::O_CLOEXEC
                | libc::O_NONBLOCK
                | if directory { libc::O_DIRECTORY } else { 0 };
            let parent_fd = self.directories[&parent].as_raw_fd();
            // SAFETY: parent_fd is held open by self; name is NUL terminated.
            let fd = unsafe { libc::openat(parent_fd, name.as_ptr(), flags) };
            if fd < 0 {
                return Err(Error::new("file unavailable".into()));
            }
            // SAFETY: fd is a newly opened descriptor, now owned by file.
            let mut file = unsafe { File::from_raw_fd(fd) };
            if directory {
                self.directories.insert(child.clone(), file);
                // Keep descriptor use independent of hub size and nesting. The
                // openat above has already consumed its parent descriptor; after
                // eviction, the new child and root remain pinned and available.
                if self.directories.len() > 16 {
                    self.directories
                        .retain(|path, _| path.as_os_str().is_empty() || path == &child);
                }
                parent = child;
                continue;
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
            return Ok(bytes);
        }
        unreachable!("nonempty path always has a leaf")
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn cached_directories_pin_contained_bytes_and_fresh_snapshot_rejects_replacement_symlink() {
        let mut nonce = [0; 8];
        getrandom::fill(&mut nonce).unwrap();
        let root =
            std::env::temp_dir().join(format!("bn-snapshot-{:x}", u64::from_ne_bytes(nonce)));
        std::fs::create_dir_all(root.join("hub/docs")).unwrap();
        std::fs::create_dir(root.join("outside")).unwrap();
        std::fs::write(root.join("hub/docs/a.md"), b"inside").unwrap();
        std::fs::write(root.join("outside/a.md"), b"outside secret").unwrap();
        let mut reader = SnapshotReader::new(&root.join("hub")).unwrap();
        assert_eq!(reader.read(Path::new("docs/a.md")).unwrap(), b"inside");
        assert_eq!(reader.directories.len(), 2);
        std::fs::rename(root.join("hub/docs"), root.join("hub/old-docs")).unwrap();
        std::os::unix::fs::symlink(root.join("outside"), root.join("hub/docs")).unwrap();
        assert_eq!(reader.read(Path::new("docs/a.md")).unwrap(), b"inside");
        assert!(read_snapshot_file(&root.join("hub"), Path::new("docs/a.md")).is_err());
        std::os::unix::fs::symlink(root.join("outside/a.md"), root.join("hub/old-docs/link.md"))
            .unwrap();
        assert!(reader.read(Path::new("docs/link.md")).is_err());
        assert!(reader.read(Path::new("docs/../outside/a.md")).is_err());
        std::fs::remove_dir_all(root).unwrap();
    }
}
