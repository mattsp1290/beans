use super::dispatch::*;
use crate::{
    domain::{frontmatter::Error, plan},
    gitops::Hub,
    ops::{PlanChange, PlanMutation},
    vault::{Index, LoadOptions, NoteData, NoteKind, Resolved},
};
use clap::ArgMatches;
use std::path::PathBuf;
pub fn execute(
    m: &ArgMatches,
    hub: &Hub,
    resolved: Resolved,
    explicit: Option<PathBuf>,
    json: bool,
) -> Result<(), Error> {
    let (action, s) = m.subcommand().unwrap();
    let ix = Index::load_snapshot(
        &hub.dir,
        LoadOptions {
            explicit_workflow: explicit,
        },
    )?;
    if action == "init" {
        let out = value(s, "output");
        if out.is_empty() {
            return Err(Error::new("--output is required".into()));
        }
        if std::fs::symlink_metadata(&out).is_ok() {
            return Err(Error::new("plan output already exists".into()));
        }
        let prefix = crate::ops::prefix_for(&hub.dir, &resolved.project);
        let mut exists = |id: &str| ix.lookup(id.as_bytes()).is_some();
        let id = plan::id::new_id(
            &String::from_utf8_lossy(&prefix),
            Some(&mut exists),
            ix.hub_config.ids.length as isize,
        );
        plan::write_scaffold_path(
            std::path::Path::new(&out),
            &id,
            &value(s, "title"),
            &crate::ops::records::now(),
        )?;
        return output(&serde_json::json!({"id":id,"directory":out,"status":"draft"}));
    }
    if matches!(action, "validate" | "put") {
        let dir = value(s, "directory");
        let bundle = match plan::load_path(std::path::Path::new(&dir)) {
            Ok(b) => b,
            Err(e) => {
                if json {
                    output(
                        &serde_json::json!({"valid":false,"issues":[{"code":"invalid","message":e.to_string()}]}),
                    )?;
                }
                return Err(e);
            }
        };
        let id = bundle.plan.as_ref().unwrap().id.clone();
        if action == "validate" {
            return output(
                &serde_json::json!({"valid":true,"id":id,"status":bundle.plan.as_ref().unwrap().status,"issues":[]}),
            );
        }
        let original = plan::capture_snapshot(std::path::Path::new(&dir))?;
        let mut op = PlanMutation::new(resolved, id.clone(), PlanChange::Put(original.clone()));
        let result = hub.mutate(&mut op)?;
        let mut refreshed = false;
        if let Ok(mut current) = plan::load_path(std::path::Path::new(&dir))
            && plan::capture_snapshot(std::path::Path::new(&dir))
                .ok()
                .as_ref()
                == Some(&original)
        {
            current.plan.as_mut().unwrap().updated = op.updated.clone();
            crate::gitops::write_file(
                &PathBuf::from(&dir).join("plan.md"),
                &plan::revise_manifest(
                    &original.files[b"plan.md".as_slice()],
                    current.plan.as_ref().unwrap(),
                )?,
            )?;
            refreshed = true;
        }
        return output(
            &serde_json::json!({"id":id,"path":op.path,"updated":op.updated,"local_revision_refreshed":refreshed,"commit":result.sha,"pushed":result.pushed}),
        );
    }
    if action == "list" {
        let status = value(s, "status");
        return print_notes(
            ix.project_plans(&resolved.project)
                .into_iter()
                .filter(
                    |n| matches!(&n.data,NoteData::Plan(p) if status.is_empty()||p.status==status),
                )
                .collect(),
            json,
        );
    }
    let id = if matches!(action, "link" | "unlink" | "status") {
        value(s, "plan-id")
    } else {
        value(s, "id")
    };
    let n = ix
        .note_by_id(NoteKind::Plan, id.as_bytes())
        .ok_or_else(|| Error::new("plan not found".into()))?;
    if action == "show" {
        if json {
            return output(&note_json(n));
        }
        print!("{}", String::from_utf8_lossy(&n.source));
        if n.source.is_empty() {
            let NoteData::Plan(p) = &n.data else {
                unreachable!()
            };
            print!("{}", String::from_utf8_lossy(&plan::encode(Some(p))?));
        }
        return Ok(());
    }
    if action == "status" {
        let NoteData::Plan(p) = &n.data else {
            unreachable!()
        };
        return output(
            &serde_json::to_value(ix.plan_execution(p.id.as_bytes()))
                .map_err(|e| Error::new(e.to_string()))?,
        );
    }
    if action == "get" {
        let out = value(s, "output");
        if out.is_empty() {
            return Err(Error::new("--output is required".into()));
        }
        std::fs::create_dir(&out).map_err(|e| Error::new(e.to_string()))?;
        let path = PathBuf::from(String::from_utf8_lossy(&n.graph.path).as_ref());
        let snapshot = plan::capture_snapshot(&hub.dir.join(path.parent().unwrap()))?;
        for (name, bytes) in snapshot.files {
            crate::gitops::write_file(&PathBuf::from(&out).join(name.to_string()), &bytes)?;
        }
        return output(&serde_json::json!({"id":id,"directory":out}));
    }
    let mut op = PlanMutation::new(
        resolved,
        id.clone(),
        PlanChange::Link {
            node: value(s, "node-id"),
            issue: value(s, "issue-id"),
            unlink: action == "unlink",
            force: flag(s, "force"),
        },
    );
    let result = hub.mutate(&mut op)?;
    mutation_output(&id, result, json)
}
