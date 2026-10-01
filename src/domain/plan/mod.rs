//! Portable plan models. Lifecycle validation is distinct from manifest,
//! graph and filesystem bundle validation.
mod bundle;
mod parse;
pub use bundle::{
    MAX_BUNDLE_SIZE, MAX_FILE_SIZE, load, load_path, load_snapshot, load_snapshot_bytes,
    valid_section_path, write_scaffold, write_scaffold_path,
};
mod reference;
pub use reference::set_node_ref;
mod scaffold;
pub use super::yaml_string::YamlString;
pub use scaffold::scaffold;
mod timestamp;
pub use parse::{parse, parse_bytes};
mod encode;
pub use encode::encode;
mod graph;
pub mod id;
mod summary;
pub use graph::{GraphError, parse_graph, parse_graph_bytes};
pub use summary::{parse_summary, parse_summary_bytes};
mod lifecycle;
use super::issue::Timestamp;
pub use lifecycle::{valid_status, validate};
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;

pub const DRAFT: &str = "draft";
pub const BLOCKED: &str = "blocked";
pub const READY: &str = "ready";
pub const COMPLETE: &str = "complete";

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct Plan {
    pub id: String,
    pub aliases: Vec<YamlString>,
    pub title: String,
    pub slug: String,
    pub status: String,
    pub created: Timestamp,
    pub updated: Timestamp,
    pub sections: Vec<YamlString>,
    pub section_bodies: Vec<Section>,
    pub body: String,
    pub path: YamlString,
    pub summary: Summary,
    pub graph: ChangeGraph,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct Section {
    pub path: YamlString,
    pub markdown: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct Summary {
    pub outcome: String,
    pub affected_areas: String,
    pub execution_order: String,
    pub risks: String,
    pub change_graph: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct ChangeGraph {
    pub version: i64,
    pub nodes: Option<Vec<GraphNode>>,
    pub edges: Option<Vec<GraphEdge>>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct GraphNode {
    pub id: String,
    pub label: String,
    pub kind: String,
    #[serde(rename = "ref", skip_serializing_if = "String::is_empty")]
    pub reference: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct GraphEdge {
    pub from: String,
    pub to: String,
    pub kind: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub label: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Bundle {
    pub plan: Option<Plan>,
    pub sections: Vec<Section>,
    pub root: YamlString,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct BundleSnapshot {
    pub files: BTreeMap<YamlString, Vec<u8>>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct ValidationIssue {
    pub path: YamlString,
    #[serde(skip_serializing_if = "is_zero")]
    pub line: i64,
    pub code: YamlString,
    pub message: YamlString,
}
fn is_zero(value: &i64) -> bool {
    *value == 0
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ValidationError {
    pub issues: Vec<ValidationIssue>,
}
impl ValidationError {
    pub fn diagnostic(&self) -> super::error::Error {
        match self.issues.first() {
            Some(issue) => super::error::Error::from_bytes(
                [issue.path.as_bytes(), b": ", issue.message.as_bytes()].concat(),
            ),
            None => super::error::Error::new("invalid plan".into()),
        }
    }
}
impl std::fmt::Display for ValidationError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        std::fmt::Display::fmt(&self.diagnostic(), f)
    }
}
impl std::error::Error for ValidationError {}
