//! Lossless prompt documents. Parsed presentation and all unowned bytes are
//! retained; only changed prompt-owned fields and explicitly changed body/log
//! content are rendered.
use super::authored_yaml;
use super::frontmatter::{EditResult, Error, Field, Frontmatter, Node, NodeKind};
use super::issue::{Timestamp, quoted};
use super::issue_encode::{comment_suffix, instant, raw_link, timestamp_pair};
use super::log::{LogEntry, parse_section_bytes};
use super::prompt;
use super::text::{Link, RequestBody, split_request_body};
use super::yaml_render::{flow_pair, link_pair, string_pair};
use serde::{Deserialize, Serialize};
use std::collections::HashSet;

const OWNED: &[&str] = &[
    "id",
    "aliases",
    "title",
    "status",
    "priority",
    "labels",
    "from",
    "to",
    "claimed_by",
    "issue",
    "created",
    "updated",
];
const REQUIRED: &[&str] = &[
    "id", "title", "status", "priority", "from", "to", "created", "updated",
];

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct PromptMetadata {
    pub id: String,
    pub aliases: Vec<String>,
    pub title: String,
    pub status: String,
    pub priority: i64,
    pub labels: Vec<String>,
    pub from: String,
    pub to: String,
    pub claimed_by: Option<String>,
    pub issue: Link,
    pub created: Timestamp,
    pub updated: Timestamp,
    pub project: String,
}

#[derive(Clone, Debug)]
pub struct PromptDocument {
    pub metadata: PromptMetadata,
    pub body: super::yaml_string::YamlString,
    pub log: Vec<LogEntry>,
    /// Unknown fields to append when encoding a newly authored prompt.
    pub new_extra: authored_yaml::Node,
    document: Option<Frontmatter>,
    original_metadata: PromptMetadata,
    original_log_len: usize,
}

impl PromptDocument {
    pub fn new(metadata: PromptMetadata) -> Self {
        Self {
            original_metadata: metadata.clone(),
            metadata,
            body: Default::default(),
            log: Vec::new(),
            new_extra: authored_yaml::Node::default(),
            document: None,
            original_log_len: 0,
        }
    }

    pub fn parse(path: &str, source: &str) -> Result<Self, Error> {
        Self::parse_bytes(path, source.as_bytes())
    }

    pub fn parse_bytes(path: &str, source: &[u8]) -> Result<Self, Error> {
        if source.windows(2).any(|window| window == b"\r\n") {
            return Err(Error::new(format!(
                "{path}: has Windows line endings (\\r\\n); bn requires \\n"
            )));
        }
        let project = project_from_path(path)?;
        let document = Frontmatter::parse_bytes(path, source)?;
        let mut metadata = PromptMetadata {
            project,
            ..PromptMetadata::default()
        };
        let mut seen = HashSet::new();
        for field in document.fields() {
            let key = field.key.as_str();
            if !OWNED.contains(&key) {
                continue;
            }
            if !seen.insert(key) {
                return Err(Error::new(format!(
                    "{path}: line {}: duplicate frontmatter key {}",
                    document.diagnostic_line(field.key_line),
                    quoted(key)
                )));
            }
            metadata.read(key, &field.value).map_err(|error| {
                Error::new(format!(
                    "{path}: line {}: {error}",
                    document.diagnostic_line(field.value.line)
                ))
            })?;
        }
        for key in REQUIRED {
            if !seen.contains(key) {
                return Err(Error::new(format!(
                    "{path}: frontmatter is missing required key {}",
                    quoted(key)
                )));
            }
        }
        metadata
            .validate()
            .map_err(|error| Error::new(format!("{path}: {error}")))?;
        let sections = split_request_body(document.body());
        let body = super::yaml_string::YamlString::from_bytes(
            document.slice_bytes(sections.before_log).into(),
        );
        let log = parse_section_bytes(document.slice_bytes(sections.log));
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

    /// Replace all instruction Markdown preceding the owned Log section without
    /// normalizing template or explicitly supplied body bytes.
    pub fn set_body(&mut self, text: &str) {
        self.set_body_bytes(text.as_bytes());
    }

    pub fn set_body_bytes(&mut self, text: &[u8]) {
        self.body = super::yaml_string::YamlString::from_bytes(text.to_vec());
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
        String::from_utf8(self.render_body_bytes()?)
            .map_err(|_| Error::new("body is not UTF-8; use render_body_bytes".into()))
    }

    pub fn render_body_bytes(&self) -> Result<Vec<u8>, Error> {
        let original = self.original_body();
        let mut output = self.body.as_bytes().to_vec();
        let entries = self.log.get(self.original_log_len..).unwrap_or_default();
        if !original.log.is_empty() {
            let raw = self.document.as_ref().unwrap().slice_bytes(original.log);
            output.extend_from_slice(&super::log::append_to_section_bytes(raw, entries)?);
        } else if !entries.is_empty() {
            if !output.is_empty() {
                if !output.ends_with(b"\n") {
                    output.extend_from_slice(b"\n\n");
                } else if !output.ends_with(b"\n\n") {
                    output.push(b'\n');
                }
            }
            output.extend_from_slice(b"## Log\n");
            for entry in entries {
                output.extend_from_slice(&entry.line_bytes()?);
            }
        }
        if let Some(document) = &self.document {
            output.extend_from_slice(document.slice_bytes(original.tail));
        }
        Ok(output)
    }

    pub fn original_bytes(&self) -> &[u8] {
        self.document
            .as_ref()
            .map_or(&[], Frontmatter::original_bytes)
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

    pub fn encode(&mut self) -> Result<EditResult, Error> {
        self.metadata.normalize_identities()?;
        if self.document.is_none()
            && !self.metadata.id.is_empty()
            && !self.metadata.aliases.contains(&self.metadata.id)
        {
            self.metadata.aliases.insert(0, self.metadata.id.clone());
        }
        self.metadata.validate()?;
        let Some(document) = &self.document else {
            let mut output = String::from("---\n");
            for &key in OWNED {
                if let Some(field) = self.metadata.render(key, "")? {
                    output.push_str(&field);
                }
            }
            output.push_str(&super::extra_encode::mapping_fields(&self.new_extra)?);
            output.push_str("---\n");
            let mut output = output.into_bytes();
            output.extend_from_slice(&self.render_body_bytes()?);
            return Ok(EditResult {
                bytes: output,
                copies: Vec::new(),
            });
        };
        let mut rendered = Vec::new();
        for &key in OWNED {
            if self.metadata.changed(&self.original_metadata, key) {
                let comment = document
                    .fields()
                    .iter()
                    .find(|field| field.key == key)
                    .filter(|field| field.value.kind == NodeKind::Scalar)
                    .map_or(String::new(), |field| document.scalar_comment(field));
                rendered.push((key, self.metadata.render(key, &comment)?));
            }
        }
        let changes: Vec<_> = rendered
            .iter()
            .map(|(key, value)| (*key, value.as_ref().map(|value| value.as_bytes())))
            .collect();
        document.splice_owned(OWNED, &changes, &self.render_body_bytes()?)
    }
}

impl PromptMetadata {
    pub fn validate(&self) -> Result<(), Error> {
        if !prompt::valid_id(&self.id) {
            return Err(Error::new(format!(
                "invalid prompt id {}",
                quoted(&self.id)
            )));
        }
        if self.title.trim().is_empty() {
            return Err(Error::new("title must not be blank".into()));
        }
        if !(0..=4).contains(&self.priority) {
            return Err(Error::new("priority must be between 0 and 4".into()));
        }
        let mut labels = HashSet::new();
        if self.labels.iter().any(|label| !labels.insert(label)) {
            return Err(Error::new("labels must contain unique strings".into()));
        }
        prompt::normalize_identity(&self.from)?;
        prompt::normalize_identity(&self.to)?;
        prompt::validate_state(&self.status, self.claimed_by.as_deref())?;
        let zero = instant(&Timestamp::default());
        if instant(&self.created) == zero || instant(&self.updated) == zero {
            return Err(Error::new(
                "created and updated must be RFC3339 timestamps".into(),
            ));
        }
        Ok(())
    }

    fn normalize_identities(&mut self) -> Result<(), Error> {
        self.from = prompt::normalize_identity(&self.from)?;
        self.to = prompt::normalize_identity(&self.to)?;
        self.claimed_by = self
            .claimed_by
            .as_deref()
            .map(prompt::normalize_identity)
            .transpose()?;
        Ok(())
    }

    fn read(&mut self, key: &str, node: &Node) -> Result<(), Error> {
        match key {
            "id" => self.id = node.scalar(key)?.to_owned(),
            "aliases" => self.aliases = strings(node, key)?,
            "title" => self.title = node.scalar(key)?.to_owned(),
            "status" => self.status = node.scalar(key)?.to_owned(),
            "priority" => {
                let raw = node.scalar(key)?;
                self.priority = raw.trim().parse().map_err(|_| {
                    Error::new(format!("priority must be an integer, got {}", quoted(raw)))
                })?;
            }
            "labels" => self.labels = strings(node, key)?,
            "from" => self.from = prompt::normalize_identity(node.scalar(key)?)?,
            "to" => self.to = prompt::normalize_identity(node.scalar(key)?)?,
            "claimed_by" => self.claimed_by = Some(prompt::normalize_identity(node.scalar(key)?)?),
            "issue" => self.issue = Link::parse(node.scalar(key)?),
            "created" => self.created = Timestamp::read(key, node)?,
            "updated" => self.updated = Timestamp::read(key, node)?,
            _ => unreachable!("prompt owned key"),
        }
        Ok(())
    }

    fn changed(&self, previous: &Self, key: &str) -> bool {
        match key {
            "id" => self.id != previous.id,
            "aliases" => self.aliases != previous.aliases,
            "title" => self.title != previous.title,
            "status" => self.status != previous.status,
            "priority" => self.priority != previous.priority,
            "labels" => self.labels != previous.labels,
            "from" => self.from != previous.from,
            "to" => self.to != previous.to,
            "claimed_by" => self.claimed_by != previous.claimed_by,
            "issue" => self.issue != previous.issue,
            "created" => instant(&self.created) != instant(&previous.created),
            "updated" => instant(&self.updated) != instant(&previous.updated),
            _ => unreachable!("prompt owned key"),
        }
    }

    fn render(&self, key: &str, comment: &str) -> Result<Option<String>, Error> {
        let output = match key {
            "id" => string_pair(key, &self.id, comment),
            "aliases" => flow_pair(key, &self.aliases),
            "title" => string_pair(key, &self.title, comment),
            "status" => string_pair(key, &self.status, comment),
            "priority" => format!("{key}: {}{}\n", self.priority, comment_suffix(comment)),
            "labels" => {
                if self.labels.is_empty() {
                    return Ok(None);
                }
                flow_pair(key, &self.labels)
            }
            "from" => string_pair(key, &self.from, comment),
            "to" => string_pair(key, &self.to, comment),
            "claimed_by" => {
                let Some(value) = &self.claimed_by else {
                    return Ok(None);
                };
                string_pair(key, value, comment)
            }
            "issue" => {
                if self.issue.is_zero() {
                    return Ok(None);
                }
                link_pair(key, &raw_link(&self.issue), comment)
            }
            "created" => timestamp_pair(key, &self.created, comment)?,
            "updated" => timestamp_pair(key, &self.updated, comment)?,
            _ => unreachable!("prompt owned key"),
        };
        Ok(Some(output))
    }
}

fn strings(node: &Node, key: &str) -> Result<Vec<String>, Error> {
    Ok(node
        .string_list(key)?
        .into_iter()
        .map(str::to_owned)
        .collect())
}

fn project_from_path(path: &str) -> Result<String, Error> {
    let absolute = path.starts_with('/');
    let mut parts = Vec::new();
    for part in path.split('/') {
        match part {
            "" | "." => {}
            ".." if parts.last().is_some_and(|part| *part != "..") => {
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
            && tail[2] == "prompts"
            && tail[3]
                .strip_suffix(".md")
                .is_some_and(|stem| !stem.is_empty())
        {
            return Ok(tail[1].to_owned());
        }
    }
    Err(Error::new(format!(
        "{path}: prompt path must be projects/<project>/prompts/<id>-<slug>.md"
    )))
}
