use crate::{
    markdown,
    vault::{Index, Note, NoteData},
};
use serde_json::{Value, json};
fn s(v: &[u8]) -> String {
    String::from_utf8_lossy(v).into_owned()
}
pub fn backlinks(index: &Index, note: &Note, archived: bool) -> Value {
    json!(index.issue_backlinks(&note.graph.basename, archived).iter().filter(|r| r.kind != crate::vault::LinkKind::RequestIssue).filter_map(|r| index.lookup(&r.from).map(|n| json!({"from":s(n.graph.id.as_deref().unwrap_or(&n.graph.basename)),"kind":r.kind,"note_kind":n.graph.kind,"path":s(&n.graph.path),"title":s(&n.title)}))).collect::<Vec<_>>())
}
pub fn note(index: &Index, n: &Note, detail: bool) -> Value {
    let mut v = match &n.data {
        NoteData::Issue(d) => {
            let mut v = serde_json::to_value(&d.metadata).unwrap();
            v["parent"] = json!(d.metadata.parent.target);
            v["blocked_by"] = json!(
                d.metadata
                    .blocked_by
                    .iter()
                    .map(|l| &l.target)
                    .collect::<Vec<_>>()
            );
            v["description"] = json!(d.description.to_string());
            v["log"] = json!(d.log);
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
            v
        }
        NoteData::Request(d) => {
            let mut v = serde_json::to_value(&d.metadata).unwrap();
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
            v["log"] = json!(d.log);
            v
        }
        NoteData::Plan(d) => {
            let mut v = serde_json::to_value(d).unwrap();
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
        v["backlinks"] = backlinks(index, n, false);
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
