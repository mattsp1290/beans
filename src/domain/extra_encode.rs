//! Presentation of authored unknown YAML nodes in new domain documents.
use super::authored_yaml::{
    DOUBLE_QUOTED, FLOW, FOLDED, Kind, LITERAL, Node, SINGLE_QUOTED, TAGGED,
};
use super::frontmatter::Error;
use super::yaml_render::{double_quoted, implicit_tag, scalar_value};

pub(super) fn mapping_fields(node: &Node) -> Result<String, Error> {
    if node.kind != Kind::Mapping {
        return Ok(String::new());
    }
    content_fields(&node.content)
}

/// Handoffs append Extra.Content regardless of the root node's kind.
pub(super) fn content_fields(content: &[Node]) -> Result<String, Error> {
    for child in content.iter().take(content.len() / 2 * 2) {
        validate(child)?;
    }
    pairs(content, 0)
}

fn validate(node: &Node) -> Result<(), Error> {
    if node.kind == Kind::Zero && node != &Node::default() {
        return Err(Error::new(
            "yaml: cannot encode node with unknown kind 0".into(),
        ));
    }
    let (value, kind) = if node.kind == Kind::Alias {
        (&node.value, "alias")
    } else {
        (&node.anchor, "anchor")
    };
    if node.kind == Kind::Alias && value.is_empty() {
        return Err(Error::new("yaml: alias value must not be empty".into()));
    }
    if !value
        .bytes()
        .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'_' | b'-'))
    {
        return Err(Error::new(format!(
            "yaml: {kind} value must contain alphanumerical characters only"
        )));
    }
    let children = if node.kind == Kind::Sequence {
        &node.content[..]
    } else if node.kind == Kind::Mapping {
        &node.content[..node.content.len() / 2 * 2]
    } else {
        &[]
    };
    for child in children {
        validate(child)?;
    }
    Ok(())
}

fn short_tag(tag: &str) -> String {
    tag.strip_prefix("tag:yaml.org,2002:")
        .map_or_else(|| tag.to_owned(), |suffix| format!("!!{suffix}"))
}

fn properties(node: &Node) -> String {
    let tag = short_tag(&node.tag);
    let inferred = match node.kind {
        Kind::Mapping => "!!map",
        Kind::Sequence => "!!seq",
        Kind::Scalar => implicit_tag(&node.value),
        _ => "",
    };
    let styled_string = node.kind == Kind::Scalar
        && tag == "!!str"
        && node.style & (SINGLE_QUOTED | DOUBLE_QUOTED | LITERAL | FOLDED) != 0;
    let emitted = if node.style & TAGGED == 0
        && (tag == inferred || tag == "!!str" && node.kind == Kind::Scalar || styled_string)
    {
        ""
    } else {
        &tag
    };
    let mut output = String::new();
    if !node.anchor.is_empty() {
        output.push('&');
        output.push_str(&node.anchor);
    }
    if !emitted.is_empty() {
        if !output.is_empty() {
            output.push(' ');
        }
        if emitted.starts_with('!') {
            output.push_str(emitted);
        } else {
            output.push_str("!<");
            output.push_str(emitted);
            output.push('>');
        }
    }
    output
}

fn comments(raw: &str, indent: usize) -> String {
    let mut output = String::new();
    for line in raw.split('\n').filter(|line| !line.is_empty()) {
        output.push_str(&" ".repeat(indent));
        if !line.starts_with('#') {
            output.push_str("# ");
        }
        output.push_str(line);
        output.push('\n');
    }
    output
}

fn inline_comment(raw: &str) -> String {
    if raw.is_empty() {
        String::new()
    } else if raw.starts_with('#') {
        format!(" {raw}")
    } else {
        format!(" # {raw}")
    }
}

fn scalar(node: &Node, flow: bool) -> String {
    let tag = short_tag(&node.tag);
    if node.style & DOUBLE_QUOTED != 0 {
        return double_quoted(&node.value);
    }
    let special = node.value.chars().any(|ch| {
        ch != '\t'
            && ch != '\n'
            && !((' '..='~').contains(&ch)
                || ('\u{a0}'..='\u{fffd}').contains(&ch) && ch != '\u{feff}')
    });
    let breaks: Vec<_> = node.value.chars().collect();
    let space_break = breaks.windows(2).any(|p| p[0] == ' ' && p[1] == '\n');
    let break_space = breaks.windows(2).any(|p| p[0] == '\n' && p[1] == ' ');
    if node.style & SINGLE_QUOTED != 0
        && !special
        && !node.value.contains('\t')
        && !space_break
        && !break_space
    {
        let mut output = String::from("'");
        let mut after_break = false;
        for ch in node.value.chars() {
            if ch == '\n' {
                if !after_break {
                    output.push('\n');
                }
                output.push(ch);
                after_break = true;
            } else {
                if after_break {
                    output.push_str(if flow { "    " } else { "  " });
                }
                if ch == '\'' {
                    output.push(ch);
                }
                output.push(ch);
                after_break = false;
            }
        }
        output.push('\'');
        return output;
    }
    if !flow && node.style & (LITERAL | FOLDED) != 0 {
        if special || space_break || node.value.ends_with(' ') || node.value.is_empty() {
            return double_quoted(&node.value);
        }
        let chomp = if !node.value.ends_with('\n') {
            "-"
        } else if node.value == "\n" || node.value.ends_with("\n\n") {
            "+"
        } else {
            ""
        };
        let indent = if node.value.starts_with([' ', '\n']) {
            "2"
        } else {
            ""
        };
        let folded = node.style & LITERAL == 0;
        let indicator = if folded { '>' } else { '|' };
        let mut output = format!("{indicator}{indent}{chomp}\n");
        let content = if node.value.chars().all(|ch| ch == '\n') {
            &node.value[1..]
        } else {
            &node.value
        };
        let lines: Vec<_> = content.split_inclusive('\n').collect();
        for (index, line) in lines.iter().enumerate() {
            if *line != "\n" {
                output.push_str("  ");
            }
            output.push_str(line);
            if folded
                && line.ends_with('\n')
                && !line.starts_with([' ', '\n'])
                && lines
                    .get(index + 1)
                    .is_none_or(|next| !next.starts_with(' '))
            {
                output.push('\n');
            }
        }
        if !output.ends_with('\n') {
            output.push('\n');
        }
        return output;
    }
    if node.style & SINGLE_QUOTED != 0 {
        return double_quoted(&node.value);
    }
    let force_quote =
        tag == "!!str" && node.style & TAGGED == 0 && implicit_tag(&node.value) != "!!str";
    scalar_value(&node.value, flow, force_quote, false)
}

fn inline(node: &Node, flow: bool) -> Result<String, Error> {
    if node.kind == Kind::Alias {
        return Ok(format!("*{}", node.value));
    }
    let rendered = match node.kind {
        Kind::Zero => "null".to_owned(),
        Kind::Scalar => scalar(node, flow),
        Kind::Alias => format!("*{}", node.value),
        Kind::Sequence => format!(
            "[{}]",
            node.content
                .iter()
                .map(|node| inline(node, true))
                .collect::<Result<Vec<_>, _>>()?
                .join(", ")
        ),
        Kind::Mapping => {
            let mut fields = Vec::new();
            for pair in node.content.as_chunks::<2>().0 {
                fields.push(format!(
                    "{}: {}",
                    inline(&pair[0], true)?,
                    inline(&pair[1], true)?
                ));
            }
            format!("{{{}}}", fields.join(", "))
        }
    };
    let properties = properties(node);
    Ok(if properties.is_empty() {
        rendered
    } else {
        format!("{properties} {rendered}")
    })
}

fn block_container(node: &Node) -> bool {
    matches!(node.kind, Kind::Sequence | Kind::Mapping)
        && node.style & FLOW == 0
        && !node.content.is_empty()
}

fn body(node: &Node, indent: usize) -> Result<String, Error> {
    if node.kind == Kind::Mapping {
        return pairs(&node.content, indent);
    }
    let mut output = String::new();
    for item in &node.content {
        output.push_str(&comments(&item.head_comment, indent));
        output.push_str(&" ".repeat(indent));
        output.push_str("- ");
        if block_container(item) {
            let properties = properties(item);
            let nested = body(item, indent + 2)?;
            if properties.is_empty() && item.head_comment.is_empty() {
                output.push_str(nested.trim_start_matches(' '));
            } else {
                output.push_str(&properties);
                output.push('\n');
                output.push_str(&nested);
            }
        } else {
            output.push_str(&with_indent(&inline(item, false)?, indent));
            finish_value(&mut output, item);
        }
        output.push_str(&comments(&item.foot_comment, indent));
    }
    Ok(output)
}

fn with_indent(rendered: &str, indent: usize) -> String {
    let mut output = String::new();
    for (index, line) in rendered.split_inclusive('\n').enumerate() {
        if index > 0 && line != "\n" {
            output.push_str(&" ".repeat(indent));
        }
        output.push_str(line);
    }
    output
}

fn finish_value(output: &mut String, node: &Node) {
    if output.ends_with('\n') {
        // Block scalar line comments belong on the header, before its content.
        if !node.line_comment.is_empty() {
            let start = output.rfind(": ").map_or(0, |index| index + 2);
            if let Some(end) = output[start..].find('\n') {
                output.insert_str(start + end, &inline_comment(&node.line_comment));
            }
        }
    } else {
        output.push_str(&inline_comment(&node.line_comment));
        output.push('\n');
    }
}

fn pairs(content: &[Node], indent: usize) -> Result<String, Error> {
    let mut output = String::new();
    for pair in content.as_chunks::<2>().0 {
        let (key, value) = (&pair[0], &pair[1]);
        output.push_str(&comments(&key.head_comment, indent));
        output.push_str(&" ".repeat(indent));
        output.push_str(&inline(key, true)?);
        output.push(':');
        if block_container(value) {
            let properties = properties(value);
            if !properties.is_empty() {
                output.push(' ');
                output.push_str(&properties);
            }
            output.push('\n');
            output.push_str(&comments(&value.head_comment, indent + 2));
            output.push_str(&body(value, indent + 2)?);
        } else {
            let rendered = inline(value, false)?;
            if !rendered.is_empty() {
                output.push(' ');
            }
            output.push_str(&with_indent(&rendered, indent));
            finish_value(&mut output, value);
        }
        if block_container(value) && !value.foot_comment.is_empty() {
            output.push('\n');
        }
        output.push_str(&comments(&value.foot_comment, indent));
        output.push_str(&comments(&key.foot_comment, indent));
        if !block_container(value) && !value.head_comment.is_empty() {
            output.push('\n');
            output.push_str(&comments(&value.head_comment, indent));
        }
    }
    Ok(output)
}
