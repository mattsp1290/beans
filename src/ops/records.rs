//! Request and continuation operations retain authored documents and reread on replay.
use crate::{
    domain::{
        frontmatter::Error,
        handoff::{HandoffDocument, HandoffMetadata},
        issue::Timestamp,
        log::LogEntry,
        request::{self, RequestDocument, RequestMetadata},
        text::Link,
    },
    gitops::{Operation, check_hub_write_path, remove_hub_file, write_hub_file},
    vault::{Index, LoadOptions, NoteData, NoteKind, Resolved},
};
use std::path::{Path, PathBuf};
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
        let request_family = matches!(
            self.change,
            RecordChange::RequestCreate(_) | RecordChange::RequestUpdate(_)
        );
        let kind = if request_family {
            NoteKind::Request
        } else {
            NoteKind::Handoff
        };
        let creating = matches!(
            self.change,
            RecordChange::RequestCreate(_) | RecordChange::HandoffCreate { .. }
        );
        if creating && self.id.is_empty() {
            let prefix = String::from_utf8_lossy(&super::prefix_for(hub, &self.resolved.project))
                .into_owned();
            let mut exists = |id: &str| {
                ix.lookup(id.as_bytes()).is_some()
                    || ix
                        .ordered_notes()
                        .iter()
                        .any(|n| n.graph.id.as_deref() == Some(id.as_bytes()))
            };
            self.id = crate::domain::id::new_id(
                &format!("{prefix}-{}", if request_family { "r" } else { "h" }),
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
        let path = existing
            .map(|n| PathBuf::from(String::from_utf8_lossy(&n.graph.path).as_ref()))
            .unwrap_or_else(|| {
                base.join(if request_family {
                    "requests"
                } else {
                    "handoffs"
                })
                .join(format!("{}.md", self.id))
            });
        check_hub_write_path(hub, &path)?;
        self.id = existing
            .and_then(|n| n.graph.id.as_ref())
            .map(|v| String::from_utf8_lossy(v).into_owned())
            .unwrap_or_else(|| self.id.clone());
        let bytes = if request_family {
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
        } else {
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
