//! Continuation notes own a whole verbatim body and enforce a hub-relative
//! filename on reading. Encoding deliberately permits later-invalid edits.
use super::authored_yaml;
use super::frontmatter::{EditResult, Error, Field, Frontmatter, NodeKind};
use super::issue::{Timestamp, quoted};
use super::issue_encode::{instant, timestamp_pair};
use super::text::Link;
use super::yaml_render::{flow_pair, string_pair};
use serde::{Deserialize, Serialize};
use std::collections::HashSet;

const OWNED: &[&str] = &["id", "aliases", "title", "issue", "created", "updated"];

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct HandoffMetadata {
    pub id: String,
    pub aliases: Vec<String>,
    pub title: String,
    pub issue: Link,
    pub created: Timestamp,
    pub updated: Timestamp,
    pub project: String,
    pub archived: bool,
}

#[derive(Clone, Debug)]
pub struct HandoffDocument {
    pub metadata: HandoffMetadata,
    pub body: super::yaml_string::YamlString,
    pub new_extra: authored_yaml::Node,
    document: Option<Frontmatter>,
    original_metadata: HandoffMetadata,
}

impl HandoffDocument {
    pub fn new(metadata: HandoffMetadata) -> Self {
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
        let (project, archived) =
            path_info(path).ok_or_else(|| Error::new(format!("{path}: not a handoff path")))?;
        let mut metadata = HandoffMetadata {
            project: project.to_owned(),
            archived,
            ..HandoffMetadata::default()
        };
        let mut seen = HashSet::new();
        for field in document.fields() {
            let key = field.key.as_str();
            // Unlike memories/requests/issues, unknown keys also own uniqueness.
            if !seen.insert(key) {
                return Err(Error::new(format!(
                    "{path}: line {}: duplicate frontmatter key {}",
                    document.diagnostic_line(field.key_line),
                    quoted(key)
                )));
            }
            let mut read = || -> Result<(), Error> {
                match key {
                    "id" => metadata.id = field.value.scalar(key)?.to_owned(),
                    "title" => metadata.title = field.value.scalar(key)?.to_owned(),
                    "aliases" => {
                        metadata.aliases = field
                            .value
                            .string_list(key)?
                            .into_iter()
                            .map(str::to_owned)
                            .collect()
                    }
                    "issue" => metadata.issue = Link::parse(field.value.scalar(key)?),
                    "created" => metadata.created = Timestamp::read(key, &field.value)?,
                    "updated" => metadata.updated = Timestamp::read(key, &field.value)?,
                    _ => {}
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
        let zero = instant(&Timestamp::default());
        if metadata.id.is_empty()
            || metadata.title.is_empty()
            || instant(&metadata.created) == zero
            || instant(&metadata.updated) == zero
        {
            return Err(Error::new(format!(
                "{path}: frontmatter is missing required handoff fields"
            )));
        }
        if !super::id::valid_id(&metadata.id) {
            return Err(Error::new(format!(
                "{path}: invalid handoff id {}",
                quoted(&metadata.id)
            )));
        }
        let filename = path
            .rsplit('/')
            .next()
            .unwrap()
            .strip_suffix(".md")
            .unwrap();
        if filename != metadata.id && !filename.starts_with(&format!("{}-", metadata.id)) {
            return Err(Error::new(format!(
                "{path}: filename does not match handoff id {}",
                quoted(&metadata.id)
            )));
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
            .flat_map(|d| d.fields())
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
            output.push_str(&super::extra_encode::content_fields(
                &self.new_extra.content,
            )?);
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

impl HandoffMetadata {
    fn changed(&self, old: &Self, key: &str) -> bool {
        match key {
            "id" => self.id != old.id,
            "aliases" => self.aliases != old.aliases,
            "title" => self.title != old.title,
            "issue" => self.issue != old.issue,
            "created" => instant(&self.created) != instant(&old.created),
            "updated" => instant(&self.updated) != instant(&old.updated),
            _ => unreachable!("owned handoff key"),
        }
    }
    fn render(&self, key: &str, comment: &str) -> Result<Option<String>, Error> {
        Ok(Some(match key {
            "id" => string_pair(key, &self.id, comment),
            "aliases" => flow_pair(
                key,
                if self.aliases.is_empty() {
                    std::slice::from_ref(&self.id)
                } else {
                    &self.aliases
                },
            ),
            "title" => string_pair(key, &self.title, comment),
            "issue" => {
                if self.issue.is_zero() {
                    return Ok(None);
                }
                string_pair(key, &self.issue.raw, comment)
            }
            "created" => timestamp_pair(key, &self.created, comment)?,
            "updated" => timestamp_pair(key, &self.updated, comment)?,
            _ => unreachable!("owned handoff key"),
        }))
    }
}

fn path_info(path: &str) -> Option<(&str, bool)> {
    let parts: Vec<_> = path.split('/').collect();
    match parts.as_slice() {
        ["projects", project, "handoffs", file] if !project.is_empty() && file.ends_with(".md") => {
            Some((project, false))
        }
        ["projects", project, "handoffs", "archive", year, file]
            if !project.is_empty()
                && year.len() == 4
                && year.bytes().all(|b| b.is_ascii_digit())
                && file.ends_with(".md") =>
        {
            Some((project, true))
        }
        _ => None,
    }
}
