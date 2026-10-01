use super::{WorkflowConfig, WorkflowFile};
use crate::domain::{
    config::decode_workflow_toml,
    file_io::{path_error, path_name},
    frontmatter::Error,
};
use std::{fs::File, io::Read, path::Path};
pub fn decode_workflow_file(name: &str, data: &[u8]) -> Result<WorkflowFile, Error> {
    decode_workflow_file_bytes(name.as_bytes(), data)
}
pub fn decode_workflow_file_bytes(name: &[u8], data: &[u8]) -> Result<WorkflowFile, Error> {
    // filepath.Ext also treats a leading dot and a trailing dot as extensions.
    let base = name.rsplit(|b| *b == b'/').next().unwrap_or(name);
    let ext = base
        .iter()
        .rposition(|b| *b == b'.')
        .map(|i| {
            crate::domain::yaml_string::YamlString::from_bytes(base[i..].into())
                .to_string()
                .to_lowercase()
        })
        .unwrap_or_default();
    let result = match ext.as_str() {
        ".toml" => decode_workflow_toml(data),
        ".yaml" | ".yml" => super::yaml::decode(data),
        _ => Err(Error::new(format!(
            "unsupported extension {} (use .toml, .yaml, or .yml)",
            crate::domain::yaml_string::YamlString::from(ext).quoted()
        ))),
    };
    let mut prefix = b"workflow config ".to_vec();
    prefix.extend_from_slice(name);
    result.map_err(|e| e.context(&prefix))
}
pub fn load_workflow(
    explicit: Option<&Path>,
    project_toml: &[u8],
    hub_toml: &[u8],
) -> Result<WorkflowConfig, Error> {
    let mut workflow = WorkflowConfig::built_in();
    if let Some(path) = explicit.filter(|p| !p.as_os_str().is_empty()) {
        let mut prefix = b"workflow config ".to_vec();
        prefix.extend_from_slice(path_name(path).as_bytes());
        let mut file =
            File::open(path).map_err(|e| path_error("open", path, e).context(&prefix))?;
        let mut data = Vec::new();
        file.read_to_end(&mut data)
            .map_err(|e| path_error("read", path, e).context(&prefix))?;
        let file = decode_workflow_file_bytes(path_name(path).as_bytes(), &data)?;
        workflow = workflow.merge(&file);
        workflow.validate().map_err(|e| e.context(&prefix))?;
        return Ok(workflow);
    }
    for (name, data) in [
        ("hub beans.toml", hub_toml),
        ("project beans.toml", project_toml),
    ] {
        if data.is_empty() {
            continue;
        }
        let file =
            decode_workflow_file("beans.toml", data).map_err(|e| e.context(name.as_bytes()))?;
        workflow = workflow.merge(&file);
    }
    workflow.validate()?;
    Ok(workflow)
}
