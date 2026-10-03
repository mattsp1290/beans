//! Clone-scoped state anchored outside the worktree with no-follow descriptors.
use crate::domain::frontmatter::Error;
use std::{
    ffi::{CString, OsStr},
    fs::{self, File},
    io::{Read, Write},
    os::{
        fd::{AsRawFd, FromRawFd},
        unix::ffi::OsStrExt,
    },
    path::{Path, PathBuf},
};

fn error(e: std::io::Error) -> Error {
    Error::new(format!("hub state (requires writable clone parent): {e}"))
}
fn name(s: &OsStr) -> Result<CString, Error> {
    CString::new(s.as_bytes()).map_err(|_| Error::new("NUL in state path".into()))
}
fn directory(parent: &File, part: &OsStr) -> Result<File, Error> {
    let c = name(part)?;
    if unsafe { libc::mkdirat(parent.as_raw_fd(), c.as_ptr(), 0o700) } < 0
        && std::io::Error::last_os_error().kind() != std::io::ErrorKind::AlreadyExists
    {
        return Err(error(std::io::Error::last_os_error()));
    }
    let fd = unsafe {
        libc::openat(
            parent.as_raw_fd(),
            c.as_ptr(),
            libc::O_RDONLY | libc::O_DIRECTORY | libc::O_NOFOLLOW | libc::O_CLOEXEC,
        )
    };
    if fd < 0 {
        return Err(error(std::io::Error::last_os_error()));
    }
    Ok(unsafe { File::from_raw_fd(fd) })
}
pub struct HubState {
    pub target: PathBuf,
    pub path: PathBuf,
    directory: File,
}
impl HubState {
    pub fn new(target: &Path) -> Result<Self, Error> {
        let target = if target.exists() {
            fs::canonicalize(target).map_err(error)?
        } else {
            let base = target
                .file_name()
                .filter(|b| *b != OsStr::new(".beans-state"))
                .ok_or_else(|| Error::new("invalid or reserved hub basename".into()))?;
            let parent = target
                .parent()
                .filter(|p| !p.as_os_str().is_empty())
                .unwrap_or(Path::new("."));
            fs::create_dir_all(parent).map_err(error)?;
            fs::canonicalize(parent).map_err(error)?.join(base)
        };
        let base = target
            .file_name()
            .filter(|b| *b != OsStr::new(".beans-state"))
            .ok_or_else(|| Error::new("invalid or reserved hub basename".into()))?;
        if target.exists() {
            if !target.is_dir() {
                return Err(Error::new("hub must be an ordinary clone directory".into()));
            }
            if fs::symlink_metadata(target.join(".git"))
                .is_ok_and(|m| !m.is_dir() || m.file_type().is_symlink())
            {
                return Err(Error::new("linked worktrees are unsupported".into()));
            }
            if target.join("HEAD").exists()
                && target.join("objects").is_dir()
                && !target.join(".git").is_dir()
            {
                return Err(Error::new("bare hubs are unsupported".into()));
            }
        }
        let parent = target
            .parent()
            .ok_or_else(|| Error::new("root hub is unsupported".into()))?;
        let path = parent.join(".beans-state").join(base);
        if path.starts_with(&target) {
            return Err(Error::new("hub state must be outside worktree".into()));
        }
        let c = name(parent.as_os_str())?;
        let fd = unsafe {
            libc::open(
                c.as_ptr(),
                libc::O_RDONLY | libc::O_DIRECTORY | libc::O_NOFOLLOW | libc::O_CLOEXEC,
            )
        };
        if fd < 0 {
            return Err(error(std::io::Error::last_os_error()));
        }
        let root = unsafe { File::from_raw_fd(fd) };
        let namespace = directory(&root, OsStr::new(".beans-state"))?;
        let directory = directory(&namespace, base)?;
        Ok(Self {
            target,
            path,
            directory,
        })
    }
    pub fn open(&self, entry: &str, create: bool) -> Result<File, Error> {
        let c = name(OsStr::new(entry))?;
        let flags = libc::O_RDWR
            | libc::O_NOFOLLOW
            | libc::O_CLOEXEC
            | libc::O_NONBLOCK
            | if create { libc::O_CREAT } else { 0 };
        let fd = unsafe { libc::openat(self.directory.as_raw_fd(), c.as_ptr(), flags, 0o600) };
        if fd < 0 {
            return Err(error(std::io::Error::last_os_error()));
        }
        let file = unsafe { File::from_raw_fd(fd) };
        if !file.metadata().map_err(error)?.is_file() {
            return Err(Error::new("unsafe non-regular hub state file".into()));
        }
        Ok(file)
    }
    pub fn read(&self, entry: &str) -> Result<Vec<u8>, Error> {
        let mut bytes = Vec::new();
        self.open(entry, false)?
            .read_to_end(&mut bytes)
            .map_err(error)?;
        Ok(bytes)
    }
    pub fn exists(&self, entry: &str) -> Result<bool, Error> {
        let c = name(OsStr::new(entry))?;
        let mut st = std::mem::MaybeUninit::<libc::stat>::uninit();
        if unsafe {
            libc::fstatat(
                self.directory.as_raw_fd(),
                c.as_ptr(),
                st.as_mut_ptr(),
                libc::AT_SYMLINK_NOFOLLOW,
            )
        } < 0
        {
            let e = std::io::Error::last_os_error();
            if e.kind() == std::io::ErrorKind::NotFound {
                return Ok(false);
            }
            return Err(error(e));
        }
        if unsafe { st.assume_init() }.st_mode & libc::S_IFMT != libc::S_IFREG {
            return Err(Error::new("unsafe hub state entry".into()));
        }
        Ok(true)
    }
    pub fn remove(&self, entry: &str) -> Result<(), Error> {
        if !self.exists(entry)? {
            return Ok(());
        }
        let c = name(OsStr::new(entry))?;
        if unsafe { libc::unlinkat(self.directory.as_raw_fd(), c.as_ptr(), 0) } < 0 {
            return Err(error(std::io::Error::last_os_error()));
        }
        Ok(())
    }
    pub fn write(&self, entry: &str, bytes: &[u8]) -> Result<(), Error> {
        self.exists(entry)?;
        let mut random = [0u8; 8];
        getrandom::fill(&mut random).map_err(|e| Error::new(e.to_string()))?;
        let temp = format!(".state-{:x}", u64::from_ne_bytes(random));
        let c = name(OsStr::new(&temp))?;
        let fd = unsafe {
            libc::openat(
                self.directory.as_raw_fd(),
                c.as_ptr(),
                libc::O_WRONLY | libc::O_CREAT | libc::O_EXCL | libc::O_NOFOLLOW | libc::O_CLOEXEC,
                0o600,
            )
        };
        if fd < 0 {
            return Err(error(std::io::Error::last_os_error()));
        }
        let result = unsafe { File::from_raw_fd(fd) }
            .write_all(bytes)
            .map_err(error);
        let dest = name(OsStr::new(entry))?;
        let result = result.and_then(|_| {
            if unsafe {
                libc::renameat(
                    self.directory.as_raw_fd(),
                    c.as_ptr(),
                    self.directory.as_raw_fd(),
                    dest.as_ptr(),
                )
            } < 0
            {
                Err(error(std::io::Error::last_os_error()))
            } else {
                Ok(())
            }
        });
        if result.is_err() {
            unsafe {
                libc::unlinkat(self.directory.as_raw_fd(), c.as_ptr(), 0);
            }
        }
        result
    }
}
