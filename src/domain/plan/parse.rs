use super::{
    Plan, YamlString,
    graph::{duplicate_errors, type_error},
    id::valid_slug,
    parse_summary, timestamp, valid_status,
};
use crate::domain::{
    frontmatter::{Error, Node, NodeKind},
    issue::{Timestamp, quoted},
};
use std::collections::{HashMap, HashSet};
const OWNED: &[&str] = &[
    "id", "aliases", "title", "slug", "status", "created", "updated", "sections",
];
fn scalar(node: &Node) -> Result<&str, Error> {
    if node.kind != NodeKind::Scalar {
        return Err(Error("must be scalar".into()));
    }
    Ok(node.value.as_deref().unwrap_or_default())
}
fn anchors<'a>(node: &'a Node, result: &mut HashMap<usize, &'a Node>) {
    if node.kind != NodeKind::Alias && node.anchor_id != 0 {
        result.insert(node.anchor_id, node);
    }
    for child in &node.children {
        anchors(child, result);
    }
}
fn decoded_string(node: &Node) -> Result<YamlString, Error> {
    let value = node.value.as_deref().unwrap_or_default();
    let resolved = crate::domain::yaml_render::implicit_tag(value);
    if matches!(
        node.tag.as_str(),
        "!!null" | "!!int" | "!!float" | "!!bool" | "!!timestamp"
    ) && resolved != node.tag
        && !(node.tag == "!!float" && resolved == "!!int")
    {
        return Err(Error(format!(
            "yaml: cannot decode {resolved} `{value}` as a {}",
            node.tag
        )));
    }
    if node.tag == "!!binary" {
        return super::yaml_string::binary(value)
            .map(YamlString::from_bytes)
            .ok_or_else(|| Error("yaml: !!binary value contains invalid base64 data".into()));
    }
    Ok(value.into())
}
fn list(node: &Node, anchors: &HashMap<usize, &Node>) -> Result<Vec<YamlString>, Error> {
    let node = if node.kind == NodeKind::Alias {
        anchors
            .get(&node.anchor_id)
            .copied()
            .ok_or_else(|| Error("yaml: unknown anchor".into()))?
    } else {
        node
    };
    if node.kind == NodeKind::Scalar {
        decoded_string(node)?;
    }
    if node.tag == "!!null" && node.kind == NodeKind::Scalar {
        return Ok(Vec::new());
    }
    let mut errors = Vec::new();
    let mut result = Vec::new();
    if node.kind != NodeKind::Sequence {
        let duplicates = if node.kind == NodeKind::Mapping {
            duplicate_errors(node)
        } else {
            Vec::new()
        };
        if duplicates.is_empty() {
            errors.push(type_error(node, "[]string"));
        } else {
            errors.extend(duplicates);
        }
    } else {
        for child in &node.children {
            let child = if child.kind == NodeKind::Alias {
                anchors
                    .get(&child.anchor_id)
                    .copied()
                    .ok_or_else(|| Error("yaml: unknown anchor".into()))?
            } else {
                child
            };
            if child.kind == NodeKind::Scalar {
                let value = decoded_string(child)?;
                if child.tag != "!!null" {
                    result.push(value);
                }
            } else {
                let duplicates = if child.kind == NodeKind::Mapping {
                    duplicate_errors(child)
                } else {
                    Vec::new()
                };
                if duplicates.is_empty() {
                    errors.push(type_error(child, "string"));
                } else {
                    errors.extend(duplicates);
                }
            }
        }
    }
    if errors.is_empty() {
        Ok(result)
    } else {
        Err(Error(format!(
            "yaml: unmarshal errors:\n  {}",
            errors.join("\n  ")
        )))
    }
}
/// Parse a strict plan manifest; section files and authored lifecycle are validated separately.
pub fn parse(path: &str, data: &[u8]) -> Result<Plan, Error> {
    let fail = |message: &str| Error(format!("{path}: {message}"));
    if data.contains(&0) {
        return Err(fail("contains NUL bytes"));
    }
    let text = std::str::from_utf8(data).map_err(|_| fail("invalid UTF-8"))?;
    if text.contains('\r') {
        return Err(fail("requires LF line endings"));
    }
    if !text.ends_with('\n') {
        return Err(fail("missing final newline"));
    }
    if !text.starts_with("---\n") {
        return Err(fail("missing frontmatter"));
    }
    let end = text[4..]
        .find("\n---\n")
        .ok_or_else(|| fail("frontmatter has no closing fence"))?
        + 4;
    let fm = &text[4..end];
    if fm.trim().is_empty() {
        return Err(fail("frontmatter must be mapping"));
    }
    let root = crate::domain::yaml::parse_optional(path, fm)?
        .ok_or_else(|| fail("frontmatter must be mapping"))?;
    if root.kind != NodeKind::Mapping {
        return Err(fail("frontmatter must be mapping"));
    }
    let mut p = Plan {
        path: path.into(),
        body: text[end + 5..].into(),
        ..Plan::default()
    };
    let mut seen = HashSet::new();
    let mut anchor_map = HashMap::new();
    anchors(&root, &mut anchor_map);
    for pair in root.children.as_chunks::<2>().0 {
        let key = pair[0].value.as_deref().unwrap_or_default();
        let node = &pair[1];
        if !seen.insert(key) && OWNED.contains(&key) {
            return Err(fail(&format!("duplicate frontmatter key {}", quoted(key))));
        }
        if !OWNED.contains(&key) {
            return Err(fail(&format!("unknown frontmatter key {}", quoted(key))));
        }
        let decoded = (|| -> Result<(), Error> {
            match key {
                "id" => p.id = scalar(node)?.into(),
                "title" => p.title = scalar(node)?.into(),
                "slug" => p.slug = scalar(node)?.into(),
                "status" => p.status = scalar(node)?.into(),
                "aliases" => p.aliases = list(node, &anchor_map)?,
                "sections" => p.sections = list(node, &anchor_map)?,
                "created" => p.created = timestamp::parse(scalar(node)?)?,
                "updated" => p.updated = timestamp::parse(scalar(node)?)?,
                _ => unreachable!(),
            }
            Ok(())
        })();
        decoded.map_err(|e| fail(&format!("{key}: {e}")))?;
    }
    for key in &OWNED[..7] {
        if !seen.contains(key) {
            return Err(fail(&format!("missing required key {}", quoted(key))));
        }
    }
    let zero = |v: &Timestamp| v.seconds == Timestamp::default().seconds && v.nanoseconds == 0;
    if p.id.is_empty()
        || p.title.is_empty()
        || !valid_status(&p.status)
        || !valid_slug(&p.slug)
        || zero(&p.created)
        || zero(&p.updated)
    {
        return Err(fail("invalid required frontmatter values"));
    }
    if !p.aliases.iter().any(|a| a.as_bytes() == p.id.as_bytes()) {
        return Err(fail("aliases must include id"));
    }
    (p.summary, p.graph) = parse_summary(path, &p.body)?;
    Ok(p)
}
