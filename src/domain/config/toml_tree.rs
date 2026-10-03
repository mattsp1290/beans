//! Parsed TOML values retain real byte spans for key-context diagnostics.
use serde::{
    Deserialize,
    de::{MapAccess, SeqAccess, Visitor},
};
use std::fmt;
use toml::Spanned;

#[derive(Debug)]
pub(super) enum Node {
    String(String),
    Integer(i64),
    Float,
    Bool(bool),
    Array(Vec<Spanned<Node>>),
    Table(Vec<(String, Spanned<Node>)>),
}
impl<'de> Deserialize<'de> for Node {
    fn deserialize<D: serde::Deserializer<'de>>(de: D) -> Result<Self, D::Error> {
        struct NodeVisitor;
        impl<'de> Visitor<'de> for NodeVisitor {
            type Value = Node;
            fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
                f.write_str("a TOML value")
            }
            fn visit_str<E: serde::de::Error>(self, s: &str) -> Result<Node, E> {
                Ok(Node::String(s.into()))
            }
            fn visit_string<E: serde::de::Error>(self, s: String) -> Result<Node, E> {
                Ok(Node::String(s))
            }
            fn visit_i64<E: serde::de::Error>(self, n: i64) -> Result<Node, E> {
                Ok(Node::Integer(n))
            }
            fn visit_u64<E: serde::de::Error>(self, n: u64) -> Result<Node, E> {
                i64::try_from(n)
                    .map(Node::Integer)
                    .map_err(|_| E::custom(format!("{n} is out of range for int64")))
            }
            fn visit_i128<E: serde::de::Error>(self, n: i128) -> Result<Node, E> {
                i64::try_from(n)
                    .map(Node::Integer)
                    .map_err(|_| E::custom(format!("{n} is out of range for int64")))
            }
            fn visit_u128<E: serde::de::Error>(self, n: u128) -> Result<Node, E> {
                i64::try_from(n)
                    .map(Node::Integer)
                    .map_err(|_| E::custom(format!("{n} is out of range for int64")))
            }
            fn visit_f64<E: serde::de::Error>(self, _: f64) -> Result<Node, E> {
                Ok(Node::Float)
            }
            fn visit_bool<E: serde::de::Error>(self, value: bool) -> Result<Node, E> {
                Ok(Node::Bool(value))
            }
            fn visit_seq<A: SeqAccess<'de>>(self, mut seq: A) -> Result<Node, A::Error> {
                let mut values = Vec::new();
                while let Some(value) = seq.next_element()? {
                    values.push(value);
                }
                Ok(Node::Array(values))
            }
            fn visit_map<A: MapAccess<'de>>(self, mut map: A) -> Result<Node, A::Error> {
                let mut values = Vec::new();
                while let Some(key) = map.next_key::<String>()? {
                    // Serde represents a date as a map whose string payload has
                    // no span. The enclosing real span distinguishes this from
                    // an authored table with the same private field name.
                    let value = if key == "$__toml_private_datetime" {
                        Spanned::new(0..0, map.next_value::<Node>()?)
                    } else {
                        map.next_value()?
                    };
                    values.push((key, value));
                }
                Ok(Node::Table(values))
            }
        }
        de.deserialize_any(NodeVisitor)
    }
}

pub(super) fn line(source: &str, offset: usize) -> usize {
    1 + source.as_bytes()[..offset.min(source.len())]
        .iter()
        .filter(|&&b| b == b'\n')
        .count()
}
impl Node {
    pub(super) fn go_type(&self, source: &str, span: std::ops::Range<usize>) -> &'static str {
        match self {
            Self::String(_) => "string",
            Self::Integer(_) => "int64",
            Self::Float => "float64",
            Self::Bool(_) => "bool",
            Self::Array(_) => "[]any",
            Self::Table(values) => {
                let text = source.get(span).unwrap_or_default();
                if values.len() == 1
                    && values[0].0 == "$__toml_private_datetime"
                    && text.as_bytes().first().is_some_and(u8::is_ascii_digit)
                {
                    "time.Time"
                } else {
                    "map[string]any"
                }
            }
        }
    }
}
