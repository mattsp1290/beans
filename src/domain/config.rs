//! Configuration models. Decoding and workflow source precedence are separate.
use super::{duration::parse_duration, workflow::WorkflowFile, yaml_string::YamlString};
use serde::{Deserialize, Serialize};
mod decode;
mod encode;
mod toml_metadata;
pub(crate) use decode::decode_workflow_toml;
pub(crate) use decode::equal_field;
pub use decode::{
    decode_hub_config, decode_project_config, decode_user_config, load_hub_config,
    load_project_config, load_user_config,
};
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
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct UserGitConfig {
    pub lock_timeout: YamlString,
    pub command_timeout: YamlString,
    pub network_timeout: YamlString,
    pub cleanup_timeout: YamlString,
    pub diagnostics: bool,
}
impl Default for UserGitConfig {
    fn default() -> Self {
        Self {
            lock_timeout: "30s".into(),
            command_timeout: "30s".into(),
            network_timeout: "60s".into(),
            cleanup_timeout: "10s".into(),
            diagnostics: false,
        }
    }
}
impl UserGitConfig {
    pub fn is_default(&self) -> bool {
        self == &Self::default()
    }
    pub fn policy(&self) -> Result<crate::gitops::ExecutionPolicy, super::frontmatter::Error> {
        let duration = |key: &str, value: &YamlString, zero: bool| {
            let n = parse_duration(value.as_bytes())
                .map_err(|e| e.context(format!("git.{key}").as_bytes()))?;
            if n < 0 || (!zero && n == 0) {
                return Err(super::frontmatter::Error::new(format!(
                    "git.{key} must be a finite {}duration",
                    if zero { "nonnegative " } else { "positive " }
                )));
            }
            Ok(std::time::Duration::from_nanos(n as u64))
        };
        Ok(crate::gitops::ExecutionPolicy {
            lock_timeout: duration("lock_timeout", &self.lock_timeout, true)?,
            command_timeout: duration("command_timeout", &self.command_timeout, false)?,
            network_timeout: duration("network_timeout", &self.network_timeout, false)?,
            cleanup_timeout: duration("cleanup_timeout", &self.cleanup_timeout, false)?,
            diagnostics: self.diagnostics,
        })
    }
}
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct UserConfig {
    pub actor: YamlString,
    pub hub: UserHubConfig,
    pub fetch: UserFetchConfig,
    #[serde(skip_serializing_if = "UserGitConfig::is_default")]
    pub git: UserGitConfig,
}
impl UserConfig {
    pub fn throttle_duration(&self) -> i64 {
        parse_duration(self.fetch.throttle.trimmed().as_bytes())
            .ok()
            .filter(|d| *d >= 0)
            .unwrap_or(DEFAULT_FETCH_THROTTLE)
    }
}
