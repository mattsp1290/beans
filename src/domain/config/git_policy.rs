//! Validated Git coordination policy shared by configuration and effects.
use crate::domain::{duration::parse_duration, frontmatter::Error, yaml_string::YamlString};
use serde::{Deserialize, Serialize};
use std::time::Duration;

#[derive(Clone, Debug)]
pub struct ExecutionPolicy {
    pub lock_timeout: Duration,
    pub command_timeout: Duration,
    pub network_timeout: Duration,
    pub cleanup_timeout: Duration,
    pub diagnostics: bool,
}
impl Default for ExecutionPolicy {
    fn default() -> Self {
        Self {
            lock_timeout: Duration::from_secs(30),
            command_timeout: Duration::from_secs(30),
            network_timeout: Duration::from_secs(60),
            cleanup_timeout: Duration::from_secs(10),
            diagnostics: false,
        }
    }
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
    pub fn policy(&self) -> Result<ExecutionPolicy, Error> {
        let duration = |key: &str, value: &YamlString, zero: bool| {
            let n = parse_duration(value.as_bytes())
                .map_err(|e| e.context(format!("git.{key}").as_bytes()))?;
            if n < 0 || (!zero && n == 0) {
                return Err(Error::new(format!(
                    "git.{key} must be a finite {}duration",
                    if zero { "nonnegative " } else { "positive " }
                )));
            }
            Ok(std::time::Duration::from_nanos(n as u64))
        };
        Ok(ExecutionPolicy {
            lock_timeout: duration("lock_timeout", &self.lock_timeout, true)?,
            command_timeout: duration("command_timeout", &self.command_timeout, false)?,
            network_timeout: duration("network_timeout", &self.network_timeout, false)?,
            cleanup_timeout: duration("cleanup_timeout", &self.cleanup_timeout, false)?,
            diagnostics: self.diagnostics,
        })
    }
}
