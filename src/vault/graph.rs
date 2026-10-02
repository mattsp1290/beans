//! Ordered note ownership and link resolution, independent of disk effects.
//! The disk index supplies metadata from the production domain decoders.
use super::go_lower::lower;
use crate::domain::{error::Error, yaml_string::YamlString};
use serde::{Deserialize, Serialize};
use std::collections::{BTreeMap, HashMap};
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum NoteKind {
    Issue,
    Doc,
    Memory,
    Request,
    Plan,
    Handoff,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum LinkKind {
    Body,
    Parent,
    BlockedBy,
    RequestIssue,
    Embed,
    HandoffIssue,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RawLink {
    pub target: Vec<u8>,
    pub kind: LinkKind,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct LinkRef {
    pub from: Vec<u8>,
    pub to: Vec<u8>,
    pub kind: LinkKind,
}
#[derive(Clone, Debug)]
pub struct GraphNote {
    pub kind: NoteKind,
    pub path: Vec<u8>,
    pub basename: Vec<u8>,
    pub id: Option<Vec<u8>>,
    pub aliases: Vec<Vec<u8>>,
    pub raw_out: Vec<RawLink>,
    pub outlinks: Vec<LinkRef>,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Warning {
    pub path: Vec<u8>,
    pub error: Error,
}
#[derive(Default)]
pub struct NoteGraph {
    order: Vec<GraphNote>,
    notes: HashMap<Vec<u8>, usize>,
    by_path: HashMap<Vec<u8>, usize>,
    ids: HashMap<(NoteKind, Vec<u8>), usize>,
    aliases: HashMap<Vec<u8>, Vec<u8>>,
    backlinks: HashMap<Vec<u8>, Vec<LinkRef>>,
    parse_warnings: BTreeMap<Vec<u8>, Error>,
    warnings: Vec<Warning>,
}
fn string(bytes: &[u8]) -> YamlString {
    YamlString::from_bytes(bytes.into())
}
impl NoteGraph {
    pub(crate) fn replace_notes(&mut self, notes: Vec<GraphNote>) {
        self.order = notes;
    }
    pub fn register(&mut self, note: GraphNote) {
        self.order.push(note);
    }
    pub fn remove_path(&mut self, path: &[u8]) {
        if let Some(index) = self.order.iter().position(|n| n.path == path) {
            self.order.remove(index);
        }
    }
    pub fn clear_parse_warning(&mut self, path: &[u8]) {
        self.parse_warnings.remove(path);
    }
    pub fn add_parse_warning(&mut self, path: Vec<u8>, error: Error) {
        self.parse_warnings.insert(path, error);
    }
    pub fn ordered_notes(&self) -> &[GraphNote] {
        &self.order
    }
    pub fn warnings(&self) -> &[Warning] {
        &self.warnings
    }
    pub fn aliases(&self) -> &HashMap<Vec<u8>, Vec<u8>> {
        &self.aliases
    }
    pub fn backlinks(&self) -> &HashMap<Vec<u8>, Vec<LinkRef>> {
        &self.backlinks
    }
    pub fn by_id(&self, kind: NoteKind, id: &[u8]) -> Option<&GraphNote> {
        self.ids.get(&(kind, id.into())).map(|&i| &self.order[i])
    }
    pub fn by_path(&self, path: &[u8]) -> Option<&GraphNote> {
        self.by_path.get(path).map(|&i| &self.order[i])
    }
    pub fn by_basename(&self, basename: &[u8]) -> Option<&GraphNote> {
        self.notes.get(basename).map(|&i| &self.order[i])
    }
    pub fn rebuild(&mut self) {
        self.notes.clear();
        self.by_path.clear();
        self.ids.clear();
        self.aliases.clear();
        self.backlinks.clear();
        let mut duplicates = Vec::new();
        for (i, note) in self.order.iter().enumerate() {
            self.by_path.insert(note.path.clone(), i);
            if let Some(&first) = self.notes.get(note.basename.as_slice()) {
                duplicates.push(Warning {
                    path: note.path.clone(),
                    error: Error::from_bytes(
                        [
                            format!(
                                "duplicate note basename {} (also ",
                                string(&note.basename).quoted()
                            )
                            .as_bytes(),
                            &self.order[first].path,
                            b"); links resolve to the first",
                        ]
                        .concat(),
                    ),
                });
            } else {
                self.notes.insert(note.basename.clone(), i);
            }
            if note.kind != NoteKind::Doc
                && note.kind != NoteKind::Memory
                && let Some(id) = &note.id
            {
                let key = (note.kind, id.clone());
                if let Some(&first) = self.ids.get(&key) {
                    let kind = match note.kind {
                        NoteKind::Issue => "issue",
                        NoteKind::Request => "request",
                        NoteKind::Plan => "plan",
                        NoteKind::Handoff => "handoff",
                        _ => unreachable!(),
                    };
                    duplicates.push(Warning {
                        path: note.path.clone(),
                        error: Error::from_bytes(
                            [
                                format!("duplicate {kind} id {} (also ", string(id).quoted())
                                    .as_bytes(),
                                &self.order[first].path,
                                b"); the first is used",
                            ]
                            .concat(),
                        ),
                    });
                } else {
                    self.ids.insert(key, i);
                }
            }
        }
        for note in &self.order {
            if note.kind == NoteKind::Memory {
                continue;
            }
            for alias in &note.aliases {
                let alias = string(alias).trimmed().as_bytes().to_vec();
                if alias.is_empty() || alias == note.basename {
                    continue;
                }
                self.aliases
                    .entry(alias)
                    .or_insert_with(|| note.basename.clone());
            }
        }
        let mut unresolved = Vec::new();
        for i in 0..self.order.len() {
            let mut outlinks = Vec::new();
            for raw in &self.order[i].raw_out {
                let mut link = LinkRef {
                    from: self.order[i].basename.clone(),
                    to: raw.target.clone(),
                    kind: raw.kind,
                };
                if let Some(target) = self.lookup(&raw.target) {
                    link.to = target.basename.clone();
                    self.backlinks
                        .entry(link.to.clone())
                        .or_default()
                        .push(link.clone());
                } else {
                    unresolved.push(Warning {
                        path: self.order[i].path.clone(),
                        error: Error::from_bytes(
                            [
                                b"unresolved link [[".as_slice(),
                                &raw.target,
                                b"]] in ",
                                &self.order[i].path,
                            ]
                            .concat(),
                        ),
                    });
                }
                outlinks.push(link);
            }
            self.order[i].outlinks = outlinks;
        }
        self.warnings = self
            .parse_warnings
            .iter()
            .map(|(path, error)| Warning {
                path: path.clone(),
                error: error.clone(),
            })
            .collect();
        self.warnings.extend(duplicates);
        self.warnings.extend(unresolved);
    }
    pub fn lookup(&self, target: &[u8]) -> Option<&GraphNote> {
        let trimmed = string(target).trimmed();
        let mut target = trimmed.as_bytes();
        while target.starts_with(b"/") {
            target = &target[1..];
        }
        while target.ends_with(b"/") {
            target = &target[..target.len() - 1];
        }
        if target.contains(&b'/') {
            let mut with_md = target.to_vec();
            if !with_md.ends_with(b".md") {
                with_md.extend(b".md");
            }
            if let Some(note) = self.by_path(&with_md) {
                return Some(note);
            }
            let suffix = [b"/".as_slice(), &with_md].concat();
            let mut matches = self
                .by_path
                .iter()
                .filter(|(path, _)| path.ends_with(&suffix));
            if let Some((_, &i)) = matches.next()
                && matches.next().is_none()
            {
                return Some(&self.order[i]);
            }
            target = target.rsplit(|&b| b == b'/').next().unwrap();
        }
        let target = target.strip_suffix(b".md").unwrap_or(target);
        if let Some(note) = self.by_basename(target) {
            return Some(note);
        }
        if let Some(basename) = self.aliases.get(target)
            && let Some(note) = self.by_basename(basename)
        {
            return Some(note);
        }
        if let Some(note) = self.by_id(NoteKind::Issue, target) {
            let basename = note.path.rsplit(|&b| b == b'/').next().unwrap_or_default();
            if let Some(note) = self.by_basename(basename.strip_suffix(b".md").unwrap_or(basename))
            {
                return Some(note);
            }
        }
        if let Some(note) = self.by_id(NoteKind::Handoff, target)
            && let Some(note) = self.by_path(&note.path)
        {
            return Some(note);
        }
        let target = lower(target);
        self.notes
            .iter()
            .find(|(basename, _)| lower(basename) == target)
            .map(|(_, &i)| &self.order[i])
    }
}
