use crate::vault::{Index, Note, NoteKind};
use comrak::{
    Arena, Options,
    nodes::{AlertType, NodeAlert, NodeValue},
};
use serde::Serialize;
#[derive(Serialize)]
pub struct TocEntry {
    pub level: u8,
    pub id: String,
    pub text: String,
}
pub struct Rendered {
    pub html: String,
    pub toc: Vec<TocEntry>,
}
pub fn encode(path: &str) -> String {
    path.bytes()
        .map(|b| {
            if b.is_ascii_alphanumeric() || b"-_.~/".contains(&b) {
                (b as char).to_string()
            } else {
                format!("%{b:02X}")
            }
        })
        .collect()
}
fn text<'a>(node: &'a comrak::nodes::AstNode<'a>) -> String {
    node.descendants()
        .filter_map(|n| match &n.data.borrow().value {
            NodeValue::Text(s) => Some(s.to_string()),
            NodeValue::Code(c) => Some(c.literal.clone()),
            _ => None,
        })
        .collect()
}
fn href(note: &Note) -> String {
    let id = String::from_utf8_lossy(note.graph.id.as_deref().unwrap_or_default());
    match note.graph.kind {
        NoteKind::Issue => format!("/issues/{}", encode(&id)),
        NoteKind::Request => format!("/requests/{}", encode(&id)),
        NoteKind::Plan => format!("/plans/{}", encode(&id)),
        NoteKind::Memory => format!(
            "/search?q={}",
            encode(&String::from_utf8_lossy(&note.graph.basename))
        ),
        _ => format!(
            "/wiki/{}",
            encode(String::from_utf8_lossy(&note.graph.path).trim_end_matches(".md"))
        ),
    }
}
fn asset(index: &Index, path: &str, target: &str) -> Option<String> {
    let relative = path
        .rsplit_once('/')
        .map(|(p, _)| format!("{p}/{target}"))
        .unwrap_or_default();
    [target.to_owned(), relative]
        .into_iter()
        .find(|a| index.assets.contains(a.as_bytes()))
        .or_else(|| {
            index
                .assets
                .iter()
                .find(|a| a.rsplit(|b| *b == b'/').next() == Some(target.as_bytes()))
                .map(|a| String::from_utf8_lossy(a).into_owned())
        })
}
pub fn render(source: &str, index: &Index, path: &str) -> Rendered {
    render_inner(source, index, path, true)
}
fn render_inner(source: &str, index: &Index, path: &str, embeds: bool) -> Rendered {
    let mut options = Options::default();
    options.extension.front_matter_delimiter = Some("---".into());
    options.extension.table = true;
    options.extension.tasklist = true;
    options.extension.strikethrough = true;
    options.extension.footnotes = true;
    options.extension.wikilinks_title_after_pipe = true;
    options.extension.highlight = true;
    options.extension.alerts = false;
    options.extension.header_id_prefix = Some(String::new());
    let arena = Arena::new();
    let root = comrak::parse_document(&arena, source, &options);
    callouts(root);
    let mut replacements = Vec::new();
    for node in root.descendants() {
        let mut data = node.data.borrow_mut();

        match &mut data.value {
            NodeValue::WikiLink(link) => {
                let original = link.url.clone();
                let (target, fragment) = original
                    .split_once('#')
                    .map_or((original.as_str(), ""), |(t, f)| (t, f));
                let note = index.lookup(target.as_bytes());
                link.url = note
                    .map(href)
                    .unwrap_or_else(|| format!("/wiki/new?title={}", encode(target)));
                if !fragment.is_empty() {
                    link.url.push('#');
                    link.url
                        .push_str(&encode(&comrak::Anchorizer::new().anchorize(fragment)));
                }
            }
            NodeValue::Text(value) if value.contains("![[") => {
                let mut rest = value.as_ref();
                let mut expanded = String::new();
                while let Some(start) = rest.find("![[") {
                    expanded.push_str(&rest[..start]);
                    let tail = &rest[start + 3..];
                    let Some(end) = tail.find("]]") else {
                        expanded.push_str(&rest[start..]);
                        rest = "";
                        break;
                    };
                    let (target, label) = tail[..end]
                        .split_once('|')
                        .unwrap_or((&tail[..end], &tail[..end]));
                    let (target, fragment) = target.split_once('#').unwrap_or((target, ""));
                    let rendered = if let Some(asset) = asset(index, path, target) {
                        format!(
                            "<img class=\"embed\" src=\"/api/assets/{}\" alt=\"{}\" />",
                            encode(&asset),
                            escape(label)
                        )
                    } else if embeds && let Some(note) = index.lookup(target.as_bytes()) {
                        let source =
                            String::from_utf8_lossy(if note.graph.kind == NoteKind::Issue {
                                &note.description
                            } else {
                                &note.body
                            });
                        let source = if fragment.is_empty() {
                            source.as_ref()
                        } else {
                            section(&source, fragment)
                        };
                        format!(
                            "<div class=\"embed\">{}</div>",
                            render_inner(
                                source,
                                index,
                                &String::from_utf8_lossy(&note.graph.path),
                                false
                            )
                            .html
                        )
                    } else {
                        let url = index
                            .lookup(target.as_bytes())
                            .map(href)
                            .unwrap_or_else(|| format!("/wiki/new?title={}", encode(target)));
                        format!("<a href=\"{}\">{}</a>", escape(&url), escape(label))
                    };
                    let marker = format!("BNEMBED{}END", replacements.len());
                    replacements.push((marker.clone(), rendered));
                    expanded.push_str(&marker);
                    rest = &tail[end + 2..];
                }
                expanded.push_str(rest);
                *value = expanded.into();
            }
            NodeValue::Image(link) if !link.url.contains(':') && !link.url.starts_with('/') => {
                if let Some(target) = local_destination(&link.url)
                    && let Some(asset) = asset(index, path, &target)
                {
                    link.url = format!("/api/assets/{}", encode(&asset));
                }
            }
            _ => (),
        }
    }
    let headings: Vec<_> = root
        .descendants()
        .filter_map(|n| match n.data.borrow().value {
            NodeValue::Heading(h) => Some((h.level, text(n))),
            _ => None,
        })
        .collect();
    // Expand hashtags only in parsed prose text, never inside links or code.
    // Capture heading text first so generated markers cannot leak into the TOC.
    for node in root.descendants() {
        if node.ancestors().skip(1).any(|parent| {
            matches!(
                parent.data.borrow().value,
                NodeValue::Link(_) | NodeValue::WikiLink(_) | NodeValue::Image(_)
            )
        }) {
            continue;
        }
        if let NodeValue::Text(value) = &mut node.data.borrow_mut().value {
            *value = hashtags(value, &mut replacements).into();
        }
    }
    let mut html = String::new();
    comrak::format_html(root, &options, &mut html).expect("render into memory");
    let mut toc = Vec::new();
    let mut pos = 0;
    let mut anchorizer = comrak::Anchorizer::new();
    for (level, text) in headings {
        let original_id = anchorizer.anchorize(&text);
        if let Some(start) = html[pos..].find(&format!("<h{level} id=\"")) {
            let start = pos + start + format!("<h{level} id=\"").len();
            if let Some(mut end) = html[start..].find('"') {
                // Hashtag display markers must not alter stable heading anchors.
                if html[start..start + end].contains("bnhashtag") {
                    html.replace_range(start..start + end, &original_id);
                    end = original_id.len();
                }
                toc.push(TocEntry {
                    level,
                    id: html[start..start + end].into(),
                    text,
                });
                pos = start + end;
            }
        }
    }
    html = html.replace(
        "data-wikilink=\"true\"",
        "data-wikilink=\"true\" class=\"wikilink\"",
    );
    for kind in ["note", "tip", "important", "warning", "caution"] {
        html = html.replace(
            &format!("class=\"markdown-alert markdown-alert-{kind}\""),
            &format!(
                "class=\"callout markdown-alert markdown-alert-{kind}\" data-callout=\"{kind}\""
            ),
        );
    }
    html = html.replace(
        "class=\"markdown-alert-title\"",
        "class=\"callout-title markdown-alert-title\"",
    );
    for (marker, rendered) in replacements {
        html = html
            .replace(&format!("<p>{marker}</p>"), &rendered)
            .replace(&marker, &rendered);
    }
    Rendered { html, toc }
}
fn section<'a>(source: &'a str, fragment: &str) -> &'a str {
    let mut start = None;
    let mut offset = 0;
    for line in source.split_inclusive('\n') {
        let trimmed = line.trim_end();
        let level = trimmed.bytes().take_while(|b| *b == b'#').count();
        if level > 0 && trimmed.as_bytes().get(level) == Some(&b' ') {
            let title = trimmed[level + 1..].trim();
            if let Some((from, depth)) = start {
                if level <= depth {
                    return &source[from..offset];
                }
            } else if title.eq_ignore_ascii_case(fragment)
                || title.to_lowercase().replace(' ', "-") == fragment
            {
                start = Some((offset, level));
            }
        }
        offset += line.len();
    }
    start.map_or("", |(from, _)| &source[from..])
}

fn escape(value: &str) -> String {
    value
        .replace('&', "&amp;")
        .replace('<', "&lt;")
        .replace('>', "&gt;")
        .replace('\"', "&quot;")
}

fn callouts<'a>(root: &'a comrak::nodes::AstNode<'a>) {
    let nodes: Vec<_> = root.descendants().collect();
    for node in nodes {
        if !matches!(node.data.borrow().value, NodeValue::BlockQuote) {
            continue;
        }
        let Some(paragraph) = node.first_child() else {
            continue;
        };
        if !matches!(paragraph.data.borrow().value, NodeValue::Paragraph) {
            continue;
        }
        let mut firstline = Vec::new();
        for child in paragraph.children() {
            firstline.push(child);
            if matches!(
                child.data.borrow().value,
                NodeValue::SoftBreak | NodeValue::LineBreak
            ) {
                break;
            }
        }
        let line: String = firstline.iter().map(|n| text(n)).collect();
        let Some(rest) = line.strip_prefix("[!") else {
            continue;
        };
        let Some((kind, title)) = rest.split_once(']') else {
            continue;
        };
        if kind.is_empty() || !kind.chars().all(|c| c.is_ascii_alphanumeric() || c == '-') {
            continue;
        }
        let alert_type = match kind.to_ascii_lowercase().as_str() {
            "tip" => AlertType::Tip,
            "important" => AlertType::Important,
            "warning" => AlertType::Warning,
            "danger" | "caution" => AlertType::Caution,
            _ => AlertType::Note,
        };
        let title = title.trim_start_matches(['+', '-']).trim();
        node.data.borrow_mut().value = NodeValue::Alert(Box::new(NodeAlert {
            alert_type,
            title: Some(if title.is_empty() {
                kind.to_owned()
            } else {
                title.to_owned()
            }),
            multiline: false,
            fence_length: 0,
            fence_offset: 0,
        }));
        for child in firstline {
            child.detach();
        }
        if paragraph.first_child().is_none() {
            paragraph.detach();
        }
    }
}

// Markdown destinations are URL text; decode exactly once before index lookup.
// Reject traversal and encoded separators even if basename lookup could resolve.
fn local_destination(url: &str) -> Option<String> {
    let mut bytes = Vec::new();
    let mut input = url.bytes();
    while let Some(byte) = input.next() {
        if byte == b'%' {
            let high = (input.next()? as char).to_digit(16)?;
            let low = (input.next()? as char).to_digit(16)?;
            let decoded = (high * 16 + low) as u8;
            if matches!(decoded, b'/' | b'\\') {
                return None;
            }
            bytes.push(decoded);
        } else {
            bytes.push(byte);
        }
    }
    let target = String::from_utf8(bytes).ok()?;
    if !crate::vault::valid_public_path(&target) || target.contains(':') {
        return None;
    }
    Some(target)
}

fn hashtags(source: &str, replacements: &mut Vec<(String, String)>) -> String {
    let mut output = String::new();
    let mut copied = 0;
    for (start, ch) in source.char_indices() {
        if ch != '#'
            || source[..start]
                .chars()
                .next_back()
                .is_some_and(|c| c.is_alphanumeric() || matches!(c, '_' | '/' | '#'))
        {
            continue;
        }
        let tail = &source[start + 1..];
        let length = tail
            .char_indices()
            .take_while(|(_, c)| c.is_alphanumeric() || matches!(c, '_' | '-' | '/'))
            .map(|(i, c)| i + c.len_utf8())
            .last()
            .unwrap_or(0);
        if length == 0 {
            continue;
        }
        let end = start + 1 + length;
        let tag = &source[start + 1..end];
        output.push_str(&source[copied..start]);
        let marker = format!("BNHASHTAG{}END", replacements.len());
        let url = encode(tag).replace('/', "%2F");
        replacements.push((
            marker.clone(),
            format!(
                "<a class=\"hashtag\" href=\"/search?q={url}\">#{}</a>",
                escape(tag)
            ),
        ));
        output.push_str(&marker);
        copied = end;
    }
    output.push_str(&source[copied..]);
    output
}
