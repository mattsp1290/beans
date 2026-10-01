use super::{States, WorkflowFile};
use crate::domain::{
    frontmatter::{Error, Node, NodeKind},
    yaml_decode::{decoded_string, duplicate_errors, type_error},
    yaml_string::YamlString,
};
use std::collections::{BTreeMap, HashMap, HashSet};
fn anchors<'a>(node: &'a Node, out: &mut HashMap<usize, &'a Node>) {
    if node.kind != NodeKind::Alias && node.anchor_id != 0 {
        out.insert(node.anchor_id, node);
    }
    for n in &node.children {
        anchors(n, out);
    }
}
struct Decoder<'a> {
    anchors: HashMap<usize, &'a Node>,
    active: HashSet<usize>,
    errors: Vec<String>,
}
impl<'a> Decoder<'a> {
    fn resolve(&self, node: &'a Node) -> Result<&'a Node, Error> {
        if node.kind != NodeKind::Alias {
            return Ok(node);
        }
        if self.active.contains(&std::ptr::from_ref(node).addr()) {
            return Err(Error::new(format!(
                "yaml: anchor '{}' value contains itself",
                node.anchor_name
            )));
        }
        self.anchors
            .get(&node.anchor_id)
            .copied()
            .ok_or_else(|| Error::new("yaml: unknown anchor".into()))
    }
    fn string(&mut self, node: &'a Node) -> Result<Option<YamlString>, Error> {
        let node = self.resolve(node)?;
        if node.kind == NodeKind::Scalar {
            let value = decoded_string(node)?;
            return Ok((node.tag != "!!null").then_some(value));
        }
        self.reject(node, "string");
        Ok(None)
    }
    fn reject(&mut self, node: &Node, target: &str) {
        let duplicates = if node.kind == NodeKind::Mapping {
            duplicate_errors(node)
        } else {
            Vec::new()
        };
        if duplicates.is_empty() {
            self.errors.push(type_error(node, target))
        } else {
            self.errors.extend(duplicates)
        }
    }
    fn list(&mut self, node: &'a Node) -> Result<States, Error> {
        let node = self.resolve(node)?;
        if node.kind == NodeKind::Scalar {
            decoded_string(node)?;
            if node.tag == "!!null" {
                return Ok(None);
            }
        }
        if node.kind != NodeKind::Sequence {
            self.reject(node, "[]string");
            return Ok(None);
        }
        let mut result = Vec::new();
        for child in &node.children {
            if let Some(value) = self.string(child)? {
                result.push(value)
            }
        }
        Ok(Some(result))
    }
    fn fields(&mut self, node: &'a Node, target: &str) -> Result<Vec<(&'a Node, &'a Node)>, Error> {
        let alias = (node.kind == NodeKind::Alias).then_some(std::ptr::from_ref(node).addr());
        let node = self.resolve(node)?;
        if node.kind == NodeKind::Scalar {
            decoded_string(node)?;
            if node.tag == "!!null" {
                return Ok(Vec::new());
            }
        }
        if node.kind != NodeKind::Mapping {
            self.reject(node, target);
            return Ok(Vec::new());
        }
        let duplicates = duplicate_errors(node);
        if !duplicates.is_empty() {
            self.errors.extend(duplicates);
            return Ok(Vec::new());
        }
        if let Some(alias) = alias {
            self.active.insert(alias);
        }
        let mut fields = Vec::new();
        let mut merge = None;
        let mut seen = HashSet::new();
        for pair in node.children.as_chunks::<2>().0 {
            if pair[0].tag == "!!merge" {
                merge = Some(&pair[1]);
                continue;
            }
            if let Some(key) = self.string(&pair[0])? {
                seen.insert(key);
                fields.push((&pair[0], &pair[1]));
            }
        }
        if let Some(merge) = merge {
            let resolved = self.resolve(merge)?;
            let maps = match resolved.kind {
                NodeKind::Mapping => vec![merge],
                NodeKind::Sequence if merge.kind != NodeKind::Alias => {
                    resolved.children.iter().collect()
                }
                _ => {
                    return Err(Error::new(
                        "yaml: map merge requires map or sequence of maps as the value".into(),
                    ));
                }
            };
            for map in maps {
                if self.resolve(map)?.kind != NodeKind::Mapping {
                    return Err(Error::new(
                        "yaml: map merge requires map or sequence of maps as the value".into(),
                    ));
                }
                for (key, value) in self.fields(map, target)? {
                    if let Some(name) = self.string(key)?
                        && seen.insert(name)
                    {
                        fields.push((key, value));
                    }
                }
            }
        }
        if let Some(alias) = alias {
            self.active.remove(&alias);
        }
        Ok(fields)
    }
    fn workflow(&mut self, node: &'a Node) -> Result<WorkflowFile, Error> {
        let mut cfg = WorkflowFile::default();
        for (key, value) in self.fields(node, "issue.WorkflowFile")? {
            let Some(name) = self.string(key)? else {
                continue;
            };
            match name.as_bytes() {
                b"statuses" => cfg.statuses = self.list(value)?,
                b"default" => cfg.default = self.string(value)?.unwrap_or_default(),
                b"active" => cfg.active = self.list(value)?,
                b"terminal" => cfg.terminal = self.list(value)?,
                b"transitions" => {
                    let resolved = self.resolve(value)?;
                    if resolved.kind == NodeKind::Scalar && resolved.tag == "!!null" {
                        decoded_string(resolved)?;
                        continue;
                    }
                    let mut transitions = BTreeMap::new();
                    for (key, value) in self.fields(value, "map[string][]string")? {
                        if let Some(name) = self.string(key)? {
                            transitions.insert(name, self.list(value)?);
                        }
                    }
                    cfg.transitions = Some(transitions);
                }
                _ => self.errors.push(format!(
                    "line {}: field {name} not found in type issue.WorkflowFile",
                    key.line
                )),
            }
        }
        Ok(cfg)
    }
}
pub(super) fn decode(data: &[u8]) -> Result<WorkflowFile, Error> {
    let text = std::str::from_utf8(data)
        .map_err(|_| Error::new("yaml: invalid leading UTF-8 octet".into()))?;
    let Some(node) =
        crate::domain::yaml::parse_optional_with_syntax("workflow", text, syntax_error).map_err(
            |e| {
                let message = e
                    .as_bytes()
                    .strip_prefix(b"workflow: frontmatter: ")
                    .or_else(|| e.as_bytes().strip_prefix(b"workflow: "))
                    .unwrap_or(e.as_bytes());
                Error::from_bytes(message.into())
            },
        )?
    else {
        return Err(Error::new("EOF".into()));
    };
    let mut anchor_map = HashMap::new();
    anchors(&node, &mut anchor_map);
    let mut decoder = Decoder {
        anchors: anchor_map,
        active: HashSet::new(),
        errors: Vec::new(),
    };
    let mut cfg = WorkflowFile::default();
    for (key, value) in decoder.fields(&node, "issue.workflowFile")? {
        if let Some(name) = decoder.string(key)? {
            if name.as_bytes() == b"workflow" {
                cfg = decoder.workflow(value)?
            } else {
                decoder.errors.push(format!(
                    "line {}: field {name} not found in type issue.workflowFile",
                    key.line
                ))
            }
        }
    }
    if !decoder.errors.is_empty() {
        return Err(Error::new(format!(
            "yaml: unmarshal errors:\n  {}",
            decoder.errors.join("\n  ")
        )));
    }
    Ok(cfg)
}
fn syntax_error(error: &yaml_rust2::scanner::ScanError, stack: &[Node]) -> Error {
    let (line, message) = match error.info() {
        "while parsing a node, did not find expected node content"
        | "\"-\" is only valid inside a block" => (
            error.marker().line().saturating_sub(1),
            "did not find expected node content",
        ),
        "while parsing a flow sequence, expected ',' or ']'" => {
            let context = stack.last().map_or(0, |n| n.line.saturating_sub(1));
            (
                if context > 0 {
                    context
                } else {
                    error.marker().line().saturating_sub(1)
                },
                "did not find expected ',' or ']'",
            )
        }
        "while parsing a quoted scalar, found invalid Unicode character escape code" => (
            error.marker().line(),
            "found invalid Unicode character escape code",
        ),
        _ => return Error::new(error.to_string()),
    };
    let location = if line > 0 {
        format!("line {line}: ")
    } else {
        String::new()
    };
    Error::new(format!("yaml: {location}{message}"))
}
