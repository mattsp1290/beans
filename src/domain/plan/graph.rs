use super::{ChangeGraph, GraphEdge, GraphNode};
use crate::domain::{
    frontmatter::{Node, NodeKind},
    issue::quoted,
};
use std::collections::HashSet;

#[derive(Clone, Debug)]
pub struct GraphError {
    pub graph: ChangeGraph,
    message: String,
}
impl std::fmt::Display for GraphError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.message)
    }
}
impl std::error::Error for GraphError {}
fn failure(path: &str, message: &str, graph: ChangeGraph) -> GraphError {
    GraphError {
        graph,
        message: format!("{path}: {message}"),
    }
}
fn allowed(node: &Node, keys: &[&str]) -> bool {
    node.kind == NodeKind::Mapping
        && node.children.as_chunks::<2>().0.iter().all(|p| {
            p[0].kind == NodeKind::Scalar
                && keys.contains(&p[0].value.as_deref().unwrap_or_default())
        })
}
fn unsafe_yaml(node: &Node) -> bool {
    node.kind == NodeKind::Alias
        || !matches!(
            node.tag.as_str(),
            "" | "!!map" | "!!seq" | "!!str" | "!!int" | "!!null"
        )
        || node.children.iter().any(unsafe_yaml)
}
pub(super) fn duplicate_errors(node: &Node) -> Vec<String> {
    let pairs: Vec<_> = node.children.as_chunks::<2>().0.iter().collect();
    let mut errors = Vec::new();
    for i in 0..pairs.len() {
        for j in i + 1..pairs.len() {
            if pairs[i][0].kind == pairs[j][0].kind && pairs[i][0].value == pairs[j][0].value {
                errors.push(format!(
                    "line {}: mapping key {} already defined at line {}",
                    pairs[j][0].line,
                    quoted(pairs[j][0].value.as_deref().unwrap_or_default()),
                    pairs[i][0].line
                ));
            }
        }
    }
    errors
}
pub(super) fn type_error(node: &Node, target: &str) -> String {
    let value = node.value.as_deref().unwrap_or_default();
    let snippet = if matches!(node.tag.as_str(), "!!seq" | "!!map") {
        String::new()
    } else {
        let value = if value.len() > 10 {
            // yaml.v3 truncates to seven bytes. Go's JSON encoder replaces
            // each invalid trailing byte separately when a rune is split.
            let end = value.floor_char_boundary(7);
            format!("{}{}...", &value[..end], "�".repeat(7 - end))
        } else {
            value.to_owned()
        };
        format!(" `{value}`")
    };
    format!(
        "line {}: cannot unmarshal {}{snippet} into {target}",
        node.line, node.tag
    )
}
fn string_value(node: &Node, errors: &mut Vec<String>) -> String {
    if node.tag == "!!null" && node.kind == NodeKind::Scalar {
        return String::new();
    }
    if node.kind == NodeKind::Scalar {
        node.value.clone().unwrap_or_default()
    } else {
        errors.push(type_error(node, "string"));
        String::new()
    }
}
fn integer(value: &str) -> Option<i64> {
    let value = value.replace('_', "");
    let negative = value.starts_with('-');
    let unsigned = value.strip_prefix(['+', '-']).unwrap_or(&value);
    let (radix, digits) = if let Some(s) = unsigned
        .strip_prefix("0x")
        .or_else(|| unsigned.strip_prefix("0X"))
    {
        (16, s)
    } else if let Some(s) = unsigned
        .strip_prefix("0b")
        .or_else(|| unsigned.strip_prefix("0B"))
    {
        (2, s)
    } else if let Some(s) = unsigned
        .strip_prefix("0o")
        .or_else(|| unsigned.strip_prefix("0O"))
    {
        (8, s)
    } else if unsigned.len() > 1 && unsigned.starts_with('0') {
        (8, unsigned)
    } else {
        (10, unsigned)
    };
    let n = i128::from_str_radix(digits, radix).ok()?;
    i64::try_from(if negative { -n } else { n }).ok()
}

pub fn parse_graph(path: &str, text: &str) -> Result<ChangeGraph, GraphError> {
    let empty = ChangeGraph::default();
    let lines: Vec<_> = text.split('\n').collect();
    let mut start = None;
    for (i, line) in lines.iter().enumerate() {
        if line.trim() == "```bn-change-graph" && start.replace(i).is_some() {
            return Err(failure(path, "multiple bn-change-graph fences", empty));
        }
    }
    let start =
        start.ok_or_else(|| failure(path, "missing bn-change-graph fence", empty.clone()))?;
    let end = (start + 1..lines.len())
        .find(|&i| lines[i].trim() == "```")
        .ok_or_else(|| failure(path, "unterminated bn-change-graph fence", empty.clone()))?;
    if lines[..start]
        .iter()
        .chain(&lines[end + 1..])
        .any(|line| !line.trim().is_empty())
    {
        return Err(failure(
            path,
            "Change graph may only contain graph fence",
            empty,
        ));
    }
    let yaml = lines[start + 1..end].join("\n");
    if yaml.trim().is_empty() {
        return Err(failure(
            path,
            "graph contains an unknown or invalid field",
            empty,
        ));
    }
    let root = crate::domain::yaml::parse_optional(path, &yaml)
        .map_err(|error| GraphError {
            graph: empty.clone(),
            message: error.to_string().replace(": frontmatter:", ": graph YAML:"),
        })?
        .ok_or_else(|| {
            failure(
                path,
                "graph contains an unknown or invalid field",
                empty.clone(),
            )
        })?;
    if !allowed(&root, &["version", "nodes", "edges"]) {
        return Err(failure(
            path,
            "graph contains an unknown or invalid field",
            empty,
        ));
    }
    if unsafe_yaml(&root) {
        return Err(failure(
            path,
            "graph YAML aliases and custom tags are not allowed",
            empty,
        ));
    }
    for pair in root.children.as_chunks::<2>().0 {
        let key = pair[0].value.as_deref().unwrap_or_default();
        let value = &pair[1];
        if value.kind == NodeKind::Sequence {
            let fields = match key {
                "nodes" => Some((&["id", "label", "kind", "ref"][..], "node")),
                "edges" => Some((&["from", "to", "kind", "label"][..], "edge")),
                _ => None,
            };
            if let Some((fields, kind)) = fields {
                for item in &value.children {
                    if !allowed(item, fields) {
                        return Err(failure(
                            path,
                            &format!("graph {kind} contains an unknown field"),
                            empty,
                        ));
                    }
                }
            }
        }
    }
    let mut errors = duplicate_errors(&root);
    let mut g = ChangeGraph::default();
    if errors.is_empty() {
        for pair in root.children.as_chunks::<2>().0 {
            let key = pair[0].value.as_deref().unwrap_or_default();
            let value = &pair[1];
            match key {
                "version" => {
                    if value.tag != "!!null" || value.kind != NodeKind::Scalar {
                        if value.tag == "!!int" {
                            let raw = value.value.as_deref().unwrap_or_default();
                            let resolved = crate::domain::yaml_render::implicit_tag(raw);
                            if resolved != "!!int" {
                                return Err(failure(
                                    path,
                                    &format!(
                                        "graph YAML: yaml: cannot decode {resolved} `{raw}` as a !!int"
                                    ),
                                    empty,
                                ));
                            }
                        }
                        if value.tag == "!!int"
                            && let Some(version) =
                                integer(value.value.as_deref().unwrap_or_default())
                        {
                            g.version = version;
                        } else {
                            errors.push(type_error(value, "int"));
                        }
                    }
                }
                "nodes" | "edges" => {
                    if value.tag == "!!null" && value.kind == NodeKind::Scalar {
                        continue;
                    }
                    if value.kind != NodeKind::Sequence {
                        errors.push(type_error(
                            value,
                            if key == "nodes" {
                                "[]plan.GraphNode"
                            } else {
                                "[]plan.GraphEdge"
                            },
                        ));
                        continue;
                    }
                    let mut nodes = Vec::new();
                    let mut edges = Vec::new();
                    for item in &value.children {
                        let duplicates = duplicate_errors(item);
                        if !duplicates.is_empty() {
                            errors.extend(duplicates);
                            continue;
                        }
                        let mut node = GraphNode::default();
                        let mut edge = GraphEdge::default();
                        for p in item.children.as_chunks::<2>().0 {
                            let text = string_value(&p[1], &mut errors);
                            match (key, p[0].value.as_deref().unwrap_or_default()) {
                                ("nodes", "id") => node.id = text,
                                ("nodes", "label") => node.label = text,
                                ("nodes", "kind") => node.kind = text,
                                ("nodes", "ref") => node.reference = text,
                                ("edges", "from") => edge.from = text,
                                ("edges", "to") => edge.to = text,
                                ("edges", "kind") => edge.kind = text,
                                ("edges", "label") => edge.label = text,
                                _ => {}
                            }
                        }
                        if key == "nodes" {
                            nodes.push(node);
                        } else {
                            edges.push(edge);
                        }
                    }
                    if key == "nodes" {
                        g.nodes = Some(nodes);
                    } else {
                        g.edges = Some(edges);
                    }
                }
                _ => {}
            }
        }
    }
    if !errors.is_empty() {
        return Err(failure(
            path,
            &format!(
                "graph YAML: yaml: unmarshal errors:\n  {}",
                errors.join("\n  ")
            ),
            empty,
        ));
    }
    if g.version != 1 {
        return Err(failure(path, "graph version must be 1", g));
    }
    let nodes = g.nodes.as_deref().unwrap_or_default();
    let edges = g.edges.as_deref().unwrap_or_default();
    if nodes.len() > 200 || edges.len() > 400 {
        return Err(failure(path, "graph exceeds size limit", g));
    }
    let mut seen = HashSet::new();
    for node in nodes {
        let id = node.id.as_bytes();
        if id.is_empty()
            || id.len() > 64
            || !id[0].is_ascii_lowercase()
            || !id
                .iter()
                .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || *b == b'-')
            || node.label.trim().is_empty()
            || node.label.contains('\n')
            || node.label.len() > 120
            || !matches!(
                node.kind.as_str(),
                "artifact" | "component" | "interface" | "data" | "workflow" | "external"
            )
        {
            return Err(failure(
                path,
                &format!("invalid graph node {}", quoted(&node.id)),
                g,
            ));
        }
        if !seen.insert(node.id.as_str()) {
            return Err(failure(
                path,
                &format!("duplicate graph node {}", quoted(&node.id)),
                g,
            ));
        }
    }
    let mut seen_edges = HashSet::new();
    for edge in edges {
        if !seen.contains(edge.from.as_str())
            || !seen.contains(edge.to.as_str())
            || !matches!(
                edge.kind.as_str(),
                "precedes" | "affects" | "enables" | "produces" | "replaces" | "contains"
            )
            || !seen_edges.insert((&edge.from, &edge.to, &edge.kind, &edge.label))
        {
            return Err(failure(path, "invalid graph edge", g));
        }
    }
    Ok(g)
}
