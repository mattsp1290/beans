use crate::domain::yaml_string::YamlString;
use comrak::{Arena, Options, nodes::NodeValue, parse_document};
use std::collections::HashSet;
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Link {
    pub target: Vec<u8>,
    pub fragment: Vec<u8>,
    pub alias: Vec<u8>,
    pub embed: bool,
}
fn display(bytes: &[u8]) -> String {
    // Goldmark's reader only splits on LF. Comrak also splits on lone CR;
    // mask those bytes in the context view without changing byte offsets.
    let bytes = bytes
        .iter()
        .enumerate()
        .map(|(i, &b)| {
            if b == b'\r' && bytes.get(i + 1) != Some(&b'\n') {
                1
            } else {
                b
            }
        })
        .collect::<Vec<_>>();
    YamlString::from_bytes(bytes).to_string()
}
fn body(source: &[u8]) -> &[u8] {
    let mut lines = source.split_inclusive(|&b| b == b'\n');
    let mut first = lines.next().unwrap_or_default();
    let mut prefix = 0;
    if first
        .iter()
        .all(|b| matches!(b, b' ' | b'\t' | b'\r' | b'\n'))
    {
        prefix = first.len();
        first = lines.next().unwrap_or_default();
    }
    let delimiter = first.strip_suffix(b"\n").unwrap_or(first);
    let delimiter = delimiter.strip_suffix(b"\r").unwrap_or(delimiter);
    if delimiter.len() < 3
        || !matches!(delimiter[0], b'-' | b'+')
        || !delimiter.iter().all(|b| *b == delimiter[0])
    {
        return source;
    }
    let mut consumed = prefix + first.len();
    for line in lines {
        consumed += line.len();
        let trimmed = line.strip_suffix(b"\n").unwrap_or(line);
        let trimmed = trimmed.strip_suffix(b"\r").unwrap_or(trimmed);
        if trimmed == delimiter {
            return &source[consumed..];
        }
    }
    b""
}
fn options() -> Options<'static> {
    let mut options = Options::default();
    options.extension.table = true;
    options.extension.strikethrough = true;
    options.extension.tasklist = true;
    options.extension.autolink = true;
    options.extension.wikilinks_title_after_pipe = true;
    options
}
fn offset(source: &str, line: usize, column: usize) -> usize {
    let start = source
        .split_inclusive('\n')
        .take(line.saturating_sub(1))
        .map(str::len)
        .sum::<usize>();
    (start + column.saturating_sub(1)).min(source.len())
}
fn excluded(source: &str, at: usize, width: usize) -> bool {
    let arena = Arena::new();
    let options = options();
    let root = parse_document(&arena, source, &options);
    root.descendants().any(|node| {
        let data = node.data.borrow();
        let pos = data.sourcepos;
        let start = offset(source, pos.start.line, pos.start.column);
        let end = offset(source, pos.end.line, pos.end.column);
        if matches!(data.value, NodeValue::TableCell) && start <= at && at <= end {
            return at + width > end + 1;
        }
        let autolink = matches!(data.value, NodeValue::Link(..))
            && source.as_bytes().get(start) == Some(&b'<');
        if matches!(data.value, NodeValue::Link(..) | NodeValue::Image(..))
            && start < at
            && at <= end
        {
            // Children are the displayed label. Destination/title bytes are
            // consumed by the ordinary link parser, not the wikilink parser.
            let in_label = node.children().any(|child| {
                let pos = child.data.borrow().sourcepos;
                offset(source, pos.start.line, pos.start.column) <= at
                    && at <= offset(source, pos.end.line, pos.end.column)
            });
            if !in_label {
                return true;
            }
        }
        if !autolink
            && !matches!(
                data.value,
                NodeValue::Code(..)
                    | NodeValue::CodeBlock(..)
                    | NodeValue::HtmlBlock(..)
                    | NodeValue::HtmlInline(..)
            )
        {
            return false;
        }
        start <= at && at <= end
    })
}
/// Raw link fields come from the Go-compatible inline grammar. Temporary
/// markers let the Markdown parser handle enclosing block and inline context.
/// Caller bytes are never changed or serialized by this adapter.
pub fn links(source: &[u8]) -> Vec<Link> {
    let source = body(source);
    let mut parsed = display(source);
    let mut marker = "beans_index_wikilink_".to_owned();
    while parsed.contains(&marker) {
        marker.push('_');
    }
    let mut candidates = Vec::new();
    let mut shift = 0isize;
    let mut i = 0;
    while i < source.len() {
        let (open, embed) = if source[i..].starts_with(b"![[") {
            (3, true)
        } else if source[i..].starts_with(b"[[") {
            (2, false)
        } else {
            i += 1;
            continue;
        };
        let escapes = source[..i]
            .iter()
            .rev()
            .take_while(|&&b| b == b'\\')
            .count();
        if escapes % 2 == 1 {
            i += 1;
            continue;
        }
        let end_line = source[i..]
            .iter()
            .position(|&b| b == b'\n')
            .map_or(source.len(), |n| i + n);
        let Some(close) = source[i..end_line].windows(2).position(|b| b == b"]]") else {
            i += 1;
            continue;
        };
        if close < open {
            i += 1;
            continue;
        }
        let mut target = &source[i + open..i + close];
        let label = if let Some(pipe) = target.iter().position(|&b| b == b'|') {
            let label = &target[pipe + 1..];
            target = &target[..pipe];
            label
        } else {
            target
        };
        if target.is_empty() || label.is_empty() {
            i += 1;
            continue;
        }
        let end = i + close + 2;
        let start = (display(&source[..i]).len() as isize + shift) as usize;
        let width = display(&source[i..end]).len();
        if excluded(&parsed, start, width) {
            i = end;
            continue;
        }
        let fragment = if let Some(hash) = target.iter().rposition(|&b| b == b'#') {
            let fragment = &target[hash + 1..];
            target = &target[..hash];
            fragment
        } else {
            b""
        };
        let default = if fragment.is_empty() {
            target.to_vec()
        } else {
            [target, b"#", fragment].concat()
        };
        let link = Link {
            target: target.into(),
            fragment: fragment.into(),
            alias: if label == default {
                Vec::new()
            } else {
                label.into()
            },
            embed,
        };
        let replacement = format!("[[{marker}{}]]", candidates.len());
        parsed.replace_range(start..start + width, &replacement);
        shift += replacement.len() as isize - width as isize;
        candidates.push(link);
        i = end;
    }
    let mut seen = HashSet::new();
    candidates
        .into_iter()
        .filter(|link| seen.insert((link.target.clone(), link.fragment.clone(), link.embed)))
        .collect()
}
