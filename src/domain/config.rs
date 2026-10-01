//! Configuration models. Decoding and workflow source precedence are separate.
use super::{duration::parse_duration, workflow::WorkflowFile, yaml_string::YamlString};
use serde::{Deserialize, Serialize};
mod encode;
pub use encode::{encode_project_config, encode_user_config};
pub const DEFAULT_FETCH_THROTTLE: i64 = 60_000_000_000;
const DEFAULT_TYPES: &[&str] = &["task", "bug", "feature", "epic", "chore"];
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct TypesConfig {
    pub names: Option<Vec<YamlString>>,
}
impl TypesConfig {
    pub fn built_in() -> Self {
        Self {
            names: Some(DEFAULT_TYPES.iter().map(|s| (*s).into()).collect()),
        }
    }
    pub fn valid_type(&self, name: &[u8]) -> bool {
        match &self.names {
            Some(names) if !names.is_empty() => names.iter().any(|s| s.as_bytes() == name),
            _ => DEFAULT_TYPES.iter().any(|s| s.as_bytes() == name),
        }
    }
}
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct IDsConfig {
    pub length: i64,
}
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct HubConfig {
    pub workflow: WorkflowFile,
    pub types: TypesConfig,
    pub ids: IDsConfig,
}
impl HubConfig {
    pub fn built_in() -> Self {
        Self {
            types: TypesConfig::built_in(),
            ids: IDsConfig { length: 4 },
            ..Self::default()
        }
    }
}
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct ProjectConfig {
    pub name: YamlString,
    pub prefix: YamlString,
    pub remotes: Option<Vec<YamlString>>,
    pub workflow: WorkflowFile,
}
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct UserHubConfig {
    pub remote: YamlString,
    pub branch: YamlString,
}
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct UserFetchConfig {
    pub throttle: YamlString,
}
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct UserConfig {
    pub actor: YamlString,
    pub hub: UserHubConfig,
    pub fetch: UserFetchConfig,
}
impl UserConfig {
    pub fn throttle_duration(&self) -> i64 {
        parse_duration(self.fetch.throttle.trimmed().as_bytes())
            .ok()
            .filter(|d| *d >= 0)
            .unwrap_or(DEFAULT_FETCH_THROTTLE)
    }
}
