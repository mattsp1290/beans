//! Existing issue encoding. Ownership and rendering are resolved before the
//! frontmatter adapter derives byte spans and invokes the verified splice path.
use super::frontmatter::{EditResult, Error, NodeKind};
use super::issue::{IssueDocument, IssueMetadata, OWNED, Timestamp};
use super::text::Link;
use super::yaml_render::{flow_pair, link_pair, string_pair};
use time::OffsetDateTime;

impl IssueDocument {
    pub fn encode(&self) -> Result<EditResult, Error> {
        let Some(document) = &self.document else {
            return self.encode_new();
        };
        let mut rendered = Vec::new();
        for &key in OWNED {
            if self.metadata.changed(&self.original_metadata, key) {
                let comment = document
                    .fields()
                    .iter()
                    .find(|field| field.key == key)
                    .filter(|field| field.value.kind == NodeKind::Scalar)
                    .map_or(String::new(), |field| {
                        scalar_comment(
                            document.original(),
                            field.start,
                            field.end,
                            field.value.line,
                            field.key_line,
                        )
                    });
                rendered.push((key, self.metadata.render(key, &comment)?));
            }
        }
        let changes: Vec<_> = rendered
            .iter()
            .map(|(key, value)| (*key, value.as_ref().map(|value| value.as_bytes())))
            .collect();
        document.splice_owned(OWNED, &changes, self.render_body()?.as_bytes())
    }
    fn encode_new(&self) -> Result<EditResult, Error> {
        let mut output = String::from("---\n");
        for &key in OWNED {
            if let Some(field) = self.metadata.render(key, "")? {
                output.push_str(&field);
            }
        }
        output.push_str("---\n");
        output.push_str(&self.render_body()?);
        Ok(EditResult {
            bytes: output.into_bytes(),
            copies: Vec::new(),
        })
    }
}

impl IssueMetadata {
    fn changed(&self, previous: &Self, key: &str) -> bool {
        match key {
            "id" => self.id != previous.id,
            "aliases" => self.aliases != previous.aliases,
            "title" => self.title != previous.title,
            "type" => self.kind != previous.kind,
            "status" => self.status != previous.status,
            "priority" => self.priority != previous.priority,
            "labels" => self.labels != previous.labels,
            "assignee" => self.assignee != previous.assignee,
            "parent" => self.parent != previous.parent,
            "blocked_by" => self.blocked_by != previous.blocked_by,
            "url" => self.url != previous.url,
            "created" => instant(&self.created) != instant(&previous.created),
            "updated" => instant(&self.updated) != instant(&previous.updated),
            _ => unreachable!("owned key"),
        }
    }

    fn render(&self, key: &str, comment: &str) -> Result<Option<String>, Error> {
        let output = match key {
            "id" => string_pair(key, &self.id, comment),
            "title" => string_pair(key, &self.title, comment),
            "type" => string_pair(key, &self.kind, comment),
            "status" => string_pair(key, &self.status, comment),
            "priority" => format!("{key}: {}{}\n", self.priority, comment_suffix(comment)),
            "aliases" => {
                let mut aliases = self.aliases.clone();
                if !self.id.is_empty() && !aliases.contains(&self.id) {
                    aliases.insert(0, self.id.clone());
                }
                flow_pair(key, &aliases)
            }
            "labels" => {
                if self.labels.is_empty() {
                    return Ok(None);
                }
                flow_pair(key, &self.labels)
            }
            "assignee" => {
                if self.assignee.is_empty() {
                    return Ok(None);
                }
                string_pair(key, &self.assignee, comment)
            }
            "url" => {
                if self.url.is_empty() {
                    return Ok(None);
                }
                string_pair(key, &self.url, comment)
            }
            "parent" => {
                if self.parent.is_zero() {
                    return Ok(None);
                }
                link_pair(key, &raw_link(&self.parent), comment)
            }
            "blocked_by" => {
                if self.blocked_by.is_empty() {
                    return Ok(None);
                }
                let mut output = format!("{key}:\n");
                for link in &self.blocked_by {
                    let scalar = link_pair("", &raw_link(link), "");
                    output.push_str("  - ");
                    output.push_str(scalar.strip_prefix(": ").unwrap());
                }
                output
            }
            "created" => timestamp_pair(key, &self.created, comment)?,
            "updated" => timestamp_pair(key, &self.updated, comment)?,
            _ => unreachable!("owned key"),
        };
        Ok(Some(output))
    }
}

fn instant(value: &Timestamp) -> (i64, u32) {
    (value.seconds, value.nanoseconds)
}
fn comment_suffix(comment: &str) -> String {
    if comment.is_empty() {
        String::new()
    } else {
        format!(" {comment}")
    }
}
fn raw_link(link: &Link) -> String {
    if link.raw.is_empty() {
        format!("[[{}]]", link.target)
    } else {
        link.raw.clone()
    }
}

fn timestamp_pair(key: &str, value: &Timestamp, comment: &str) -> Result<String, Error> {
    let date = OffsetDateTime::from_unix_timestamp(value.seconds)
        .map_err(|_| Error("issue timestamp is out of range".into()))?;
    let year = if date.year() < 0 {
        format!("-{:04}", -date.year())
    } else {
        format!("{:04}", date.year())
    };
    let tag = if (0..=9999).contains(&date.year()) {
        ""
    } else {
        "!!timestamp "
    };
    Ok(format!(
        "{key}: {tag}{year}-{:02}-{:02}T{:02}:{:02}:{:02}Z{}\n",
        date.month() as u8,
        date.day(),
        date.hour(),
        date.minute(),
        date.second(),
        comment_suffix(comment)
    ))
}

// Locate a scalar's trailing comment without treating hashes inside quoted
// values or block content as comments. The original slice remains immutable.
fn scalar_comment(
    source: &str,
    start: usize,
    end: usize,
    value_line: usize,
    key_line: usize,
) -> String {
    let fragment = &source[start..end];
    let offset: usize = fragment
        .split_inclusive('\n')
        .take(value_line.saturating_sub(key_line))
        .map(str::len)
        .sum();
    let Some(value) = fragment.get(offset..) else {
        return String::new();
    };
    let value = if value_line == key_line {
        value.split_once(':').map_or(value, |(_, value)| value)
    } else {
        value
    };
    let mut value = value.trim_start();
    while value.starts_with(['&', '!']) {
        let Some((_, rest)) = value.split_once(char::is_whitespace) else {
            return String::new();
        };
        value = rest.trim_start();
    }
    let value = if value.starts_with(['|', '>']) {
        value.split('\n').next().unwrap()
    } else {
        value
    };
    let mut quoted = value.chars().next().filter(|ch| matches!(ch, '\'' | '"'));
    let mut escaped = false;
    let mut previous = ' ';
    let mut chars = value.char_indices().peekable();
    while let Some((byte, ch)) = chars.next() {
        if byte == 0 && quoted.is_some() {
            previous = ch;
            continue;
        }
        if escaped {
            escaped = false;
            previous = ch;
            continue;
        }
        if quoted == Some('"') && ch == '\\' {
            escaped = true;
        } else if quoted == Some(ch) {
            if ch == '\'' && chars.peek().is_some_and(|(_, next)| *next == '\'') {
                chars.next();
            } else {
                quoted = None;
            }
        } else if quoted.is_none() && ch == '#' && previous.is_whitespace() {
            return value[byte..].split('\n').next().unwrap().to_owned();
        }
        previous = ch;
    }
    String::new()
}
