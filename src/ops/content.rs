//! Small content operations: memories, docs, project registration and issue retention.
use crate::{
    domain::{
        frontmatter::Error,
        memory::{self, MemoryDocument, MemoryMetadata},
    },
    gitops::{Operation, check_hub_write_path, remove_hub_file, write_hub_file},
    vault::{Index, LoadOptions, NoteData, Resolved},
};
use std::path::{Path, PathBuf};
#[derive(Clone, Debug)]
pub enum ContentChange {
    Remember {
        key: String,
        body: String,
        kind: String,
        tags: Vec<String>,
        global: bool,
    },
    Forget {
        key: String,
        global: bool,
    },
    Doc {
        path: String,
        global: bool,
    },
    Project {
        link: bool,
    },
    Delete {
        reference: String,
        force: bool,
    },
}
pub struct ContentMutation {
    pub resolved: Resolved,
    pub change: ContentChange,
    pub id: String,
    pub actor: String,
    pub explicit_workflow: Option<PathBuf>,
    present: bool,
    at: crate::domain::issue::Timestamp,
}
impl ContentMutation {
    pub fn with_actor(mut self, actor: &str) -> Self {
        self.actor = actor.into();
        self
    }
    pub fn new(resolved: Resolved, change: ContentChange) -> Self {
        Self {
            resolved,
            change,
            id: String::new(),
            actor: "bn".into(),
            explicit_workflow: std::env::var_os("BN_CONFIG")
                .filter(|v| !v.is_empty())
                .map(PathBuf::from),
            present: false,
            at: super::records::now(),
        }
    }
}
impl Operation for ContentMutation {
    fn subject(&self) -> String {
        format!("bn: content {}", self.id)
    }
    fn after_rebase(&mut self, present: bool) {
        self.present = present;
    }
    fn apply(&mut self, hub: &Path) -> Result<Vec<PathBuf>, Error> {
        let project = String::from_utf8_lossy(&self.resolved.project);
        let base = PathBuf::from("projects").join(project.as_ref());
        check_hub_write_path(hub, &base.join("beans.toml"))?;
        let ix = Index::load_snapshot(
            hub,
            LoadOptions {
                explicit_workflow: self.explicit_workflow.clone(),
            },
        )?;
        let (relative, bytes) = match &self.change {
            ContentChange::Remember {
                key,
                body,
                kind,
                tags,
                global,
            } => {
                let key = if key.is_empty() {
                    crate::domain::id::slug(body)
                        .chars()
                        .take(memory::KEY_MAX_LEN)
                        .collect::<String>()
                        .trim_end_matches('-')
                        .to_owned()
                } else {
                    key.clone()
                };
                if !memory::valid_key(&key) || !memory::valid_type(kind) {
                    return Err(Error::new("invalid memory key or type".into()));
                }
                let relative = if *global {
                    PathBuf::from("memories")
                } else {
                    base.join("memories")
                }
                .join(format!("{key}.md"));
                check_hub_write_path(hub, &relative)?;
                let mut doc = if hub.join(&relative).exists() {
                    MemoryDocument::parse_bytes(
                        &relative.to_string_lossy(),
                        &std::fs::read(hub.join(&relative))
                            .map_err(|e| Error::new(e.to_string()))?,
                    )?
                } else {
                    MemoryDocument::new(MemoryMetadata {
                        key: key.clone(),
                        created: self.at.clone(),
                        ..Default::default()
                    })
                };
                if !doc.original_bytes().is_empty()
                    && doc.body.as_bytes() == body.as_bytes()
                    && doc.metadata.kind == *kind
                    && doc.metadata.tags == *tags
                {
                    self.id = key;
                    return Ok(vec![]);
                }
                doc.body = body.clone().into();
                doc.metadata.kind = kind.clone();
                doc.metadata.tags = tags.clone();
                doc.metadata.updated = self.at.clone();
                self.id = key;
                (relative, doc.encode()?.bytes)
            }
            ContentChange::Forget { key, global } => {
                if !memory::valid_key(key) {
                    return Err(Error::new("invalid memory key".into()));
                }
                let p = if *global {
                    PathBuf::from("memories")
                } else {
                    base.join("memories")
                }
                .join(format!("{key}.md"));
                check_hub_write_path(hub, &p)?;
                if !hub.join(&p).exists() {
                    if self.present {
                        return Ok(vec![]);
                    }
                    return Err(Error::new("memory not found".into()));
                }
                remove_hub_file(hub, &p)?;
                self.id = key.clone();
                self.present = true;
                return Ok(vec![p]);
            }
            ContentChange::Doc { path, global } => {
                if path.trim().is_empty() {
                    return Err(Error::new("doc path must not be empty".into()));
                }
                let path = if path.ends_with(".md") {
                    path.clone()
                } else {
                    format!("{path}.md")
                };
                let p = if *global {
                    PathBuf::from("docs")
                } else {
                    base.join("docs")
                }
                .join(path);
                check_hub_write_path(hub, &p)?;
                if hub.join(&p).exists() {
                    if self.present {
                        return Ok(vec![]);
                    }
                    return Err(Error::new("doc already exists".into()));
                }
                self.id = p.to_string_lossy().into_owned();
                let title = p.file_stem().unwrap().to_string_lossy();
                let bytes = [
                    hub.join(&base).join("templates/doc.md"),
                    hub.join("templates/doc.md"),
                ]
                .into_iter()
                .find_map(|p| std::fs::read(p).ok())
                .unwrap_or_else(|| format!("# {title}\n").into_bytes());
                (p, bytes)
            }
            ContentChange::Project { link } => {
                let mut paths = super::scaffold_paths(hub, &self.resolved)?;
                if *link {
                    if self.resolved.repo_remote.is_empty() {
                        return Err(Error::new("current repository has no origin remote".into()));
                    }
                    let p = base.join("beans.toml");
                    let source =
                        std::fs::read(hub.join(&p)).map_err(|e| Error::new(e.to_string()))?;
                    let remote = crate::domain::yaml_string::YamlString::from_bytes(
                        self.resolved.repo_remote.clone(),
                    );
                    if let Some(bytes) = super::project_config::add_remote(&source, &remote)? {
                        write_hub_file(hub, &p, &bytes)?;
                        paths.push(p);
                    }
                }
                self.id = project.into_owned();
                self.present = true;
                return Ok(paths);
            }
            ContentChange::Delete { reference, force } => {
                let (_, n) = ix.resolve_issue_ref(reference.as_bytes());
                let Some(n) = n else {
                    if self.present {
                        return Ok(vec![]);
                    }
                    return Err(Error::new("issue not found".into()));
                };
                let NoteData::Issue(doc) = &n.data else {
                    unreachable!()
                };
                let refs = ix.issue_backlinks(&n.graph.basename, true);
                if !*force && !refs.is_empty() {
                    return Err(Error::new(
                        "issue is referenced; use --force to delete and remove structural links"
                            .into(),
                    ));
                }
                let mut updates = Vec::new();
                if *force {
                    for other in ix.project_issues(b"", true) {
                        let NoteData::Issue(source) = &other.data else {
                            unreachable!()
                        };
                        if source.metadata.id == doc.metadata.id {
                            continue;
                        }
                        let mut d = source.as_ref().clone();
                        let targets = |l: &crate::domain::text::Link| {
                            ix.resolve_issue_ref(l.raw.as_bytes())
                                .1
                                .is_some_and(|target| target.graph.path == n.graph.path)
                        };
                        if targets(&d.metadata.parent) {
                            d.metadata.parent = Default::default();
                        }
                        d.metadata.blocked_by.retain(|l| !targets(l));
                        if d.metadata != source.metadata {
                            d.append_log(crate::domain::log::LogEntry {
                                at: self.at.clone(),
                                event: format!("removed dependency {}", doc.metadata.id).into(),
                                actor: self.actor.clone().into(),
                                repo: crate::domain::yaml_string::YamlString::from_bytes(
                                    self.resolved.project.clone(),
                                ),
                                sha: crate::domain::yaml_string::YamlString::from_bytes(
                                    self.resolved.repo_head.clone(),
                                ),
                                branch: crate::domain::yaml_string::YamlString::from_bytes(
                                    self.resolved.repo_branch.clone(),
                                ),
                                ..Default::default()
                            });
                            let path =
                                PathBuf::from(String::from_utf8_lossy(&other.graph.path).as_ref());
                            check_hub_write_path(hub, &path)?;
                            updates.push((path, d.encode()?.bytes));
                        }
                    }
                }
                let p = PathBuf::from(String::from_utf8_lossy(&n.graph.path).as_ref());
                check_hub_write_path(hub, &p)?;
                let mut paths = Vec::new();
                for (path, bytes) in updates {
                    write_hub_file(hub, &path, &bytes)?;
                    paths.push(path);
                }
                remove_hub_file(hub, &p)?;
                self.id = doc.metadata.id.clone();
                self.present = true;
                paths.push(p);
                return Ok(paths);
            }
        };
        check_hub_write_path(hub, &relative)?;
        let mut paths = if matches!(
            self.change,
            ContentChange::Remember { global: true, .. } | ContentChange::Doc { global: true, .. }
        ) {
            vec![]
        } else {
            super::scaffold_paths(hub, &self.resolved)?
        };
        write_hub_file(hub, &relative, &bytes)?;
        paths.push(relative);
        self.present = true;
        Ok(paths)
    }
}
