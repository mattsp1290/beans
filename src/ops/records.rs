//! Request and continuation operations retain authored documents and reread on replay.
use crate::{
    domain::{
        frontmatter::Error,
        handoff::{HandoffDocument, HandoffMetadata},
        issue::Timestamp,
        log::LogEntry,
        prompt::{self, PromptDocument, PromptMetadata},
        request::{self, RequestDocument, RequestMetadata},
        text::Link,
    },
    gitops::{Operation, check_hub_write_path, remove_hub_file, write_hub_file},
    vault::{Index, LoadOptions, NoteData, NoteKind, Resolved},
};
use std::{
    ffi::{OsStr, OsString},
    os::unix::ffi::OsStrExt,
    path::{Path, PathBuf},
};

fn filename_reserves_id(name: &OsStr, id: &str) -> bool {
    let Some(stem) = name.as_bytes().strip_suffix(b".md") else {
        return false;
    };
    stem == id.as_bytes()
        || stem
            .strip_prefix(id.as_bytes())
            .is_some_and(|suffix| suffix.starts_with(b"-"))
}

fn directory_filenames(path: &Path) -> Result<Vec<OsString>, Error> {
    let entries = match std::fs::read_dir(path) {
        Ok(entries) => entries,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(Vec::new()),
        Err(error) => return Err(Error::new(error.to_string())),
    };
    entries
        .map(|entry| {
            entry
                .map(|entry| entry.file_name())
                .map_err(|error| Error::new(error.to_string()))
        })
        .collect()
}
#[derive(Clone, Debug, Default)]
pub struct RequestEdit {
    pub title: Option<String>,
    pub status: Option<String>,
    pub priority: Option<i64>,
    pub requested_by: Option<String>,
    pub body: Option<String>,
    pub labels: Vec<String>,
    pub unlabels: Vec<String>,
    pub issues: Vec<String>,
    pub unlink: bool,
    pub force: bool,
}
#[derive(Clone, Debug, Default)]
pub struct PromptEdit {
    pub title: Option<String>,
    pub status: Option<String>,
    pub priority: Option<i64>,
    pub from: Option<String>,
    pub to: Option<String>,
    pub body: Option<Vec<u8>>,
    pub issue: Option<String>,
    pub note: Option<String>,
    pub labels: Vec<String>,
    pub unlabels: Vec<String>,
    pub claim: bool,
    pub unlink: bool,
    pub force: bool,
}
#[derive(Clone, Debug)]
pub enum RecordChange {
    RequestCreate(RequestEdit),
    RequestUpdate(RequestEdit),
    HandoffCreate {
        title: String,
        body: Vec<u8>,
        issue: Option<String>,
    },
    HandoffAttach(Option<String>),
    HandoffArchive(bool),
    PromptCreate(PromptEdit),
    PromptUpdate(PromptEdit),
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum RecordFamily {
    Request,
    Handoff,
    Prompt,
}
impl RecordFamily {
    fn of(change: &RecordChange) -> Self {
        match change {
            RecordChange::RequestCreate(_) | RecordChange::RequestUpdate(_) => Self::Request,
            RecordChange::HandoffCreate { .. }
            | RecordChange::HandoffAttach(_)
            | RecordChange::HandoffArchive(_) => Self::Handoff,
            RecordChange::PromptCreate(_) | RecordChange::PromptUpdate(_) => Self::Prompt,
        }
    }
    fn kind(self) -> NoteKind {
        match self {
            Self::Request => NoteKind::Request,
            Self::Handoff => NoteKind::Handoff,
            Self::Prompt => NoteKind::Prompt,
        }
    }
    fn infix(self) -> &'static str {
        match self {
            Self::Request => "r",
            Self::Handoff => "h",
            Self::Prompt => "p",
        }
    }
    fn directory(self) -> &'static str {
        match self {
            Self::Request => "requests",
            Self::Handoff => "handoffs",
            Self::Prompt => "prompts",
        }
    }
    fn creating(change: &RecordChange) -> bool {
        matches!(
            change,
            RecordChange::RequestCreate(_)
                | RecordChange::HandoffCreate { .. }
                | RecordChange::PromptCreate(_)
        )
    }
}
pub struct RecordMutation {
    pub resolved: Resolved,
    pub reference: String,
    pub change: RecordChange,
    pub id: String,
    pub explicit: Option<PathBuf>,
    log: LogEntry,
    present: bool,
}
pub fn now() -> Timestamp {
    Timestamp {
        seconds: time::OffsetDateTime::now_utc().unix_timestamp(),
        nanoseconds: 0,
        offset_seconds: 0,
    }
}
impl RecordMutation {
    pub fn new(
        resolved: Resolved,
        reference: String,
        change: RecordChange,
        actor: String,
        explicit: Option<PathBuf>,
    ) -> Self {
        let log = LogEntry {
            at: now(),
            actor: actor.into(),
            repo: crate::domain::yaml_string::YamlString::from_bytes(resolved.project.clone()),
            sha: crate::domain::yaml_string::YamlString::from_bytes(resolved.repo_head.clone()),
            branch: crate::domain::yaml_string::YamlString::from_bytes(
                resolved.repo_branch.clone(),
            ),
            event: "record updated".into(),
            ..Default::default()
        };
        Self {
            resolved,
            reference,
            change,
            id: String::new(),
            explicit,
            log,
            present: false,
        }
    }
}
impl Operation for RecordMutation {
    fn subject(&self) -> String {
        format!(
            "bn: record {}",
            if self.id.is_empty() {
                &self.reference
            } else {
                &self.id
            }
        )
    }
    fn after_rebase(&mut self, present: bool) {
        self.present = present;
    }
    fn apply(&mut self, hub: &Path) -> Result<Vec<PathBuf>, Error> {
        let project = String::from_utf8_lossy(&self.resolved.project);
        let base = PathBuf::from("projects").join(project.as_ref());
        check_hub_write_path(hub, &base.join("beans.toml"))?;
        let ix = Index::load_snapshot(
            hub,
            LoadOptions {
                explicit_workflow: self.explicit.clone(),
            },
        )?;
        let family = RecordFamily::of(&self.change);
        let kind = family.kind();
        let creating = RecordFamily::creating(&self.change);
        if let RecordChange::PromptCreate(edit) | RecordChange::PromptUpdate(edit) = &self.change {
            if edit
                .note
                .as_ref()
                .is_some_and(|note| note.trim().is_empty())
            {
                return Err(Error::new("prompt note must not be blank".into()));
            }
            prompt::validate_update_combination(
                edit.to.is_some(),
                edit.claim,
                edit.status.as_deref(),
            )?;
            if edit.claim
                && edit
                    .status
                    .as_deref()
                    .is_some_and(|status| status != prompt::IN_PROGRESS)
            {
                return Err(Error::new(
                    "prompt claim can only be combined with in_progress status".into(),
                ));
            }
            if edit.unlink && edit.issue.is_some() {
                return Err(Error::new(
                    "prompt link and unlink cannot be combined".into(),
                ));
            }
        }
        if creating && self.id.is_empty() {
            let prefix = String::from_utf8_lossy(&super::prefix_for(hub, &self.resolved.project))
                .into_owned();
            let filenames = directory_filenames(&hub.join(base.join(family.directory())))?;
            let mut exists = |id: &str| {
                ix.lookup(id.as_bytes()).is_some()
                    || ix
                        .ordered_notes()
                        .iter()
                        .any(|n| n.graph.id.as_deref() == Some(id.as_bytes()))
                    || filenames.iter().any(|name| filename_reserves_id(name, id))
            };
            self.id = crate::domain::id::new_id(
                &format!("{prefix}-{}", family.infix()),
                Some(&mut exists),
                ix.hub_config.ids.length as isize,
            );
        }
        if creating
            && ix.lookup(self.id.as_bytes()).is_some()
            && ix.note_by_id(kind, self.id.as_bytes()).is_none()
        {
            return Err(Error::new(format!(
                "record ID collides with an existing hub artifact: {}",
                self.id
            )));
        }
        let existing = if creating {
            ix.note_by_id(kind, self.id.as_bytes())
        } else {
            ix.note_by_id(kind, self.reference.as_bytes()).or_else(|| {
                ix.lookup(self.reference.as_bytes())
                    .filter(|n| n.graph.kind == kind)
            })
        };
        if let Some(n) = existing
            && n.project != self.resolved.project
        {
            return Err(Error::new("record belongs to a different project".into()));
        }
        if creating && existing.is_some() {
            if self.present {
                return Ok(vec![]);
            }
            return Err(Error::new(format!("record ID collision: {}", self.id)));
        }
        if !creating && existing.is_none() {
            return Err(Error::new(format!("record not found: {}", self.reference)));
        }
        if self.present && existing.is_some() {
            return Ok(vec![]);
        }
        let path = existing
            .map(|n| PathBuf::from(String::from_utf8_lossy(&n.graph.path).as_ref()))
            .unwrap_or_else(|| {
                let filename = match &self.change {
                    RecordChange::PromptCreate(edit) => {
                        let title = edit.title.as_deref().unwrap_or_default();
                        crate::domain::id::filename(&self.id, &crate::domain::id::slug(title))
                    }
                    _ => format!("{}.md", self.id),
                };
                base.join(family.directory()).join(filename)
            });
        check_hub_write_path(hub, &path)?;
        if creating {
            match std::fs::symlink_metadata(hub.join(&path)) {
                Ok(_) => {
                    return Err(Error::new(format!(
                        "record destination already exists: {}",
                        path.display()
                    )));
                }
                Err(error) if error.kind() == std::io::ErrorKind::NotFound => {}
                Err(error) => return Err(Error::new(error.to_string())),
            }
        }
        self.id = existing
            .and_then(|n| n.graph.id.as_ref())
            .map(|v| String::from_utf8_lossy(v).into_owned())
            .unwrap_or_else(|| self.id.clone());
        let bytes = match family {
            RecordFamily::Request => {
                let mut doc = if existing.is_some() {
                    RequestDocument::parse_bytes(
                        &path.to_string_lossy(),
                        &std::fs::read(hub.join(&path)).map_err(|e| Error::new(e.to_string()))?,
                    )?
                } else {
                    RequestDocument::new(RequestMetadata {
                        id: self.id.clone(),
                        title: String::new(),
                        status: request::OPEN.into(),
                        priority: 2,
                        created: self.log.at.clone(),
                        updated: self.log.at.clone(),
                        ..Default::default()
                    })
                };
                let (RecordChange::RequestCreate(edit) | RecordChange::RequestUpdate(edit)) =
                    &self.change
                else {
                    unreachable!()
                };
                if creating && edit.body.is_none() {
                    doc.body = crate::domain::yaml_string::YamlString::from_bytes(
                        crate::domain::template::load_request_template(&hub.join(&base), hub),
                    );
                }
                let old = doc.metadata.clone();
                let old_body = doc.body.clone();
                if let Some(v) = &edit.title {
                    if v.trim().is_empty() {
                        return Err(Error::new("title must not be empty".into()));
                    }
                    doc.metadata.title = v.clone();
                }
                if let Some(v) = &edit.status {
                    if !request::valid_status(v) {
                        return Err(Error::new("invalid request status".into()));
                    }
                    if !edit.force {
                        request::validate_transition(&doc.metadata.status, v)?;
                    }
                    doc.metadata.status = v.clone();
                }
                if let Some(v) = edit.priority {
                    if !(0..=4).contains(&v) {
                        return Err(Error::new("priority must be between 0 and 4".into()));
                    }
                    doc.metadata.priority = v;
                }
                if let Some(v) = &edit.requested_by {
                    doc.metadata.requested_by = v.clone();
                }
                if let Some(v) = &edit.body {
                    doc.body = v.clone().into();
                }
                doc.metadata.labels.retain(|v| !edit.unlabels.contains(v));
                for v in &edit.labels {
                    if !doc.metadata.labels.contains(v) {
                        doc.metadata.labels.push(v.clone());
                    }
                }
                for v in &edit.issues {
                    let link = if edit.unlink {
                        let (_, n) = ix.resolve_issue_ref(v.as_bytes());
                        n.map(|n| {
                            Link::parse(&format!(
                                "[[{}]]",
                                String::from_utf8_lossy(n.graph.id.as_ref().unwrap())
                            ))
                        })
                        .unwrap_or_else(|| Link::parse(v))
                    } else {
                        super::issues::canonical_link(&ix, v)?
                    };
                    if edit.unlink {
                        doc.metadata.issues.retain(|l| l.target != link.target);
                    } else if !doc.metadata.issues.iter().any(|l| l.target == link.target) {
                        doc.metadata.issues.push(link);
                    }
                }
                if !creating && old == doc.metadata && old_body == doc.body {
                    return Ok(vec![]);
                }
                if !self.present {
                    doc.append_log(self.log.clone());
                }
                doc.encode()?.bytes
            }
            RecordFamily::Handoff => {
                let mut doc = if let Some(n) = existing {
                    let NoteData::Handoff(d) = &n.data else {
                        unreachable!()
                    };
                    d.as_ref().clone()
                } else {
                    let RecordChange::HandoffCreate { title, body, .. } = &self.change else {
                        unreachable!()
                    };
                    let mut d = HandoffDocument::new(HandoffMetadata {
                        id: self.id.clone(),
                        title: title.clone(),
                        created: self.log.at.clone(),
                        updated: self.log.at.clone(),
                        ..Default::default()
                    });
                    d.body = crate::domain::yaml_string::YamlString::from_bytes(body.clone());
                    d
                };
                let attach = match &self.change {
                    RecordChange::HandoffCreate { issue, .. } => Some(issue),
                    RecordChange::HandoffAttach(issue) => Some(issue),
                    _ => None,
                };
                if let Some(issue) = attach {
                    let link = issue
                        .as_ref()
                        .map(|v| super::issues::canonical_link(&ix, v))
                        .transpose()?
                        .unwrap_or_default();
                    if !creating && doc.metadata.issue == link {
                        return Ok(vec![]);
                    }
                    doc.metadata.issue = link;
                    doc.metadata.updated = self.log.at.clone();
                }
                doc.encode()?.bytes
            }
            RecordFamily::Prompt => {
                let edit = match &self.change {
                    RecordChange::PromptCreate(edit) | RecordChange::PromptUpdate(edit) => edit,
                    _ => unreachable!(),
                };
                if creating && edit.claim {
                    return Err(Error::new(
                        "prompt cannot be claimed during creation".into(),
                    ));
                }
                let mut doc = if existing.is_some() {
                    PromptDocument::parse_bytes(
                        &path.to_string_lossy(),
                        &std::fs::read(hub.join(&path))
                            .map_err(|error| Error::new(error.to_string()))?,
                    )?
                } else {
                    let title = edit
                        .title
                        .clone()
                        .ok_or_else(|| Error::new("prompt title is required".into()))?;
                    let from = edit
                        .from
                        .as_deref()
                        .ok_or_else(|| Error::new("prompt sender is required".into()))
                        .and_then(prompt::normalize_identity)?;
                    let to = edit
                        .to
                        .as_deref()
                        .ok_or_else(|| Error::new("prompt recipient is required".into()))
                        .and_then(prompt::normalize_identity)?;
                    let mut document = PromptDocument::new(PromptMetadata {
                        id: self.id.clone(),
                        title,
                        status: prompt::OPEN.into(),
                        priority: 2,
                        from,
                        to,
                        created: self.log.at.clone(),
                        updated: self.log.at.clone(),
                        ..Default::default()
                    });
                    let body = edit.body.clone().unwrap_or_else(|| {
                        crate::domain::template::load_prompt_template(&hub.join(&base), hub)
                    });
                    document.set_body_bytes(&body);
                    document
                };
                let old_metadata = doc.metadata.clone();
                let old_body = doc.body.clone();
                if let Some(title) = &edit.title {
                    if title.trim().is_empty() {
                        return Err(Error::new("prompt title must not be blank".into()));
                    }
                    doc.metadata.title = title.clone();
                }
                if let Some(priority) = edit.priority {
                    if !(0..=4).contains(&priority) {
                        return Err(Error::new("priority must be between 0 and 4".into()));
                    }
                    doc.metadata.priority = priority;
                }
                if let Some(from) = &edit.from {
                    doc.metadata.from = prompt::normalize_identity(from)?;
                }
                let actor = String::from_utf8_lossy(self.log.actor.as_bytes()).into_owned();
                let mut state = prompt::State::new(
                    &doc.metadata.status,
                    doc.metadata.claimed_by.as_deref(),
                    &doc.metadata.to,
                )?;
                if let Some(to) = &edit.to {
                    state = state.reassign(to, edit.force)?;
                }
                if edit.claim {
                    state = state.claim(&actor)?;
                }
                if let Some(status) = &edit.status
                    && !(edit.claim && status == prompt::IN_PROGRESS)
                {
                    state = state.transition(status, Some(&actor), edit.force)?;
                }
                doc.metadata.status = state.status;
                doc.metadata.claimed_by = state.claimed_by;
                doc.metadata.to = state.to;
                if let Some(body) = &edit.body {
                    doc.set_body_bytes(body);
                }
                doc.metadata
                    .labels
                    .retain(|label| !edit.unlabels.contains(label));
                for label in &edit.labels {
                    if !doc.metadata.labels.contains(label) {
                        doc.metadata.labels.push(label.clone());
                    }
                }
                if let Some(issue) = &edit.issue {
                    doc.metadata.issue = super::issues::canonical_link(&ix, issue)?;
                } else if edit.unlink {
                    doc.metadata.issue = Link::default();
                }
                let changed = creating || old_metadata != doc.metadata || old_body != doc.body;
                if !changed && edit.note.is_none() {
                    return Ok(vec![]);
                }
                let event = if let Some(note) = &edit.note {
                    note.clone()
                } else if creating {
                    "prompt created".into()
                } else if edit.claim {
                    "prompt claimed".into()
                } else if let Some(status) = &edit.status {
                    format!("prompt status: {status}")
                } else if let Some(issue) = &edit.issue {
                    let link = super::issues::canonical_link(&ix, issue)?;
                    format!("prompt linked: {}", link.target)
                } else if edit.unlink {
                    "prompt unlinked".into()
                } else {
                    "prompt updated".into()
                };
                self.log.event = event.into();
                doc.append_log(self.log.clone());
                doc.encode()?.bytes
            }
        };
        if let RecordChange::HandoffArchive(archive) = self.change {
            let archived = matches!(existing.map(|n|&n.data),Some(NoteData::Handoff(h)) if h.metadata.archived);
            if archived == archive {
                return Ok(vec![]);
            }
            let destination = if archive {
                base.join("handoffs/archive")
                    .join(
                        time::OffsetDateTime::from_unix_timestamp(self.log.at.seconds)
                            .unwrap()
                            .year()
                            .to_string(),
                    )
                    .join(path.file_name().unwrap())
            } else {
                base.join("handoffs").join(path.file_name().unwrap())
            };
            check_hub_write_path(hub, &destination)?;
            if hub.join(&destination).exists() {
                return Err(Error::new(
                    "archive/restore destination already exists".into(),
                ));
            }
            write_hub_file(hub, &destination, &bytes)?;
            remove_hub_file(hub, &path)?;
            self.present = true;
            return Ok(vec![destination, path]);
        }
        let mut paths = super::scaffold_paths(hub, &self.resolved)?;
        write_hub_file(hub, &path, &bytes)?;
        paths.push(path);
        self.present = true;
        Ok(paths)
    }
}
