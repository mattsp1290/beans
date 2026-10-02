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
    gitops::{Operation, write_file},
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
}
pub struct IssueMutation {
    pub resolved: Resolved,
    pub reference: String,
    pub change: IssueChange,
    pub actor: String,
    pub explicit_workflow: Option<PathBuf>,
    pub id: String,
    log: LogEntry,
    baseline_matches: Option<usize>,
}
impl IssueMutation {
    pub fn new(
        resolved: Resolved,
        reference: String,
        change: IssueChange,
        actor: String,
        explicit_workflow: Option<PathBuf>,
    ) -> Self {
        let at = time::OffsetDateTime::now_utc();
        let event = match &change {
            IssueChange::Create { .. } => "created".to_owned(),
            IssueChange::Update { claim: true, .. } => "claimed".to_owned(),
            IssueChange::Update { .. } => "updated".to_owned(),
            IssueChange::Note(s) => s.clone(),
            IssueChange::Close(s) => format!("closed: {s}"),
        };
        // Freeze timestamp/context before any Git effects; compare formatted bytes
        // because stored log timestamps intentionally use whole seconds.
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
            log,
            baseline_matches: None,
        }
    }
}
impl Operation for IssueMutation {
    fn subject(&self) -> String {
        format!(
            "bn: {} {}",
            match self.change {
                IssueChange::Create { .. } => "create",
                IssueChange::Update { .. } => "update",
                IssueChange::Note(_) => "note",
                IssueChange::Close(_) => "close",
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
        let index = Index::load_with_options(
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
                        index
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
                    if self.baseline_matches.is_some()
                        && existing
                            .log
                            .iter()
                            .any(|entry| entry.format_bytes().ok() == self.log.format_bytes().ok())
                    {
                        return Ok(Vec::new());
                    }
                    return Err(Error::new(format!("issue ID collision: {}", self.id)));
                }
                if index
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
        if matches!(self.change, IssueChange::Close(_))
            && workflow.is_terminal(document.metadata.status.as_bytes())
        {
            return Ok(Vec::new());
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
            IssueChange::Close(_) => {
                document.metadata.status = if workflow.is_terminal(b"closed") {
                    "closed".into()
                } else {
                    workflow
                        .terminal
                        .as_ref()
                        .and_then(|s| s.first())
                        .ok_or_else(|| Error::new("workflow has no terminal state".into()))?
                        .to_string()
                };
            }
            _ => (),
        }
        if matches!(self.change, IssueChange::Update { .. })
            && document.metadata == original_metadata
        {
            return Ok(Vec::new());
        }
        let frozen = self.log.format_bytes()?;
        let matching = document
            .log
            .iter()
            .filter(|entry| entry.format_bytes().ok().as_ref() == Some(&frozen))
            .count();
        let baseline = *self.baseline_matches.get_or_insert(matching);
        if matching <= baseline {
            document.append_log(self.log.clone());
        }
        let bytes = document.encode()?.bytes;
        let mut paths =
            create_project_files(hub, &self.resolved.project, &self.resolved.repo_remote)?
                .unwrap_or_default()
                .into_iter()
                .map(|p| PathBuf::from(OsString::from_vec(p)))
                .collect::<Vec<_>>();
        write_file(&hub.join(&relative), &bytes)?;
        paths.push(relative);
        Ok(paths)
    }
}
