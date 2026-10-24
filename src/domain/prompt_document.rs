//! Lossless prompt documents. Parsed presentation and all unowned bytes are
//! retained; only changed prompt-owned fields and explicitly changed body/log
//! content are rendered.
use super::authored_yaml;
use super::frontmatter::{EditResult, Error, Field, Frontmatter, Node};
use super::issue::{Timestamp, quoted};
use super::issue_encode::{comment_suffix, instant, raw_link, timestamp_pair};
use super::log::LogEntry;
use super::lossless_record::{LosslessRecord, RecordMetadata};
use super::prompt;
use super::text::{Link, RequestBody};
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
    record: LosslessRecord,
    original_metadata: PromptMetadata,
}

impl PromptDocument {
    pub fn new(metadata: PromptMetadata) -> Self {
        Self {
            original_metadata: metadata.clone(),
            metadata,
            body: Default::default(),
            log: Vec::new(),
            new_extra: authored_yaml::Node::default(),
            record: LosslessRecord::new(),
        }
    }

    pub fn parse(path: &str, source: &str) -> Result<Self, Error> {
        Self::parse_bytes(path, source.as_bytes())
    }

    pub fn parse_bytes(path: &str, source: &[u8]) -> Result<Self, Error> {
        let parsed = LosslessRecord::parse::<PromptMetadata>(path, source)?;
        Ok(Self {
            original_metadata: parsed.metadata.clone(),
            metadata: parsed.metadata,
            body: parsed.body,
            log: parsed.log,
            new_extra: authored_yaml::Node::default(),
            record: parsed.record,
        })
    }

    pub fn unknown_fields(&self) -> impl Iterator<Item = &Field> {
        self.record.unknown_fields::<PromptMetadata>()
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
        LosslessRecord::append_log(&mut self.metadata.updated, &mut self.log, entry);
    }

    pub fn render_body(&self) -> Result<String, Error> {
        self.record
            .render_body::<PromptMetadata>(&self.body, &self.log)
    }

    pub fn render_body_bytes(&self) -> Result<Vec<u8>, Error> {
        self.record
            .render_body_bytes::<PromptMetadata>(&self.body, &self.log)
    }

    pub fn original_bytes(&self) -> &[u8] {
        self.record.original_bytes()
    }

    pub fn original(&self) -> &str {
        self.record.original()
    }

    pub fn frontmatter(&self) -> Option<&Frontmatter> {
        self.record.frontmatter()
    }

    pub fn original_body(&self) -> RequestBody<'_> {
        self.record.original_body()
    }

    pub fn encode(&mut self) -> Result<EditResult, Error> {
        self.metadata.normalize_identities()?;
        if self.record.frontmatter().is_none()
            && !self.metadata.id.is_empty()
            && !self.metadata.aliases.contains(&self.metadata.id)
        {
            self.metadata.aliases.insert(0, self.metadata.id.clone());
        }
        self.record.encode::<PromptMetadata>(
            &self.metadata,
            &self.original_metadata,
            &self.body,
            &self.log,
            &self.new_extra,
        )
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

impl RecordMetadata for PromptMetadata {
    const KIND: &'static str = "prompt";
    const DIRECTORY: &'static str = "prompts";
    const OWNED: &'static [&'static str] = OWNED;
    const REQUIRED: &'static [&'static str] = REQUIRED;

    fn set_project(&mut self, project: String) {
        self.project = project;
    }
    fn read(&mut self, key: &str, node: &Node) -> Result<(), Error> {
        self.read(key, node)
    }
    fn validate(&self) -> Result<(), Error> {
        PromptMetadata::validate(self)
    }
    fn changed(&self, previous: &Self, key: &str) -> bool {
        self.changed(previous, key)
    }
    fn render(&self, key: &str, comment: &str) -> Result<Option<String>, Error> {
        self.render(key, comment)
    }
}
