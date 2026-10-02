//! Linux bundle capture and portable snapshot validation, separate from hub writes.
use super::{
    Bundle, BundleSnapshot, Section, YamlString, encode, id, parse_bytes, scaffold, validate,
};
use crate::domain::{
    file_io::path_error,
    frontmatter::Error,
    issue::{Timestamp, quoted},
};
use std::os::unix::{
    ffi::OsStrExt,
    fs::{DirBuilderExt, OpenOptionsExt},
    io::IntoRawFd,
};
use std::{
    collections::BTreeSet,
    fs::{self, File, OpenOptions},
    io::{self, Read, Write},
    path::Path,
};

pub const MAX_FILE_SIZE: usize = 512 << 10;
pub const MAX_BUNDLE_SIZE: usize = 2 << 20;

pub fn valid_section_path(name: &[u8]) -> bool {
    name.starts_with(b"sections/")
        && name.ends_with(b".md")
        && name.len() > b"sections/.md".len()
        && !name.contains(&b'\\')
        && name.iter().filter(|&&b| b == b'/').count() == 1
        && !name.windows(2).any(|w| w == b"..")
}

fn valid_file_name(name: &[u8]) -> bool {
    name == b"plan.md" || valid_section_path(name)
}

// Go's path.Join is lexical and uses '/' even when the root contains redundant
// separators or dot segments. The root retained on Bundle itself is unmodified.
pub fn load_snapshot(root: &str, snapshot: &BundleSnapshot) -> Result<Bundle, Error> {
    load_snapshot_bytes(root.as_bytes(), snapshot)
}
pub fn load_snapshot_bytes(root: &[u8], snapshot: &BundleSnapshot) -> Result<Bundle, Error> {
    let data = snapshot
        .files
        .get(b"plan.md".as_slice())
        .ok_or_else(|| Error::new("missing plan.md".into()).context(root))?;
    if data.len() > MAX_FILE_SIZE {
        return Err(Error::new("plan.md: file exceeds 512KiB".into()));
    }
    let mut total = 0usize;
    for (name, bytes) in &snapshot.files {
        if !valid_file_name(name.as_bytes()) {
            return Err(Error::new("invalid bundle file".into()).context(name.as_bytes()));
        }
        total = total
            .checked_add(bytes.len())
            .ok_or_else(|| Error::new("bundle exceeds 2MiB".into()))?;
        if bytes.len() > MAX_FILE_SIZE {
            return Err(Error::new("file exceeds 512KiB".into()).context(name.as_bytes()));
        }
    }
    if total > MAX_BUNDLE_SIZE {
        return Err(Error::new("bundle exceeds 2MiB".into()));
    }
    let mut plan = parse_bytes(&crate::vault::paths::join(&[root, b"plan.md"]), data)?;
    let prefix = plan.id.split("-plan-").next().unwrap_or_default();
    if !id::valid_id(prefix, &plan.id) {
        return Err(Error::new(format!(
            "plan.md: invalid plan id {}",
            quoted(&plan.id)
        )));
    }
    let mut seen = BTreeSet::new();
    let mut sections = Vec::new();
    for name in &plan.sections {
        if !valid_section_path(name.as_bytes()) || !seen.insert(name.clone()) {
            return Err(Error::new(format!(
                "plan.md: invalid or duplicate section {}",
                name.quoted()
            )));
        }
        let bytes = snapshot
            .files
            .get(name)
            .ok_or_else(|| Error::new("listed section missing".into()).context(name.as_bytes()))?;
        if bytes.contains(&0)
            || bytes.contains(&b'\r')
            || bytes.last() != Some(&b'\n')
            || std::str::from_utf8(bytes).is_err()
        {
            return Err(Error::new("must be UTF-8 LF text ending in newline".into())
                .context(name.as_bytes()));
        }
        sections.push(Section {
            path: name.clone(),
            markdown: std::str::from_utf8(bytes).unwrap().into(),
        });
    }
    for name in snapshot.files.keys() {
        if name.as_bytes() != b"plan.md" && !seen.contains(name) {
            return Err(Error::new("section is not listed".into()).context(name.as_bytes()));
        }
    }
    validate(Some(&plan))?;
    plan.section_bodies = sections.clone();
    Ok(Bundle {
        plan: Some(plan),
        sections,
        root: YamlString::from_bytes(root.into()),
    })
}

impl Bundle {
    /// Go deliberately ignores manifest encoding errors here; publication
    /// validation is a separate operation. Sections overwrite repeated paths.
    pub fn snapshot(&self) -> BundleSnapshot {
        let mut snapshot = BundleSnapshot::default();
        snapshot.files.insert(
            "plan.md".into(),
            encode(self.plan.as_ref()).unwrap_or_default(),
        );
        for section in &self.sections {
            snapshot
                .files
                .insert(section.path.clone(), section.markdown.as_bytes().into());
        }
        snapshot
    }
}
impl BundleSnapshot {
    pub fn paths(&self) -> Vec<YamlString> {
        self.files.keys().cloned().collect()
    }
}

fn read_file(path: &Path) -> Result<Vec<u8>, Error> {
    let mut file = File::open(path).map_err(|e| path_error("open", path, e))?;
    let mut bytes = Vec::new();
    file.read_to_end(&mut bytes)
        .map_err(|e| path_error("read", path, e))?;
    Ok(bytes)
}

fn capture(
    root: &Path,
    directory: &Path,
    files: &mut BundleSnapshot,
    total: &mut u64,
) -> Result<(), Error> {
    let entries = fs::read_dir(directory).map_err(|e| path_error("open", directory, e))?;
    let mut children = Vec::new();
    for entry in entries {
        children.push(entry.map_err(|e| path_error("readdirent", directory, e))?);
    }
    children.sort_by_key(|e| e.file_name());
    for entry in children {
        let full = entry.path();
        let name = YamlString::from_bytes(
            full.strip_prefix(root)
                .unwrap()
                .as_os_str()
                .as_bytes()
                .into(),
        );
        let kind = entry
            .file_type()
            .map_err(|e| path_error("lstat", &full, e))?;
        if kind.is_symlink() {
            return Err(Error::new("symlinks are not allowed".into()).context(name.as_bytes()));
        }
        if kind.is_dir() {
            if name.as_bytes() != b"sections" {
                return Err(Error::new("unexpected directory".into()).context(name.as_bytes()));
            }
            capture(root, &full, files, total)?;
            continue;
        }
        if !kind.is_file() {
            return Err(Error::new("non-regular file".into()).context(name.as_bytes()));
        }
        if name.as_bytes() != b"plan.md" && !name.as_bytes().starts_with(b"sections/") {
            return Err(Error::new("unexpected file".into()).context(name.as_bytes()));
        }
        if !name.as_bytes().ends_with(b".md") {
            return Err(
                Error::new("only Markdown files are allowed".into()).context(name.as_bytes())
            );
        }
        let info = fs::symlink_metadata(&full).map_err(|e| path_error("lstat", &full, e))?;
        if info.len() > MAX_FILE_SIZE as u64 {
            return Err(Error::new("file exceeds 512KiB".into()).context(name.as_bytes()));
        }
        *total += info.len();
        if *total > MAX_BUNDLE_SIZE as u64 {
            return Err(Error::new("bundle exceeds 2MiB".into()));
        }
        files.files.insert(name, read_file(&full)?);
    }
    Ok(())
}

pub fn load(root: &str) -> Result<Bundle, Error> {
    load_path(Path::new(root))
}
pub fn load_path(path: &Path) -> Result<Bundle, Error> {
    let root = path.as_os_str().as_bytes();
    let metadata = fs::symlink_metadata(path).map_err(|e| path_error("lstat", path, e))?;
    if !metadata.is_dir() || metadata.file_type().is_symlink() {
        return Err(Error::new("bundle root must be a directory".into()).context(root));
    }
    let mut snapshot = BundleSnapshot::default();
    capture(path, path, &mut snapshot, &mut 0)?;
    load_snapshot_bytes(root, &snapshot)
}

pub fn write_scaffold(
    directory: &str,
    id: &str,
    title: &str,
    now: &Timestamp,
) -> Result<(), Error> {
    write_scaffold_path(Path::new(directory), id, title, now)
}
pub fn write_scaffold_path(
    directory: &Path,
    id: &str,
    title: &str,
    now: &Timestamp,
) -> Result<(), Error> {
    fs::DirBuilder::new()
        .mode(0o755)
        .create(directory)
        .map_err(|e| path_error("mkdir", directory, e))?;
    let file_path = directory.join("plan.md");
    let result = (|| {
        let data = scaffold(id, title, now)?;
        let mut file = OpenOptions::new()
            .write(true)
            .create(true)
            .truncate(true)
            .mode(0o644)
            .open(&file_path)
            .map_err(|e| path_error("open", &file_path, e))?;
        // Go File.Write retries EINTR but reports a successful short write
        // instead of silently completing it with a second write.
        let written = loop {
            match file.write(&data) {
                Err(error) if error.kind() == io::ErrorKind::Interrupted => continue,
                Err(error) => break Err(path_error("write", &file_path, error)),
                Ok(size) if size != data.len() => break Err(Error::new("short write".into())),
                Ok(_) => break Ok(()),
            }
        };
        // File's Drop discards close errors; os.WriteFile returns them when
        // writing succeeded. Consume ownership before closing exactly once.
        let descriptor = file.into_raw_fd();
        // SAFETY: descriptor belongs to the consumed File and is closed once.
        let closed = unsafe { libc::close(descriptor) };
        let close_error = (closed < 0).then(io::Error::last_os_error);
        written?;
        if let Some(error) = close_error {
            return Err(path_error("close", &file_path, error));
        }
        Ok(())
    })();
    result.map_err(|e| e.context(b"write scaffold"))
}
