//! Body-only templates. Read errors fall through; successful empty or non-UTF-8
//! files are overrides too. Keep bytes until a caller decides how to use them.
use std::os::unix::ffi::{OsStrExt, OsStringExt};
use std::path::{Path, PathBuf};

pub fn default_template(kind: &str) -> &'static [u8] {
    match kind {
        "bug" => include_bytes!("templates/bug.md"),
        "feature" => include_bytes!("templates/feature.md"),
        "epic" => include_bytes!("templates/epic.md"),
        "chore" => include_bytes!("templates/chore.md"),
        "request" => default_request_template(),
        _ => include_bytes!("templates/task.md"),
    }
}

pub fn default_request_template() -> &'static [u8] {
    include_bytes!("templates/request.md")
}

pub fn load_template(kind: &str, project: &Path, hub: &Path) -> Vec<u8> {
    for directory in [project, hub] {
        if directory.as_os_str().is_empty() {
            continue;
        }
        if let Ok(bytes) = std::fs::read(template_path(directory, kind)) {
            return bytes;
        }
    }
    default_template(kind).to_vec()
}

pub fn load_request_template(project: &Path, hub: &Path) -> Vec<u8> {
    load_template("request", project, hub)
}

// filepath.Join cleans the complete Linux path before opening it, and a slash
// at the start of a later argument does not replace the preceding directory.
// PathBuf::push would replace it, while an uncleaned path can fail at a missing
// component even when `..` eliminates that component in Go.
fn template_path(directory: &Path, kind: &str) -> PathBuf {
    let mut raw = directory.as_os_str().as_bytes().to_vec();
    raw.extend_from_slice(b"/templates/");
    raw.extend_from_slice(kind.as_bytes());
    raw.extend_from_slice(b".md");
    let absolute = raw.starts_with(b"/");
    let mut parts: Vec<&[u8]> = Vec::new();
    for part in raw.split(|&byte| byte == b'/') {
        match part {
            b"" | b"." => (),
            b".." => {
                if parts.last().is_some_and(|last| *last != b"..") {
                    parts.pop();
                } else if !absolute {
                    parts.push(part);
                }
            }
            _ => parts.push(part),
        }
    }
    let mut clean = if absolute { vec![b'/'] } else { Vec::new() };
    for part in parts {
        if !clean.is_empty() && !clean.ends_with(b"/") {
            clean.push(b'/');
        }
        clean.extend_from_slice(part);
    }
    PathBuf::from(std::ffi::OsString::from_vec(clean))
}
