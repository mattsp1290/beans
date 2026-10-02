//! Shared scalar validation and aggregate diagnostics.
use super::{
    frontmatter::{Error, Node, NodeKind},
    issue::quoted,
    yaml_string::YamlString,
};
pub(crate) fn duplicate_errors(node: &Node) -> Vec<String> {
    let pairs: Vec<_> = node.children.as_chunks::<2>().0.iter().collect();
    let mut errors = Vec::new();
    for i in 0..pairs.len() {
        for j in i + 1..pairs.len() {
            if pairs[i][0].kind == pairs[j][0].kind
                && key_text(&pairs[i][0]) == key_text(&pairs[j][0])
            {
                errors.push(format!(
                    "line {}: mapping key {} already defined at line {}",
                    pairs[j][0].line,
                    quoted(key_text(&pairs[j][0])),
                    pairs[i][0].line
                ));
            }
        }
    }
    errors
}
pub(crate) fn type_error(node: &Node, target: &str) -> String {
    let value = node.value.as_deref().unwrap_or_default();
    let snippet = if matches!(node.tag.as_str(), "!!seq" | "!!map") {
        String::new()
    } else {
        let value = if value.len() > 10 {
            // Diagnostic snippets bound their length to seven bytes, replacing
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
pub(crate) fn decoded_string(node: &Node) -> Result<YamlString, Error> {
    let value = node.value.as_deref().unwrap_or_default();
    let resolved = crate::domain::yaml_render::implicit_tag(value);
    if matches!(
        node.tag.as_str(),
        "!!null" | "!!int" | "!!float" | "!!bool" | "!!timestamp"
    ) && resolved != node.tag
        && !(node.tag == "!!float" && resolved == "!!int")
    {
        return Err(Error::new(format!(
            "yaml: cannot decode {resolved} `{value}` as a {}",
            node.tag
        )));
    }
    if node.tag == "!!binary" {
        return crate::domain::yaml_string::binary(value)
            .map(YamlString::from_bytes)
            .ok_or_else(|| Error::new("yaml: !!binary value contains invalid base64 data".into()));
    }
    Ok(value.into())
}

fn key_text(node: &Node) -> &str {
    if node.kind == NodeKind::Alias {
        &node.anchor_name
    } else {
        node.value.as_deref().unwrap_or_default()
    }
}
