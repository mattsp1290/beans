use crate::{
    domain::{config, frontmatter::Error},
    gitops::{check_hub_write_path, write_hub_file},
    vault::{read_public_file, valid_project_name},
};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::{collections::BTreeMap, path::Path};

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub struct GithubRepository {
    pub id: String,
    pub full_name: String,
    pub html_url: String,
}
impl GithubRepository {
    fn validate(&self) -> Result<(), Error> {
        let parts: Vec<_> = self.full_name.split('/').collect();
        if self.id.is_empty()
            || !self.id.bytes().all(|b| b.is_ascii_digit())
            || self.id.parse::<u64>().is_err()
            || self.id.starts_with('0')
            || parts.len() != 2
            || parts.iter().any(|p| {
                p.is_empty()
                    || *p == "."
                    || *p == ".."
                    || p.len() > 100
                    || !p
                        .bytes()
                        .all(|b| b.is_ascii_alphanumeric() || b"-_.".contains(&b))
            })
            || self.html_url != format!("https://github.com/{}", self.full_name)
        {
            return Err(Error::new(
                "invalid GitHub repository identity or canonical URL".into(),
            ));
        }
        Ok(())
    }
}
#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case", deny_unknown_fields)]
pub enum ProjectChange {
    Create {
        name: Option<String>,
        repositories: Vec<GithubRepository>,
    },
    Rename {
        name: String,
    },
    AddRepository {
        repository: GithubRepository,
    },
    RemoveRepository {
        repository_id: String,
    },
    Archive,
}
#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ProjectRequest {
    pub schema_version: u32,
    pub project_id: String,
    pub operation_id: String,
    pub expected_revision: Option<String>,
    pub change: ProjectChange,
}
#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct ProjectState {
    repositories: Vec<GithubRepository>,
    archived: bool,
    operations: BTreeMap<String, String>,
}
#[derive(Clone, Debug, Serialize)]
pub struct ProjectSnapshot {
    pub id: String,
    pub name: String,
    pub revision: String,
    pub archived: bool,
    pub repositories: Vec<GithubRepository>,
}
fn digest(bytes: &[u8]) -> String {
    format!(
        "sha256:{}",
        Sha256::digest(bytes)
            .iter()
            .map(|b| format!("{b:02x}"))
            .collect::<String>()
    )
}
fn valid_operation(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= 128
        && value
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b"-_".contains(&b))
}
fn validate_name(name: &str) -> Result<(), Error> {
    if name.trim().is_empty() || name.len() > 200 || name.chars().any(char::is_control) {
        return Err(Error::new(
            "project display name must contain 1 to 200 printable bytes".into(),
        ));
    }
    Ok(())
}
fn path(id: &str) -> Result<String, Error> {
    if !valid_project_name(id.as_bytes()) || id.len() > 100 {
        return Err(Error::new("invalid project identity".into()));
    }
    Ok(format!("projects/{id}/beans.toml"))
}
fn state(source: &[u8]) -> Result<Option<ProjectState>, Error> {
    config::decode_project_config(source)?;
    let text = std::str::from_utf8(source).map_err(|e| Error::new(e.to_string()))?;
    let fields: BTreeMap<String, toml::Value> =
        toml::from_str(text).map_err(|e| Error::new(e.to_string()))?;
    let matching: Vec<_> = fields
        .iter()
        .filter(|(key, _)| config::equal_field(key, "github_project"))
        .collect();
    if matching.len() > 1 {
        return Err(Error::new("ambiguous project field github_project".into()));
    }
    let Some((_, value)) = matching.first() else {
        return Ok(None);
    };
    let state: ProjectState = (*value)
        .clone()
        .try_into()
        .map_err(|e: toml::de::Error| Error::new(e.to_string()))?;
    if state.repositories.is_empty() {
        return Err(Error::new(
            "managed project has empty repository membership".into(),
        ));
    }
    let mut ids = std::collections::BTreeSet::new();
    for repository in &state.repositories {
        repository.validate()?;
        if !ids.insert(&repository.id) {
            return Err(Error::new(
                "managed project has duplicate repository identities".into(),
            ));
        }
    }
    if state.operations.iter().any(|(id, hash)| {
        !valid_operation(id)
            || hash.len() != 71
            || !hash.starts_with("sha256:")
            || !hash[7..].bytes().all(|b| b.is_ascii_hexdigit())
    }) {
        return Err(Error::new(
            "invalid managed project operation receipt".into(),
        ));
    }
    Ok(Some(state))
}
pub fn managed(source: &[u8]) -> Result<bool, Error> {
    Ok(state(source)?.is_some())
}
fn snapshot(id: &str, source: &[u8], state: ProjectState) -> Result<ProjectSnapshot, Error> {
    let config = config::decode_project_config(source)?;
    Ok(ProjectSnapshot {
        id: id.into(),
        name: config.name.to_string(),
        revision: digest(source),
        archived: state.archived,
        repositories: state.repositories,
    })
}
pub fn inspect(hub: &Path, id: &str) -> Result<ProjectSnapshot, Error> {
    let source = read_public_file(hub, &path(id)?)?;
    let state = state(&source)?
        .ok_or_else(|| Error::new("project is not a managed GitHub project".into()))?;
    snapshot(id, &source, state)
}
pub fn catalog(hub: &Path) -> Result<Vec<ProjectSnapshot>, Error> {
    let Some(ids) = crate::vault::project_dirs(hub)? else {
        return Ok(vec![]);
    };
    let mut projects = Vec::new();
    for id in ids {
        let id = std::str::from_utf8(&id).map_err(|e| Error::new(e.to_string()))?;
        let source = read_public_file(hub, &path(id)?)?;
        if let Some(state) = state(&source)?
            && !state.archived
        {
            projects.push(snapshot(id, &source, state)?);
        }
    }
    Ok(projects)
}
fn edit(source: &[u8], values: BTreeMap<&str, toml::Value>) -> Result<Vec<u8>, Error> {
    let text = std::str::from_utf8(source).map_err(|e| Error::new(e.to_string()))?;
    let fields: BTreeMap<String, toml::Spanned<toml::Value>> =
        toml::from_str(text).map_err(|e| Error::new(e.to_string()))?;
    let mut changes = Vec::new();
    let mut missing = String::new();
    for (key, value) in values {
        let fields: Vec<_> = fields
            .iter()
            .filter(|(k, _)| config::equal_field(k, key))
            .collect();
        if fields.len() > 1 {
            return Err(Error::new(format!("ambiguous project field {key}")));
        }
        if let Some((_, field)) = fields.first() {
            changes.push((field.span(), value.to_string()));
        } else {
            missing.push_str(&format!("{key} = {value}\n"));
        }
    }
    changes.sort_by_key(|(span, _)| span.start);
    let mut out = missing.into_bytes();
    let mut from = 0;
    for (span, value) in changes {
        out.extend_from_slice(&source[from..span.start]);
        out.extend_from_slice(value.as_bytes());
        from = span.end;
    }
    out.extend_from_slice(&source[from..]);
    config::decode_project_config(&out)?;
    Ok(out)
}
impl ProjectRequest {
    pub fn validate(&self) -> Result<(), Error> {
        path(&self.project_id)?;
        if self.schema_version != 1 || !valid_operation(&self.operation_id) {
            return Err(Error::new(
                "invalid project request schema or operation identity".into(),
            ));
        }
        let creating = matches!(self.change, ProjectChange::Create { .. });
        if creating != self.expected_revision.is_none() {
            return Err(Error::new(
                "create requires null expected_revision; edits require expected_revision".into(),
            ));
        }
        match &self.change {
            ProjectChange::Create { name, repositories } => {
                if let Some(name) = name {
                    validate_name(name)?;
                }
                if repositories.is_empty() || repositories.len() > 100 {
                    return Err(Error::new("project requires 1 to 100 repositories".into()));
                }
                for repository in repositories {
                    repository.validate()?;
                }
            }
            ProjectChange::Rename { name } => validate_name(name)?,
            ProjectChange::AddRepository { repository } => repository.validate()?,
            ProjectChange::RemoveRepository { repository_id } => {
                if repository_id.is_empty()
                    || !repository_id.bytes().all(|b| b.is_ascii_digit())
                    || repository_id.parse::<u64>().is_err()
                    || repository_id.starts_with('0')
                {
                    return Err(Error::new("invalid repository identity".into()));
                }
            }
            ProjectChange::Archive => {}
        }
        Ok(())
    }
    pub(crate) fn apply(
        &self,
        hub: &Path,
    ) -> Result<(Vec<std::path::PathBuf>, ProjectSnapshot, bool), Error> {
        self.validate()?;
        let relative = path(&self.project_id)?;
        check_hub_write_path(hub, Path::new(&relative))?;
        let source = match std::fs::symlink_metadata(hub.join(&relative)) {
            Ok(_) => Some(read_public_file(hub, &relative)?),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => None,
            Err(e) => return Err(Error::new(e.to_string())),
        };
        let request_hash =
            digest(&serde_json::to_vec(self).map_err(|e| Error::new(e.to_string()))?);
        let old = source.as_deref().map(state).transpose()?.flatten();
        if let Some(previous) = old
            .as_ref()
            .and_then(|s| s.operations.get(&self.operation_id))
        {
            if previous != &request_hash {
                return Err(Error::new(
                    "project operation identity reused with conflicting request".into(),
                ));
            }
            return Ok((
                vec![],
                snapshot(&self.project_id, source.as_deref().unwrap(), old.unwrap())?,
                true,
            ));
        }
        let (mut state, mut name) = match (&self.change, source.as_deref(), old) {
            (ProjectChange::Create { name, repositories }, None, None) => {
                let mut unique = Vec::new();
                for repository in repositories {
                    if !unique
                        .iter()
                        .any(|r: &GithubRepository| r.id == repository.id)
                    {
                        unique.push(repository.clone());
                    }
                }
                let name = name
                    .clone()
                    .unwrap_or_else(|| unique[0].full_name.split('/').nth(1).unwrap().into());
                (
                    ProjectState {
                        repositories: unique,
                        archived: false,
                        operations: BTreeMap::new(),
                    },
                    name,
                )
            }
            (ProjectChange::Create { .. }, _, _) => {
                return Err(Error::new("project identity already exists".into()));
            }
            (_, Some(source), Some(state)) => {
                if Some(digest(source)) != self.expected_revision {
                    return Err(Error::new(
                        "project revision conflict; reload before editing".into(),
                    ));
                }
                if state.archived {
                    return Err(Error::new("project is archived".into()));
                }
                (
                    state,
                    config::decode_project_config(source)?.name.to_string(),
                )
            }
            _ => return Err(Error::new("managed project not found".into())),
        };
        match &self.change {
            ProjectChange::Create { .. } => {}
            ProjectChange::Rename { name: new } => name = new.clone(),
            ProjectChange::AddRepository { repository } => {
                if let Some(existing) = state
                    .repositories
                    .iter_mut()
                    .find(|r| r.id == repository.id)
                {
                    *existing = repository.clone();
                } else {
                    if state.repositories.len() >= 100 {
                        return Err(Error::new("project repository limit reached".into()));
                    }
                    state.repositories.push(repository.clone());
                }
            }
            ProjectChange::RemoveRepository { repository_id } => {
                state.repositories.retain(|r| r.id != *repository_id);
                if state.repositories.is_empty() {
                    return Err(Error::new(
                        "cannot remove the last project repository".into(),
                    ));
                }
            }
            ProjectChange::Archive => state.archived = true,
        }
        state
            .operations
            .insert(self.operation_id.clone(), request_hash);
        let mut values = BTreeMap::from([
            ("name", toml::Value::String(name)),
            (
                "remotes",
                toml::Value::Array(
                    state
                        .repositories
                        .iter()
                        .map(|r| toml::Value::String(r.html_url.clone()))
                        .collect(),
                ),
            ),
            (
                "github_project",
                toml::Value::try_from(&state).map_err(|e| Error::new(e.to_string()))?,
            ),
        ]);
        if source.is_none() {
            values.insert("prefix", toml::Value::String(self.project_id.clone()));
        }
        let bytes = edit(source.as_deref().unwrap_or_default(), values)?;
        let result = snapshot(&self.project_id, &bytes, state)?;
        write_hub_file(hub, Path::new(&relative), &bytes)?;
        Ok((vec![relative.into()], result, false))
    }
}
