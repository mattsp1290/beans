//! Hub-relative path rules; callers still decide which directories to walk.
use super::go_lower::lower;
use super::graph::NoteKind;
pub fn classify(path: &[u8]) -> Option<(NoteKind, Vec<u8>)> {
    if !path.ends_with(b".md") {
        return None;
    }
    let parts: Vec<_> = path.split(|&b| b == b'/').collect();
    if parts[0] == b"projects" {
        if parts.len() < 4 {
            return None;
        }
        let count = parts.len() - 2;
        let kind = match parts[2] {
            b"issues" if count == 2 => NoteKind::Issue,
            b"archive" if count >= 2 => NoteKind::Issue,
            b"docs" if count >= 2 => NoteKind::Doc,
            b"memories" if count == 2 => NoteKind::Memory,
            b"requests" if count == 2 => NoteKind::Request,
            b"handoffs"
                if count == 2 || count == 4 && parts[3] == b"archive" && parts[4].len() == 4 =>
            {
                NoteKind::Handoff
            }
            _ => return None,
        };
        Some((kind, parts[1].into()))
    } else {
        match parts[0] {
            b"docs" if parts.len() >= 2 => Some((NoteKind::Doc, Vec::new())),
            b"memories" if parts.len() == 2 => Some((NoteKind::Memory, Vec::new())),
            _ => None,
        }
    }
}
pub fn is_asset_path(path: &[u8]) -> bool {
    let base = path.rsplit(|&b| b == b'/').next().unwrap_or_default();
    let ext = base
        .iter()
        .rposition(|&b| b == b'.')
        .map_or(&b""[..], |i| &base[i..]);
    let ext = lower(ext);
    if !matches!(
        ext.as_slice(),
        b".png" | b".jpg" | b".jpeg" | b".gif" | b".svg" | b".webp"
    ) {
        return false;
    }
    let parts: Vec<_> = path.split(|&b| b == b'/').collect();
    if parts[0] == b"projects" {
        parts.len() >= 4 && matches!(parts[2], b"docs" | b"issues" | b"archive")
    } else {
        parts.len() >= 2 && matches!(parts[0], b"docs" | b"issues" | b"archive")
    }
}
pub fn skip_dir_name(name: &[u8]) -> bool {
    name.starts_with(b".") || name == b"templates"
}
pub fn is_plans_directory(path: &[u8]) -> bool {
    let parts: Vec<_> = path.split(|&b| b == b'/').collect();
    parts.len() == 3 && parts[0] == b"projects" && !parts[1].is_empty() && parts[2] == b"plans"
}
pub fn plan_bundle_project(path: &[u8]) -> Option<Vec<u8>> {
    let parts: Vec<_> = path.split(|&b| b == b'/').collect();
    (parts.len() == 4 && parts[0] == b"projects" && parts[2] == b"plans").then(|| parts[1].into())
}
pub fn plan_manifest_project(path: &[u8]) -> Option<Vec<u8>> {
    let parts: Vec<_> = path.split(|&b| b == b'/').collect();
    (parts.len() == 5 && parts[0] == b"projects" && parts[2] == b"plans" && parts[4] == b"plan.md")
        .then(|| parts[1].into())
}
