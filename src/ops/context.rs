use crate::{
    domain::{
        config::{TypesConfig, load_hub_config, load_project_config},
        workflow::{WorkflowConfig, load_workflow},
        yaml_string::YamlString,
    },
    vault::paths::join,
};
use std::{
    ffi::OsString,
    fs::File,
    io::Read,
    os::unix::ffi::{OsStrExt, OsStringExt},
    path::{Path, PathBuf},
};
fn joined(parts: &[&[u8]]) -> PathBuf {
    OsString::from_vec(join(parts)).into()
}
// os.ReadFile retains partial data even when reading fails; these operation
// reads intentionally ignore the error while keeping any bytes received.
fn read_ignoring_error(path: &Path) -> Vec<u8> {
    let mut bytes = Vec::new();
    if let Ok(mut file) = File::open(path) {
        let _ = file.read_to_end(&mut bytes);
    }
    bytes
}
/// Snapshot hub bytes/config at operation-context construction, while project
/// and explicit workflow files remain live for each workflow lookup.
pub struct OperationConfig {
    hub: PathBuf,
    explicit: Option<PathBuf>,
    hub_toml: Vec<u8>,
    pub types: TypesConfig,
    pub id_length: i64,
}
impl OperationConfig {
    pub fn load(hub: &Path, bn_config: &[u8]) -> Self {
        let explicit = YamlString::from_bytes(bn_config.into())
            .trimmed()
            .as_bytes()
            .to_vec();
        let hub_path = joined(&[hub.as_os_str().as_bytes(), b"beans.toml"]);
        let hub_toml = read_ignoring_error(&hub_path);
        // A malformed hub yields zero types/ID length in opsEnv, distinct from
        // the defaults returned for an absent hub configuration file.
        let cfg = load_hub_config(&hub_path).unwrap_or_default();
        Self {
            hub: hub.into(),
            explicit: (!explicit.is_empty()).then(|| OsString::from_vec(explicit).into()),
            hub_toml,
            types: cfg.types,
            id_length: cfg.ids.length,
        }
    }
    pub fn workflow_for(&self, project: &[u8]) -> WorkflowConfig {
        let path = joined(&[
            self.hub.as_os_str().as_bytes(),
            b"projects",
            project,
            b"beans.toml",
        ]);
        let project_toml = read_ignoring_error(&path);
        load_workflow(self.explicit.as_deref(), &project_toml, &self.hub_toml)
            .unwrap_or_else(|_| WorkflowConfig::built_in())
    }
}
pub fn prefix_for(hub: &Path, project: &[u8]) -> Vec<u8> {
    let path = joined(&[
        hub.as_os_str().as_bytes(),
        b"projects",
        project,
        b"beans.toml",
    ]);
    if let Ok(cfg) = load_project_config(&path)
        && !cfg.prefix.trimmed().as_bytes().is_empty()
    {
        return cfg.prefix.as_bytes().into();
    }
    project.into()
}
