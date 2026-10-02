use super::{ChangeGraph, GraphEdge, GraphNode, YamlString};
use crate::domain::{
    frontmatter::{Error, Node, NodeKind},
    issue::quoted,
};
use std::collections::HashSet;

#[derive(Clone, Debug)]
pub struct GraphError {
    pub graph: ChangeGraph,
    message: Error,
}
impl std::fmt::Display for GraphError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        std::fmt::Display::fmt(&self.message, f)
    }
}
impl std::error::Error for GraphError {}
impl GraphError {
    pub fn diagnostic(&self) -> Error {
        self.message.clone()
    }
    pub fn as_bytes(&self) -> &[u8] {
        self.message.as_bytes()
    }
}
fn failure(path: &[u8], message: &str, graph: ChangeGraph) -> GraphError {
    GraphError {
        graph,
        message: Error::new(message.into()).context(path),
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
pub(super) use crate::domain::yaml_decode::{duplicate_errors, type_error};

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
    parse_graph_bytes(path.as_bytes(), text)
}
pub fn parse_graph_bytes(path: &[u8], text: &str) -> Result<ChangeGraph, GraphError> {
    let view = YamlString::from_bytes(path.into()).to_string();
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
    let root = crate::domain::yaml::parse_optional(&view, &yaml)
        .map_err(|error| GraphError {
            graph: empty.clone(),
            message: graph_yaml_error(error, &view, path),
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

fn graph_yaml_error(error: Error, view: &str, path: &[u8]) -> Error {
    let prefix = [view.as_bytes(), b": frontmatter: "].concat();
    match error.as_bytes().strip_prefix(prefix.as_slice()) {
        Some(rest) => Error::from_bytes(rest.into()).context(&[path, b": graph YAML"].concat()),
        None => error.with_path(view, path),
    }
}
