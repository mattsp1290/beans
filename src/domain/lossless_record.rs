//! Canonical lossless codec shell for frontmatter records with Markdown bodies
//! and append-only Log sections. Record adapters own only metadata semantics.
use super::authored_yaml;
use super::frontmatter::{EditResult, Error, Field, Frontmatter, Node, NodeKind};
use super::issue::{Timestamp, quoted};
use super::issue_encode::instant;
use super::log::{LogEntry, parse_section_bytes};
use super::text::{RequestBody, split_request_body};
use super::yaml_string::YamlString;
use std::collections::HashSet;

pub(super) trait RecordMetadata: Clone + Default {
    const KIND: &'static str;
    const DIRECTORY: &'static str;
    const OWNED: &'static [&'static str];
    const REQUIRED: &'static [&'static str];

    fn set_project(&mut self, project: String);
    fn read(&mut self, key: &str, node: &Node) -> Result<(), Error>;
    fn validate(&self) -> Result<(), Error>;
    fn changed(&self, previous: &Self, key: &str) -> bool;
    fn render(&self, key: &str, comment: &str) -> Result<Option<String>, Error>;

    /// Requests historically suppress an extra separator after a freshly
    /// authored frontmatter marker. Other record bodies always separate logs.
    fn separate_before_new_log(body: &[u8]) -> bool {
        !body.is_empty()
    }
}

#[derive(Clone, Debug)]
pub(super) struct LosslessRecord {
    document: Option<Frontmatter>,
    original_log_len: usize,
}

pub(super) struct ParsedRecord<M> {
    pub metadata: M,
    pub body: YamlString,
    pub log: Vec<LogEntry>,
    pub record: LosslessRecord,
}

impl LosslessRecord {
    pub fn new() -> Self {
        Self {
            document: None,
            original_log_len: 0,
        }
    }

    pub fn parse<M: RecordMetadata>(path: &str, source: &[u8]) -> Result<ParsedRecord<M>, Error> {
        // Keep this check ahead of path validation: both record codecs expose
        // the same deterministic diagnostic ordering.
        if source.windows(2).any(|window| window == b"\r\n") {
            return Err(Error::new(format!(
                "{path}: has Windows line endings (\\r\\n); bn requires \\n"
            )));
        }
        let project = project_from_path::<M>(path)?;
        let document = Frontmatter::parse_bytes(path, source)?;
        let mut metadata = M::default();
        metadata.set_project(project);
        let mut seen = HashSet::new();
        for field in document.fields() {
            let key = field.key.as_str();
            if !M::OWNED.contains(&key) {
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
        for key in M::REQUIRED {
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
        let body = YamlString::from_bytes(document.slice_bytes(sections.before_log).into());
        let log = parse_section_bytes(document.slice_bytes(sections.log));
        let original_log_len = log.len();
        Ok(ParsedRecord {
            metadata,
            body,
            log,
            record: Self {
                document: Some(document),
                original_log_len,
            },
        })
    }

    pub fn unknown_fields<M: RecordMetadata>(&self) -> impl Iterator<Item = &Field> {
        self.document
            .iter()
            .flat_map(|document| document.fields())
            .filter(|field| !M::OWNED.contains(&field.key.as_str()))
    }

    pub fn append_log(metadata_updated: &mut Timestamp, log: &mut Vec<LogEntry>, entry: LogEntry) {
        if instant(&entry.at) > instant(metadata_updated) {
            *metadata_updated = Timestamp {
                offset_seconds: 0,
                ..entry.at.clone()
            };
        }
        log.push(entry);
    }

    pub fn render_body<M: RecordMetadata>(
        &self,
        body: &YamlString,
        log: &[LogEntry],
    ) -> Result<String, Error> {
        String::from_utf8(self.render_body_bytes::<M>(body, log)?)
            .map_err(|_| Error::new("body is not UTF-8; use render_body_bytes".into()))
    }

    pub fn render_body_bytes<M: RecordMetadata>(
        &self,
        body: &YamlString,
        log: &[LogEntry],
    ) -> Result<Vec<u8>, Error> {
        let original = self.original_body();
        let mut output = body.as_bytes().to_vec();
        let entries = log.get(self.original_log_len..).unwrap_or_default();
        if !original.log.is_empty() {
            let raw = self.document.as_ref().unwrap().slice_bytes(original.log);
            output.extend_from_slice(&super::log::append_to_section_bytes(raw, entries)?);
        } else if !entries.is_empty() {
            if M::separate_before_new_log(&output) {
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

    pub fn encode<M: RecordMetadata>(
        &self,
        metadata: &M,
        original_metadata: &M,
        body: &YamlString,
        log: &[LogEntry],
        new_extra: &authored_yaml::Node,
    ) -> Result<EditResult, Error> {
        metadata.validate()?;
        let body = self.render_body_bytes::<M>(body, log)?;
        let Some(document) = &self.document else {
            let mut output = String::from("---\n");
            for &key in M::OWNED {
                if let Some(field) = metadata.render(key, "")? {
                    output.push_str(&field);
                }
            }
            output.push_str(&super::extra_encode::mapping_fields(new_extra)?);
            output.push_str("---\n");
            let mut output = output.into_bytes();
            output.extend_from_slice(&body);
            return Ok(EditResult {
                bytes: output,
                copies: Vec::new(),
            });
        };

        let mut rendered = Vec::new();
        for &key in M::OWNED {
            if metadata.changed(original_metadata, key) {
                let comment = document
                    .fields()
                    .iter()
                    .find(|field| field.key == key)
                    .filter(|field| field.value.kind == NodeKind::Scalar)
                    .map_or(String::new(), |field| document.scalar_comment(field));
                rendered.push((key, metadata.render(key, &comment)?));
            }
        }
        let changes: Vec<_> = rendered
            .iter()
            .map(|(key, value)| (*key, value.as_ref().map(|value| value.as_bytes())))
            .collect();
        document.splice_owned(M::OWNED, &changes, &body)
    }
}

fn project_from_path<M: RecordMetadata>(path: &str) -> Result<String, Error> {
    // Lexical normalization intentionally avoids symlink and filesystem state.
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
            && tail[2] == M::DIRECTORY
            && tail[3]
                .strip_suffix(".md")
                .is_some_and(|stem| !stem.is_empty())
        {
            return Ok(tail[1].to_owned());
        }
    }
    Err(Error::new(format!(
        "{path}: {} path must be projects/<project>/{}/<id>-<slug>.md",
        M::KIND,
        M::DIRECTORY
    )))
}
