//! YAML trees authored for new documents. Existing unknown frontmatter remains
//! in immutable source spans and is never serialized through this module.
use serde::{Deserialize, Serialize};

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Kind {
    #[default]
    Zero,
    Scalar,
    Sequence,
    Mapping,
    Alias,
}

pub const TAGGED: u8 = 1;
pub const DOUBLE_QUOTED: u8 = 2;
pub const SINGLE_QUOTED: u8 = 4;
pub const LITERAL: u8 = 8;
pub const FOLDED: u8 = 16;
pub const FLOW: u8 = 32;

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct Node {
    pub kind: Kind,
    pub tag: String,
    pub value: String,
    pub style: u8,
    pub anchor: String,
    /// Mapping children alternate keys and values, preserving order/duplicates.
    pub content: Vec<Node>,
    pub head_comment: String,
    pub line_comment: String,
    pub foot_comment: String,
}

impl Node {
    pub fn string(value: impl Into<String>) -> Self {
        Self {
            kind: Kind::Scalar,
            tag: "!!str".into(),
            value: value.into(),
            ..Self::default()
        }
    }
    pub fn mapping(entries: impl IntoIterator<Item = (Node, Node)>) -> Self {
        Self {
            kind: Kind::Mapping,
            tag: "!!map".into(),
            content: entries.into_iter().flat_map(|(k, v)| [k, v]).collect(),
            ..Self::default()
        }
    }
    pub fn sequence(values: impl IntoIterator<Item = Node>) -> Self {
        Self {
            kind: Kind::Sequence,
            tag: "!!seq".into(),
            content: values.into_iter().collect(),
            ..Self::default()
        }
    }
}
