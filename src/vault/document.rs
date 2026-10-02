//! Forgiving document metadata, distinct from typed note frontmatter.
use crate::domain::yaml_string::YamlString;
pub use crate::domain::yaml_value::YamlValue;
use std::collections::BTreeMap;
#[derive(Clone, Debug, PartialEq)]
pub struct DocMetadata {
    pub frontmatter: Option<BTreeMap<Vec<u8>, YamlValue>>,
    pub body: Vec<u8>,
    pub title: Vec<u8>,
    pub tags: Option<Vec<Vec<u8>>>,
}
pub fn split_doc_frontmatter(data: &[u8]) -> (Option<&[u8]>, &[u8]) {
    let Some(rest) = data.strip_prefix(b"---\n") else {
        return (None, data);
    };
    let mut offset = 0;
    for line in rest.split_inclusive(|&b| b == b'\n') {
        let trimmed = line.strip_suffix(b"\n").unwrap_or(line);
        let trimmed = trimmed.strip_suffix(b"\r").unwrap_or(trimmed);
        if trimmed == b"---" {
            return (Some(&rest[..offset]), &rest[offset + line.len()..]);
        }
        offset += line.len();
    }
    (None, data)
}
pub fn doc_metadata(basename: &[u8], data: &[u8]) -> DocMetadata {
    let (yaml, body) = split_doc_frontmatter(data);
    let frontmatter = yaml.and_then(crate::domain::yaml_value::string_map);
    let field = |key: &[u8]| frontmatter.as_ref().and_then(|m| m.get(key));
    let mut title = match field(b"title") {
        Some(YamlValue::String(s)) => s.clone(),
        _ => Vec::new(),
    };
    if title.is_empty() {
        for line in body.split(|&b| b == b'\n') {
            let trimmed = YamlString::from_bytes(line.into()).trimmed();
            if let Some(heading) = trimmed.as_bytes().strip_prefix(b"# ") {
                title = YamlString::from_bytes(heading.into())
                    .trimmed()
                    .as_bytes()
                    .into();
                break;
            }
        }
    }
    if title.is_empty() {
        title = basename.into();
    }
    let tags = match field(b"tags") {
        Some(YamlValue::String(s)) if !s.is_empty() => Some(vec![s.clone()]),
        Some(YamlValue::Sequence(items)) => Some(
            items
                .iter()
                .filter_map(|v| {
                    if let YamlValue::String(s) = v {
                        Some(s.clone())
                    } else {
                        None
                    }
                })
                .collect(),
        ),
        _ => None,
    };
    DocMetadata {
        frontmatter,
        body: body.into(),
        title,
        tags,
    }
}
