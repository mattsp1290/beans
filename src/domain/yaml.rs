use super::frontmatter::{Error, Node, NodeKind};
use yaml_rust2::parser::{Event, Parser, Tag};
use yaml_rust2::scanner::{Scanner, TScalarStyle, TokenType};

fn scalar_null(value: &str, style: TScalarStyle, tag: Option<&Tag>) -> bool {
    if let Some(tag) = tag {
        return tag.handle == "tag:yaml.org,2002:" && tag.suffix == "null";
    }
    style == TScalarStyle::Plain && matches!(value, "" | "~" | "null" | "Null" | "NULL")
}

pub(crate) fn parse(path: &str, text: &str) -> Result<Node, Error> {
    let mut parser = Parser::new_from_str(text);
    let mut positions = SourcePositions::scan(text);
    let mut stack: Vec<Node> = Vec::new();
    let mut root = None;
    loop {
        let (event, marker) = parser.next_token().map_err(|error| {
            // Exact yaml.v3 syntax-error presentation is completed alongside
            // the typed codec differential corpus; scanner text is retained.
            Error(format!("{path}: frontmatter: {error}"))
        })?;
        let node = match event {
            Event::MappingStart(anchor, ref tag) | Event::SequenceStart(anchor, ref tag) => {
                stack.push(Node {
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
                .ok_or_else(|| Error(format!("{path}: invalid YAML container")))?,
            Event::Scalar(value, style, anchor, tag) => {
                let null = scalar_null(&value, style, tag.as_ref());
                // This release marks nonempty block scalars at their first
                // content character rather than at |/>. Recover the header
                // from the scanner token interval; do not guess by subtracting
                // one line, since leading blank lines and split values exist.
                let line = if matches!(style, TScalarStyle::Literal | TScalarStyle::Folded) {
                    positions.block_headers.pop_front().ok_or_else(|| {
                        Error(format!("{path}: could not locate block scalar header"))
                    })?
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
                    kind: NodeKind::Scalar,
                    line,
                    value: Some(value),
                    null: Some(null),
                    children: Vec::new(),
                }
            }
            Event::Alias(_) => Node {
                kind: NodeKind::Alias,
                line: marker.line(),
                value: None,
                null: None,
                children: Vec::new(),
            },
            Event::DocumentEnd | Event::StreamEnd => break,
            _ => continue,
        };
        if let Some(parent) = stack.last_mut() {
            parent.children.push(node);
        } else {
            root = Some(node);
        }
    }
    root.ok_or_else(|| Error(format!("{path}: line 2: frontmatter is empty")))
}

struct SourcePositions {
    block_headers: std::collections::VecDeque<usize>,
    anchors: Vec<usize>,
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
        let mut anchors = Vec::new();
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
            match &token.1 {
                TokenType::Anchor(_) => anchors.push(token.0.line()),
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
            anchors,
            tags: tags.into(),
            empty_values: empty_values.into(),
        }
    }
}
