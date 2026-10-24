//! Request ownership differs from issue ownership and is validated on both
//! reading and writing. Only presentation primitives are shared.
use super::authored_yaml;
use super::frontmatter::{Error, Field, Frontmatter, Node};
use super::issue::{Timestamp, quoted};
use super::issue_encode::instant;
use super::log::LogEntry;
use super::lossless_record::LosslessRecord;
use super::request;
use super::text::{Link, RequestBody};
use serde::{Deserialize, Serialize};

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
pub(super) const REQUIRED: &[&str] = &[
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
    pub body: super::yaml_string::YamlString,
    pub log: Vec<LogEntry>,
    /// Used only for newly authored documents; parsed unknown bytes are retained.
    pub new_extra: authored_yaml::Node,
    pub(super) record: LosslessRecord,
    pub(super) original_metadata: RequestMetadata,
}

impl RequestDocument {
    pub fn new(metadata: RequestMetadata) -> Self {
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
        let parsed = LosslessRecord::parse::<RequestMetadata>(path, source)?;
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
        self.record.unknown_fields::<RequestMetadata>()
    }

    /// Replace all Markdown preceding the Log, retaining one final newline.
    pub fn set_body(&mut self, text: &str) {
        self.set_body_bytes(text.as_bytes());
    }
    pub fn set_body_bytes(&mut self, text: &[u8]) {
        let end = text.iter().rposition(|&b| b != b'\n').map_or(0, |i| i + 1);
        let mut value = text[..end].to_vec();
        if !value.is_empty() {
            value.push(b'\n');
        }
        self.body = super::yaml_string::YamlString::from_bytes(value);
    }

    pub fn append_log(&mut self, entry: LogEntry) {
        LosslessRecord::append_log(&mut self.metadata.updated, &mut self.log, entry);
    }

    pub fn render_body(&self) -> Result<String, Error> {
        self.record
            .render_body::<RequestMetadata>(&self.body, &self.log)
    }

    pub fn render_body_bytes(&self) -> Result<Vec<u8>, Error> {
        self.record
            .render_body_bytes::<RequestMetadata>(&self.body, &self.log)
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
}

impl RequestMetadata {
    pub fn validate(&self) -> Result<(), Error> {
        if !request::valid_id(&self.id) {
            return Err(Error::new(format!(
                "invalid request id {}",
                quoted(&self.id)
            )));
        }
        if self.title.trim().is_empty() {
            return Err(Error::new("title must not be blank".into()));
        }
        if !request::valid_status(&self.status) {
            return Err(Error::new(format!(
                "invalid request status {}",
                quoted(&self.status)
            )));
        }
        if !(0..=4).contains(&self.priority) {
            return Err(Error::new("priority must be between 0 and 4".into()));
        }
        let zero = instant(&Timestamp::default());
        if instant(&self.created) == zero || instant(&self.updated) == zero {
            return Err(Error::new(
                "created and updated must be RFC3339 timestamps".into(),
            ));
        }
        if !self.aliases.contains(&self.id) {
            return Err(Error::new(format!(
                "aliases must contain request id {}",
                quoted(&self.id)
            )));
        }
        Ok(())
    }

    pub(super) fn read(&mut self, key: &str, node: &Node) -> Result<(), Error> {
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
                    Error::new(format!("priority must be an integer, got {}", quoted(raw)))
                })?;
            }
            "created" => self.created = Timestamp::read(key, node)?,
            "updated" => self.updated = Timestamp::read(key, node)?,
            _ => unreachable!("request owned key"),
        }
        Ok(())
    }
}
