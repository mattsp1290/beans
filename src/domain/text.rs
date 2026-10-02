//! Literal link and section rules shared by note codecs. These intentionally
//! follow the stored-format rules rather than a Markdown renderer's AST.
use serde::{Deserialize, Serialize};

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct Link {
    pub raw: String,
    pub target: String,
}

impl Link {
    pub fn parse(input: &str) -> Self {
        let raw = input.trim();
        let target = raw
            .strip_prefix("[[")
            .and_then(|text| text.strip_suffix("]]"))
            .map_or(raw, |text| {
                text.split(['|', '#']).next().unwrap_or_default().trim()
            });
        Self {
            raw: raw.to_owned(),
            target: target.to_owned(),
        }
    }

    pub fn new(target: &str) -> Self {
        Self {
            raw: format!("[[{target}]]"),
            target: target.to_owned(),
        }
    }

    pub fn is_zero(&self) -> bool {
        self.raw.is_empty() && self.target.is_empty()
    }
}

#[derive(Debug, PartialEq, Eq, Serialize)]
pub struct IssueBody<'a> {
    pub description: &'a str,
    pub body: &'a str,
    pub log: &'a str,
    pub tail: &'a str,
}

#[derive(Debug, PartialEq, Eq, Serialize)]
pub struct RequestBody<'a> {
    pub before_log: &'a str,
    pub log: &'a str,
    pub tail: &'a str,
}

#[derive(Default)]
struct Sections {
    first: Option<usize>,
    log: Option<usize>,
    end: Option<usize>,
    closed: bool,
}

fn scan(text: &str, honor_fences: bool) -> Sections {
    let mut sections = Sections::default();
    let mut in_fence = false;
    let mut offset = 0;
    for line in text.split_inclusive('\n') {
        let content = line.strip_suffix('\n').unwrap_or(line);
        let trimmed = content.trim_start_matches(' ');
        let fence = content.len() - trimmed.len() <= 3
            && (trimmed.starts_with("```") || trimmed.starts_with("~~~"));
        if honor_fences && fence {
            in_fence = !in_fence;
        } else if !in_fence && content.starts_with("## ") {
            sections.first.get_or_insert(offset);
            if sections.log.is_none() && content.trim_end_matches([' ', '\t']) == "## Log" {
                sections.log = Some(offset);
            } else if sections.log.is_some() && sections.end.is_none() {
                sections.end = Some(offset);
            }
        }
        offset += line.len();
    }
    sections.closed = !in_fence;
    sections
}

fn sections(text: &str) -> Sections {
    let fenced = scan(text, true);
    if fenced.closed {
        fenced
    } else {
        scan(text, false)
    }
}

pub fn split_issue_body(text: &str) -> IssueBody<'_> {
    let sections = sections(text);
    let first = sections.first.unwrap_or(text.len());
    let log = sections.log.unwrap_or(text.len());
    let end = sections.end.unwrap_or(text.len());
    IssueBody {
        description: &text[..first],
        body: &text[first..log],
        log: &text[log..end],
        tail: &text[end..],
    }
}

pub fn split_request_body(text: &str) -> RequestBody<'_> {
    let sections = sections(text);
    let log = sections.log.unwrap_or(text.len());
    let end = sections.end.unwrap_or(text.len());
    RequestBody {
        before_log: &text[..log],
        log: &text[log..end],
        tail: &text[end..],
    }
}
