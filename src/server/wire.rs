use crate::{
    markdown,
    vault::{Index, Note, NoteData},
};
use serde_json::{Value, json};
fn s(v: &[u8]) -> String {
    String::from_utf8_lossy(v).into_owned()
}
pub fn backlinks(index: &Index, note: &Note, archived: bool) -> Value {
    backlinks_without_derived_prompts(index, note, archived, &[])
}
fn backlinks_without_derived_prompts(
    index: &Index,
    note: &Note,
    archived: bool,
    derived_prompts: &[Vec<u8>],
) -> Value {
    json!(index.issue_backlinks(&note.graph.basename, archived).iter().filter(|r| {
        !matches!(r.kind, crate::vault::LinkKind::RequestIssue)
            && !(matches!(r.kind, crate::vault::LinkKind::PromptIssue)
                && derived_prompts.contains(&r.from))
    }).filter_map(|r| index.lookup(&r.from).map(|n| json!({"from":s(n.graph.id.as_deref().unwrap_or(&n.graph.basename)),"kind":r.kind,"note_kind":n.graph.kind,"path":s(&n.graph.path),"title":s(&n.title)}))).collect::<Vec<_>>())
}
/// Stable HTTP-only prompt list projection. Domain and CLI JSON deliberately
/// retain different timestamp and relationship representations.
pub fn prompt_summary(n: &Note) -> Value {
    let NoteData::Prompt(document) = &n.data else {
        unreachable!("prompt summary received a non-prompt note")
    };
    let metadata = &document.metadata;
    json!({
        "id": metadata.id,
        "title": metadata.title,
        "status": metadata.status,
        "priority": metadata.priority,
        "labels": metadata.labels,
        "from": metadata.from,
        "to": metadata.to,
        "claimed_by": metadata.claimed_by,
        "created": timestamp(&metadata.created),
        "updated": timestamp(&metadata.updated),
        "project": s(&n.project),
        "path": s(&n.graph.path),
        "has_issue": !metadata.issue.target.is_empty(),
    })
}
pub fn prompt_detail(index: &Index, n: &Note) -> Value {
    let NoteData::Prompt(document) = &n.data else {
        unreachable!("prompt detail received a non-prompt note")
    };
    let mut value = prompt_summary(n);
    let rendered = markdown::render(&document.body.to_string(), index, &s(&n.graph.path));
    value["html"] = json!(rendered.html);
    value["toc"] = json!(rendered.toc);
    value["backlinks"] = backlinks(index, n, false);
    value["outlinks"] = json!(
        n.graph
            .outlinks
            .iter()
            .map(|link| json!({"to":s(&link.to),"kind":link.kind}))
            .collect::<Vec<_>>()
    );
    value["log"] = log_entries(&document.log);
    value["issue"] = if document.metadata.issue.target.is_empty() {
        Value::Null
    } else {
        let (id, resolved) = index.resolve_issue_ref(document.metadata.issue.target.as_bytes());
        resolved.map_or_else(
            || json!({"id":s(&id),"title":"","missing":true}),
            |issue| json!({"id":s(issue.graph.id.as_deref().unwrap_or(&id)),"title":s(&issue.title),"missing":false}),
        )
    };
    value
}
pub fn note(index: &Index, n: &Note, detail: bool) -> Value {
    note_with_backlinks(index, n, detail, false)
}
pub fn detail(index: &Index, n: &Note, include_archived_handoffs: bool) -> Value {
    note_with_backlinks(index, n, true, include_archived_handoffs)
}
fn note_with_backlinks(
    index: &Index,
    n: &Note,
    detail: bool,
    include_archived_handoffs: bool,
) -> Value {
    let mut derived_prompts = Vec::new();
    let mut v = match &n.data {
        NoteData::Issue(d) => {
            let mut v = serde_json::to_value(&d.metadata).unwrap();
            timestamps(&mut v, &d.metadata.created, &d.metadata.updated);
            v["parent"] = json!(d.metadata.parent.target);
            v["blocked_by"] = json!(
                d.metadata
                    .blocked_by
                    .iter()
                    .map(|l| &l.target)
                    .collect::<Vec<_>>()
            );
            v["description"] = json!(d.description.to_string());
            v["log"] = log_entries(&d.log);
            v["workflow"] = json!(index.workflow_for(&n.project));
            v["children"] = json!(
                index
                    .children(d.metadata.id.as_bytes())
                    .iter()
                    .map(|c| summary(c))
                    .collect::<Vec<_>>()
            );
            let (found, missing) = index.blockers(n);
            v["blockers"] = json!(
                found
                    .iter()
                    .map(|c| summary(c))
                    .chain(missing.iter().map(|id| json!({"id":id,"missing":true})))
                    .collect::<Vec<_>>()
            );
            v["requests"] = json!(index.ordered_notes().iter().filter(|r| matches!(&r.data, NoteData::Request(r) if r.metadata.issues.iter().any(|l| l.target == d.metadata.id || l.target.as_bytes() == n.graph.basename))).map(|r| note(index,r,false)).collect::<Vec<_>>());
            let prompts = index
                .project_prompts(&crate::vault::PromptFilter {
                    terminal: true,
                    ..Default::default()
                })
                .into_iter()
                .filter(|prompt| {
                    matches!(&prompt.data, NoteData::Prompt(document)
                        if index
                            .resolve_issue_ref(document.metadata.issue.target.as_bytes())
                            .1
                            .is_some_and(|issue| issue.graph.path == n.graph.path))
                })
                .collect::<Vec<_>>();
            derived_prompts.extend(prompts.iter().map(|prompt| prompt.graph.basename.clone()));
            v["prompts"] = json!(prompts.into_iter().map(prompt_summary).collect::<Vec<_>>());
            v
        }
        NoteData::Request(d) => {
            let mut v = serde_json::to_value(&d.metadata).unwrap();
            timestamps(&mut v, &d.metadata.created, &d.metadata.updated);
            v["issue_count"] = json!(d.metadata.issues.len());
            v["issues"] = json!(
                d.metadata
                    .issues
                    .iter()
                    .map(|l| index
                        .resolve_issue_ref(l.target.as_bytes())
                        .1
                        .map(summary)
                        .unwrap_or_else(|| json!({"id":l.target,"missing":true})))
                    .collect::<Vec<_>>()
            );
            v["log"] = log_entries(&d.log);
            v
        }
        NoteData::Plan(d) => {
            let mut v = serde_json::to_value(d).unwrap();
            timestamps(&mut v, &d.created, &d.updated);
            v["section_count"] = json!(d.sections.len());
            if detail {
                v["summary"] = json!({"status":d.status,"outcome_html":markdown::render(&d.summary.outcome,index,&s(&n.graph.path)).html,"affected_areas_html":markdown::render(&d.summary.affected_areas,index,&s(&n.graph.path)).html,"execution_order_html":markdown::render(&d.summary.execution_order,index,&s(&n.graph.path)).html,"risks_html":markdown::render(&d.summary.risks,index,&s(&n.graph.path)).html,"graph":{"version":d.graph.version,"nodes":d.graph.nodes.as_deref().unwrap_or_default(),"edges":d.graph.edges.as_deref().unwrap_or_default()}});
                v["sections"] = json!(d.section_bodies.iter().map(|section| json!({"path":section.path,"html":markdown::render(&section.markdown,index,&s(&n.graph.path)).html})).collect::<Vec<_>>());
                let mut execution = json!(index.plan_execution(d.id.as_bytes()));
                if let Some(nodes) = execution["nodes"].as_array_mut() {
                    for node in nodes {
                        if node["blockers"].is_null() {
                            node["blockers"] = json!([]);
                        }
                    }
                }
                v["execution"] = execution;
            }
            v
        }
        NoteData::Doc(d) => {
            json!({"title":s(&n.title),"kind":n.graph.kind,"frontmatter":d.frontmatter.as_ref().map(|m| m.iter().map(|(k,v)|(s(k),yaml(v))).collect::<serde_json::Map<_,_>>()).unwrap_or_default()})
        }
        _ => json!({"title":s(&n.title),"kind":n.graph.kind,"frontmatter":{}}),
    };
    v["path"] = json!(s(&n.graph.path));
    v["project"] = json!(s(&n.project));
    if detail {
        let text = if matches!(n.data, NoteData::Issue(_)) {
            s(&n.description)
        } else {
            s(&n.body)
        };
        let rendered = markdown::render(&text, index, &s(&n.graph.path));
        v["html"] = json!(rendered.html);
        v["toc"] = json!(rendered.toc);
        v["backlinks"] = backlinks_without_derived_prompts(
            index,
            n,
            include_archived_handoffs,
            &derived_prompts,
        );
        v["outlinks"] = json!(
            n.graph
                .outlinks
                .iter()
                .map(|l| json!({"to":s(&l.to),"kind":l.kind}))
                .collect::<Vec<_>>()
        );
    }
    v
}
fn summary(n: &Note) -> Value {
    let NoteData::Issue(d) = &n.data else {
        return json!({});
    };
    json!({"id":d.metadata.id,"title":d.metadata.title,"status":d.metadata.status,"priority":d.metadata.priority,"project":s(&n.project),"archived":d.metadata.archived})
}

fn yaml(v: &crate::vault::YamlValue) -> Value {
    use crate::vault::YamlValue as Y;
    match v {
        Y::Null => Value::Null,
        Y::String(v) => json!(s(v)),
        Y::Bool(v) => json!(v),
        Y::Int(v) => json!(v),
        Y::Uint(v) => json!(v),
        Y::Float(v) => json!(v),
        Y::Timestamp(v) => json!(v),
        Y::Sequence(v) => json!(v.iter().map(yaml).collect::<Vec<_>>()),
        Y::StringMap(v) => Value::Object(v.iter().map(|(k, v)| (s(k), yaml(v))).collect()),
        Y::Map(_) => Value::Null,
    }
}

// HTTP owns RFC3339 strings; retained domain Serialize stays numeric for fixtures.
fn timestamps(
    value: &mut Value,
    created: &crate::domain::issue::Timestamp,
    updated: &crate::domain::issue::Timestamp,
) {
    value["created"] = json!(timestamp(created));
    value["updated"] = json!(timestamp(updated));
}
fn timestamp(value: &crate::domain::issue::Timestamp) -> String {
    // Parsed timestamps have bounded calendar years; express their original offset
    // without normalizing away fractional precision or changing the shared codec.
    let date = time::OffsetDateTime::from_unix_timestamp(value.seconds)
        .expect("parsed timestamp has a representable calendar")
        + time::Duration::seconds(i64::from(value.offset_seconds));
    let fraction = if value.nanoseconds == 0 {
        String::new()
    } else {
        format!(".{:09}", value.nanoseconds)
            .trim_end_matches('0')
            .to_owned()
    };
    let offset = if value.offset_seconds == 0 {
        "Z".to_owned()
    } else {
        let seconds = value.offset_seconds.unsigned_abs();
        format!(
            "{}{:02}:{:02}",
            if value.offset_seconds < 0 { '-' } else { '+' },
            seconds / 3600,
            seconds % 3600 / 60
        )
    };
    format!(
        "{:04}-{:02}-{:02}T{:02}:{:02}:{:02}{fraction}{offset}",
        date.year(),
        date.month() as u8,
        date.day(),
        date.hour(),
        date.minute(),
        date.second()
    )
}
fn log_entries(entries: &[crate::domain::log::LogEntry]) -> Value {
    json!(
        entries
            .iter()
            .map(|entry| {
                let mut value = serde_json::to_value(entry).unwrap();
                value["at"] = json!(timestamp(&entry.at));
                value
            })
            .collect::<Vec<_>>()
    )
}
