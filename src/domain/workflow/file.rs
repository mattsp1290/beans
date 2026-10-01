use super::{WorkflowConfig, WorkflowFile};
use crate::domain::{
    config::decode_workflow_toml,
    file_io::{path_error, path_name},
    frontmatter::Error,
};
use std::{fs::File, io::Read, path::Path};
pub fn decode_workflow_file(name: &str, data: &[u8]) -> Result<WorkflowFile, Error> {
    // filepath.Ext also treats a leading dot and a trailing dot as extensions.
    let base = name.rsplit('/').next().unwrap_or(name);
    let ext = base
        .rfind('.')
        .map(|i| base[i..].to_lowercase())
        .unwrap_or_default();
    let result = match ext.as_str() {
        ".toml" => decode_workflow_toml(data),
        ".yaml" | ".yml" => super::yaml::decode(data),
        _ => Err(Error(format!(
            "unsupported extension {} (use .toml, .yaml, or .yml)",
            crate::domain::yaml_string::YamlString::from(ext).quoted()
        ))),
    };
    result.map_err(|e| Error(format!("workflow config {name}: {e}")))
}
pub fn load_workflow(
    explicit: Option<&Path>,
    project_toml: &[u8],
    hub_toml: &[u8],
) -> Result<WorkflowConfig, Error> {
    let mut workflow = WorkflowConfig::built_in();
    if let Some(path) = explicit.filter(|p| !p.as_os_str().is_empty()) {
        let prefix = format!("workflow config {}", path_name(path));
        let mut file = File::open(path)
            .map_err(|e| Error(format!("{prefix}: {}", path_error("open", path, e))))?;
        let mut data = Vec::new();
        file.read_to_end(&mut data)
            .map_err(|e| Error(format!("{prefix}: {}", path_error("read", path, e))))?;
        let file = decode_workflow_file(&path_name(path).to_string(), &data)?;
        workflow = workflow.merge(&file);
        workflow
            .validate()
            .map_err(|e| Error(format!("{prefix}: {e}")))?;
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
            decode_workflow_file("beans.toml", data).map_err(|e| Error(format!("{name}: {e}")))?;
        workflow = workflow.merge(&file);
    }
    workflow.validate()?;
    Ok(workflow)
}
