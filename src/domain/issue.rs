//! Typed issue frontmatter. The retained document owns the exact input bytes;
//! body/log mutation and field rendering are implemented separately.
use super::frontmatter::{Error, Frontmatter, Node};
use super::text::{IssueBody, Link, split_issue_body};
use serde::{Deserialize, Serialize};
use std::collections::HashSet;
use time::{Date, Month, PrimitiveDateTime, Time};

pub(super) const OWNED: &[&str] = &[
    "id",
    "aliases",
    "title",
    "type",
    "status",
    "priority",
    "labels",
    "assignee",
    "parent",
    "blocked_by",
    "url",
    "created",
    "updated",
];
const REQUIRED: &[&str] = &[
    "id", "title", "type", "status", "priority", "created", "updated",
];

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Timestamp {
    pub seconds: i64,
    pub nanoseconds: u32,
    pub offset_seconds: i32,
}

impl Default for Timestamp {
    fn default() -> Self {
        // Go time.Time's zero value is 0001-01-01T00:00:00Z.
        Self {
            seconds: -62_135_596_800,
            nanoseconds: 0,
            offset_seconds: 0,
        }
    }
}

impl Timestamp {
    pub(super) fn read(key: &str, node: &Node) -> Result<Self, Error> {
        let raw = node.scalar(key)?;
        parse_timestamp(raw.trim()).ok_or_else(|| {
            Error(format!(
                "{key} must be an RFC3339 timestamp, got {}",
                quoted(raw)
            ))
        })
    }
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct IssueMetadata {
    pub id: String,
    pub title: String,
    #[serde(rename = "type")]
    pub kind: String,
    pub status: String,
    pub priority: i64,
    pub labels: Vec<String>,
    pub assignee: String,
    pub parent: Link,
    pub blocked_by: Vec<Link>,
    pub url: String,
    pub created: Timestamp,
    pub updated: Timestamp,
    pub aliases: Vec<String>,
    pub project: String,
    pub archived: bool,
}

#[derive(Clone, Debug)]
pub struct IssueDocument {
    pub metadata: IssueMetadata,
    pub log: Vec<super::log::LogEntry>,
    pub description: super::yaml_string::YamlString,
    pub body: super::yaml_string::YamlString,
    /// Authored unknown fields for a new issue. Parsed documents retain their
    /// original unknown bytes and ignore changes to this tree, as Go does.
    pub new_extra: super::authored_yaml::Node,
    pub(super) document: Option<Frontmatter>,
    pub(super) original_metadata: IssueMetadata,
    original_log_len: usize,
}

impl IssueDocument {
    /// Construct a new issue without retained source or parsed log history.
    pub fn new(metadata: IssueMetadata) -> Self {
        Self {
            original_metadata: metadata.clone(),
            metadata,
            document: None,
            log: Vec::new(),
            description: Default::default(),
            body: Default::default(),
            new_extra: super::authored_yaml::Node::default(),
            original_log_len: 0,
        }
    }

    pub fn parse(path: &str, source: &str) -> Result<Self, Error> {
        Self::parse_bytes(path, source.as_bytes())
    }
    pub fn parse_bytes(path: &str, source: &[u8]) -> Result<Self, Error> {
        let document = Frontmatter::parse_bytes(path, source)?;
        let mut metadata = IssueMetadata::default();
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
        let parts: Vec<_> = path.split('/').collect();
        for (index, part) in parts.iter().enumerate() {
            if *part == "projects"
                && let Some(project) = parts.get(index + 1)
            {
                metadata.project = (*project).to_owned();
            }
            metadata.archived |= *part == "archive";
        }
        let sections = split_issue_body(document.body());
        let description = super::yaml_string::YamlString::from_bytes(
            document.slice_bytes(sections.description).into(),
        );
        let body =
            super::yaml_string::YamlString::from_bytes(document.slice_bytes(sections.body).into());
        let log = super::log::parse_section(sections.log);
        let original_log_len = log.len();
        Ok(Self {
            original_metadata: metadata.clone(),
            metadata,
            log,
            description,
            body,
            new_extra: super::authored_yaml::Node::default(),
            document: Some(document),
            original_log_len,
        })
    }

    /// Semantic values and source spans for parsed user-owned fields, in order.
    pub fn unknown_fields(&self) -> impl Iterator<Item = &super::frontmatter::Field> {
        self.document
            .iter()
            .flat_map(|document| document.fields())
            .filter(|field| !OWNED.contains(&field.key.as_str()))
    }

    /// Append semantics compare instants, then store a later Updated in UTC.
    pub fn append_log(&mut self, entry: super::log::LogEntry) {
        if (entry.at.seconds, entry.at.nanoseconds)
            > (
                self.metadata.updated.seconds,
                self.metadata.updated.nanoseconds,
            )
        {
            self.metadata.updated = Timestamp {
                offset_seconds: 0,
                ..entry.at.clone()
            };
        }
        self.log.push(entry);
    }

    pub fn set_description(&mut self, text: &str) {
        self.set_description_bytes(text.as_bytes());
    }
    pub fn set_description_bytes(&mut self, text: &[u8]) {
        let end = text.iter().rposition(|&b| b != b'\n').map_or(0, |i| i + 1);
        let text = &text[..end];
        let followed = !self.body.is_empty() || !self.log.is_empty() || !self.body().log.is_empty();
        let mut value = text.to_vec();
        if !text.is_empty() || followed {
            value.extend_from_slice(if followed && !text.is_empty() {
                b"\n\n"
            } else {
                b"\n"
            });
        }
        self.description = super::yaml_string::YamlString::from_bytes(value);
    }

    /// Render the body only. Existing parsed log entries are deliberately not
    /// reserialized: only entries beyond the original length are appended.
    /// Full-document encoding combines this with rendered owned frontmatter.
    pub fn render_body(&self) -> Result<String, Error> {
        String::from_utf8(self.render_body_bytes()?)
            .map_err(|_| Error("body is not UTF-8; use render_body_bytes".into()))
    }

    pub fn render_body_bytes(&self) -> Result<Vec<u8>, Error> {
        let original = self.body();
        let mut output = self.description.as_bytes().to_vec();
        output.extend_from_slice(self.body.as_bytes());
        let entries = self.log.get(self.original_log_len..).unwrap_or_default();
        if !original.log.is_empty() {
            let raw = self.document.as_ref().unwrap().slice_bytes(original.log);
            output.extend_from_slice(&super::log::append_to_section_bytes(raw, entries)?);
        } else if !entries.is_empty() {
            if !output.is_empty() && !output.ends_with(b"---\n") {
                if !output.ends_with(b"\n") {
                    output.extend_from_slice(b"\n\n");
                } else if !output.ends_with(b"\n\n") {
                    output.push(b'\n');
                }
            }
            output.extend_from_slice(b"## Log\n");
            for entry in entries {
                output.extend_from_slice(entry.line()?.as_bytes());
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

    pub fn body(&self) -> IssueBody<'_> {
        split_issue_body(self.document.as_ref().map_or("", Frontmatter::body))
    }

    pub fn frontmatter(&self) -> Option<&Frontmatter> {
        self.document.as_ref()
    }
}

impl IssueMetadata {
    fn read(&mut self, key: &str, node: &Node) -> Result<(), Error> {
        match key {
            "id" => self.id = node.scalar(key)?.to_owned(),
            "title" => self.title = node.scalar(key)?.to_owned(),
            "type" => self.kind = node.scalar(key)?.to_owned(),
            "status" => self.status = node.scalar(key)?.to_owned(),
            "assignee" => self.assignee = node.scalar(key)?.to_owned(),
            "url" => self.url = node.scalar(key)?.to_owned(),
            "priority" => {
                let raw = node.scalar(key)?;
                self.priority = raw.trim().parse::<i64>().map_err(|_| {
                    Error(format!("priority must be an integer, got {}", quoted(raw)))
                })?;
            }
            "labels" => {
                self.labels = node
                    .string_list(key)?
                    .into_iter()
                    .map(str::to_owned)
                    .collect()
            }
            "aliases" => {
                self.aliases = node
                    .string_list(key)?
                    .into_iter()
                    .map(str::to_owned)
                    .collect()
            }
            "parent" => self.parent = Link::parse(node.scalar(key)?),
            "blocked_by" => {
                self.blocked_by = node
                    .string_list(key)?
                    .into_iter()
                    .filter(|value| !value.trim().is_empty())
                    .map(Link::parse)
                    .collect()
            }
            "created" => self.created = Timestamp::read(key, node)?,
            "updated" => self.updated = Timestamp::read(key, node)?,
            _ => unreachable!("only owned keys are dispatched"),
        }
        Ok(())
    }
}

// Go %q error messages use mnemonic control escapes and literal printable
// Unicode. Retaining raw scalar text here avoids quoting a coerced value.
pub(crate) fn quoted(value: &str) -> String {
    let mut output = String::from("\"");
    for ch in value.chars() {
        match ch {
            '\x07' => output.push_str("\\a"),
            '\x08' => output.push_str("\\b"),
            '\x0c' => output.push_str("\\f"),
            '\n' => output.push_str("\\n"),
            '\r' => output.push_str("\\r"),
            '\t' => output.push_str("\\t"),
            '\x0b' => output.push_str("\\v"),
            '\\' => output.push_str("\\\\"),
            '"' => output.push_str("\\\""),
            ch if ch.is_ascii_control() => output.push_str(&format!("\\x{:02x}", ch as u32)),
            ch if !super::go_print::printable(ch) => {
                if ch as u32 <= 0xffff {
                    output.push_str(&format!("\\u{:04x}", ch as u32));
                } else {
                    output.push_str(&format!("\\U{:08x}", ch as u32));
                }
            }
            ch => output.push(ch),
        }
    }
    output.push('"');
    output
}

// time.Parse(RFC3339) accepts a one-digit hour, comma fractions, and zone
// hour 24/minute 60. Preserve those stored-format rules instead of relying on
// a stricter RFC parser. All character indexing below is guarded ASCII input.
pub(crate) fn parse_timestamp(value: &str) -> Option<Timestamp> {
    if !value.is_ascii() {
        return None;
    }
    let (date, clock) = value.split_once('T')?;
    let date_parts: Vec<_> = date.split('-').collect();
    if date_parts.len() != 3
        || date_parts[0].len() != 4
        || date_parts[1].len() != 2
        || date_parts[2].len() != 2
    {
        return None;
    }
    fn digits(value: &str) -> Option<u32> {
        if value.is_empty() || !value.bytes().all(|byte| byte.is_ascii_digit()) {
            return None;
        }
        value.parse().ok()
    }
    let year = digits(date_parts[0])? as i32;
    let month = Month::try_from(u8::try_from(digits(date_parts[1])?).ok()?).ok()?;
    let day = u8::try_from(digits(date_parts[2])?).ok()?;
    let date = Date::from_calendar_date(year, month, day).ok()?;
    let (clock, offset) = if let Some(clock) = clock.strip_suffix('Z') {
        (clock, 0)
    } else {
        let index = clock.find(['+', '-'])?;
        let zone = &clock[index..];
        if zone.len() != 6 || &zone[3..4] != ":" {
            return None;
        }
        let hours = digits(&zone[1..3])?;
        let minutes = digits(&zone[4..6])?;
        if hours > 24 || minutes > 60 {
            return None;
        }
        let offset = (hours * 3600 + minutes * 60) as i32;
        (
            &clock[..index],
            if zone.starts_with('-') {
                -offset
            } else {
                offset
            },
        )
    };
    let (clock, nanoseconds) = if let Some(index) = clock.find(['.', ',']) {
        let fraction = &clock[index + 1..];
        if fraction.is_empty() || !fraction.bytes().all(|byte| byte.is_ascii_digit()) {
            return None;
        }
        let count = fraction.len().min(9);
        let nanoseconds = digits(&fraction[..count])? * 10_u32.pow(9 - count as u32);
        (&clock[..index], nanoseconds)
    } else {
        (clock, 0)
    };
    let parts: Vec<_> = clock.split(':').collect();
    if parts.len() != 3
        || !(1..=2).contains(&parts[0].len())
        || parts[1].len() != 2
        || parts[2].len() != 2
    {
        return None;
    }
    let hour = u8::try_from(digits(parts[0])?).ok()?;
    let minute = u8::try_from(digits(parts[1])?).ok()?;
    let second = u8::try_from(digits(parts[2])?).ok()?;
    let clock = Time::from_hms_nano(hour, minute, second, nanoseconds).ok()?;
    Some(Timestamp {
        seconds: PrimitiveDateTime::new(date, clock)
            .assume_utc()
            .unix_timestamp()
            - i64::from(offset),
        nanoseconds,
        offset_seconds: offset,
    })
}
