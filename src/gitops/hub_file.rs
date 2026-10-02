//! Hub-relative writes anchored to directory descriptors. Symlinks are never
//! followed, and generated temporary files carry separate recovery evidence.
use crate::domain::frontmatter::Error;
use std::{
    ffi::{CString, OsStr},
    fs::File,
    io::{Read, Write},
    os::{
        fd::{AsRawFd, FromRawFd},
        unix::ffi::OsStrExt,
    },
    path::{Component, Path, PathBuf},
};
fn error(e: std::io::Error) -> Error {
    Error::new(e.to_string())
}
fn cstr(value: &OsStr) -> Result<CString, Error> {
    CString::new(value.as_bytes()).map_err(|_| Error::new("NUL in hub write path".into()))
}
fn components(relative: &Path) -> Result<Vec<&OsStr>, Error> {
    if relative.as_os_str().is_empty() || relative.is_absolute() {
        return Err(Error::new("invalid hub-relative write path".into()));
    }
    relative
        .components()
        .map(|c| match c {
            Component::Normal(value) => Ok(value),
            _ => Err(Error::new("invalid hub-relative write path".into())),
        })
        .collect()
}
fn root(hub: &Path) -> Result<File, Error> {
    let path = cstr(hub.as_os_str())?;
    let fd = unsafe {
        libc::open(
            path.as_ptr(),
            libc::O_RDONLY | libc::O_DIRECTORY | libc::O_NOFOLLOW | libc::O_CLOEXEC,
        )
    };
    if fd < 0 {
        return Err(error(std::io::Error::last_os_error()));
    }
    Ok(unsafe { File::from_raw_fd(fd) })
}
fn child(parent: &File, name: &OsStr, create: bool) -> Result<Option<File>, Error> {
    let name = cstr(name)?;
    let mut fd = unsafe {
        libc::openat(
            parent.as_raw_fd(),
            name.as_ptr(),
            libc::O_RDONLY | libc::O_DIRECTORY | libc::O_NOFOLLOW | libc::O_CLOEXEC,
        )
    };
    if fd < 0 && std::io::Error::last_os_error().kind() == std::io::ErrorKind::NotFound {
        if !create {
            return Ok(None);
        }
        let made = unsafe { libc::mkdirat(parent.as_raw_fd(), name.as_ptr(), 0o755) };
        if made < 0 && std::io::Error::last_os_error().kind() != std::io::ErrorKind::AlreadyExists {
            return Err(error(std::io::Error::last_os_error()));
        }
        fd = unsafe {
            libc::openat(
                parent.as_raw_fd(),
                name.as_ptr(),
                libc::O_RDONLY | libc::O_DIRECTORY | libc::O_NOFOLLOW | libc::O_CLOEXEC,
            )
        };
    }
    if fd < 0 {
        return Err(Error::new(format!(
            "unsafe hub write ancestor (symlink or non-directory): {}",
            std::io::Error::last_os_error()
        )));
    }
    Ok(Some(unsafe { File::from_raw_fd(fd) }))
}
fn reject_symlink(parent: &File, name: &OsStr) -> Result<(), Error> {
    let name = cstr(name)?;
    let mut stat = std::mem::MaybeUninit::<libc::stat>::uninit();
    let result = unsafe {
        libc::fstatat(
            parent.as_raw_fd(),
            name.as_ptr(),
            stat.as_mut_ptr(),
            libc::AT_SYMLINK_NOFOLLOW,
        )
    };
    if result < 0 {
        let e = std::io::Error::last_os_error();
        if e.kind() == std::io::ErrorKind::NotFound {
            return Ok(());
        }
        return Err(error(e));
    }
    if unsafe { stat.assume_init() }.st_mode & libc::S_IFMT == libc::S_IFLNK {
        return Err(Error::new("symlink in hub write destination".into()));
    }
    Ok(())
}
/// Check every existing ancestor before a batch starts; each actual write also
/// uses no-follow descriptors to close the validation/write race.
pub fn check_hub_write_path(hub: &Path, relative: &Path) -> Result<(), Error> {
    let parts = components(relative)?;
    let mut parent = root(hub)?;
    for part in &parts[..parts.len() - 1] {
        match child(&parent, part, false)? {
            Some(next) => parent = next,
            None => return Ok(()),
        }
    }
    reject_symlink(&parent, parts.last().unwrap())
}
fn unlink(parent: &File, name: &CString) -> Result<(), Error> {
    if unsafe { libc::unlinkat(parent.as_raw_fd(), name.as_ptr(), 0) } == 0 {
        return Ok(());
    }
    let e = std::io::Error::last_os_error();
    if e.kind() == std::io::ErrorKind::NotFound {
        Ok(())
    } else {
        Err(error(e))
    }
}
/// Prepared atomic write. Drop clears its exact temporary and ownership record;
/// an interrupted process leaves both so the locked recovery path can verify it.
pub struct PreparedHubWrite {
    parent: File,
    owners: Option<File>,
    temp: CString,
    target: CString,
    owner: CString,
}
impl PreparedHubWrite {
    pub fn prepare(hub: &Path, relative: &Path, bytes: &[u8]) -> Result<Self, Error> {
        check_hub_write_path(hub, relative)?;
        let parts = components(relative)?;
        let mut parent = root(hub)?;
        for part in &parts[..parts.len() - 1] {
            parent = child(&parent, part, true)?.unwrap();
        }
        reject_symlink(&parent, parts.last().unwrap())?;
        let hub_root = root(hub)?;
        let owners = match child(&hub_root, OsStr::new(".git"), false)? {
            Some(git) => child(&git, OsStr::new("bn-temp-owners"), true)?,
            None => None,
        };
        let mut random = [0u8; 16];
        getrandom::fill(&mut random).map_err(|e| Error::new(e.to_string()))?;
        let nonce: String = random.iter().map(|b| format!("{b:02x}")).collect();
        let temp_name = format!(".bn-write-{nonce}");
        let temp = cstr(OsStr::new(&temp_name))?;
        let owner = cstr(OsStr::new(&format!("{nonce}.json")))?;
        let temporary = relative.parent().unwrap_or(Path::new("")).join(&temp_name);
        let record = serde_json::to_vec(&temporary.as_os_str().as_bytes())
            .map_err(|e| Error::new(e.to_string()))?;
        if let Some(owners) = &owners {
            let fd = unsafe {
                libc::openat(
                    owners.as_raw_fd(),
                    owner.as_ptr(),
                    libc::O_WRONLY
                        | libc::O_CREAT
                        | libc::O_EXCL
                        | libc::O_NOFOLLOW
                        | libc::O_CLOEXEC,
                    0o600,
                )
            };
            if fd < 0 {
                return Err(error(std::io::Error::last_os_error()));
            }
            let mut evidence = unsafe { File::from_raw_fd(fd) };
            evidence.write_all(&record).map_err(error)?;
            evidence.sync_all().map_err(error)?;
        }
        let prepared = Self {
            parent,
            owners,
            temp,
            target: cstr(parts.last().unwrap())?,
            owner,
        };
        let fd = unsafe {
            libc::openat(
                prepared.parent.as_raw_fd(),
                prepared.temp.as_ptr(),
                libc::O_WRONLY | libc::O_CREAT | libc::O_EXCL | libc::O_NOFOLLOW | libc::O_CLOEXEC,
                0o600,
            )
        };
        if fd < 0 {
            return Err(error(std::io::Error::last_os_error()));
        }
        let mut file = unsafe { File::from_raw_fd(fd) };
        file.write_all(bytes).map_err(error)?;
        file.sync_all().map_err(error)?;
        Ok(prepared)
    }
    pub fn commit(self) -> Result<(), Error> {
        reject_symlink(&self.parent, OsStr::from_bytes(self.target.as_bytes()))?;
        if unsafe {
            libc::renameat(
                self.parent.as_raw_fd(),
                self.temp.as_ptr(),
                self.parent.as_raw_fd(),
                self.target.as_ptr(),
            )
        } < 0
        {
            return Err(error(std::io::Error::last_os_error()));
        }
        Ok(())
    }
}
impl Drop for PreparedHubWrite {
    fn drop(&mut self) {
        let _ = unlink(&self.parent, &self.temp);
        if let Some(owners) = &self.owners {
            let _ = unlink(owners, &self.owner);
        }
    }
}
pub fn write_hub_file(hub: &Path, relative: &Path, bytes: &[u8]) -> Result<(), Error> {
    PreparedHubWrite::prepare(hub, relative, bytes)?.commit()
}
/// Exact registered orphan candidates; arbitrary basename matches are ignored.
pub(crate) fn owned_orphans(hub: &Path) -> Result<Vec<(PathBuf, PathBuf)>, Error> {
    let hub_root = root(hub)?;
    let Some(git) = child(&hub_root, OsStr::new(".git"), false)? else {
        return Ok(Vec::new());
    };
    let Some(owners) = child(&git, OsStr::new("bn-temp-owners"), false)? else {
        return Ok(Vec::new());
    };
    let dir = PathBuf::from(format!("/proc/self/fd/{}", owners.as_raw_fd()));
    let entries = match std::fs::read_dir(&dir) {
        Ok(e) => e,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(Vec::new()),
        Err(e) => return Err(error(e)),
    };
    let mut out = Vec::new();
    for entry in entries {
        let entry = entry.map_err(error)?;
        if !entry.file_type().map_err(error)?.is_file() {
            continue;
        }
        let name = entry.file_name();
        let name = name.to_string_lossy();
        let Some(nonce) = name
            .strip_suffix(".json")
            .filter(|n| n.len() == 32 && n.bytes().all(|b| b.is_ascii_hexdigit()))
        else {
            continue;
        };
        let record_name = cstr(&entry.file_name())?;
        let fd = unsafe {
            libc::openat(
                owners.as_raw_fd(),
                record_name.as_ptr(),
                libc::O_RDONLY | libc::O_NOFOLLOW | libc::O_CLOEXEC,
            )
        };
        if fd < 0 {
            return Err(error(std::io::Error::last_os_error()));
        }
        let mut file = unsafe { File::from_raw_fd(fd) };
        let mut record = Vec::new();
        file.read_to_end(&mut record).map_err(error)?;
        let bytes: Vec<u8> = serde_json::from_slice(&record)
            .map_err(|e| Error::new(format!("invalid temporary ownership evidence: {e}")))?;
        use std::os::unix::ffi::OsStringExt;
        let relative = PathBuf::from(std::ffi::OsString::from_vec(bytes));
        components(&relative)?;
        if relative
            .file_name()
            .is_none_or(|n| n != OsStr::new(&format!(".bn-write-{nonce}")))
        {
            return Err(Error::new("temporary ownership evidence mismatch".into()));
        }
        check_hub_write_path(hub, &relative)?;
        out.push((relative, PathBuf::from(entry.file_name())));
    }
    Ok(out)
}
pub(crate) fn remove_registered_orphan(
    hub: &Path,
    relative: &Path,
    evidence: &Path,
) -> Result<(), Error> {
    let parts = components(relative)?;
    let mut parent = root(hub)?;
    for part in &parts[..parts.len() - 1] {
        let Some(next) = child(&parent, part, false)? else {
            remove_evidence(hub, evidence)?;
            return Ok(());
        };
        parent = next;
    }
    reject_symlink(&parent, parts.last().unwrap())?;
    unlink(&parent, &cstr(parts.last().unwrap())?)?;
    remove_evidence(hub, evidence)
}

fn remove_evidence(hub: &Path, evidence: &Path) -> Result<(), Error> {
    let parts = components(evidence)?;
    if parts.len() != 1 {
        return Err(Error::new("invalid temporary evidence path".into()));
    }
    let hub_root = root(hub)?;
    let git = child(&hub_root, OsStr::new(".git"), false)?
        .ok_or_else(|| Error::new("temporary recovery metadata disappeared".into()))?;
    let owners = child(&git, OsStr::new("bn-temp-owners"), false)?
        .ok_or_else(|| Error::new("temporary recovery metadata disappeared".into()))?;
    unlink(&owners, &cstr(parts[0])?)
}

/// Remove one explicitly selected record through the same no-follow boundary.
/// This never traverses or recursively deletes a directory.
pub fn remove_hub_file(hub: &Path, relative: &Path) -> Result<(), Error> {
    let parts = components(relative)?;
    let mut parent = root(hub)?;
    for part in &parts[..parts.len() - 1] {
        match child(&parent, part, false)? {
            Some(next) => parent = next,
            None => return Ok(()),
        }
    }
    let name = parts.last().unwrap();
    reject_symlink(&parent, name)?;
    unlink(&parent, &cstr(name)?)
}
