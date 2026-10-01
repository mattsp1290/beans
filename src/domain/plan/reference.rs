use super::{ChangeGraph, Plan, parse_summary, validate};
use crate::domain::{
    byte_edit,
    frontmatter::{EditResult, Error},
    issue::quoted,
    yaml_render::scalar_value_indented,
};
use beans_kernel::splice::Span;
fn base60(value: &str) -> bool {
    let value = value.strip_prefix(['+', '-']).unwrap_or(value);
    let (clock, fraction) = value
        .split_once('.')
        .map_or((value, None), |(a, b)| (a, Some(b)));
    if fraction.is_some_and(|s| !s.bytes().all(|b| b.is_ascii_digit() || b == b'_')) {
        return false;
    }
    let mut parts = clock.split(':');
    let first = parts.next().unwrap_or_default();
    if !first.as_bytes().first().is_some_and(u8::is_ascii_digit)
        || !first.bytes().all(|b| b.is_ascii_digit() || b == b'_')
    {
        return false;
    }
    let mut count = 0;
    for part in parts {
        count += 1;
        if part.is_empty()
            || part.len() > 2
            || !part.bytes().all(|b| b.is_ascii_digit())
            || (part.len() == 2 && part.as_bytes()[0] > b'5')
        {
            return false;
        }
    }
    count > 0
}
fn string(value: &str) -> String {
    let old_bool = matches!(
        value,
        "y" | "Y"
            | "yes"
            | "Yes"
            | "YES"
            | "on"
            | "On"
            | "ON"
            | "n"
            | "N"
            | "no"
            | "No"
            | "NO"
            | "off"
            | "Off"
            | "OFF"
    );
    scalar_value_indented(value, false, old_bool || base60(value), true, 2, 4)
}
fn field(out: &mut String, prefix: &str, key: &str, value: &str) {
    out.push_str(prefix);
    out.push_str(key);
    out.push_str(": ");
    let rendered = string(value);
    for (i, line) in rendered
        .split_inclusive(['\n', '\u{85}', '\u{2028}', '\u{2029}'])
        .enumerate()
    {
        if i > 0
            && !line
                .chars()
                .all(|c| matches!(c, '\n' | '\u{85}' | '\u{2028}' | '\u{2029}'))
        {
            out.push_str("      ");
        }
        out.push_str(line);
    }
    if !out.ends_with('\n') {
        out.push('\n');
    }
}
fn render(graph: &ChangeGraph) -> String {
    let mut out = format!("version: {}\n", graph.version);
    let nodes = graph.nodes.as_deref().unwrap_or_default();
    if nodes.is_empty() {
        out.push_str("nodes: []\n");
    } else {
        out.push_str("nodes:\n");
        for node in nodes {
            field(&mut out, "    - ", "id", &node.id);
            field(&mut out, "      ", "label", &node.label);
            field(&mut out, "      ", "kind", &node.kind);
            if !node.reference.is_empty() {
                field(&mut out, "      ", "ref", &node.reference);
            }
        }
    }
    let edges = graph.edges.as_deref().unwrap_or_default();
    if edges.is_empty() {
        out.push_str("edges: []\n");
    } else {
        out.push_str("edges:\n");
        for edge in edges {
            field(&mut out, "    - ", "from", &edge.from);
            field(&mut out, "      ", "to", &edge.to);
            field(&mut out, "      ", "kind", &edge.kind);
            if !edge.label.is_empty() {
                field(&mut out, "      ", "label", &edge.label);
            }
        }
    }
    out
}
/// Canonicalize the graph fence, preserving Go's in-memory failure side effects.
/// Successful results expose the copy geometry from the production splice engine.
pub fn set_node_ref(
    plan: Option<&mut Plan>,
    node_id: &str,
    reference: &str,
) -> Result<EditResult, Error> {
    let p = plan.ok_or_else(|| Error("nil plan".into()))?;
    let node = p
        .graph
        .nodes
        .as_mut()
        .and_then(|nodes| nodes.iter_mut().find(|n| n.id == node_id))
        .ok_or_else(|| Error(format!("graph node {} not found", quoted(node_id))))?;
    node.reference = reference.into();
    let data = render(&p.graph);
    let start = p
        .body
        .find("```bn-change-graph\n")
        .ok_or_else(|| Error(format!("{}: missing bn-change-graph fence", p.path)))?;
    let content = start + "```bn-change-graph\n".len();
    let end = p.body[content..]
        .find("\n```")
        .map(|i| content + i)
        .ok_or_else(|| Error(format!("{}: unterminated bn-change-graph fence", p.path)))?;
    let result = byte_edit::apply(
        p.body.as_bytes(),
        vec![(
            Span {
                start: content,
                end,
            },
            data.as_bytes(),
        )],
    )?;
    p.body = String::from_utf8(result.bytes.clone())
        .map_err(|_| Error("invalid UTF-8 plan body".into()))?;
    (p.summary, p.graph) = parse_summary(&p.path, &p.body)?;
    validate(Some(p)).map_err(|e| Error(e.to_string()))?;
    Ok(result)
}
