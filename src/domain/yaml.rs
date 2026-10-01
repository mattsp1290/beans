use super::frontmatter::{Error, Node, NodeKind};
use yaml_rust2::parser::{Event, Parser, Tag};
use yaml_rust2::scanner::{Scanner, TScalarStyle, TokenType};

fn resolved_tag(tag: Option<&Tag>, default: &str) -> String {
    let Some(tag) = tag else {
        return default.to_owned();
    };
    let value = tag.handle.clone() + &tag.suffix;
    if value == "!" {
        return default.to_owned();
    }
    if let Some(short) = value.strip_prefix("tag:yaml.org,2002:") {
        format!("!!{short}")
    } else {
        value
    }
}

fn scalar_null(value: &str, style: TScalarStyle, tag: Option<&Tag>) -> bool {
    if let Some(tag) = tag {
        return tag.handle == "tag:yaml.org,2002:" && tag.suffix == "null";
    }
    style == TScalarStyle::Plain && matches!(value, "" | "~" | "null" | "Null" | "NULL")
}

pub(crate) fn parse_raw(path: &str, text: &str, raw: &[u8]) -> Result<Node, Error> {
    parse_optional_inner(path, text, raw, None)?
        .ok_or_else(|| Error::new(format!("{path}: line 2: frontmatter is empty")))
}

pub(crate) fn parse_optional(path: &str, text: &str) -> Result<Option<Node>, Error> {
    parse_optional_inner(path, text, text.as_bytes(), None)
}
type SyntaxFormatter = fn(&yaml_rust2::scanner::ScanError, &[Node]) -> Error;
pub(crate) fn parse_optional_with_syntax(
    path: &str,
    text: &str,
    syntax: SyntaxFormatter,
) -> Result<Option<Node>, Error> {
    parse_optional_inner(path, text, text.as_bytes(), Some(syntax))
}
fn parse_optional_inner(
    path: &str,
    text: &str,
    raw: &[u8],
    syntax: Option<SyntaxFormatter>,
) -> Result<Option<Node>, Error> {
    let mut reader = super::yaml_reader::Reader::new_raw(text, raw);
    reader.check(path, 1, 0)?;
    let adapted = adapt_quote_indentation(text);
    let adapted = if syntax.is_some() {
        adapt_alias_keys(&adapted)
    } else {
        adapted
    };
    let mut parser = Parser::new_from_str(&adapted);
    let mut positions = SourcePositions::scan(&adapted);
    let mut stack: Vec<Node> = Vec::new();
    let mut root = None;
    loop {
        let (event, marker) = parser.next_token().map_err(|error| {
            if let Err(reader_error) =
                reader.check(path, error.marker().line(), error.marker().col())
            {
                return reader_error;
            }
            if error.info() == "while parsing a node, did not find expected node content" {
                if let Err(reader_error) = reader.error_lookahead(
                    path,
                    &adapted,
                    error.marker().line(),
                    error.marker().col(),
                ) {
                    return reader_error;
                }
                if let Some(syntax) = syntax {
                    return syntax(&error, &stack);
                }
                // yaml.v3 parse_node uses the attempted node's token mark,
                // not the enclosing collection's opening mark.
                let line = error.marker().line().saturating_sub(1);
                let location = if line > 0 {
                    format!("line {line}: ")
                } else {
                    String::new()
                };
                return Error::new(format!(
                    "{path}: frontmatter: yaml: {location}did not find expected node content"
                ));
            }
            if error.info() == "while parsing a block mapping, did not find expected key"
                || (error.info() == "wrongly indented line in block scalar"
                    && stack
                        .last()
                        .is_some_and(|node| node.kind == NodeKind::Mapping))
            {
                if let Err(reader_error) = reader.error_lookahead(
                    path,
                    &adapted,
                    error.marker().line(),
                    error.marker().col(),
                ) {
                    return reader_error;
                }
                // yaml.v3 reports the parser context's zero-based line when
                // nonzero, falling back to the problem mark at the root.
                let line = stack
                    .last()
                    .map_or(0, |node: &Node| node.line.saturating_sub(1));
                let line = if line > 0 {
                    line
                } else {
                    error.marker().line().saturating_sub(1)
                };
                let where_ = if line > 0 {
                    format!("line {line}: ")
                } else {
                    String::new()
                };
                return Error::new(format!(
                    "{path}: frontmatter: yaml: {where_}did not find expected key"
                ));
            }
            if error.info() == "while parsing node, found unknown anchor" {
                for token in Scanner::new(adapted.chars()) {
                    if token.0.index() == error.marker().index()
                        && let TokenType::Alias(name) = token.1
                    {
                        return Error::new(format!(
                            "{path}: frontmatter: yaml: unknown anchor '{name}' referenced"
                        ));
                    }
                }
            }
            if error.info() == "simple key expected" {
                return Error::new(format!(
                    "{path}: frontmatter: yaml: line {}: could not find expected ':'",
                    error.marker().line().saturating_sub(1)
                ));
            }
            // Exact yaml.v3 syntax-error presentation is completed alongside
            // the typed codec differential corpus; scanner text is retained.
            if let Some(syntax) = syntax {
                return syntax(&error, &stack);
            }
            Error::new(format!("{path}: frontmatter: {error}"))
        })?;
        reader.check(path, marker.line(), marker.col())?;
        let node = match event {
            Event::MappingStart(anchor, ref tag) | Event::SequenceStart(anchor, ref tag) => {
                stack.push(Node {
                    anchor_id: anchor,
                    anchor_name: positions
                        .anchor_names
                        .get(anchor.wrapping_sub(1))
                        .cloned()
                        .unwrap_or_default(),
                    tag: resolved_tag(
                        tag.as_ref(),
                        if matches!(event, Event::MappingStart(..)) {
                            "!!map"
                        } else {
                            "!!seq"
                        },
                    ),
                    kind: if matches!(event, Event::MappingStart(..)) {
                        NodeKind::Mapping
                    } else {
                        NodeKind::Sequence
                    },
                    line: positions.node_line(marker.line(), anchor, tag.is_some()),
                    value: None,
                    null: None,
                    children: Vec::new(),
                });
                continue;
            }
            Event::MappingEnd | Event::SequenceEnd => stack
                .pop()
                .ok_or_else(|| Error::new(format!("{path}: invalid YAML container")))?,
            Event::Scalar(mut value, style, anchor, tag) => {
                if matches!(
                    style,
                    TScalarStyle::SingleQuoted | TScalarStyle::DoubleQuoted
                ) && let Some(Some(adapted)) = positions.quoted_values.pop_front()
                {
                    value = adapted;
                }
                let null = scalar_null(&value, style, tag.as_ref());
                // This release marks nonempty block scalars at their first
                // content character rather than at |/>. Recover the header
                // from the scanner token interval; do not guess by subtracting
                // one line, since leading blank lines and split values exist.
                let line = if matches!(style, TScalarStyle::Literal | TScalarStyle::Folded) {
                    let header = positions.block_headers.pop_front().ok_or_else(|| {
                        Error::new(format!("{path}: could not locate block scalar header"))
                    })?;
                    // The scanner supplies a virtual LF at EOF for an empty
                    // keep-chomp block. yaml.v3 retains only physical blank
                    // content lines; a header alone has an empty value.
                    if !value.is_empty()
                        && value.chars().all(|ch| ch == '\n')
                        && text.split_inclusive('\n').nth(header).is_none()
                    {
                        value.pop();
                    }
                    header
                } else {
                    marker.line()
                };
                let line = if value.is_empty()
                    && style == TScalarStyle::Plain
                    && stack.last().is_some_and(|node| {
                        node.kind == NodeKind::Mapping && node.children.len() % 2 == 1
                    }) {
                    positions.empty_values.pop_front().unwrap_or(line)
                } else {
                    line
                };
                let line = positions.node_line(line, anchor, tag.is_some());
                Node {
                    anchor_id: anchor,
                    anchor_name: positions
                        .anchor_names
                        .get(anchor.wrapping_sub(1))
                        .cloned()
                        .unwrap_or_default(),
                    tag: resolved_tag(
                        tag.as_ref(),
                        if style == TScalarStyle::Plain {
                            if value == "<<" {
                                "!!merge"
                            } else {
                                super::yaml_render::implicit_tag(&value)
                            }
                        } else {
                            "!!str"
                        },
                    ),
                    kind: NodeKind::Scalar,
                    line,
                    value: Some(value),
                    null: Some(null),
                    children: Vec::new(),
                }
            }
            Event::Alias(anchor_id) => Node {
                anchor_id,
                anchor_name: positions.alias_names.pop_front().unwrap_or_default(),
                tag: String::new(),
                kind: NodeKind::Alias,
                line: marker.line(),
                value: None,
                null: None,
                children: Vec::new(),
            },
            Event::DocumentEnd => {
                reader.document_end(path, &adapted, marker.line(), marker.col())?;
                break;
            }
            Event::StreamEnd => break,
            _ => continue,
        };
        if let Some(parent) = stack.last_mut() {
            parent.children.push(node);
        } else {
            root = Some(node);
        }
    }
    Ok(root)
}

// yaml.v3 allows a multiline quoted scalar's closing quote at column zero.
// yaml-rust2 rejects it before emitting the scalar token. Normalize only this
// parser representation, guided by the scanner's opening marker and a complete
// quoted lexeme; physical line numbers and retained document bytes stay intact.
fn adapt_quote_indentation(text: &str) -> String {
    let mut adapted = text.to_owned();
    loop {
        let mut scanner = Scanner::new(adapted.chars());
        for _ in scanner.by_ref() {}
        let Some(error) = scanner.get_error() else {
            break;
        };
        if error.info() != "invalid indentation in quoted scalar" {
            break;
        }
        let marker = error.marker();
        let Some(line) = adapted.split_inclusive('\n').nth(marker.line() - 1) else {
            break;
        };
        let before: usize = adapted
            .split_inclusive('\n')
            .take(marker.line() - 1)
            .map(str::len)
            .sum();
        let Some((column, _)) = line.char_indices().nth(marker.col()) else {
            break;
        };
        let start = before + column;
        let Some(end) = quoted_end(&adapted[start..]) else {
            break;
        };
        let closing = start + end - 1;
        let line_start = adapted[..closing].rfind('\n').map_or(0, |index| index + 1);
        if line_start <= start
            || !adapted[line_start..closing]
                .chars()
                .all(|ch| matches!(ch, ' ' | '\t'))
        {
            break;
        }
        let required = marker.col().max(1);
        if closing - line_start >= required {
            break;
        }
        adapted.insert_str(line_start, &" ".repeat(required - (closing - line_start)));
    }
    adapted
}

fn quoted_end(source: &str) -> Option<usize> {
    let quote = source.chars().next()?;
    if !matches!(quote, '\'' | '"') {
        return None;
    }
    let mut chars = source.char_indices().peekable();
    chars.next();
    while let Some((byte, ch)) = chars.next() {
        if quote == '"' && ch == '\\' {
            chars.next();
            continue;
        }
        if ch == quote {
            if quote == '\'' && chars.peek().is_some_and(|(_, next)| *next == '\'') {
                chars.next();
            } else {
                return Some(byte + ch.len_utf8());
            }
        }
    }
    None
}

struct SourcePositions {
    block_headers: std::collections::VecDeque<usize>,
    quoted_values: std::collections::VecDeque<Option<String>>,
    anchors: Vec<usize>,
    anchor_names: Vec<String>,
    alias_names: std::collections::VecDeque<String>,
    tags: std::collections::VecDeque<usize>,
    empty_values: std::collections::VecDeque<usize>,
}

impl SourcePositions {
    fn node_line(&mut self, line: usize, anchor: usize, tagged: bool) -> usize {
        let mut first = line;
        if anchor > 0
            && let Some(position) = self.anchors.get(anchor - 1)
        {
            first = first.min(*position);
        }
        if tagged && let Some(position) = self.tags.pop_front() {
            first = first.min(position);
        }
        first
    }

    fn scan(text: &str) -> Self {
        let lines: Vec<_> = text.lines().collect();
        let mut headers = Vec::new();
        let mut quoted_values = Vec::new();
        let mut anchors = Vec::new();
        let mut anchor_names = Vec::new();
        let mut alias_names = Vec::new();
        let mut tags = Vec::new();
        let mut empty_values = Vec::new();
        let mut pending_value = None;
        let mut previous_line = 1;
        let mut previous_column = 0;
        for token in Scanner::new(text.chars()) {
            match &token.1 {
                TokenType::Key
                | TokenType::BlockEnd
                | TokenType::FlowMappingEnd
                | TokenType::FlowEntry
                | TokenType::StreamEnd
                | TokenType::DocumentEnd => {
                    if let Some(line) = pending_value.take() {
                        empty_values.push(line);
                    }
                }
                TokenType::Value => pending_value = Some(token.0.line()),
                TokenType::Scalar(..)
                | TokenType::Alias(_)
                | TokenType::BlockMappingStart
                | TokenType::FlowMappingStart
                | TokenType::BlockSequenceStart
                | TokenType::FlowSequenceStart => pending_value = None,
                _ => {}
            }
            if let TokenType::Scalar(TScalarStyle::SingleQuoted | TScalarStyle::DoubleQuoted, _) =
                &token.1
            {
                quoted_values.push(adapt_quoted_unicode_breaks(
                    text,
                    token.0.line(),
                    token.0.col(),
                ));
            }
            match &token.1 {
                TokenType::Anchor(name) => {
                    anchors.push(token.0.line());
                    anchor_names.push(name.clone());
                }
                TokenType::Alias(name) => alias_names.push(name.clone()),
                TokenType::Tag(..) => tags.push(token.0.line()),
                _ => {}
            }
            if let TokenType::Scalar(style @ (TScalarStyle::Literal | TScalarStyle::Folded), _) =
                token.1
            {
                let indicator = if style == TScalarStyle::Literal {
                    '|'
                } else {
                    '>'
                };
                'header: for line in previous_line..=token.0.line().min(lines.len()) {
                    let original = lines[line - 1];
                    let start = if line == previous_line {
                        original
                            .char_indices()
                            .nth(previous_column)
                            .map_or(original.len(), |(byte, _)| byte)
                    } else {
                        0
                    };
                    let fragment = &original[start..];
                    let comment = fragment
                        .char_indices()
                        .find(|&(byte, ch)| {
                            ch == '#'
                                && (byte == 0 || fragment[..byte].ends_with(char::is_whitespace))
                        })
                        .map_or(fragment.len(), |(byte, _)| byte);
                    let header = &fragment[..comment];
                    for (byte, ch) in header.char_indices() {
                        if ch == indicator
                            && header[byte + 1..].chars().all(|ch| {
                                ch.is_ascii_digit() || matches!(ch, '+' | '-') || ch.is_whitespace()
                            })
                        {
                            headers.push(line);
                            break 'header;
                        }
                    }
                }
            }
            previous_line = token.0.line();
            previous_column = token.0.col();
        }
        Self {
            block_headers: headers.into(),
            quoted_values: quoted_values.into(),
            anchors,
            anchor_names,
            alias_names: alias_names.into(),
            tags: tags.into(),
            empty_values: empty_values.into(),
        }
    }
}
fn adapt_alias_keys(text: &str) -> String {
    let mut positions = Vec::new();
    for token in Scanner::new(text.chars()) {
        if let TokenType::Alias(name) = token.1
            && let Some(bare) = name.strip_suffix(':')
            && !bare.is_empty()
            && bare
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'_' | b'-'))
            && let Some((byte, _)) = text.char_indices().nth(token.0.index() + 1 + bare.len())
        {
            positions.push(byte);
        }
    }
    let mut out = text.to_owned();
    for byte in positions.into_iter().rev() {
        out.insert(byte, ' ');
    }
    out
}

// yaml.v3 treats raw NEL/LS/PS as quoted-scalar line breaks; yaml-rust2's
// YAML 1.2 scanner treats them as content. Adapt only the raw quoted lexeme,
// then decode it with the same parser. Escaped \L/\P/\N remain untouched.
fn adapt_quoted_unicode_breaks(text: &str, line: usize, column: usize) -> Option<String> {
    let lines: Vec<_> = text.split_inclusive('\n').collect();
    let before: usize = lines
        .iter()
        .take(line.checked_sub(1)?)
        .map(|line| line.len())
        .sum();
    let start = before + lines.get(line - 1)?.char_indices().nth(column)?.0;
    let source = text.get(start..)?;
    let quote = source.chars().next()?;
    if !matches!(quote, '\'' | '"') {
        return None;
    }
    let mut chars = source.char_indices().peekable();
    chars.next();
    let mut end = None;
    while let Some((byte, ch)) = chars.next() {
        if quote == '"' && ch == '\\' {
            chars.next();
            continue;
        }
        if ch == quote {
            if quote == '\'' && chars.peek().is_some_and(|(_, next)| *next == '\'') {
                chars.next();
            } else {
                end = Some(byte + ch.len_utf8());
                break;
            }
        }
    }
    let raw = &source[..end?];
    if !raw.contains(['\u{85}', '\u{2028}', '\u{2029}']) {
        return None;
    }
    let mut normalized = String::new();
    let mut chars = raw.chars().peekable();
    while let Some(ch) = chars.next() {
        if ch == '\\' && quote == '"' {
            normalized.push(ch);
            if let Some(next) = chars.next() {
                normalized.push(next);
            }
        } else if matches!(ch, '\u{85}' | '\u{2028}' | '\u{2029}') {
            while normalized.ends_with([' ', '\t']) {
                normalized.pop();
            }
            normalized.push(if ch == '\u{85}' { '\n' } else { ch });
            while chars.peek().is_some_and(|ch| matches!(ch, ' ' | '\t')) {
                chars.next();
            }
        } else {
            normalized.push(ch);
        }
    }
    let mut parser = Parser::new_from_str(&normalized);
    loop {
        match parser.next_token().ok()?.0 {
            Event::Scalar(value, ..) => return Some(value),
            Event::StreamEnd => return None,
            _ => {}
        }
    }
}
