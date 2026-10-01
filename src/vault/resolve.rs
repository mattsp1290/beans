use super::{
    git::{GitResolver, SystemGit},
    normalize_remote_url,
    paths::join,
    project_name, valid_project_name,
};
use crate::domain::{
    config::load_project_config, file_io::path_error, frontmatter::Error, yaml_string::YamlString,
};
use std::{
    ffi::OsString,
    os::unix::ffi::{OsStrExt, OsStringExt},
    path::{Path, PathBuf},
};
pub const OUTSIDE_REPO: &str =
    "not inside a git repository; pass --project <name> or run inside the project's repository";
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Resolved {
    pub hub_dir: PathBuf,
    pub project: Vec<u8>,
    pub project_dir: PathBuf,
    pub repo_root: Vec<u8>,
    pub repo_remote: Vec<u8>,
    pub repo_head: Vec<u8>,
    pub repo_branch: Vec<u8>,
    pub created: bool,
    pub notice: String,
    pub candidate: Vec<u8>,
}
#[derive(Default)]
pub struct ResolveOptions<'a> {
    pub cwd: Option<&'a Path>,
    pub flag_project: &'a [u8],
    pub write: bool,
    pub all_projects: bool,
    pub git: Option<&'a dyn GitResolver>,
    pub env: Option<&'a EnvironmentLookup<'a>>,
}
pub type EnvironmentLookup<'a> = dyn Fn(&str) -> Vec<u8> + 'a;
fn path(bytes: Vec<u8>) -> PathBuf {
    OsString::from_vec(bytes).into()
}
fn project_path(hub: &Path, name: &[u8]) -> PathBuf {
    path(join(&[hub.as_os_str().as_bytes(), b"projects", name]))
}
fn config_path(hub: &Path, name: &[u8]) -> PathBuf {
    let dir = project_path(hub, name);
    path(join(&[dir.as_os_str().as_bytes(), b"beans.toml"]))
}
fn exists(hub: &Path, name: &[u8]) -> bool {
    std::fs::metadata(config_path(hub, name)).is_ok()
}
fn text(bytes: &[u8]) -> YamlString {
    YamlString::from_bytes(bytes.into())
}
fn matches(remotes: Option<&[YamlString]>, want: &[u8]) -> bool {
    remotes
        .unwrap_or_default()
        .iter()
        .any(|r| normalize_remote_url(r.as_bytes()).unwrap_or_else(|_| r.as_bytes().into()) == want)
}
fn directories(hub: &Path) -> Result<Vec<Vec<u8>>, Error> {
    let dir = path(join(&[hub.as_os_str().as_bytes(), b"projects"]));
    let entries = match std::fs::read_dir(&dir) {
        Ok(entries) => entries,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(Vec::new()),
        Err(e) => return Err(path_error("open", &dir, e)),
    };
    let mut names = Vec::new();
    for entry in entries {
        let entry = entry.map_err(|e| path_error("readdirent", &dir, e))?;
        let name = entry.file_name().as_bytes().to_vec();
        if !name.starts_with(b".")
            && entry
                .file_type()
                .map_err(|e| path_error("lstat", &entry.path(), e))?
                .is_dir()
        {
            names.push(name);
        }
    }
    names.sort();
    Ok(names)
}
pub fn project_dirs(hub: &Path) -> Result<Option<Vec<Vec<u8>>>, Error> {
    let names: Vec<_> = directories(hub)?
        .into_iter()
        .filter(|name| exists(hub, name))
        .collect();
    Ok((!names.is_empty()).then_some(names))
}
pub fn resolve(hub: &Path, opts: ResolveOptions<'_>) -> Result<Resolved, Error> {
    let cwd = match opts.cwd.filter(|p| !p.as_os_str().is_empty()) {
        Some(cwd) => cwd.to_path_buf(),
        None => std::env::current_dir().map_err(|e| {
            let s = e.to_string();
            Error(format!(
                "getwd: {}",
                s.split(" (os error ").next().unwrap_or(&s).to_lowercase()
            ))
        })?,
    };
    let git = opts.git.unwrap_or(&SystemGit);
    let mut res = Resolved {
        hub_dir: hub.into(),
        ..Resolved::default()
    };
    let root = git.toplevel(&cwd);
    if root.found {
        res.repo_root = root.value;
        let root = path(res.repo_root.clone());
        let remote = git.remote_url(&root);
        if remote.found {
            res.repo_remote = normalize_remote_url(&remote.value).unwrap_or_default();
        }
        let head = git.head_commit(&root);
        if head.found && head.value.len() >= 7 {
            res.repo_head = head.value[..7].into();
        }
        let branch = git.branch(&root);
        if branch.found {
            res.repo_branch = branch.value;
        }
    }
    let mut name = text(opts.flag_project).trimmed().as_bytes().to_vec();
    if name.is_empty() {
        let value = opts.env.map_or_else(
            || {
                std::env::var_os("BEANS_PROJECT")
                    .unwrap_or_default()
                    .as_bytes()
                    .to_vec()
            },
            |env| env("BEANS_PROJECT"),
        );
        name = text(&value).trimmed().as_bytes().to_vec();
    }
    if !name.is_empty() {
        if !valid_project_name(&name) {
            return Err(Error(format!(
                "invalid project name {} (use [a-z0-9-])",
                text(&name).quoted()
            )));
        }
        res.project_dir = project_path(hub, &name);
        if !exists(hub, &name) {
            if !opts.write {
                return Err(Error(format!(
                    "project {} does not exist in the hub",
                    text(&name)
                )));
            }
            res.created = true;
        }
        res.project = name;
        return Ok(res);
    }
    if res.repo_root.is_empty() {
        return if opts.all_projects {
            Ok(res)
        } else {
            Err(Error(OUTSIDE_REPO.into()))
        };
    }
    let mut basename = res.repo_root.as_slice();
    while basename.ends_with(b"/") {
        basename = &basename[..basename.len() - 1];
    }
    let basename = basename.rsplit(|&b| b == b'/').next().unwrap_or_default();
    let candidate = project_name(basename).into_bytes();
    res.candidate = candidate.clone();
    if exists(hub, &candidate) {
        let cfg = load_project_config(&config_path(hub, &candidate))?;
        if !res.repo_remote.is_empty()
            && cfg.remotes.as_ref().is_some_and(|r| !r.is_empty())
            && !matches(cfg.remotes.as_deref(), &res.repo_remote)
        {
            let remotes: Vec<_> = cfg
                .remotes
                .unwrap()
                .iter()
                .map(ToString::to_string)
                .collect();
            return Err(Error(format!(
                "projects/{} belongs to {}; run bn project create <other-name> --link to use a different name",
                text(&candidate),
                remotes.join(", ")
            )));
        }
        res.project_dir = project_path(hub, &candidate);
        res.project = candidate;
        return Ok(res);
    }
    if !res.repo_remote.is_empty() {
        let mut found = Vec::new();
        for name in directories(hub)? {
            let cfg = load_project_config(&config_path(hub, &name))?;
            if matches(cfg.remotes.as_deref(), &res.repo_remote) {
                found.push(name);
            }
        }
        if found.len() > 1 {
            return Err(Error(format!(
                "remote {} is linked to more than one project: {}; pass --project",
                text(&res.repo_remote),
                found
                    .iter()
                    .map(|v| text(v).to_string())
                    .collect::<Vec<_>>()
                    .join(", ")
            )));
        }
        if let Some(name) = found.pop() {
            res.project_dir = project_path(hub, &name);
            res.project = name;
            return Ok(res);
        }
    }
    res.project_dir = project_path(hub, &candidate);
    res.project = candidate;
    if opts.write {
        res.created = true;
    } else {
        res.notice = format!("project {} has no issues yet", text(&res.project));
    }
    Ok(res)
}
