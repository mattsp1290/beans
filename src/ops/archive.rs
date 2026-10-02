//! Archive selections are evaluated against the current locked transaction snapshot.
use crate::{
    domain::frontmatter::Error,
    gitops::{Operation, check_hub_write_path, remove_hub_file, write_hub_file},
    vault::{Index, LoadOptions, Note, NoteData, NoteKind},
};
use std::path::{Path, PathBuf};

#[derive(Clone, Debug)]
pub enum ArchiveSelection {
    Issues {
        project: Vec<u8>,
        cutoff: i64,
    },
    Handoffs {
        project: Vec<u8>,
        references: Vec<String>,
        cutoff: Option<i64>,
    },
}

pub struct ArchiveMutation {
    pub selection: ArchiveSelection,
    pub explicit_workflow: Option<PathBuf>,
    pub ids: Vec<String>,
    year: i32,
    owned_moves: Vec<(String, PathBuf, PathBuf)>,
}

impl ArchiveMutation {
    pub fn new(selection: ArchiveSelection, explicit_workflow: Option<PathBuf>) -> Self {
        Self {
            selection,
            explicit_workflow,
            ids: Vec::new(),
            year: time::OffsetDateTime::now_utc().year(),
            owned_moves: Vec::new(),
        }
    }

    pub fn select<'a>(&self, index: &'a Index) -> Result<Vec<&'a Note>, Error> {
        let notes = match &self.selection {
            ArchiveSelection::Issues { project, cutoff } => index
                .project_issues(project, false)
                .into_iter()
                .filter(|n| matches!(&n.data, NoteData::Issue(d)
                    if index.workflow_for(&n.project).is_terminal(d.metadata.status.as_bytes())
                    && d.log.last().map(|entry| entry.at.seconds).unwrap_or(d.metadata.updated.seconds)
                        .max(d.metadata.updated.seconds) < *cutoff))
                .collect(),
            ArchiveSelection::Handoffs { project, references, cutoff } => {
                if references.is_empty() {
                    let cutoff = cutoff.ok_or_else(|| Error::new("archive requires IDs or --older-than".into()))?;
                    index.project_handoffs(project, false).into_iter()
                        .filter(|n| matches!(&n.data, NoteData::Handoff(d) if d.metadata.updated.seconds < cutoff))
                        .collect()
                } else {
                    let mut notes = Vec::new();
                    let mut seen = std::collections::HashSet::new();
                    for reference in references {
                        let note = index.note_by_id(NoteKind::Handoff, reference.as_bytes())
                            .or_else(|| index.lookup(reference.as_bytes()).filter(|n| n.graph.kind == NoteKind::Handoff))
                            .ok_or_else(|| Error::new(format!("handoff not found: {reference}")))?;
                        let NoteData::Handoff(document) = &note.data else { unreachable!() };
                        if !document.metadata.archived && seen.insert(document.metadata.id.clone()) {
                            notes.push(note);
                        }
                    }
                    notes
                }
            }
        };
        Ok(notes)
    }
}

impl Operation for ArchiveMutation {
    fn subject(&self) -> String {
        "bn: archive records".into()
    }

    fn after_rebase(&mut self, present: bool) {
        if !present {
            self.owned_moves.clear();
            self.ids.clear();
        }
    }

    fn apply(&mut self, hub: &Path) -> Result<Vec<PathBuf>, Error> {
        let index = Index::load_snapshot(
            hub,
            LoadOptions {
                explicit_workflow: self.explicit_workflow.clone(),
            },
        )?;
        let notes = self.select(&index)?;
        let mut moves = Vec::new();
        let mut ids = Vec::new();
        let mut retained = Vec::new();
        // A successful rename/modify rebase can keep our archive commit while
        // carrying a competing edit into the destination. Undo only this run's
        // surviving move if the freshly edited record is no longer eligible.
        for (id, from, to) in &self.owned_moves {
            let kind = match self.selection {
                ArchiveSelection::Issues { .. } => NoteKind::Issue,
                ArchiveSelection::Handoffs { .. } => NoteKind::Handoff,
            };
            let Some(note) = index
                .note_by_id(kind, id.as_bytes())
                .filter(|n| n.graph.path.as_slice() == to.to_string_lossy().as_bytes())
            else {
                continue;
            };
            let eligible = match (&self.selection, &note.data) {
                (ArchiveSelection::Issues { cutoff, .. }, NoteData::Issue(d)) => {
                    d.metadata.id == *id
                        && index
                            .workflow_for(&note.project)
                            .is_terminal(d.metadata.status.as_bytes())
                        && d.log
                            .last()
                            .map(|e| e.at.seconds)
                            .unwrap_or(d.metadata.updated.seconds)
                            .max(d.metadata.updated.seconds)
                            < *cutoff
                }
                (
                    ArchiveSelection::Handoffs {
                        cutoff: Some(cutoff),
                        ..
                    },
                    NoteData::Handoff(d),
                ) => d.metadata.id == *id && d.metadata.updated.seconds < *cutoff,
                (ArchiveSelection::Handoffs { cutoff: None, .. }, NoteData::Handoff(d)) => {
                    d.metadata.id == *id
                }
                _ => {
                    return Err(Error::new(
                        "owned archive destination changed identity".into(),
                    ));
                }
            };
            if eligible {
                retained.push((id.clone(), from.clone(), to.clone()));
                ids.push(id.clone());
            } else {
                check_hub_write_path(hub, from)?;
                check_hub_write_path(hub, to)?;
                if hub.join(from).exists() {
                    return Err(Error::new(
                        "archive retry live destination already exists".into(),
                    ));
                }
                moves.push((to.clone(), from.clone(), note.source.clone()));
            }
        }
        for note in notes {
            let from = PathBuf::from(String::from_utf8_lossy(&note.graph.path).as_ref());
            let project =
                PathBuf::from("projects").join(String::from_utf8_lossy(&note.project).as_ref());
            let (id, directory, year) = match &note.data {
                NoteData::Issue(document) => (
                    document.metadata.id.clone(),
                    project.join("archive"),
                    time::OffsetDateTime::from_unix_timestamp(document.metadata.updated.seconds)
                        .map_err(|e| Error::new(e.to_string()))?
                        .year(),
                ),
                NoteData::Handoff(document) => (
                    document.metadata.id.clone(),
                    project.join("handoffs/archive"),
                    self.year,
                ),
                _ => unreachable!(),
            };
            let to = directory
                .join(year.to_string())
                .join(from.file_name().unwrap());
            check_hub_write_path(hub, &from)?;
            check_hub_write_path(hub, &to)?;
            if hub.join(&to).exists() {
                return Err(Error::new("archive destination already exists".into()));
            }
            ids.push(id.clone());
            retained.push((id, from.clone(), to.clone()));
            moves.push((from, to, note.source.clone()));
        }
        let mut paths = Vec::new();
        for (from, to, bytes) in moves {
            write_hub_file(hub, &to, &bytes)?;
            remove_hub_file(hub, &from)?;
            paths.extend([from, to]);
        }
        self.ids = ids;
        self.owned_moves = retained;
        Ok(paths)
    }
}
