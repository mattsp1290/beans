//! Request ownership differs from issue ownership and is validated on both
//! reading and writing. Only presentation primitives are shared.
use super::authored_yaml;
use super::frontmatter::{Error, Field, Frontmatter, Node};
use super::issue::{Timestamp, quoted};
use super::issue_encode::instant;
use super::log::{LogEntry, append_to_section, parse_section};
use super::request;
use super::text::{Link, RequestBody, split_request_body};
use serde::{Deserialize, Serialize};
use std::collections::HashSet;

pub(super) const OWNED: &[&str] = &[
    "id",
    "aliases",
    "title",
    "status",
    "priority",
    "labels",
    "requested_by",
    "issues",
    "created",
    "updated",
];
const REQUIRED: &[&str] = &[
    "id", "aliases", "title", "status", "priority", "created", "updated",
];

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct RequestMetadata {
    pub id: String,
    pub title: String,
    pub status: String,
    pub priority: i64,
    pub labels: Vec<String>,
    pub requested_by: String,
    pub issues: Vec<Link>,
    pub created: Timestamp,
    pub updated: Timestamp,
    pub aliases: Vec<String>,
    pub project: String,
}

#[derive(Clone, Debug)]
pub struct RequestDocument {
    pub metadata: RequestMetadata,
    pub body: String,
    pub log: Vec<LogEntry>,
    /// Used only for newly authored documents; parsed unknown bytes are retained.
    pub new_extra: authored_yaml::Node,
    pub(super) document: Option<Frontmatter>,
    pub(super) original_metadata: RequestMetadata,
    original_log_len: usize,
}

impl RequestDocument {
    pub fn new(metadata: RequestMetadata) -> Self {
        Self {
            original_metadata: metadata.clone(),
            metadata,
            body: String::new(),
            log: Vec::new(),
            new_extra: authored_yaml::Node::default(),
            document: None,
            original_log_len: 0,
        }
    }

    pub fn parse(path: &str, source: &str) -> Result<Self, Error> {
        // Go checks CRLF before request path validation.
        if source.contains("\r\n") {
            return Err(Error(format!(
                "{path}: has Windows line endings (\\r\\n); bn requires \\n"
            )));
        }
        let project = project_from_path(path)?;
        let document = Frontmatter::parse(path, source)?;
        let mut metadata = RequestMetadata {
            project,
            ..RequestMetadata::default()
        };
        let mut seen = HashSet::new();
        for field in document.fields() {
            let key = field.key.as_str();
            if !OWNED.contains(&key) {
                continue;
            }
            if !seen.insert(key) {
                return Err(Error(format!(
                    "{path}: line {}: duplicate frontmatter key {}",
                    document.diagnostic_line(field.key_line),
                    quoted(key)
                )));
            }
            metadata.read(key, &field.value).map_err(|error| {
                Error(format!(
                    "{path}: line {}: {error}",
                    document.diagnostic_line(field.value.line)
                ))
            })?;
        }
        for key in REQUIRED {
            if !seen.contains(key) {
                return Err(Error(format!(
                    "{path}: frontmatter is missing required key {}",
                    quoted(key)
                )));
            }
        }
        metadata
            .validate()
            .map_err(|error| Error(format!("{path}: {error}")))?;
        let sections = split_request_body(document.body());
        let body = sections.before_log.to_owned();
        let log = parse_section(sections.log);
        let original_log_len = log.len();
        Ok(Self {
            original_metadata: metadata.clone(),
            metadata,
            body,
            log,
            new_extra: authored_yaml::Node::default(),
            document: Some(document),
            original_log_len,
        })
    }

    pub fn unknown_fields(&self) -> impl Iterator<Item = &Field> {
        self.document
            .iter()
            .flat_map(|document| document.fields())
            .filter(|field| !OWNED.contains(&field.key.as_str()))
    }

    /// Replace all Markdown preceding the Log, retaining one final newline.
    pub fn set_body(&mut self, text: &str) {
        let text = text.trim_end_matches('\n');
        self.body = if text.is_empty() {
            String::new()
        } else {
            format!("{text}\n")
        };
    }

    pub fn append_log(&mut self, entry: LogEntry) {
        if instant(&entry.at) > instant(&self.metadata.updated) {
            self.metadata.updated = Timestamp {
                offset_seconds: 0,
                ..entry.at.clone()
            };
        }
        self.log.push(entry);
    }

    pub fn render_body(&self) -> Result<String, Error> {
        let original = self.original_body();
        let mut output = self.body.clone();
        let entries = self.log.get(self.original_log_len..).unwrap_or_default();
        if !original.log.is_empty() {
            output.push_str(&append_to_section(original.log, entries)?);
        } else if !entries.is_empty() {
            if !output.is_empty() && !output.ends_with("---\n") {
                if !output.ends_with('\n') {
                    output.push_str("\n\n");
                } else if !output.ends_with("\n\n") {
                    output.push('\n');
                }
            }
            output.push_str("## Log\n");
            for entry in entries {
                output.push_str(&entry.line()?);
            }
        }
        output.push_str(original.tail);
        Ok(output)
    }

    pub fn original(&self) -> &str {
        self.document.as_ref().map_or("", Frontmatter::original)
    }
    pub fn frontmatter(&self) -> Option<&Frontmatter> {
        self.document.as_ref()
    }
    pub fn original_body(&self) -> RequestBody<'_> {
        split_request_body(self.document.as_ref().map_or("", Frontmatter::body))
    }
}

impl RequestMetadata {
    pub fn validate(&self) -> Result<(), Error> {
        if !request::valid_id(&self.id) {
            return Err(Error(format!("invalid request id {}", quoted(&self.id))));
        }
        if self.title.trim().is_empty() {
            return Err(Error("title must not be blank".into()));
        }
        if !request::valid_status(&self.status) {
            return Err(Error(format!(
                "invalid request status {}",
                quoted(&self.status)
            )));
        }
        if !(0..=4).contains(&self.priority) {
            return Err(Error("priority must be between 0 and 4".into()));
        }
        let zero = instant(&Timestamp::default());
        if instant(&self.created) == zero || instant(&self.updated) == zero {
            return Err(Error(
                "created and updated must be RFC3339 timestamps".into(),
            ));
        }
        if !self.aliases.contains(&self.id) {
            return Err(Error(format!(
                "aliases must contain request id {}",
                quoted(&self.id)
            )));
        }
        Ok(())
    }

    fn read(&mut self, key: &str, node: &Node) -> Result<(), Error> {
        match key {
            "id" => self.id = node.scalar(key)?.to_owned(),
            "title" => self.title = node.scalar(key)?.to_owned(),
            "status" => self.status = node.scalar(key)?.to_owned(),
            "requested_by" => self.requested_by = node.scalar(key)?.to_owned(),
            "aliases" => {
                self.aliases = node
                    .string_list(key)?
                    .into_iter()
                    .map(str::to_owned)
                    .collect()
            }
            "labels" => {
                self.labels = node
                    .string_list(key)?
                    .into_iter()
                    .map(str::to_owned)
                    .collect()
            }
            "issues" => {
                self.issues = node
                    .string_list(key)?
                    .into_iter()
                    .filter(|s| !s.trim().is_empty())
                    .map(Link::parse)
                    .collect()
            }
            "priority" => {
                let raw = node.scalar(key)?;
                self.priority = raw.trim().parse().map_err(|_| {
                    Error(format!("priority must be an integer, got {}", quoted(raw)))
                })?;
            }
            "created" => self.created = Timestamp::read(key, node)?,
            "updated" => self.updated = Timestamp::read(key, node)?,
            _ => unreachable!("request owned key"),
        }
        Ok(())
    }
}

fn project_from_path(path: &str) -> Result<String, Error> {
    // Linux filepath.Clean is lexical: do not resolve symlinks or filesystem state.
    let absolute = path.starts_with('/');
    let mut parts = Vec::new();
    for part in path.split('/') {
        match part {
            "" | "." => {}
            ".." if parts.last().is_some_and(|&p| p != "..") => {
                parts.pop();
            }
            ".." if absolute => {}
            _ => parts.push(part),
        }
    }
    if parts.len() >= 4 {
        let tail = &parts[parts.len() - 4..];
        if tail[0] == "projects"
            && !tail[1].is_empty()
            && tail[2] == "requests"
            && tail[3]
                .strip_suffix(".md")
                .is_some_and(|stem| !stem.is_empty())
        {
            return Ok(tail[1].to_owned());
        }
    }
    Err(Error(format!(
        "{path}: request path must be projects/<project>/requests/<id>-<slug>.md"
    )))
}
