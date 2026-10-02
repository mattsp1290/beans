use super::dispatch::*;
use crate::{
    domain::frontmatter::Error,
    gitops::Hub,
    ops::{RecordChange, RecordMutation, RequestEdit},
    vault::{Index, LoadOptions, NoteData, NoteKind, RequestFilter, Resolved},
};
use clap::ArgMatches;
use std::path::PathBuf;
pub fn execute(
    family: &str,
    m: &ArgMatches,
    hub: &Hub,
    resolved: Resolved,
    actor: &str,
    explicit: Option<PathBuf>,
    json: bool,
) -> Result<(), Error> {
    let (action, s) = m.subcommand().unwrap();
    let ix = Index::load_snapshot(
        &hub.dir,
        LoadOptions {
            explicit_workflow: explicit.clone(),
        },
    )?;
    let kind = if family == "request" {
        NoteKind::Request
    } else {
        NoteKind::Handoff
    };
    if action == "list" {
        let project = if flag(s, "all-projects") {
            vec![]
        } else {
            resolved.project.clone()
        };
        let mut notes = if family == "request" {
            ix.project_requests(&RequestFilter {
                project,
                status: value(s, "status").into_bytes(),
                label: value(s, "label").into_bytes(),
                priority: number(s, "priority"),
                query: value(s, "query").into_bytes(),
                terminal: flag(s, "terminal"),
            })
        } else {
            ix.project_handoffs(&project, flag(s, "archived"))
        };
        let issue = value(s, "issue");
        if !issue.is_empty() {
            let link = crate::ops::issues::canonical_link(&ix, &issue)?;
            notes.retain(
                |n| matches!(&n.data,NoteData::Handoff(d) if d.metadata.issue.target==link.target),
            );
        }
        let age = value(s, "older-than");
        if !age.is_empty() {
            let cutoff = crate::ops::records::now().seconds - super::read_commands::duration(&age)?;
            notes.retain(
                |n| matches!(&n.data,NoteData::Handoff(d) if d.metadata.updated.seconds<cutoff),
            );
        }
        match value(s, "sort").as_str() {
            "" | "created" => (),
            "updated" => notes.sort_by_key(|n| {
                std::cmp::Reverse(match &n.data {
                    NoteData::Handoff(d) => d.metadata.updated.seconds,
                    _ => 0,
                })
            }),
            "title" => notes.sort_by(|a, b| a.title.cmp(&b.title)),
            other => return Err(Error::new(format!("invalid sort: {other}"))),
        }
        if let Some(n) = number(s, "limit")
            && n > 0
        {
            notes.truncate(n as usize);
        }
        return print_notes(notes, json);
    }
    let reference = value(s, "id");
    if action == "show" {
        let n = ix
            .note_by_id(kind, reference.as_bytes())
            .or_else(|| {
                ix.lookup(reference.as_bytes())
                    .filter(|n| n.graph.kind == kind)
            })
            .ok_or_else(|| Error::new("record not found".into()))?;
        if json {
            return output(&note_json(n));
        }
        print!("{}", String::from_utf8_lossy(&n.source));
        return Ok(());
    }
    let change = if family == "request" {
        let edit = RequestEdit {
            title: if action == "create" {
                Some(value(s, "title"))
            } else {
                nonempty(s, "title")
            },
            status: nonempty(s, "status"),
            priority: number(s, "priority"),
            requested_by: nonempty(s, "requested-by"),
            body: body(s, "body-file")?,
            labels: values(s, "label"),
            unlabels: values(s, "unlabel"),
            issues: if matches!(action, "link" | "unlink") {
                values(s, "issue-id")
            } else {
                values(s, "issue")
            },
            unlink: action == "unlink",
            force: flag(s, "force"),
        };
        if action == "create" {
            RecordChange::RequestCreate(edit)
        } else {
            RecordChange::RequestUpdate(edit)
        }
    } else {
        match action {
            "create" => {
                let text = body(s, "file")?.ok_or_else(|| {
                    Error::new("handoff create requires --file (use --file - for stdin)".into())
                })?;
                let title = value(s, "title");
                RecordChange::HandoffCreate {
                    title: if title.is_empty() {
                        "Session continuation".into()
                    } else {
                        title
                    },
                    body: text.into_bytes(),
                    issue: nonempty(s, "issue"),
                }
            }
            "attach" => RecordChange::HandoffAttach(Some(value(s, "issue-id"))),
            "detach" => RecordChange::HandoffAttach(None),
            "archive" => RecordChange::HandoffArchive(true),
            "restore" => RecordChange::HandoffArchive(false),
            _ => return Err(Error::new("unknown handoff action".into())),
        }
    };
    if action == "archive" {
        if !values(s, "id").is_empty() && !value(s, "older-than").is_empty() {
            return Err(Error::new(
                "pass handoff IDs or --older-than, not both".into(),
            ));
        }
        if flag(s, "all-projects") && value(s, "older-than").is_empty() {
            return Err(Error::new("--all-projects requires --older-than".into()));
        }
    }
    let mut ids = if matches!(action, "archive" | "restore") {
        values(s, "id")
    } else {
        vec![reference]
    };
    if action == "archive" && ids.is_empty() {
        let age = value(s, "older-than");
        if age.is_empty() {
            return Err(Error::new("archive requires IDs or --older-than".into()));
        }
        let cutoff = crate::ops::records::now().seconds - super::read_commands::duration(&age)?;
        ids = ix
            .project_handoffs(
                if flag(s, "all-projects") {
                    b""
                } else {
                    &resolved.project
                },
                false,
            )
            .into_iter()
            .filter_map(|n| match &n.data {
                NoteData::Handoff(h) if h.metadata.updated.seconds <= cutoff => {
                    Some(h.metadata.id.clone())
                }
                _ => None,
            })
            .collect();
    }
    for id in ids {
        if flag(s, "dry-run") {
            println!("{id}");
            continue;
        }
        let mut scope = resolved.clone();
        if matches!(action, "archive" | "restore")
            && let Some(n) = ix.note_by_id(kind, id.as_bytes())
        {
            scope.project = n.project.clone();
        }
        let mut op = RecordMutation::new(scope, id, change.clone(), actor.into(), explicit.clone());
        let result = hub.mutate(&mut op)?;
        mutation_output(&op.id, result, json)?;
    }
    Ok(())
}
fn nonempty(m: &ArgMatches, k: &str) -> Option<String> {
    m.try_get_one::<String>(k).ok().flatten().cloned()
}
