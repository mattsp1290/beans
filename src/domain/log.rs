//! Stored log entries, including opaque list items. Parsing follows Go's
//! ASCII regex whitespace rules; formatting follows Unicode token rules.
use super::frontmatter::Error;
use super::issue::{Timestamp, parse_timestamp};
use serde::{Deserialize, Serialize};
use time::OffsetDateTime;

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct LogEntry {
    pub at: Timestamp,
    pub actor: String,
    pub repo: String,
    pub sha: String,
    pub branch: String,
    pub event: String,
    pub raw: String,
}

fn regex_space(ch: char) -> bool {
    matches!(ch, ' ' | '\t' | '\n' | '\r' | '\x0c')
}

fn token(value: &str) -> String {
    let parts: Vec<_> = value.split_whitespace().collect();
    if parts.is_empty() {
        "-".to_owned()
    } else {
        parts.join("-")
    }
}

fn log_timestamp(value: &str) -> Option<Timestamp> {
    parse_timestamp(value).or_else(|| {
        let (date, clock) = value.split_once('T')?;
        let zone = clock.find(['Z', '+', '-'])?;
        let minute_clock = &clock[..zone];
        if minute_clock.matches(':').count() != 1 || minute_clock.contains(['.', ',']) {
            return None;
        }
        parse_timestamp(&format!("{date}T{minute_clock}:00{}", &clock[zone..]))
    })
}

impl LogEntry {
    pub fn parse(text: &str) -> Option<Self> {
        let (first, rest) = text.split_once('\n').unwrap_or((text, ""));
        let (timestamp, fields) = first.strip_prefix("- ")?.split_once(' ')?;
        let at = log_timestamp(timestamp)?;
        // The Go actor is lazy: the earliest colon with a valid actor/context
        // wins. Context may contain ')' in the branch, so consume its last ')'.
        for (index, _) in fields.match_indices(": ") {
            let prefix = &fields[..index];
            let mut entry = Self {
                at: at.clone(),
                ..Self::default()
            };
            if !prefix.is_empty() && !prefix.chars().any(regex_space) {
                entry.actor = prefix.to_owned();
            } else {
                let Some((actor, context)) = prefix.split_once(" (") else {
                    continue;
                };
                if actor.is_empty() || actor.chars().any(regex_space) {
                    continue;
                }
                let Some(context) = context.strip_suffix(')') else {
                    continue;
                };
                let Some((repo, revision)) = context.split_once('@') else {
                    continue;
                };
                if repo.is_empty() || repo.chars().any(|ch| regex_space(ch) || ch == ')') {
                    continue;
                }
                let (sha, branch) = revision.split_once(' ').unwrap_or((revision, ""));
                if sha.is_empty()
                    || !sha
                        .bytes()
                        .all(|ch| ch.is_ascii_digit() || (b'a'..=b'f').contains(&ch))
                {
                    continue;
                }
                if revision.contains(' ') && (branch.is_empty() || branch.chars().any(regex_space))
                {
                    continue;
                }
                entry.actor = actor.to_owned();
                entry.repo = repo.to_owned();
                entry.sha = sha.to_owned();
                entry.branch = branch.to_owned();
            }
            entry.event = fields[index + 2..].to_owned();
            if !rest.is_empty() {
                entry.event.push('\n');
                entry.event.push_str(
                    &rest
                        .split('\n')
                        .map(|line| line.strip_prefix("  ").unwrap_or(line))
                        .collect::<Vec<_>>()
                        .join("\n"),
                );
            }
            return Some(entry);
        }
        None
    }

    pub fn format(&self) -> Result<String, Error> {
        let at = OffsetDateTime::from_unix_timestamp(self.at.seconds)
            .map_err(|_| Error("log timestamp is out of range".to_owned()))?;
        let year = if at.year() < 0 {
            format!("-{:04}", -at.year())
        } else {
            format!("{:04}", at.year())
        };
        let mut output = format!(
            "- {year}-{:02}-{:02}T{:02}:{:02}:{:02}Z {}",
            at.month() as u8,
            at.day(),
            at.hour(),
            at.minute(),
            at.second(),
            token(&self.actor)
        );
        if !self.repo.is_empty() && !self.sha.is_empty() {
            output.push_str(&format!(" ({}@{}", token(&self.repo), self.sha));
            if !self.branch.is_empty() {
                output.push_str(&format!(" {}", token(&self.branch)));
            }
            output.push(')');
        }
        output.push_str(": ");
        output.push_str(&self.event.replace('\n', "\n  "));
        Ok(output)
    }

    pub fn line(&self) -> Result<String, Error> {
        if self.raw.is_empty() {
            Ok(self.format()? + "\n")
        } else {
            Ok(self.raw.clone() + "\n")
        }
    }
}

pub fn parse_section(raw: &str) -> Vec<LogEntry> {
    if raw.is_empty() {
        return Vec::new();
    }
    let mut entries = Vec::new();
    let mut current: Vec<&str> = Vec::new();
    let flush = |current: &mut Vec<&str>, entries: &mut Vec<LogEntry>| {
        if !current.is_empty() {
            let text = current.join("\n");
            entries.push(LogEntry::parse(&text).unwrap_or_else(|| LogEntry {
                raw: text,
                ..LogEntry::default()
            }));
            current.clear();
        }
    };
    for line in raw.strip_suffix('\n').unwrap_or(raw).split('\n').skip(1) {
        if line.starts_with("- ") {
            flush(&mut current, &mut entries);
            current.push(line);
        } else if !current.is_empty() && line.starts_with("  ") {
            current.push(line);
        } else {
            flush(&mut current, &mut entries);
        }
    }
    flush(&mut current, &mut entries);
    entries
}

/// Append after the last nonblank line, preserving original blank lines.
pub fn append_to_section(raw: &str, entries: &[LogEntry]) -> Result<String, Error> {
    if entries.is_empty() {
        return Ok(raw.to_owned());
    }
    let mut lines: Vec<_> = raw.split_inclusive('\n').collect();
    // Go SplitAfter retains an empty element after a terminal newline.
    if raw.ends_with('\n') || raw.is_empty() {
        lines.push("");
    }
    let mut cut = lines.len();
    while cut > 1 && lines[cut - 1].trim().is_empty() {
        cut -= 1;
    }
    let mut output = lines[..cut].concat();
    if !output.ends_with('\n') {
        output.push('\n');
    }
    for entry in entries {
        output.push_str(&entry.line()?);
    }
    output.push_str(&lines[cut..].concat());
    Ok(output)
}
