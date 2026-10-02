//! Native JSONL importer, captured once and replayed as a single shared operation.
use crate::{
    domain::{
        frontmatter::Error,
        issue::{IssueDocument, IssueMetadata, Timestamp},
        log::LogEntry,
        memory::{self, MemoryDocument, MemoryMetadata},
        text::Link,
    },
    gitops::{Operation, check_hub_write_path, write_hub_file},
    vault::{Index, LoadOptions, Resolved},
};
use serde_json::{Value, json};
use std::path::{Path, PathBuf};
pub struct ImportMutation {
    pub resolved: Resolved,
    pub records: Vec<Value>,
    pub warnings: Vec<String>,
    pub report: Value,
    pub force: bool,
    pub dry_run: bool,
    at: Timestamp,
    present: bool,
}
impl ImportMutation {
    pub fn new(
        resolved: Resolved,
        records: Vec<Value>,
        warnings: Vec<String>,
        force: bool,
        dry_run: bool,
    ) -> Self {
        Self {
            resolved,
            records,
            warnings,
            report: json!({}),
            force,
            dry_run,
            at: super::records::now(),
            present: false,
        }
    }
}
fn text(v: &Value, k: &str) -> String {
    v[k].as_str().unwrap_or_default().into()
}
fn strings(v: &Value, k: &str) -> Vec<String> {
    v[k].as_array()
        .into_iter()
        .flatten()
        .filter_map(Value::as_str)
        .map(str::to_owned)
        .collect()
}
fn timestamp(s: &str, fallback: &Timestamp) -> Timestamp {
    time::OffsetDateTime::parse(s, &time::format_description::well_known::Rfc3339)
        .map(|t| Timestamp {
            seconds: t.unix_timestamp(),
            nanoseconds: t.nanosecond(),
            offset_seconds: t.offset().whole_seconds(),
        })
        .unwrap_or_else(|_| fallback.clone())
}
impl Operation for ImportMutation {
    fn subject(&self) -> String {
        "bn: import bd".into()
    }
    fn after_rebase(&mut self, present: bool) {
        self.present = present;
    }
    fn apply(&mut self, hub: &Path) -> Result<Vec<PathBuf>, Error> {
        let base = PathBuf::from("projects")
            .join(String::from_utf8_lossy(&self.resolved.project).as_ref());
        check_hub_write_path(hub, &base.join("beans.toml"))?;
        let explicit = std::env::var_os("BN_CONFIG").map(PathBuf::from);
        let ix = Index::load_snapshot(
            hub,
            LoadOptions {
                explicit_workflow: explicit,
            },
        )?;
        let workflow = ix.workflow_for(&self.resolved.project);
        let mut rejected = Vec::new();
        let mut warnings = self.warnings.clone();
        let mut unresolved = Vec::new();
        let mut actors = serde_json::Map::new();
        let mut issues = 0;
        let mut memories = 0;
        let mut archived = 0;
        let mut blocks = 0;
        let mut parents = 0;
        let mut writes = Vec::new();
        let mut paths_seen = std::collections::HashSet::new();
        let imported_ids: std::collections::HashSet<_> =
            self.records.iter().map(|v| text(v, "id")).collect();
        for r in &self.records {
            let (relative, bytes) = if text(r, "_type") == "memory" {
                let key = text(r, "key");
                if !memory::valid_key(&key) {
                    rejected.push(format!("memory {key}: invalid key"));
                    continue;
                }
                let relative = base.join("memories").join(format!("{key}.md"));
                check_hub_write_path(hub, &relative)?;
                let mut d = if hub.join(&relative).exists() && self.force {
                    MemoryDocument::parse_bytes(
                        &relative.to_string_lossy(),
                        &std::fs::read(hub.join(&relative))
                            .map_err(|e| Error::new(e.to_string()))?,
                    )?
                } else {
                    MemoryDocument::new(MemoryMetadata {
                        key,
                        ..Default::default()
                    })
                };
                d.body = format!("{}\n", text(r, "value").trim_end_matches('\n')).into();
                memories += 1;
                (relative, d.encode()?.bytes)
            } else {
                let id = text(r, "id");
                let title = text(r, "title");
                let status = text(r, "status");
                if !crate::domain::id::valid_id(&id)
                    || title.trim().is_empty()
                    || !workflow.is_valid(status.as_bytes())
                {
                    rejected.push(format!("{id}: invalid ID, title or workflow status"));
                    continue;
                }
                let created = timestamp(&text(r, "created_at"), &self.at);
                let updated = timestamp(&text(r, "updated_at"), &created);
                let closed = timestamp(&text(r, "closed_at"), &updated);
                let mut kind = text(r, "issue_type");
                if !ix.hub_config.types.valid_type(kind.as_bytes()) {
                    warnings.push(format!("{id}: type {kind} becomes task"));
                    kind = "task".into();
                }
                let terminal = workflow.is_terminal(status.as_bytes());
                let relative = if let Some(n) = ix.resolve_issue_ref(id.as_bytes()).1 {
                    if n.project != self.resolved.project {
                        return Err(Error::new(format!(
                            "{id}: existing issue belongs to another project"
                        )));
                    }
                    PathBuf::from(String::from_utf8_lossy(&n.graph.path).as_ref())
                } else {
                    base.join(if terminal {
                        format!(
                            "archive/{}",
                            time::OffsetDateTime::from_unix_timestamp(closed.seconds)
                                .unwrap()
                                .year()
                        )
                    } else {
                        "issues".into()
                    })
                    .join(crate::domain::id::filename(
                        &id,
                        &crate::domain::id::slug(&title),
                    ))
                };
                check_hub_write_path(hub, &relative)?;
                let mut d = if hub.join(&relative).exists() && self.force {
                    IssueDocument::parse_bytes(
                        &relative.to_string_lossy(),
                        &std::fs::read(hub.join(&relative))
                            .map_err(|e| Error::new(e.to_string()))?,
                    )?
                } else {
                    IssueDocument::new(IssueMetadata {
                        id: id.clone(),
                        created: created.clone(),
                        ..Default::default()
                    })
                };
                let old_metadata = d.metadata.clone();
                let old_description = d.description.clone();
                let old_body = d.body.clone();
                d.metadata.title = title;
                d.metadata.kind = kind;
                d.metadata.status = status;
                d.metadata.priority = r["priority"].as_i64().unwrap_or(2).clamp(0, 4);
                d.metadata.labels = strings(r, "labels");
                d.metadata.url = text(r, "url");
                let assignee = text(r, "assignee");
                d.metadata.assignee = crate::vault::project_name(assignee.as_bytes());
                if !assignee.is_empty() {
                    actors.insert(assignee, json!(d.metadata.assignee));
                }
                d.set_description(&text(r, "description"));
                let mut body = String::new();
                for (h, k) in [
                    ("Acceptance", "acceptance_criteria"),
                    ("Design", "design"),
                    ("Notes", "notes"),
                ] {
                    let t = text(r, k);
                    if !t.is_empty() {
                        body.push_str(&format!("## {h}\n{}\n\n", t.trim_end()));
                    }
                }
                d.body = body.into();
                d.metadata.parent = Default::default();
                d.metadata.blocked_by.clear();
                for dep in r["dependencies"].as_array().into_iter().flatten() {
                    let from = text(dep, "issue_id");
                    let target = text(dep, "depends_on_id");
                    if from != id || target.is_empty() {
                        continue;
                    }
                    let link = Link::parse(&format!("[[{target}]]"));
                    if !imported_ids.contains(&target)
                        && ix.issue_by_id(target.as_bytes()).is_none()
                    {
                        unresolved.push(format!("{id}: {target}"));
                    }
                    match text(dep, "type").as_str() {
                        "blocks" => {
                            d.metadata.blocked_by.push(link);
                            blocks += 1;
                        }
                        "parent-child" => {
                            d.metadata.parent = link;
                            parents += 1;
                        }
                        other => warnings.push(format!("{id}: dropped unmapped {other} edge")),
                    }
                }
                let creator = [text(r, "created_by"), text(r, "owner"), "import".into()]
                    .into_iter()
                    .find(|v| !v.is_empty())
                    .unwrap();
                let actor = crate::vault::project_name(creator.as_bytes());
                actors.insert(creator, json!(actor));
                if d.log.is_empty() {
                    d.append_log(LogEntry {
                        at: created,
                        actor: actor.clone().into(),
                        event: "created".into(),
                        ..Default::default()
                    });
                    let branch = text(r, "branch_name");
                    if !branch.is_empty() {
                        d.append_log(LogEntry {
                            at: updated.clone(),
                            actor: actor.clone().into(),
                            event: format!("note — branch {branch}").into(),
                            ..Default::default()
                        });
                    }
                    if terminal {
                        d.append_log(LogEntry {
                            at: closed,
                            actor: actor.into(),
                            event: format!("closed — {}", text(r, "close_reason")).into(),
                            ..Default::default()
                        });
                    }
                }
                d.metadata.updated = updated;
                issues += 1;
                if terminal {
                    archived += 1;
                }
                let bytes = if !d.original_bytes().is_empty()
                    && d.metadata == old_metadata
                    && d.description.to_string().trim() == old_description.to_string().trim()
                    && d.body.to_string().trim() == old_body.to_string().trim()
                {
                    d.original_bytes().to_vec()
                } else {
                    d.encode()?.bytes
                };
                (relative, bytes)
            };
            if !paths_seen.insert(relative.clone()) {
                rejected.push(format!(
                    "duplicate import destination {}",
                    relative.display()
                ));
                continue;
            }
            if hub.join(&relative).exists() && !self.force {
                if self.present && std::fs::read(hub.join(&relative)).ok().as_ref() == Some(&bytes)
                {
                    continue;
                }
                rejected.push(format!(
                    "{}: already exists (use --force)",
                    relative.display()
                ));
                continue;
            }
            writes.push((relative, bytes));
        }
        self.report = json!({"issues":issues,"memories":memories,"archived":archived,"blocks":blocks,"parents":parents,"warnings":warnings,"rejected":rejected,"unresolved":unresolved,"actors":actors});
        if self.dry_run {
            return Ok(vec![]);
        }
        if !self.report["rejected"].as_array().unwrap().is_empty() {
            return Err(Error::new(format!(
                "import rejected records: {}",
                self.report["rejected"]
            )));
        }
        let mut paths = super::scaffold_paths(hub, &self.resolved)?;
        for (path, bytes) in writes {
            write_hub_file(hub, &path, &bytes)?;
            paths.push(path);
        }
        self.present = true;
        Ok(paths)
    }
}
