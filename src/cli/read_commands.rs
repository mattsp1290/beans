use super::dispatch::*;
use crate::{
    domain::frontmatter::Error,
    gitops::Hub,
    ops::{ArchiveMutation, ArchiveSelection},
    vault::{Index, NoteData, Resolved, SearchOptions},
};
use clap::ArgMatches;
pub fn duration(s: &str) -> Result<i64, Error> {
    let split = s.find(|c: char| !c.is_ascii_digit()).unwrap_or(s.len());
    let n = s[..split]
        .parse::<i64>()
        .map_err(|_| Error::new("invalid age; use 30d or 12h".into()))?;
    let multiplier = match &s[split..] {
        "d" => 86400,
        "h" => 3600,
        "m" => 60,
        "s" => 1,
        _ => return Err(Error::new("invalid age; use 30d or 12h".into())),
    };
    n.checked_mul(multiplier)
        .ok_or_else(|| Error::new("age overflow".into()))
}
pub fn cutoff(s: &str) -> Result<i64, Error> {
    crate::ops::records::now()
        .seconds
        .checked_sub(duration(s)?)
        .ok_or_else(|| Error::new("age overflow".into()))
}
pub fn execute(
    name: &str,
    m: &ArgMatches,
    hub: &Hub,
    resolved: &Resolved,
    ix: &Index,
    json: bool,
) -> Result<(), Error> {
    let project = if flag(m, "all-projects") {
        b"".as_slice()
    } else {
        &resolved.project
    };
    match name {
        "show" => {
            let (_, n) = ix.resolve_issue_ref(value(m, "id").as_bytes());
            let n = n.ok_or_else(|| {
                Error::new("issue not found (check malformed hub documents)".into())
            })?;
            if json {
                let mut v = note_json(n);
                v["backlinks"] =
                    backlinks(ix, &n.graph.basename, flag(m, "include-archived-handoffs"));
                output(&v)
            } else {
                print!("{}", String::from_utf8_lossy(&n.source));
                Ok(())
            }
        }
        "list" | "ready" | "children" | "parents" => {
            let mut notes = if name == "ready" {
                ix.ready(project, false)
            } else if name == "children" || name == "parents" {
                let (_, n) = ix.resolve_issue_ref(value(m, "id").as_bytes());
                let n = n.ok_or_else(|| Error::new("issue not found".into()))?;
                if name == "children" {
                    ix.children(n.graph.id.as_ref().unwrap())
                } else {
                    ix.parents(n.graph.id.as_ref().unwrap())
                }
            } else {
                ix.project_issues(project, flag(m, "archived") || flag(m, "closed"))
            };
            notes.retain(|n| {
                let NoteData::Issue(d) = &n.data else {
                    return false;
                };
                let v = &d.metadata;
                (value(m, "status").is_empty() || v.status == value(m, "status"))
                    && (value(m, "type").is_empty() || v.kind == value(m, "type"))
                    && (value(m, "assignee").is_empty() || v.assignee == value(m, "assignee"))
                    && (value(m, "label").is_empty() || v.labels.contains(&value(m, "label")))
                    && (name != "list"
                        || if flag(m, "closed") {
                            ix.workflow_for(&n.project).is_terminal(v.status.as_bytes())
                        } else {
                            flag(m, "archived")
                                || !value(m, "status").is_empty()
                                || !ix.workflow_for(&n.project).is_terminal(v.status.as_bytes())
                        })
            });
            if name != "ready" {
                match value(m, "sort").as_str() {
                    "" | "created" => notes.sort_by_key(|n| match &n.data {
                        NoteData::Issue(d) => d.metadata.created.seconds,
                        _ => 0,
                    }),
                    "updated" => notes.sort_by_key(|n| {
                        std::cmp::Reverse(match &n.data {
                            NoteData::Issue(d) => d.metadata.updated.seconds,
                            _ => 0,
                        })
                    }),
                    "priority" => notes.sort_by_key(|n| match &n.data {
                        NoteData::Issue(d) => d.metadata.priority,
                        _ => 0,
                    }),
                    "title" => notes.sort_by(|a, b| a.title.cmp(&b.title)),
                    other => return Err(Error::new(format!("invalid sort: {other}"))),
                }
            }
            if let Some(limit) = number(m, "limit")
                && limit > 0
            {
                notes.truncate(limit as usize);
            }
            print_notes(notes, json)
        }
        "blocked" => output(&serde_json::json!(
            ix.blocked(project, false)
                .iter()
                .map(|b| serde_json::json!({"issue":note_json(b.issue),"blockers":b.blockers}))
                .collect::<Vec<_>>()
        )),
        "dep" => {
            let (action, s) = m.subcommand().unwrap();
            if action == "cycles" {
                let cycles = ix.cycles();
                output(&serde_json::json!(cycles))?;
                return if cycles.is_empty() {
                    Ok(())
                } else {
                    Err(Error::new("dependency cycles found".into()))
                };
            }
            let graph = ix.dependency_graph(
                if flag(s, "all-projects") {
                    b""
                } else {
                    project
                },
                false,
            );
            let id = value(s, "id");
            let mut nodes = graph.nodes;
            let mut edges = graph.edges;
            if !id.is_empty() {
                let (_, n) = ix.resolve_issue_ref(id.as_bytes());
                let n = n.ok_or_else(|| Error::new("issue not found".into()))?;
                let mut reachable = std::collections::BTreeSet::from([String::from_utf8_lossy(
                    n.graph.id.as_ref().unwrap(),
                )
                .into_owned()]);
                loop {
                    let before = reachable.len();
                    for e in &edges {
                        if reachable.contains(e.from.as_str().unwrap_or_default()) {
                            reachable.insert(e.to.to_string());
                        }
                    }
                    if before == reachable.len() {
                        break;
                    }
                }
                nodes.retain(|n| reachable.contains(n.id.as_str().unwrap_or_default()));
                edges.retain(|e| reachable.contains(e.from.as_str().unwrap_or_default()));
            }
            output(&serde_json::json!({"nodes":nodes,"edges":edges}))
        }
        "search" => {
            let kinds = value(m, "kind");
            if !kinds.is_empty()
                && kinds.split(',').any(|kind| {
                    !matches!(
                        kind,
                        "issue" | "request" | "memory" | "handoff" | "doc" | "plan"
                    )
                })
            {
                return Err(Error::new("unknown search kind".into()));
            }
            let hits = ix.search(
                values(m, "query").join(" ").as_bytes(),
                &SearchOptions {
                    kinds: if kinds.is_empty() {
                        vec![]
                    } else {
                        kinds.split(',').map(Into::into).collect()
                    },
                    include_archived_handoffs: flag(m, "include-archived-handoffs"),
                },
            );
            output(&serde_json::json!(
                hits.into_iter()
                    .filter(|h| project.is_empty()
                        || h.project.as_bytes() == project
                        || h.project.is_empty())
                    .collect::<Vec<_>>()
            ))
        }
        "doctor" => {
            let warnings: Vec<_> = ix
                .graph
                .warnings()
                .iter()
                .filter(|w| {
                    project.is_empty()
                        || !String::from_utf8_lossy(&w.path).starts_with("projects/")
                        || String::from_utf8_lossy(&w.path)
                            .starts_with(&format!("projects/{}/", String::from_utf8_lossy(project)))
                })
                .collect();
            let cycles = ix.cycles();
            let ok = warnings.is_empty() && cycles.is_empty();
            output(
                &serde_json::json!({"warnings":warnings.iter().map(|w|serde_json::json!({"path":String::from_utf8_lossy(&w.path),"message":w.error.to_string()})).collect::<Vec<_>>(),"cycles":cycles,"ok":ok}),
            )?;
            if ok {
                Ok(())
            } else {
                Err(Error::new(
                    "doctor found hub problems; inspect the diagnostics above".into(),
                ))
            }
        }
        "memories" => {
            let keywords = values(m, "keyword");
            let mut notes: Vec<_> = ix
                .ordered_notes()
                .iter()
                .filter(|n| {
                    let NoteData::Memory(d) = &n.data else {
                        return false;
                    };
                    (flag(m, "all") || n.project.is_empty() || n.project == resolved.project)
                        && (value(m, "type").is_empty() || d.metadata.kind == value(m, "type"))
                        && (value(m, "tag").is_empty()
                            || d.metadata.tags.contains(&value(m, "tag")))
                        && keywords.iter().all(|k| {
                            format!("{} {}", d.metadata.key, d.body)
                                .to_lowercase()
                                .contains(&k.to_lowercase())
                        })
                })
                .collect();
            notes.sort_by(|a, b| a.title.cmp(&b.title));
            if let Some(limit) = number(m, "limit")
                && limit > 0
            {
                notes.truncate(limit as usize);
            }
            print_notes(notes, json)
        }
        "doc" => {
            let (action, s) = m.subcommand().unwrap();
            if action == "backlinks" {
                let path = value(s, "path");
                let n = ix
                    .lookup(path.as_bytes())
                    .or_else(|| ix.note_by_path(path.as_bytes()))
                    .ok_or_else(|| Error::new("doc not found".into()))?;
                return output(&backlinks(ix, &n.graph.basename, true));
            }
            let dir = value(s, "dir");
            let notes = ix
                .ordered_notes()
                .iter()
                .filter(|n| {
                    matches!(n.data, NoteData::Doc(_))
                        && (if flag(s, "global") {
                            n.project.is_empty()
                        } else {
                            resolved.project.is_empty()
                                || n.project.is_empty()
                                || n.project == resolved.project
                        })
                        && (dir.is_empty() || String::from_utf8_lossy(&n.graph.path).contains(&dir))
                })
                .collect();
            print_notes(notes, json)
        }
        "archive" => {
            let mut op = ArchiveMutation::new(
                ArchiveSelection::Issues {
                    project: project.to_vec(),
                    cutoff: cutoff(&value(m, "older-than"))?,
                },
                std::env::var_os("BN_CONFIG")
                    .filter(|v| !v.is_empty())
                    .map(Into::into),
            );
            if flag(m, "dry-run") {
                return print_notes(op.select(ix)?, json);
            }
            let result = hub.mutate(&mut op)?;
            mutation_output("archive", result, json)
        }
        "cache" => {
            hub.clear_cache()?;
            output(&serde_json::json!({"cleared":true}))
        }
        "import" => super::import_commands::execute(m, hub, resolved, json),
        _ => Err(Error::new(format!("unknown command: {name}"))),
    }
}

fn backlinks(ix: &Index, basename: &[u8], archived: bool) -> serde_json::Value {
    serde_json::json!(ix.issue_backlinks(basename,archived).iter().map(|r|serde_json::json!({"from":String::from_utf8_lossy(&r.from),"to":String::from_utf8_lossy(&r.to),"kind":r.kind})).collect::<Vec<_>>())
}
