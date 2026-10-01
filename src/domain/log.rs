//! Stored log entries, including opaque list items. Parsing follows Go's
//! ASCII regex whitespace rules; formatting follows Unicode token rules.
use super::frontmatter::Error;
use super::issue::{Timestamp, parse_timestamp};
pub use super::yaml_string::YamlString;
use serde::{Deserialize, Serialize};
use time::OffsetDateTime;

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct LogEntry {
    pub at: Timestamp,
    pub actor: YamlString,
    pub repo: YamlString,
    pub sha: YamlString,
    pub branch: YamlString,
    pub event: YamlString,
    pub raw: YamlString,
}

fn regex_space(ch: char) -> bool {
    matches!(ch, ' ' | '\t' | '\n' | '\r' | '\x0c')
}

fn token(value: &YamlString) -> Vec<u8> {
    let view = super::source::Source::new(value.as_bytes());
    let mut output = Vec::new();
    for part in view.text.split_whitespace() {
        if !output.is_empty() {
            output.push(b'-');
        }
        let start = part.as_ptr() as usize - view.text.as_ptr() as usize;
        output.extend_from_slice(
            &value.as_bytes()[view.offsets[start]..view.offsets[start + part.len()]],
        );
    }
    if output.is_empty() {
        output.push(b'-');
    }
    output
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
        Self::parse_bytes(text.as_bytes())
    }

    pub fn parse_bytes(bytes: &[u8]) -> Option<Self> {
        let view = super::source::Source::new(bytes);
        let text = view.text.as_str();
        let capture = |part: &str| {
            if part.is_empty() {
                return Vec::new();
            }
            let start = part.as_ptr() as usize - text.as_ptr() as usize;
            bytes[view.offsets[start]..view.offsets[start + part.len()]].to_vec()
        };
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
                entry.actor = YamlString::from_bytes(capture(prefix));
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
                entry.actor = YamlString::from_bytes(capture(actor));
                entry.repo = YamlString::from_bytes(capture(repo));
                entry.sha = YamlString::from_bytes(capture(sha));
                entry.branch = YamlString::from_bytes(capture(branch));
            }
            let mut event = capture(&fields[index + 2..]);
            if !rest.is_empty() {
                event.push(b'\n');
                for (index, line) in rest.split('\n').enumerate() {
                    if index > 0 {
                        event.push(b'\n');
                    }
                    event.extend_from_slice(&capture(line.strip_prefix("  ").unwrap_or(line)));
                }
            }
            entry.event = YamlString::from_bytes(event);
            return Some(entry);
        }
        None
    }

    pub fn format(&self) -> Result<String, Error> {
        String::from_utf8(self.format_bytes()?)
            .map_err(|_| Error::new("log entry is not UTF-8; use format_bytes".into()))
    }

    pub fn format_bytes(&self) -> Result<Vec<u8>, Error> {
        let at = OffsetDateTime::from_unix_timestamp(self.at.seconds)
            .map_err(|_| Error::new("log timestamp is out of range".to_owned()))?;
        let year = if at.year() < 0 {
            format!("-{:04}", -at.year())
        } else {
            format!("{:04}", at.year())
        };
        let mut output = format!(
            "- {year}-{:02}-{:02}T{:02}:{:02}:{:02}Z ",
            at.month() as u8,
            at.day(),
            at.hour(),
            at.minute(),
            at.second(),
        )
        .into_bytes();
        output.extend_from_slice(&token(&self.actor));
        if !self.repo.is_empty() && !self.sha.is_empty() {
            output.extend_from_slice(b" (");
            output.extend_from_slice(&token(&self.repo));
            output.push(b'@');
            output.extend_from_slice(self.sha.as_bytes());
            if !self.branch.is_empty() {
                output.push(b' ');
                output.extend_from_slice(&token(&self.branch));
            }
            output.push(b')');
        }
        output.extend_from_slice(b": ");
        for (index, line) in self.event.as_bytes().split(|b| *b == b'\n').enumerate() {
            if index > 0 {
                output.extend_from_slice(b"\n  ");
            }
            output.extend_from_slice(line);
        }
        Ok(output)
    }

    pub fn line(&self) -> Result<String, Error> {
        String::from_utf8(self.line_bytes()?)
            .map_err(|_| Error::new("log entry is not UTF-8; use line_bytes".into()))
    }

    pub fn line_bytes(&self) -> Result<Vec<u8>, Error> {
        let mut line = if self.raw.is_empty() {
            self.format_bytes()?
        } else {
            self.raw.as_bytes().to_vec()
        };
        line.push(b'\n');
        Ok(line)
    }
}

pub fn parse_section(raw: &str) -> Vec<LogEntry> {
    parse_section_bytes(raw.as_bytes())
}

pub fn parse_section_bytes(raw: &[u8]) -> Vec<LogEntry> {
    if raw.is_empty() {
        return Vec::new();
    }
    let mut entries = Vec::new();
    let mut current: Vec<&[u8]> = Vec::new();
    let flush = |current: &mut Vec<&[u8]>, entries: &mut Vec<LogEntry>| {
        if !current.is_empty() {
            let text = current.join(&b'\n');
            entries.push(LogEntry::parse_bytes(&text).unwrap_or_else(|| LogEntry {
                raw: YamlString::from_bytes(text),
                ..LogEntry::default()
            }));
            current.clear();
        }
    };
    for line in raw
        .strip_suffix(b"\n")
        .unwrap_or(raw)
        .split(|b| *b == b'\n')
        .skip(1)
    {
        if line.starts_with(b"- ") {
            flush(&mut current, &mut entries);
            current.push(line);
        } else if !current.is_empty() && line.starts_with(b"  ") {
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
    String::from_utf8(append_to_section_bytes(raw.as_bytes(), entries)?)
        .map_err(|_| Error::new("log section is not UTF-8; use its byte representation".into()))
}

pub(crate) fn append_to_section_bytes(raw: &[u8], entries: &[LogEntry]) -> Result<Vec<u8>, Error> {
    if entries.is_empty() {
        return Ok(raw.into());
    }
    let mut lines: Vec<_> = raw.split_inclusive(|&b| b == b'\n').collect();
    if raw.ends_with(b"\n") || raw.is_empty() {
        lines.push(&[]);
    }
    let mut cut = lines.len();
    while cut > 1
        && super::yaml_string::YamlString::from_bytes(lines[cut - 1].into())
            .trimmed()
            .is_empty()
    {
        cut -= 1;
    }
    let mut output = lines[..cut].concat();
    if !output.ends_with(b"\n") {
        output.push(b'\n');
    }
    for entry in entries {
        output.extend_from_slice(&entry.line_bytes()?);
    }
    output.extend_from_slice(&lines[cut..].concat());
    Ok(output)
}

#[cfg(test)]
mod tests {
    #[test]
    fn production_log_tokens_match_fixed_go_whitespace_and_keep_invalid_octets() {
        let corpus: serde_json::Value =
            serde_json::from_str(include_str!("../../tests/contract/config-foundation.json"))
                .unwrap();
        let ranges: Vec<(u32, u32)> =
            serde_json::from_value(corpus["space_ranges"].clone()).unwrap();
        for cp in 0..=0x10ffff {
            let Some(ch) = char::from_u32(cp) else {
                continue;
            };
            let input = super::YamlString::from(format!(" A {ch} Z "));
            let expected = if ranges.iter().any(|&(start, end)| start <= cp && cp <= end) {
                b"A-Z".to_vec()
            } else {
                format!("A-{ch}-Z").into_bytes()
            };
            assert_eq!(super::token(&input), expected, "U+{cp:04X}");
        }
        for raw in [
            vec![255],
            vec![226, 130],
            vec![192, 175],
            vec![237, 160, 128],
            vec![0],
        ] {
            let mut input = b" \t".to_vec();
            input.extend_from_slice(&raw);
            input.extend_from_slice(b" \n");
            assert_eq!(super::token(&super::YamlString::from_bytes(input)), raw);
        }
        assert_eq!(super::token(&super::YamlString::default()), b"-");
        assert_eq!(
            super::token(&super::YamlString::from(" \t\n\u{3000}")),
            b"-"
        );
    }
}
