//! Filesystem-backed hub index. Rebuildable graph state is not persisted.
mod note;
mod reload;
mod source;
mod walk;
use super::{
    GraphNote, NoteGraph, NoteKind, glob,
    paths::{clean, join},
};
use crate::domain::{
    config::{self, HubConfig, ProjectConfig},
    file_io::{path_error, path_name},
    frontmatter::Error,
    workflow::{WorkflowConfig, load_workflow},
};
pub use note::{Note, NoteData};
use std::{
    collections::{BTreeMap, BTreeSet},
    ffi::OsString,
    fs::{self, File},
    io::Read,
    os::unix::ffi::{OsStrExt, OsStringExt},
    path::{Path, PathBuf},
};
#[derive(Clone, Debug)]
pub struct Project {
    pub name: Vec<u8>,
    pub dir: PathBuf,
    pub config: ProjectConfig,
    pub workflow: WorkflowConfig,
}
#[derive(Clone, Debug, Default)]
pub struct LoadOptions {
    pub explicit_workflow: Option<PathBuf>,
}
pub struct Index {
    pub hub_dir: PathBuf,
    pub hub_config: HubConfig,
    pub workflow: WorkflowConfig,
    pub projects: BTreeMap<Vec<u8>, Project>,
    pub assets: BTreeSet<Vec<u8>>,
    pub graph: NoteGraph,
    pub explicit_workflow: Option<PathBuf>,
    hub_toml: Vec<u8>,
    order: Vec<Note>,
    by_path: BTreeMap<Vec<u8>, usize>,
}
pub(super) fn path(bytes: &[u8]) -> PathBuf {
    OsString::from_vec(bytes.into()).into()
}
pub(super) fn read(path: &Path) -> Result<Vec<u8>, Error> {
    read_with_kind(path).map_err(|(_, e)| e)
}
fn read_with_kind(path: &Path) -> Result<Vec<u8>, (std::io::ErrorKind, Error)> {
    let mut file = File::open(path).map_err(|e| (e.kind(), path_error("open", path, e)))?;
    let mut data = Vec::new();
    file.read_to_end(&mut data)
        .map_err(|e| (e.kind(), path_error("read", path, e)))?;
    Ok(data)
}
impl Index {
    pub fn load(hub: &Path) -> Result<Self, Error> {
        Self::load_with_options(hub, LoadOptions::default())
    }
    pub fn load_with_options(hub: &Path, options: LoadOptions) -> Result<Self, Error> {
        let raw = hub.as_os_str().as_bytes();
        let absolute = if raw.starts_with(b"/") {
            clean(raw)
        } else {
            let cwd = std::env::current_dir().map_err(|e| {
                Error::new(e.to_string()).context(
                    &[
                        b"vault: resolve hub dir ".as_slice(),
                        path_name(hub).as_bytes(),
                    ]
                    .concat(),
                )
            })?;
            join(&[cwd.as_os_str().as_bytes(), raw])
        };
        let hub_dir = path(&absolute);
        let toml_path = hub_dir.join("beans.toml");
        let hub_config = config::load_hub_config(&toml_path)
            .map_err(|e| e.context(b"vault: load hub config"))?;
        let hub_toml = match read_with_kind(&toml_path) {
            Ok(v) => v,
            Err((std::io::ErrorKind::NotFound, _)) => Vec::new(),
            Err((_, e)) => {
                return Err(e.context(
                    &[b"vault: read ".as_slice(), path_name(&toml_path).as_bytes()].concat(),
                ));
            }
        };
        let workflow = load_workflow(options.explicit_workflow.as_deref(), &[], &hub_toml)
            .map_err(|e| e.context(b"vault: hub workflow"))?;
        let mut project_paths =
            glob::glob(&join(&[&absolute, b"projects", b"*", b"beans.toml"]))
                .map_err(|_| Error::new("vault: glob projects: syntax error in pattern".into()))?;
        project_paths.sort();
        let mut projects = BTreeMap::new();
        for raw_path in project_paths {
            let toml_path = path(&raw_path);
            let dir = toml_path.parent().unwrap().to_path_buf();
            let name = dir.file_name().unwrap().as_bytes().to_vec();
            let config = config::load_project_config(&toml_path).map_err(|e| {
                e.context(
                    &[
                        b"vault: load project config ".as_slice(),
                        path_name(&toml_path).as_bytes(),
                    ]
                    .concat(),
                )
            })?;
            let raw = read(&toml_path).map_err(|e| {
                e.context(&[b"vault: read ".as_slice(), path_name(&toml_path).as_bytes()].concat())
            })?;
            let workflow = load_workflow(options.explicit_workflow.as_deref(), &raw, &hub_toml)
                .map_err(|e| {
                    e.context(&[b"vault: workflow for project ".as_slice(), &name].concat())
                })?;
            projects.insert(
                name.clone(),
                Project {
                    name,
                    dir,
                    config,
                    workflow,
                },
            );
        }
        let mut index = Self {
            hub_dir,
            hub_config,
            workflow,
            projects,
            assets: BTreeSet::new(),
            graph: NoteGraph::default(),
            explicit_workflow: options.explicit_workflow,
            hub_toml,
            order: Vec::new(),
            by_path: BTreeMap::new(),
        };
        index.walk()?;
        index.rebuild();
        Ok(index)
    }
    pub fn ordered_notes(&self) -> &[Note] {
        &self.order
    }
    pub fn note_by_path(&self, path: &[u8]) -> Option<&Note> {
        self.by_path.get(path).map(|&i| &self.order[i])
    }
    pub fn lookup(&self, target: &[u8]) -> Option<&Note> {
        self.graph
            .lookup(target)
            .and_then(|n| self.note_by_path(&n.path))
    }
    pub fn note_by_id(&self, kind: NoteKind, id: &[u8]) -> Option<&Note> {
        self.graph
            .by_id(kind, id)
            .and_then(|n| self.note_by_path(&n.path))
    }
    pub fn workflow_for(&self, project: &[u8]) -> &WorkflowConfig {
        self.projects
            .get(project)
            .map_or(&self.workflow, |p| &p.workflow)
    }
    pub fn hub_toml(&self) -> &[u8] {
        &self.hub_toml
    }
    fn rebuild(&mut self) {
        self.by_path = self
            .order
            .iter()
            .enumerate()
            .map(|(i, n)| (n.graph.path.clone(), i))
            .collect();
        self.graph.replace_notes(
            self.order
                .iter()
                .map(|n| n.graph.clone())
                .collect::<Vec<GraphNote>>(),
        );
        self.graph.rebuild();
        for note in &mut self.order {
            note.graph.outlinks = self
                .graph
                .by_path(&note.graph.path)
                .unwrap()
                .outlinks
                .clone();
        }
    }
    fn remove(&mut self, rel: &[u8]) {
        if let Some(i) = self.order.iter().position(|n| n.graph.path == rel) {
            self.order.remove(i);
        }
    }
    fn warning(&mut self, rel: &[u8], error: impl ToString) {
        self.graph.add_parse_warning(rel.into(), error.to_string());
    }
}
