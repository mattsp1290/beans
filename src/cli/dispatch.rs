use crate::{
    domain::frontmatter::Error,
    gitops::Hub,
    ops::{ContentChange, ContentMutation, IssueChange, IssueFields, IssueMutation},
    vault::{Index, LoadOptions, Note, NoteData, ResolveOptions, resolve},
};
use clap::ArgMatches;
use std::{io::Read, path::PathBuf};
pub(super) fn value(m: &ArgMatches, k: &str) -> String {
    m.try_get_one::<String>(k)
        .ok()
        .flatten()
        .cloned()
        .unwrap_or_default()
}
pub(super) fn flag(m: &ArgMatches, k: &str) -> bool {
    m.try_get_one::<bool>(k)
        .ok()
        .flatten()
        .copied()
        .unwrap_or(false)
}
pub(super) fn number(m: &ArgMatches, k: &str) -> Option<i64> {
    m.try_get_one::<i64>(k).ok().flatten().copied()
}
pub(super) fn values(m: &ArgMatches, k: &str) -> Vec<String> {
    m.try_get_many::<String>(k)
        .ok()
        .flatten()
        .map(|v| v.cloned().collect())
        .unwrap_or_default()
}
fn optional(m: &ArgMatches, k: &str) -> Option<String> {
    m.try_get_one::<String>(k).ok().flatten().cloned()
}
pub(super) fn output(value: &serde_json::Value) -> Result<(), Error> {
    println!(
        "{}",
        serde_json::to_string(value).map_err(|e| Error::new(e.to_string()))?
    );
    Ok(())
}
pub(super) fn note_json(note: &Note) -> serde_json::Value {
    let mut v = match &note.data {
        NoteData::Issue(d) => {
            let mut v = serde_json::to_value(&d.metadata).unwrap();
            v["description"] = serde_json::json!(d.description.to_string());
            v["log"] = serde_json::to_value(&d.log).unwrap();
            v
        }
        NoteData::Request(d) => {
            let mut v = serde_json::to_value(&d.metadata).unwrap();
            v["log"] = serde_json::to_value(&d.log).unwrap();
            v
        }
        NoteData::Handoff(d) => serde_json::to_value(&d.metadata).unwrap(),
        NoteData::Memory(d) => serde_json::to_value(&d.metadata).unwrap(),
        NoteData::Plan(d) => serde_json::to_value(d).unwrap(),
        NoteData::Doc(d) => serde_json::json!({"title":String::from_utf8_lossy(&d.title)}),
    };
    v["path"] = serde_json::json!(String::from_utf8_lossy(&note.graph.path));
    v["body"] = serde_json::json!(String::from_utf8_lossy(&note.body));
    v["project"] = serde_json::json!(String::from_utf8_lossy(&note.project));
    v
}
pub(super) fn print_notes(notes: Vec<&Note>, json: bool) -> Result<(), Error> {
    if json {
        output(&serde_json::Value::Array(
            notes.into_iter().map(note_json).collect(),
        ))
    } else {
        for n in notes {
            println!(
                "{}\t{}",
                n.graph
                    .id
                    .as_ref()
                    .map(|s| String::from_utf8_lossy(s).into_owned())
                    .unwrap_or_else(|| String::from_utf8_lossy(&n.graph.path).into_owned()),
                String::from_utf8_lossy(&n.title)
            );
        }
        Ok(())
    }
}
pub(super) fn body(m: &ArgMatches, file: &str) -> Result<Option<String>, Error> {
    let p = value(m, file);
    let stdin = flag(m, "stdin") || p == "-";
    let inline = optional(m, "description");
    if [!p.is_empty() && p != "-", stdin, inline.is_some()]
        .into_iter()
        .filter(|v| *v)
        .count()
        > 1
    {
        return Err(Error::new(
            "choose one body source: description, file, or stdin".into(),
        ));
    }
    if stdin {
        let mut s = String::new();
        std::io::stdin()
            .read_to_string(&mut s)
            .map_err(|e| Error::new(e.to_string()))?;
        Ok(Some(s))
    } else if !p.is_empty() {
        Ok(Some(
            std::fs::read_to_string(p).map_err(|e| Error::new(e.to_string()))?,
        ))
    } else {
        Ok(inline)
    }
}
pub(super) fn mutation_output(
    id: &str,
    result: crate::gitops::MutationResult,
    json: bool,
) -> Result<(), Error> {
    if json {
        output(
            &serde_json::json!({"id":id,"key":id,"sha":result.sha,"commit":result.sha,"pushed":result.pushed,"message":result.message}),
        )
    } else {
        println!("{id}");
        if !result.message.is_empty() {
            eprintln!("bn: {}", result.message);
        }
        Ok(())
    }
}
pub fn execute(root: &ArgMatches, hub: &Hub, actor: &str) -> Result<(), Error> {
    let (name, m) = root.subcommand().unwrap();
    let action = m.subcommand().map(|(n, _)| n).unwrap_or("");
    if name == "prime" {
        print!("{}", include_str!("../../docs/prime.md"));
        return Ok(());
    }
    if name == "man" {
        print!("{}", super::manual::render());
        return Ok(());
    }
    let write = matches!(
        name,
        "create"
            | "update"
            | "note"
            | "close"
            | "reopen"
            | "delete"
            | "remember"
            | "forget"
            | "archive"
    ) || matches!(
        (name, action),
        ("dep", "add" | "remove")
            | ("request", "create" | "update" | "link" | "unlink")
            | (
                "handoff",
                "create" | "attach" | "detach" | "archive" | "restore"
            )
            | ("doc", "new")
            | ("project", "create" | "link")
            | ("plan", "put" | "link" | "unlink")
            | ("import", "bd")
    );
    if !write
        && name != "doctor"
        && !hub.no_sync
        && !flag(root, "no-fetch")
        && let Err(e) = hub.refresh()
    {
        crate::gitops::diagnostics::warning(b"bn: using local hub: ", &e);
    }
    let explicit = std::env::var_os("BN_CONFIG")
        .filter(|v| !v.is_empty())
        .map(PathBuf::from);
    let leaf = m.subcommand().map(|(_, s)| s).unwrap_or(m);
    let global = flag(leaf, "global");
    let project_override = if name == "project" && matches!(action, "create" | "link") {
        value(leaf, "name")
    } else {
        value(root, "project")
    };
    let allow_unscoped = global
        || matches!(name, "project" | "doctor" | "cache" | "memories")
        || name == "doc" && matches!(action, "list" | "backlinks")
        || flag(leaf, "all-projects");
    let lookup = |key: &str| {
        if global && key == "BEANS_PROJECT" {
            Vec::new()
        } else {
            std::env::var_os(key)
                .map(|v| v.to_string_lossy().as_bytes().to_vec())
                .unwrap_or_default()
        }
    };
    let git = crate::vault::PolicyGit(hub.executor.clone());
    let mut resolved = resolve(
        &hub.dir,
        ResolveOptions {
            flag_project: if global {
                b""
            } else {
                project_override.as_bytes()
            },
            write: write && !(name == "import" && flag(leaf, "dry-run")),
            all_projects: allow_unscoped,
            env: Some(&lookup),
            git: Some(&git),
            ..Default::default()
        },
    )?;
    let json = flag(root, "json");
    if matches!(name, "create" | "update" | "note" | "close" | "reopen")
        || name == "dep" && matches!(action, "add" | "remove")
    {
        let mut ids = if name == "close" {
            values(m, "id")
        } else {
            vec![value(m, "id")]
        };
        if name == "create" {
            ids = vec![String::new()];
        }
        if name == "dep" {
            ids = vec![value(m.subcommand().unwrap().1, "child")];
        }
        let mut before_ready = if name == "close" && flag(m, "suggest-next") {
            Index::load_snapshot(
                &hub.dir,
                LoadOptions {
                    explicit_workflow: explicit.clone(),
                },
            )?
            .ready(&resolved.project, false)
            .iter()
            .filter_map(|n| n.graph.id.clone())
            .collect::<std::collections::HashSet<_>>()
        } else {
            Default::default()
        };
        for id in ids {
            let change = match name {
                "create" => IssueChange::Create {
                    title: value(m, "title"),
                    kind: value(m, "type"),
                    priority: number(m, "priority").unwrap_or(2),
                    description: optional(m, "description"),
                },
                "update" => IssueChange::Update {
                    claim: flag(m, "claim"),
                    title: optional(m, "title"),
                    status: optional(m, "status"),
                    assignee: optional(m, "assignee"),
                    priority: number(m, "priority"),
                },
                "note" => IssueChange::Note(values(m, "text").join(" ")),
                "close" => {
                    let reason = value(m, "reason");
                    if reason.is_empty() && !flag(m, "force") {
                        return Err(Error::new("close requires --reason or --force".into()));
                    }
                    IssueChange::Close(reason)
                }
                "reopen" => IssueChange::Reopen,
                _ => {
                    let (_, s) = m.subcommand().unwrap();
                    IssueChange::Dependency {
                        target: value(s, "parent"),
                        kind: value(s, "type"),
                        remove: action == "remove",
                    }
                }
            };
            let mut op =
                IssueMutation::new(resolved.clone(), id, change, actor.into(), explicit.clone());
            op.fields = IssueFields {
                force: flag(m, "force"),
                note: optional(m, "note"),
                kind: optional(m, "type").filter(|_| name == "update"),
                description: optional(m, "description"),
                assignee: optional(m, "assignee"),
                parent: optional(m, "parent"),
                blocked_by: values(m, "blocked-by"),
                labels: values(m, "label"),
                unlabels: values(m, "unlabel"),
                url: optional(m, "url"),
            };
            let result = hub.mutate(&mut op)?;
            if name == "close" && flag(m, "suggest-next") {
                let ix = Index::load_snapshot(
                    &hub.dir,
                    LoadOptions {
                        explicit_workflow: explicit.clone(),
                    },
                )?;
                let ready = ix.ready(&op.resolved.project, false);
                let next: Vec<_> = ready
                    .iter()
                    .copied()
                    .filter(|n| {
                        !n.graph
                            .id
                            .as_ref()
                            .is_some_and(|id| before_ready.contains(id))
                    })
                    .collect();
                if json {
                    output(
                        &serde_json::json!({"id":op.id,"commit":result.sha,"sha":result.sha,"pushed":result.pushed,"message":result.message,"next_ready":next.iter().map(|n|note_json(n)).collect::<Vec<_>>()}),
                    )?;
                } else {
                    mutation_output(&op.id, result, false)?;
                    print_notes(next, false)?;
                }
                before_ready.extend(ready.into_iter().filter_map(|n| n.graph.id.clone()));
            } else {
                mutation_output(&op.id, result, json)?;
            }
        }
        return Ok(());
    }
    if name == "request" || name == "handoff" {
        return super::record_commands::execute(name, m, hub, resolved, actor, explicit, json);
    }
    if name == "plan" {
        return super::plan_commands::execute(m, hub, resolved, explicit, json);
    }
    let index = Index::load_snapshot(
        &hub.dir,
        LoadOptions {
            explicit_workflow: explicit,
        },
    )?;
    if name == "project" {
        let (_, s) = m.subcommand().unwrap();
        let pname = value(s, "name");
        if action == "create" || action == "link" {
            if !crate::vault::valid_project_name(pname.as_bytes()) {
                return Err(Error::new("invalid project name".into()));
            }
            resolved.project = pname.as_bytes().into();
            let mut op = ContentMutation::new(
                resolved,
                ContentChange::Project {
                    link: action == "link" || flag(s, "link"),
                },
            );
            let result = hub.mutate(&mut op)?;
            return mutation_output(&op.id, result, json);
        }
        let projects: Vec<_> = index
            .projects
            .values()
            .filter(|p| action == "list" || p.name == pname.as_bytes())
            .map(|p| serde_json::json!({"name":String::from_utf8_lossy(&p.name),"config":p.config}))
            .collect();
        if action == "show" && projects.is_empty() {
            return Err(Error::new("project not found".into()));
        }
        return output(&if action == "show" {
            projects.into_iter().next().unwrap()
        } else {
            serde_json::json!(projects)
        });
    }
    if matches!(name, "remember" | "forget" | "delete") || name == "doc" && action == "new" {
        let change = match name {
            "remember" => ContentChange::Remember {
                key: value(m, "key"),
                body: values(m, "text").join(" "),
                kind: value(m, "type"),
                tags: values(m, "tag"),
                global: flag(m, "global"),
            },
            "forget" => ContentChange::Forget {
                key: value(m, "key"),
                global: flag(m, "global"),
            },
            "delete" => ContentChange::Delete {
                reference: value(m, "id"),
                force: flag(m, "force"),
            },
            _ => {
                let s = m.subcommand().unwrap().1;
                ContentChange::Doc {
                    path: value(s, "path"),
                    global: flag(s, "global"),
                }
            }
        };
        let mut op = ContentMutation::new(resolved, change).with_actor(actor);
        let result = hub.mutate(&mut op)?;
        return mutation_output(&op.id, result, json);
    }
    super::read_commands::execute(name, m, hub, &resolved, &index, json)
}
