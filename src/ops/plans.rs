//! Optimistic plan publication preserves unrelated files; only captured bundle paths are owned.
use crate::{
    domain::{
        frontmatter::Error,
        issue::Timestamp,
        plan::{self, BundleSnapshot},
    },
    gitops::{Operation, check_hub_write_path, remove_hub_file, write_hub_file},
    vault::{Index, LoadOptions, NoteKind, Resolved},
};
use std::path::{Path, PathBuf};
pub enum PlanChange {
    Put(BundleSnapshot),
    Link {
        node: String,
        issue: String,
        unlink: bool,
        force: bool,
    },
}
pub struct PlanMutation {
    pub resolved: Resolved,
    pub id: String,
    pub change: PlanChange,
    pub updated: Timestamp,
    pub path: PathBuf,
    pub explicit_workflow: Option<PathBuf>,
    present: bool,
    at: Timestamp,
}
impl PlanMutation {
    pub fn new(resolved: Resolved, id: String, change: PlanChange) -> Self {
        Self {
            resolved,
            id,
            change,
            updated: Default::default(),
            path: PathBuf::new(),
            explicit_workflow: std::env::var_os("BN_CONFIG")
                .filter(|v| !v.is_empty())
                .map(PathBuf::from),
            present: false,
            at: super::records::now(),
        }
    }
}
impl Operation for PlanMutation {
    fn subject(&self) -> String {
        format!("bn: plan {}", self.id)
    }
    fn after_rebase(&mut self, present: bool) {
        self.present = present;
    }
    fn apply(&mut self, hub: &Path) -> Result<Vec<PathBuf>, Error> {
        let base = PathBuf::from("projects")
            .join(String::from_utf8_lossy(&self.resolved.project).as_ref())
            .join("plans");
        check_hub_write_path(hub, &base.join("containment-check"))?;
        let ix = Index::load_snapshot(
            hub,
            LoadOptions {
                explicit_workflow: self.explicit_workflow.clone(),
            },
        )?;
        let existing = ix.note_by_id(NoteKind::Plan, self.id.as_bytes());
        if let Some(n) = existing
            && n.project != self.resolved.project
        {
            return Err(Error::new("plan belongs to a different project".into()));
        }
        let current = existing
            .map(|n| {
                let p = PathBuf::from(String::from_utf8_lossy(&n.graph.path).as_ref());
                check_hub_write_path(hub, &p)?;
                plan::load_path(hub.join(p.parent().unwrap()).as_path())
            })
            .transpose()?;
        let (snapshot, removed) = match &self.change {
            PlanChange::Put(source) => {
                let mut desired = plan::load_snapshot("local", source)?;

                let p = desired.plan.as_mut().unwrap();
                if !plan::id::valid_id(
                    &String::from_utf8_lossy(&super::prefix_for(hub, &self.resolved.project)),
                    &p.id,
                ) {
                    return Err(Error::new("plan ID does not match project prefix".into()));
                }
                let root = if let Some(n) = existing {
                    PathBuf::from(String::from_utf8_lossy(&n.graph.path).as_ref())
                        .parent()
                        .unwrap()
                        .to_path_buf()
                } else {
                    base.join(plan::id::directory_name(&p.id, &p.slug))
                };
                self.path = root.join("plan.md");
                if let Some(c) = &current {
                    let cp = c.plan.as_ref().unwrap();
                    let current_snapshot = plan::capture_snapshot(&hub.join(&root))?;
                    if current_snapshot == *source {
                        self.updated = cp.updated.clone();
                        return Ok(vec![]);
                    }
                    if cp.status == plan::COMPLETE {
                        return Err(Error::new("completed plan is immutable".into()));
                    }
                    if self.present && cp.updated == self.updated {
                        return Ok(vec![]);
                    }
                    if cp.updated != p.updated {
                        return Err(Error::new(format!(
                            "stale plan {}: run bn plan get and merge changes",
                            self.id
                        )));
                    }
                    if cp.slug != p.slug || cp.created != p.created {
                        return Err(Error::new(
                            "plan slug and created timestamp are immutable".into(),
                        ));
                    }
                    p.updated = self.at.clone();
                    p.updated.seconds = p.updated.seconds.max(cp.updated.seconds + 1);
                }
                self.updated = p.updated.clone();
                let mut snap = source.clone();
                let p = desired.plan.as_ref().unwrap();
                snap.files.insert(
                    "plan.md".into(),
                    plan::revise_manifest(&source.files[b"plan.md".as_slice()], p)?,
                );
                let removed = current
                    .as_ref()
                    .map(|c| {
                        c.snapshot()
                            .files
                            .into_keys()
                            .filter(|k| !snap.files.contains_key(k))
                            .map(|k| root.join(k.to_string()))
                            .collect()
                    })
                    .unwrap_or_default();
                (snap, removed)
            }
            PlanChange::Link {
                node,
                issue,
                unlink,
                force,
            } => {
                let mut bundle = current.ok_or_else(|| Error::new("plan not found".into()))?;
                let p = bundle.plan.as_mut().unwrap();
                if p.status == plan::COMPLETE {
                    return Err(Error::new("completed plan is immutable".into()));
                }
                self.path =
                    PathBuf::from(String::from_utf8_lossy(&existing.unwrap().graph.path).as_ref());
                let n = p
                    .graph
                    .nodes
                    .as_ref()
                    .and_then(|ns| ns.iter().find(|n| n.id == *node))
                    .ok_or_else(|| Error::new("plan node not found".into()))?;
                let old = n.reference.clone();
                let (_, target) = ix.resolve_issue_ref(issue.as_bytes());
                let canonical = target
                    .and_then(|n| n.graph.id.as_ref())
                    .map(|v| String::from_utf8_lossy(v).into_owned());
                if !*unlink && canonical.is_none() {
                    return Err(Error::new("issue not found".into()));
                }
                let (_, old_target) = ix.resolve_issue_ref(old.as_bytes());
                let old_id = old_target
                    .and_then(|n| n.graph.id.as_ref())
                    .map(|v| String::from_utf8_lossy(v).into_owned())
                    .unwrap_or_else(|| crate::domain::text::Link::parse(&old).target);
                let desired = if *unlink {
                    if old.is_empty() {
                        self.updated = p.updated.clone();
                        return Ok(vec![]);
                    }
                    if old_id
                        != canonical
                            .clone()
                            .unwrap_or_else(|| crate::domain::text::Link::parse(issue).target)
                    {
                        return Err(Error::new(
                            "node reference does not match expected issue".into(),
                        ));
                    }
                    String::new()
                } else {
                    let new = canonical.unwrap();
                    if old_id == new {
                        self.updated = p.updated.clone();
                        return Ok(vec![]);
                    }
                    if !old.is_empty() && !*force {
                        return Err(Error::new(
                            "node already linked; use --force to replace".into(),
                        ));
                    }
                    new
                };
                plan::set_node_ref(Some(p), node, &desired)?;
                p.updated.seconds = self.at.seconds.max(p.updated.seconds + 1);
                self.updated = p.updated.clone();
                let source =
                    std::fs::read(hub.join(&self.path)).map_err(|e| Error::new(e.to_string()))?;
                let manifest = plan::revise_manifest(&source, p)?;
                let mut snapshot = bundle.snapshot();
                snapshot.files.insert("plan.md".into(), manifest);
                (snapshot, vec![])
            }
        };
        let root = self.path.parent().unwrap();
        for name in snapshot.files.keys() {
            check_hub_write_path(hub, &root.join(name.to_string()))?;
        }
        for path in &removed {
            check_hub_write_path(hub, path)?;
        }
        // Refuse authored/unregistered destination trees on first publication.
        if existing.is_none() && hub.join(root).exists() {
            return Err(Error::new("plan destination already exists".into()));
        }
        let mut paths = super::scaffold_paths(hub, &self.resolved)?;
        for (name, bytes) in snapshot.files {
            let p = root.join(name.to_string());
            write_hub_file(hub, &p, &bytes)?;
            paths.push(p);
        }
        for p in removed {
            remove_hub_file(hub, &p)?;
            paths.push(p);
        }
        self.present = true;
        Ok(paths)
    }
}
