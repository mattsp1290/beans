//! Native issue operations shared by command and future HTTP adapters.
use crate::{
    domain::{
        frontmatter::Error,
        id,
        issue::{IssueDocument, IssueMetadata, Timestamp},
        log::LogEntry,
        template,
        yaml_string::YamlString,
    },
    gitops::{Operation, check_hub_write_path, write_hub_file},
    vault::{Index, LoadOptions, Resolved, create_project_files},
};
use std::{
    ffi::OsString,
    os::unix::ffi::OsStringExt,
    path::{Path, PathBuf},
};

#[derive(Clone, Debug)]
pub enum IssueChange {
    Create {
        title: String,
        kind: String,
        priority: i64,
        description: Option<String>,
    },
    Update {
        claim: bool,
        title: Option<String>,
        status: Option<String>,
        assignee: Option<String>,
        priority: Option<i64>,
    },
    Note(String),
    Close(String),
    Reopen,
    Dependency {
        target: String,
        kind: String,
        remove: bool,
    },
}
pub struct IssueMutation {
    pub resolved: Resolved,
    pub reference: String,
    pub change: IssueChange,
    pub actor: String,
    pub explicit_workflow: Option<PathBuf>,
    pub id: String,
    pub fields: IssueFields,
    log: LogEntry,
    log_applied: bool,
    created_once: bool,
}
impl IssueMutation {
    pub fn new(
        resolved: Resolved,
        reference: String,
        change: IssueChange,
        actor: String,
        explicit_workflow: Option<PathBuf>,
    ) -> Self {
        Self::new_at(
            resolved,
            reference,
            change,
            actor,
            explicit_workflow,
            time::OffsetDateTime::now_utc(),
        )
    }
    /// Freeze an adapter-provided invocation time, independent of retry timing.
    pub fn new_at(
        resolved: Resolved,
        reference: String,
        change: IssueChange,
        actor: String,
        explicit_workflow: Option<PathBuf>,
        at: time::OffsetDateTime,
    ) -> Self {
        let event = match &change {
            IssueChange::Create { .. } => "created".to_owned(),
            IssueChange::Update { claim: true, .. } => "claimed".to_owned(),
            IssueChange::Update { .. } => "updated".to_owned(),
            IssueChange::Note(s) => s.clone(),
            IssueChange::Close(s) => format!("closed: {s}"),
            IssueChange::Reopen => "reopened".into(),
            IssueChange::Dependency { .. } => "dependency updated".into(),
        };
        // Freeze timestamp/context before Git effects. The transaction boundary
        // distinguishes identical invocations through current-run nonce survival.
        let log = LogEntry {
            at: Timestamp {
                seconds: at.unix_timestamp(),
                nanoseconds: 0,
                offset_seconds: 0,
            },
            actor: actor.clone().into(),
            repo: YamlString::from_bytes(resolved.project.clone()),
            sha: YamlString::from_bytes(resolved.repo_head.clone()),
            branch: YamlString::from_bytes(resolved.repo_branch.clone()),
            event: event.into(),
            ..LogEntry::default()
        };
        Self {
            resolved,
            reference,
            change,
            actor,
            explicit_workflow,
            id: String::new(),
            fields: IssueFields::default(),
            log,
            log_applied: false,
            created_once: false,
        }
    }
}
impl Operation for IssueMutation {
    fn after_rebase(&mut self, operation_present: bool) {
        self.log_applied = operation_present;
    }
    fn subject(&self) -> String {
        format!(
            "bn: {} {}",
            match self.change {
                IssueChange::Create { .. } => "create",
                IssueChange::Update { .. } => "update",
                IssueChange::Note(_) => "note",
                IssueChange::Close(_) => "close",
                IssueChange::Reopen => "reopen",
                IssueChange::Dependency { .. } => "dep",
            },
            if self.id.is_empty() {
                &self.reference
            } else {
                &self.id
            }
        )
    }
    fn apply(&mut self, hub: &Path) -> Result<Vec<PathBuf>, Error> {
        // Strict config loading rejects malformed overrides before writing.
        check_hub_write_path(
            hub,
            &PathBuf::from("projects")
                .join(String::from_utf8_lossy(&self.resolved.project).as_ref())
                .join("beans.toml"),
        )?;
        let index = Index::load_snapshot(
            hub,
            LoadOptions {
                explicit_workflow: self.explicit_workflow.clone(),
            },
        )?;
        let workflow = index.workflow_for(&self.resolved.project);
        let (mut document, relative) = match &self.change {
            IssueChange::Create {
                title,
                kind,
                priority,
                description,
            } => {
                if !(0..=4).contains(priority) {
                    return Err(Error::new("priority must be between 0 and 4".into()));
                }
                if title.trim().is_empty() {
                    return Err(Error::new("title must not be empty".into()));
                }
                if !index.hub_config.types.valid_type(kind.as_bytes()) {
                    return Err(Error::new(format!("invalid issue type {kind}")));
                }
                if self.id.is_empty() {
                    let prefix = super::prefix_for(hub, &self.resolved.project);
                    let mut exists = |id: &str| {
                        index.lookup(id.as_bytes()).is_some()
                            || index
                                .ordered_notes()
                                .iter()
                                .any(|note| note.graph.id.as_deref() == Some(id.as_bytes()))
                    };
                    self.id = id::new_id(
                        &String::from_utf8_lossy(&prefix),
                        Some(&mut exists),
                        index.hub_config.ids.length as isize,
                    );
                }
                if !id::valid_id(&self.id) {
                    return Err(Error::new("invalid issue prefix or generated ID".into()));
                }
                if let Some(existing) = index.issue_by_id(self.id.as_bytes()) {
                    if self.created_once
                        && self.log_applied
                        && existing
                            .log
                            .iter()
                            .any(|entry| entry.format_bytes().ok() == self.log.format_bytes().ok())
                    {
                        return Ok(Vec::new());
                    }
                    return Err(Error::new(format!("issue ID collision: {}", self.id)));
                }
                if index.lookup(self.id.as_bytes()).is_some()
                    || index
                        .ordered_notes()
                        .iter()
                        .any(|note| note.graph.id.as_deref() == Some(self.id.as_bytes()))
                {
                    return Err(Error::new(format!(
                        "issue ID collides with an existing hub record: {}",
                        self.id
                    )));
                }
                let metadata = IssueMetadata {
                    id: self.id.clone(),
                    title: title.clone(),
                    kind: kind.clone(),
                    status: workflow.default_state().to_string(),
                    priority: *priority,
                    created: self.log.at.clone(),
                    updated: self.log.at.clone(),
                    ..IssueMetadata::default()
                };
                let mut document = IssueDocument::new(metadata);
                let project = hub
                    .join("projects")
                    .join(String::from_utf8_lossy(&self.resolved.project).as_ref());
                document.body =
                    YamlString::from_bytes(template::load_template(kind, &project, hub));
                if let Some(description) = description {
                    document.set_description(description);
                }
                let relative = PathBuf::from("projects")
                    .join(String::from_utf8_lossy(&self.resolved.project).as_ref())
                    .join("issues")
                    .join(id::filename(&self.id, &id::slug(title)));
                (document, relative)
            }
            _ => {
                let (_, note) = index.resolve_issue_ref(self.reference.as_bytes());
                let note = note.ok_or_else(|| {
                    Error::new(format!(
                        "issue not found: {} (check malformed hub documents)",
                        self.reference
                    ))
                })?;
                if note.project != self.resolved.project {
                    return Err(Error::new("issue belongs to a different project".into()));
                }
                let relative = PathBuf::from(OsString::from_vec(note.graph.path.clone()));
                let bytes =
                    std::fs::read(hub.join(&relative)).map_err(|e| Error::new(e.to_string()))?;
                let document = IssueDocument::parse_bytes(&relative.to_string_lossy(), &bytes)?;
                self.id = document.metadata.id.clone();
                (document, relative)
            }
        };
        let workflow = index.workflow_for(&self.resolved.project);
        let original_metadata = document.metadata.clone();
        let original_description = document.description.clone();
        if matches!(self.change, IssueChange::Close(_))
            && workflow.is_terminal(document.metadata.status.as_bytes())
        {
            return Ok(Vec::new());
        }
        if matches!(self.change, IssueChange::Reopen)
            && !workflow.is_terminal(document.metadata.status.as_bytes())
            && !document.metadata.archived
        {
            return Ok(vec![]);
        }
        if let IssueChange::Update { claim, status, .. } = &self.change
            && workflow.is_terminal(document.metadata.status.as_bytes())
            && !self.fields.force
            && (*claim
                || status
                    .as_ref()
                    .is_some_and(|s| *s != document.metadata.status))
        {
            return Err(Error::new(
                "terminal issue status requires bn reopen or --force".into(),
            ));
        }
        match &self.change {
            IssueChange::Update {
                claim,
                title,
                status,
                assignee,
                priority,
            } => {
                if *claim {
                    if !workflow.is_valid(b"in_progress") {
                        return Err(Error::new("workflow has no in_progress state".into()));
                    }
                    document.metadata.status = "in_progress".into();
                    document.metadata.assignee = self.actor.clone();
                }
                if let Some(title) = title {
                    if title.trim().is_empty() {
                        return Err(Error::new("title must not be empty".into()));
                    }
                    document.metadata.title = title.clone();
                }
                if let Some(status) = status {
                    if !workflow.is_valid(status.as_bytes()) {
                        return Err(Error::new(format!("invalid status: {status}")));
                    }
                    document.metadata.status = status.clone();
                }
                if let Some(assignee) = assignee {
                    document.metadata.assignee = assignee.clone();
                }
                if let Some(priority) = priority {
                    if !(0..=4).contains(priority) {
                        return Err(Error::new("priority must be between 0 and 4".into()));
                    }
                    document.metadata.priority = *priority;
                }
            }
            IssueChange::Reopen => document.metadata.status = workflow.default_state().to_string(),
            IssueChange::Dependency {
                target,
                kind,
                remove,
            } => {
                let link = canonical_link(&index, target)?;
                if kind == "parent" || kind == "parent-child" {
                    if *remove {
                        if document.metadata.parent.target == link.target {
                            document.metadata.parent = Default::default();
                        }
                    } else {
                        document.metadata.parent = link;
                    }
                } else if kind == "blocks" {
                    if *remove {
                        document
                            .metadata
                            .blocked_by
                            .retain(|l| l.target != link.target);
                    } else if !document
                        .metadata
                        .blocked_by
                        .iter()
                        .any(|l| l.target == link.target)
                    {
                        document.metadata.blocked_by.push(link);
                    }
                } else {
                    return Err(Error::new(
                        "dependency type must be blocks or parent".into(),
                    ));
                }
            }
            IssueChange::Close(_) => {
                document.metadata.status = workflow
                    .terminal
                    .as_ref()
                    .and_then(|s| s.first())
                    .ok_or_else(|| Error::new("workflow has no terminal state".into()))?
                    .to_string();
            }
            _ => (),
        }
        self.fields.apply(&mut document, &index)?;
        if self.fields.parent.is_some()
            || !self.fields.blocked_by.is_empty()
            || matches!(self.change, IssueChange::Dependency { remove: false, .. })
        {
            validate_relationships(&document, &index)?;
        }
        if matches!(self.change, IssueChange::Update { .. })
            && document.metadata == original_metadata
            && document.description == original_description
            && self.fields.note.is_none()
        {
            return Ok(Vec::new());
        }
        let frozen = self.log.format_bytes()?;
        let matching = document
            .log
            .iter()
            .any(|entry| entry.format_bytes().ok().as_ref() == Some(&frozen));
        if !self.log_applied || !matching {
            document.append_log(self.log.clone());
            if let Some(text) = &self.fields.note {
                let mut entry = self.log.clone();
                entry.event = text.clone().into();
                document.append_log(entry);
            }
        }
        self.log_applied = true;
        check_hub_write_path(hub, &relative)?;
        let bytes = document.encode()?.bytes;
        let mut paths =
            create_project_files(hub, &self.resolved.project, &self.resolved.repo_remote)?
                .unwrap_or_default()
                .into_iter()
                .map(|p| PathBuf::from(OsString::from_vec(p)))
                .collect::<Vec<_>>();
        if matches!(self.change, IssueChange::Reopen) && document.metadata.archived {
            let destination = PathBuf::from("projects")
                .join(String::from_utf8_lossy(&self.resolved.project).as_ref())
                .join("issues")
                .join(relative.file_name().unwrap());
            check_hub_write_path(hub, &destination)?;
            if hub.join(&destination).exists() {
                return Err(Error::new("reopen destination already exists".into()));
            }
            write_hub_file(hub, &destination, &bytes)?;
            crate::gitops::remove_hub_file(hub, &relative)?;
            paths.push(destination);
        } else {
            write_hub_file(hub, &relative, &bytes)?;
        }
        self.created_once = true;
        paths.push(relative);
        Ok(paths)
    }
}

#[derive(Clone, Debug, Default)]
pub struct IssueFields {
    pub force: bool,
    pub note: Option<String>,
    pub kind: Option<String>,
    pub description: Option<String>,
    pub assignee: Option<String>,
    pub parent: Option<String>,
    pub blocked_by: Vec<String>,
    pub labels: Vec<String>,
    pub unlabels: Vec<String>,
    pub url: Option<String>,
}
impl IssueFields {
    fn apply(&self, doc: &mut IssueDocument, index: &Index) -> Result<(), Error> {
        if let Some(kind) = &self.kind {
            if !index.hub_config.types.valid_type(kind.as_bytes()) {
                return Err(Error::new(format!("invalid issue type {kind}")));
            }
            doc.metadata.kind = kind.clone();
        }
        if let Some(text) = &self.description {
            doc.set_description(text);
        }
        if let Some(value) = &self.assignee {
            doc.metadata.assignee = value.clone();
        }
        if let Some(value) = &self.url {
            doc.metadata.url = value.clone();
        }
        if let Some(value) = &self.parent {
            doc.metadata.parent = if value.is_empty() {
                Default::default()
            } else {
                canonical_link(index, value)?
            };
        }
        for value in &self.blocked_by {
            let link = canonical_link(index, value)?;
            if !doc
                .metadata
                .blocked_by
                .iter()
                .any(|l| l.target == link.target)
            {
                doc.metadata.blocked_by.push(link);
            }
        }
        doc.metadata.labels.retain(|v| !self.unlabels.contains(v));
        for value in &self.labels {
            if !doc.metadata.labels.contains(value) {
                doc.metadata.labels.push(value.clone());
            }
        }
        Ok(())
    }
}
pub fn canonical_link(index: &Index, value: &str) -> Result<crate::domain::text::Link, Error> {
    let (_, note) = index.resolve_issue_ref(value.as_bytes());
    let note = note.ok_or_else(|| Error::new(format!("issue not found: {value}")))?;
    Ok(crate::domain::text::Link::parse(&format!(
        "[[{}]]",
        String::from_utf8_lossy(note.graph.id.as_ref().unwrap())
    )))
}
fn validate_relationships(doc: &IssueDocument, index: &Index) -> Result<(), Error> {
    let id = &doc.metadata.id;
    let mut pending: Vec<_> = doc
        .metadata
        .blocked_by
        .iter()
        .map(|l| l.target.clone())
        .collect();
    let mut seen = std::collections::HashSet::new();
    while let Some(next) = pending.pop() {
        let next = index
            .resolve_issue_ref(next.as_bytes())
            .1
            .and_then(|n| n.graph.id.as_ref())
            .map(|v| String::from_utf8_lossy(v).into_owned())
            .unwrap_or(next);
        if next == *id {
            return Err(Error::new("dependency cycle rejected".into()));
        }
        if seen.insert(next.clone())
            && let Some(note) = index.resolve_issue_ref(next.as_bytes()).1
            && let crate::vault::NoteData::Issue(issue) = &note.data
        {
            pending.extend(issue.metadata.blocked_by.iter().map(|l| l.target.clone()));
        }
    }
    let mut next = doc.metadata.parent.target.clone();
    seen.clear();
    while !next.is_empty() {
        next = index
            .resolve_issue_ref(next.as_bytes())
            .1
            .and_then(|n| n.graph.id.as_ref())
            .map(|v| String::from_utf8_lossy(v).into_owned())
            .unwrap_or(next);
        if next == *id || !seen.insert(next.clone()) {
            return Err(Error::new("parent cycle rejected".into()));
        }
        next = index
            .resolve_issue_ref(next.as_bytes())
            .1
            .and_then(|n| {
                if let crate::vault::NoteData::Issue(d) = &n.data {
                    Some(d.metadata.parent.target.clone())
                } else {
                    None
                }
            })
            .unwrap_or_default();
    }
    Ok(())
}
