//! Restricted YAML for agent contracts, using the same document parser as bn.
use super::{
    frontmatter::{Error, Node, NodeKind},
    yaml,
};
use serde_json::{Map, Value};

pub fn restricted_yaml(text: &str) -> Result<Value, Error> {
    let root = yaml::parse_optional("agent contract", text)?
        .ok_or_else(|| Error::new("contract root must be an object".into()))?;
    if root.kind != NodeKind::Mapping {
        return Err(Error::new("contract root must be an object".into()));
    }
    value(&root)
}
fn value(node: &Node) -> Result<Value, Error> {
    if node.kind == NodeKind::Alias {
        return Err(Error::new("YAML aliases are not allowed".into()));
    }
    if !node.tag.is_empty() && !node.tag.starts_with("!!") {
        return Err(Error::new("custom YAML tags are not allowed".into()));
    }
    match node.kind {
        NodeKind::Mapping => {
            let mut map = Map::new();
            for pair in node.children.as_chunks::<2>().0 {
                let key = value(&pair[0])?;
                let key = key
                    .as_str()
                    .ok_or_else(|| Error::new("contract keys must be strings".into()))?;
                if map.insert(key.into(), value(&pair[1])?).is_some() {
                    return Err(Error::new(format!("duplicate contract key {key}")));
                }
            }
            Ok(Value::Object(map))
        }
        NodeKind::Sequence => node
            .children
            .iter()
            .map(value)
            .collect::<Result<Vec<_>, _>>()
            .map(Value::Array),
        NodeKind::Scalar => {
            let text = node.value.as_deref().unwrap_or_default();
            match node.tag.as_str() {
                "!!null" => Ok(Value::Null),
                "!!bool" => Ok(Value::Bool(text.eq_ignore_ascii_case("true"))),
                "!!int" | "!!float" => {
                    use super::yaml_value::YamlValue;
                    match super::yaml_value::scalar_value(node)
                        .map_err(|_| Error::new("invalid contract number".into()))?
                    {
                        YamlValue::Int(number) => Ok(number.into()),
                        YamlValue::Uint(number) => Ok(number.into()),
                        YamlValue::Float(number) => serde_json::Number::from_f64(number)
                            .map(Value::Number)
                            .ok_or_else(|| Error::new("contract numbers must be finite".into())),
                        _ => Err(Error::new("invalid contract number".into())),
                    }
                }
                _ => Ok(Value::String(text.into())),
            }
        }
        NodeKind::Alias => unreachable!(),
    }
}
#[cfg(test)]
mod tests {
    use super::restricted_yaml;
    #[test]
    fn contract_yaml_rejects_aliases_tags_duplicates_and_non_object_roots() {
        for text in [
            "[one, two]",
            "key: !custom value",
            "key: &x value\nother: *x",
            "key: one\nkey: two",
        ] {
            assert!(restricted_yaml(text).is_err(), "{text}");
        }
        let numeric = restricted_yaml("hex: 0x10\nfloat: .5\n").unwrap();
        assert_eq!(numeric["hex"], 16);
        assert_eq!(numeric["float"], 0.5);
        let got = restricted_yaml("version: 1\nactive: false\nitems: [one, two]\nquoted: 'true'\n")
            .unwrap();
        assert_eq!(got["version"], 1);
        assert_eq!(got["active"], false);
        assert_eq!(got["quoted"], "true");
        assert_eq!(got["items"], serde_json::json!(["one", "two"]));
    }
}
