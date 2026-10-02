use crate::vault::{Index, Note, NoteKind};
use comrak::{
    Arena, Options,
    nodes::{AlertType, AstNode, NodeAlert, NodeLink, NodeValue},
};
use serde::Serialize;
use std::{collections::HashMap, fmt::Write};

#[derive(Clone)]
enum InlineRendering {
    Hashtag,
    Embed { html: String, block: bool },
}
#[derive(Default)]
struct RenderState {
    inline: HashMap<usize, InlineRendering>,
    toc: Vec<TocEntry>,
}
comrak::create_formatter!(NativeFormatter<RenderState>, {
    NodeValue::Heading(ref heading) => |context, node, entering| {
        let rendering = comrak::html::format_node_default(context, node, entering)?;
        if entering && let Some(id) = &context.current_anchorized_id {
            context.user.toc.push(TocEntry { level: heading.level, id: id.clone(), text: node.collect_text() });
        }
        return Ok(rendering);
    },
    NodeValue::Link(ref link) => |context, node, entering| {
        match context.user.inline.get(&(std::ptr::from_ref(node) as usize)).cloned() {
            None => return comrak::html::format_node_default(context, node, entering),
            Some(InlineRendering::Embed { html, .. }) => {
                if entering { context.write_str(&html)?; }
                return Ok(comrak::html::ChildRendering::Skip);
            },
            Some(InlineRendering::Hashtag) => {
                if entering {
                    context.write_str("<a class=\"hashtag\" href=\"")?;
                    comrak::html::escape_href(context, &link.url, false)?;
                    context.write_str("\">")?;
                } else { context.write_str("</a>")?; }
            },
        }
    },
    NodeValue::Paragraph => |context, node, entering| {
        let block = |n| matches!(context.user.inline.get(&(std::ptr::from_ref(n) as usize)), Some(InlineRendering::Embed { block: true, .. }));
        if !node.descendants().any(block) {
            return comrak::html::format_node_default(context, node, entering);
        }
        // Standalone block embeds need no paragraph. Mixed prose/blocks use a
        // block container, never a div embedded inside an HTML paragraph.
        if !node.first_child().zip(node.last_child()).is_some_and(|(a, b)| std::ptr::eq(a, b)) || !node.first_child().is_some_and(block) {
            context.write_str(if entering { "<div>" } else { "</div>\n" })?;
        }
    },
});
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
    let mut inline_nodes = expand_embeds(&arena, root, index, path, embeds);
    inline_nodes.extend(expand_hashtags(&arena, root));
    let mut html = String::new();
    let state = NativeFormatter::format_document(
        root,
        &options,
        &mut html,
        RenderState {
            inline: inline_nodes,
            toc: Vec::new(),
        },
    )
    .expect("render into memory");
    let toc = state.toc;
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

/// Insert ordinary link/text nodes into prose. Keeping visible text in the tree
/// lets the formatter escape attribute contexts and allocate every heading ID
/// and self-link from the same unchanged text and collision allocator.
fn expand_hashtags<'a>(
    arena: &'a Arena<'a>,
    root: &'a AstNode<'a>,
) -> HashMap<usize, InlineRendering> {
    let mut links = HashMap::new();
    // Snapshot traversal before insertion so generated link children are never
    // revisited; authored links and image-alt text are intentionally protected.
    for node in root.descendants().collect::<Vec<_>>() {
        if protected_text(node) {
            continue;
        }
        let source = match &node.data.borrow().value {
            NodeValue::Text(value) => value.to_string(),
            _ => continue,
        };
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
            if copied < start {
                node.insert_before(
                    arena.alloc(NodeValue::Text(source[copied..start].to_owned().into()).into()),
                );
            }
            let url = format!(
                "/search?q={}",
                encode(&source[start + 1..end]).replace('/', "%2F")
            );
            let link = arena.alloc(
                NodeValue::Link(Box::new(NodeLink {
                    url,
                    title: String::new(),
                }))
                .into(),
            );
            link.append(arena.alloc(NodeValue::Text(source[start..end].to_owned().into()).into()));
            node.insert_before(link);
            links.insert(std::ptr::from_ref(link) as usize, InlineRendering::Hashtag);
            copied = end;
        }
        if copied != 0 {
            if copied < source.len() {
                node.insert_before(
                    arena.alloc(NodeValue::Text(source[copied..].to_owned().into()).into()),
                );
            }
            node.detach();
        }
    }
    links
}

fn protected_text<'a>(node: &'a AstNode<'a>) -> bool {
    node.ancestors().skip(1).any(|parent| {
        matches!(
            parent.data.borrow().value,
            NodeValue::Link(_) | NodeValue::WikiLink(_) | NodeValue::Image(_)
        )
    })
}
fn expand_embeds<'a>(
    arena: &'a Arena<'a>,
    root: &'a AstNode<'a>,
    index: &Index,
    path: &str,
    embeds: bool,
) -> HashMap<usize, InlineRendering> {
    let mut nodes = HashMap::new();
    for node in root.descendants().collect::<Vec<_>>() {
        if protected_text(node) {
            continue;
        }
        let source = match &node.data.borrow().value {
            NodeValue::Text(value) => value.to_string(),
            _ => continue,
        };
        let heading = node
            .ancestors()
            .any(|parent| matches!(parent.data.borrow().value, NodeValue::Heading(_)));
        let mut copied = 0;
        let mut cursor = 0;
        while let Some(offset) = source[cursor..].find("![[") {
            let start = cursor + offset;
            let tail = &source[start + 3..];
            let Some(end) = tail.find("]]") else {
                break;
            };
            let (target, label) = tail[..end]
                .split_once('|')
                .unwrap_or((&tail[..end], &tail[..end]));
            let (target, fragment) = target.split_once('#').unwrap_or((target, ""));
            let note = index.lookup(target.as_bytes());
            let url = note
                .map(href)
                .unwrap_or_else(|| format!("/wiki/new?title={}", encode(target)));
            let (html, block) = if let Some(asset) = asset(index, path, target) {
                (
                    format!(
                        "<img class=\"embed\" src=\"/api/assets/{}\" alt=\"{}\" />",
                        encode(&asset),
                        escape(label)
                    ),
                    false,
                )
            } else if embeds
                && !heading
                && let Some(note) = note
            {
                let source = String::from_utf8_lossy(if note.graph.kind == NoteKind::Issue {
                    &note.description
                } else {
                    &note.body
                });
                let source = if fragment.is_empty() {
                    source.as_ref()
                } else {
                    section(&source, fragment)
                };
                (
                    format!(
                        "<div class=\"embed\">{}</div>",
                        render_inner(
                            source,
                            index,
                            &String::from_utf8_lossy(&note.graph.path),
                            false
                        )
                        .html
                    ),
                    true,
                )
            } else {
                // Heading children must remain phrasing content; render a link
                // rather than a block whose body would corrupt heading identity.
                (
                    format!(
                        "<a{} href=\"{}\">{}</a>",
                        if heading { " class=\"embed\"" } else { "" },
                        escape(&url),
                        escape(label)
                    ),
                    false,
                )
            };
            if copied < start {
                node.insert_before(
                    arena.alloc(NodeValue::Text(source[copied..start].to_owned().into()).into()),
                );
            }
            let link = arena.alloc(
                NodeValue::Link(Box::new(NodeLink {
                    url,
                    title: String::new(),
                }))
                .into(),
            );
            link.append(arena.alloc(NodeValue::Text(label.to_owned().into()).into()));
            node.insert_before(link);
            nodes.insert(
                std::ptr::from_ref(link) as usize,
                InlineRendering::Embed { html, block },
            );
            cursor = start + 3 + end + 2;
            copied = cursor;
        }
        if copied != 0 {
            if copied < source.len() {
                node.insert_before(
                    arena.alloc(NodeValue::Text(source[copied..].to_owned().into()).into()),
                );
            }
            node.detach();
        }
    }
    nodes
}
