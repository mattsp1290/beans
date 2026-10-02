use super::{
    App,
    api::{error, failure},
};
use crate::{
    ops::{IssueChange, IssueFields, IssueMutation},
    vault::{NoteKind, Resolved},
};
use axum::{
    Json,
    http::{Method, StatusCode},
    response::{IntoResponse, Response},
};
use serde::Deserialize;
use serde_json::json;
use std::collections::BTreeMap;

#[derive(Default, Deserialize)]
#[serde(deny_unknown_fields)]
struct Input {
    title: Option<String>,
    description: Option<String>,
    priority: Option<i64>,
    #[serde(rename = "type")]
    kind: Option<String>,
    labels: Option<Vec<String>>,
    parent: Option<String>,
    assignee: Option<String>,
    blocked_by: Option<Vec<String>>,
    url: Option<String>,
    status: Option<String>,
    add_labels: Option<Vec<String>>,
    remove_labels: Option<Vec<String>>,
    note: Option<String>,
    claim: Option<bool>,
    force: Option<bool>,
    text: Option<String>,
    reason: Option<String>,
    target: Option<String>,
}
pub(super) fn mutate(
    app: &App,
    method: &Method,
    parts: &[&str],
    q: &BTreeMap<String, String>,
    body: &[u8],
) -> Response {
    if matches!(parts, ["api", "plans" | "requests", _]) {
        return error(405, "method_not_allowed", "resource is read-only");
    }
    let input: Input = if body.is_empty() {
        Input::default()
    } else {
        match serde_json::from_slice(body) {
            Ok(v) => v,
            Err(e) => return error(400, "invalid_json", &e.to_string()),
        }
    };
    let index = app.index.read().unwrap();
    let (p, id, change) = match parts {
        ["api", "projects", p, "issues"] if method == Method::POST => {
            if !index.projects.contains_key(p.as_bytes()) {
                return error(404, "not_found", "project not found");
            }
            (
                p.to_string(),
                String::new(),
                IssueChange::Create {
                    title: input.title.clone().unwrap_or_default(),
                    kind: input.kind.clone().unwrap_or_else(|| "task".into()),
                    priority: input.priority.unwrap_or(2),
                    description: input.description.clone(),
                },
            )
        }
        ["api", "issues", id, tail @ ..] => {
            let Some(n) = index.note_by_id(NoteKind::Issue, id.as_bytes()) else {
                return error(404, "not_found", "issue not found");
            };
            let change = match tail {
                [] if method == Method::PATCH => IssueChange::Update {
                    claim: input.claim.unwrap_or(false),
                    title: input.title.clone(),
                    status: input.status.clone(),
                    assignee: input.assignee.clone(),
                    priority: input.priority,
                },
                ["notes"]
                    if method == Method::POST
                        && input.text.as_ref().is_some_and(|s| !s.trim().is_empty()) =>
                {
                    IssueChange::Note(input.text.clone().unwrap())
                }
                ["close"]
                    if method == Method::POST
                        && input.reason.as_ref().is_some_and(|s| !s.trim().is_empty()) =>
                {
                    IssueChange::Close(input.reason.clone().unwrap())
                }
                ["reopen"] if method == Method::POST => IssueChange::Reopen,
                ["deps"] if method == Method::POST => IssueChange::Dependency {
                    target: input.target.clone().unwrap_or_default(),
                    kind: input.kind.clone().unwrap_or_default(),
                    remove: false,
                },
                ["deps", target] if method == Method::DELETE => IssueChange::Dependency {
                    target: target.to_string(),
                    kind: q.get("type").cloned().unwrap_or_else(|| "blocks".into()),
                    remove: true,
                },
                ["notes" | "close"] if method == Method::POST => {
                    return error(400, "validation_error", "non-empty text or reason required");
                }
                _ => return error(404, "not_found", "API route not found"),
            };
            (
                String::from_utf8_lossy(&n.project).into_owned(),
                id.to_string(),
                change,
            )
        }
        _ => return error(404, "not_found", "API route not found"),
    };
    drop(index);
    let resolved = Resolved {
        hub_dir: app.hub.dir.clone(),
        project: p.as_bytes().into(),
        project_dir: app.hub.dir.join("projects").join(&p),
        ..Default::default()
    };
    let create = matches!(change, IssueChange::Create { .. });
    let mut op = IssueMutation::new(
        resolved,
        id,
        change,
        app.hub.actor.clone(),
        app.options.explicit_workflow.clone(),
    );
    op.fields = IssueFields {
        force: input.force.unwrap_or(false),
        note: input.note,
        kind: input
            .kind
            .filter(|_| !matches!(op.change, IssueChange::Dependency { .. })),
        description: input.description,
        assignee: input.assignee,
        parent: input.parent,
        blocked_by: input.blocked_by.unwrap_or_default(),
        labels: input.labels.or(input.add_labels).unwrap_or_default(),
        unlabels: input.remove_labels.unwrap_or_default(),
        url: input.url,
    };
    match app.hub.mutate(&mut op) {
        Ok(result) => {
            if let Err(e) = app.refresh() {
                return failure(e);
            }
            let path = app
                .index
                .read()
                .unwrap()
                .note_by_id(NoteKind::Issue, op.id.as_bytes())
                .map(|n| String::from_utf8_lossy(&n.graph.path).into_owned())
                .unwrap_or_default();
            (if create {StatusCode::CREATED} else {StatusCode::OK},Json(json!({"id":op.id,"path":path,"commit":result.sha,"pushed":result.pushed,"message":result.message}))).into_response()
        }
        Err(e) => failure(e),
    }
}
