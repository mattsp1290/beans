//! Memories have optional timestamps and a verbatim body. Grammar helpers are
//! creation policy; parsing and encoding intentionally remain permissive.
use super::authored_yaml;
use super::frontmatter::{EditResult, Error, Field, Frontmatter, NodeKind};
use super::issue::{Timestamp, quoted};
use super::issue_encode::{instant, timestamp_pair};
use super::yaml_render::{flow_pair, string_pair};
use serde::{Deserialize, Serialize};
use std::collections::HashSet;

pub const KEY_MAX_LEN: usize = 80;
pub const TYPES: &[&str] = &["user", "feedback", "project", "reference"];
const OWNED: &[&str] = &["key", "type", "tags", "created", "updated"];

pub fn valid_key(key: &str) -> bool {
    let bytes = key.as_bytes();
    let alnum = |b: u8| b.is_ascii_lowercase() || b.is_ascii_digit();
    !bytes.is_empty()
        && bytes.len() <= KEY_MAX_LEN
        && alnum(bytes[0])
        && bytes.iter().all(|&b| alnum(b) || b == b'-')
}
pub fn valid_type(kind: &str) -> bool {
    kind.is_empty() || TYPES.contains(&kind)
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct MemoryMetadata {
    pub key: String,
    #[serde(rename = "type")]
    pub kind: String,
    pub tags: Vec<String>,
    pub created: Timestamp,
    pub updated: Timestamp,
    pub project: String,
}

#[derive(Clone, Debug)]
pub struct MemoryDocument {
    pub metadata: MemoryMetadata,
    pub body: super::yaml_string::YamlString,
    pub new_extra: authored_yaml::Node,
    document: Option<Frontmatter>,
    original_metadata: MemoryMetadata,
}

impl MemoryDocument {
    pub fn new(metadata: MemoryMetadata) -> Self {
        Self {
            original_metadata: metadata.clone(),
            metadata,
            body: Default::default(),
            new_extra: authored_yaml::Node::default(),
            document: None,
        }
    }
    pub fn parse(path: &str, source: &str) -> Result<Self, Error> {
        Self::parse_bytes(path, source.as_bytes())
    }
    pub fn parse_bytes(path: &str, source: &[u8]) -> Result<Self, Error> {
        let document = Frontmatter::parse_bytes(path, source)?;
        let mut metadata = MemoryMetadata::default();
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
            let mut read = || -> Result<(), Error> {
                match key {
                    "key" => metadata.key = field.value.scalar(key)?.to_owned(),
                    "type" => metadata.kind = field.value.scalar(key)?.to_owned(),
                    "tags" => {
                        metadata.tags = field
                            .value
                            .string_list(key)?
                            .into_iter()
                            .map(str::to_owned)
                            .collect()
                    }
                    "created" | "updated" => {
                        let raw = field.value.scalar(key)?;
                        if !raw.trim().is_empty() {
                            let timestamp = Timestamp::read(key, &field.value)?;
                            if key == "created" {
                                metadata.created = timestamp;
                            } else {
                                metadata.updated = timestamp;
                            }
                        }
                    }
                    _ => unreachable!("owned memory key"),
                }
                Ok(())
            };
            read().map_err(|error| {
                Error::new(format!(
                    "{path}: line {}: {error}",
                    document.diagnostic_line(field.value.line)
                ))
            })?;
        }
        if metadata.key.is_empty() {
            return Err(Error::new(format!(
                "{path}: frontmatter is missing required key \"key\""
            )));
        }
        let parts: Vec<_> = path.split('/').collect();
        for (index, part) in parts.iter().enumerate() {
            if *part == "projects"
                && let Some(project) = parts.get(index + 1)
            {
                metadata.project = (*project).to_owned();
            }
        }
        Ok(Self {
            original_metadata: metadata.clone(),
            metadata,
            body: super::yaml_string::YamlString::from_bytes(document.body_bytes().into()),
            new_extra: authored_yaml::Node::default(),
            document: Some(document),
        })
    }
    pub fn original_bytes(&self) -> &[u8] {
        self.document
            .as_ref()
            .map_or(&[], Frontmatter::original_bytes)
    }
    pub fn original(&self) -> &str {
        self.document.as_ref().map_or("", Frontmatter::original)
    }
    pub fn unknown_fields(&self) -> impl Iterator<Item = &Field> {
        self.document
            .iter()
            .flat_map(|document| document.fields())
            .filter(|f| !OWNED.contains(&f.key.as_str()))
    }
    pub fn encode(&self) -> Result<EditResult, Error> {
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
            output.extend_from_slice(self.body.as_bytes());
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
                    .find(|f| f.key == key)
                    .filter(|f| f.value.kind == NodeKind::Scalar)
                    .map_or(String::new(), |f| document.scalar_comment(f));
                rendered.push((key, self.metadata.render(key, &comment)?));
            }
        }
        let changes: Vec<_> = rendered
            .iter()
            .map(|(key, value)| (*key, value.as_ref().map(|v| v.as_bytes())))
            .collect();
        document.splice_owned(OWNED, &changes, self.body.as_bytes())
    }
}

impl MemoryMetadata {
    fn changed(&self, old: &Self, key: &str) -> bool {
        match key {
            "key" => self.key != old.key,
            "type" => self.kind != old.kind,
            "tags" => self.tags != old.tags,
            "created" => instant(&self.created) != instant(&old.created),
            "updated" => instant(&self.updated) != instant(&old.updated),
            _ => unreachable!("owned memory key"),
        }
    }
    fn render(&self, key: &str, comment: &str) -> Result<Option<String>, Error> {
        let value = match key {
            "key" => string_pair(key, &self.key, comment),
            "type" => {
                if self.kind.is_empty() {
                    return Ok(None);
                }
                string_pair(key, &self.kind, comment)
            }
            "tags" => {
                if self.tags.is_empty() {
                    return Ok(None);
                }
                flow_pair(key, &self.tags)
            }
            "created" | "updated" => {
                let value = if key == "created" {
                    &self.created
                } else {
                    &self.updated
                };
                if instant(value) == instant(&Timestamp::default()) {
                    return Ok(None);
                }
                timestamp_pair(key, value, comment)?
            }
            _ => unreachable!("owned memory key"),
        };
        Ok(Some(value))
    }
}
