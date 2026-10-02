use super::*;
use crate::vault::{NoteData, NoteKind, RequestFilter, SearchOptions};
use axum::{Json, http::StatusCode};
use serde::Serialize;
use serde_json::json;
use std::collections::BTreeMap;
pub fn error(status: u16, code: &str, message: &str) -> Response {
    (
        StatusCode::from_u16(status).unwrap(),
        Json(json!({"error":{"code":code,"message":message}})),
    )
        .into_response()
}
pub(super) fn failure(e: Error) -> Response {
    let message = e.to_string();
    let (status, code) = if message.starts_with("issue not found:") {
        (404, "not_found")
    } else if message == "another bn is running on this hub (lock held)" {
        (423, "hub_locked")
    } else if message.starts_with("dependency cycle rejected")
        || message.starts_with("parent cycle rejected")
    {
        (409, "dependency_cycle")
    } else if message.starts_with("hub has an interrupted") || message.starts_with("hub is on") {
        (409, "git_conflict")
    } else if message.starts_with("git ")
        || message.starts_with("push rejected")
        || message.starts_with("rebase ")
    {
        (502, "git_error")
    } else {
        (400, "validation_error")
    };
    error(status, code, &message)
}
fn decode(raw: &str) -> Option<String> {
    let mut out = Vec::new();
    let mut bytes = raw.bytes();
    while let Some(b) = bytes.next() {
        if b == b'%' {
            let a = (bytes.next()? as char).to_digit(16)?;
            let b = (bytes.next()? as char).to_digit(16)?;
            out.push((a * 16 + b) as u8);
        } else {
            out.push(b);
        }
    }
    String::from_utf8(out).ok()
}
fn query(uri: &Uri) -> BTreeMap<String, String> {
    uri.query()
        .unwrap_or_default()
        .split('&')
        .filter_map(|p| p.split_once('='))
        .filter_map(|(k, v)| Some((decode(k)?, decode(&v.replace('+', " "))?)))
        .collect()
}
fn project<'a>(p: &'a str, app: &'a App) -> &'a [u8] {
    if p == "_all" {
        b""
    } else if p.is_empty() {
        app.project.as_bytes()
    } else {
        p.as_bytes()
    }
}
fn json_response<T: Serialize>(v: T) -> Response {
    Json(v).into_response()
}
pub fn handle(app: &App, method: &Method, uri: &Uri, body: &[u8]) -> Response {
    let Some(path) = decode(uri.path()) else {
        return error(400, "invalid_path", "invalid URL encoding");
    };
    let q = query(uri);
    let get = method == Method::GET || method == Method::HEAD;
    if path != "/api" && !path.starts_with("/api/") {
        if !get {
            return error(405, "method_not_allowed", "method not allowed");
        }
        let name = path.trim_start_matches('/');
        if let Some((_, bytes)) = ASSETS.iter().find(|(n, _)| *n == name) {
            return files::response(name, bytes.to_vec());
        }
        if name.starts_with("assets/") || name.rsplit('/').next().is_some_and(|n| n.contains('.')) {
            return error(404, "not_found", "asset not found");
        }
        return files::response(
            "index.html",
            ASSETS
                .iter()
                .find(|(n, _)| *n == "index.html")
                .unwrap()
                .1
                .to_vec(),
        );
    }
    let parts: Vec<_> = path.trim_start_matches('/').split('/').collect();
    if !get {
        return super::mutations::mutate(app, method, &parts, &q, body);
    }
    let index = app.index.read().unwrap();
    let qp = project(
        q.get("project").map(String::as_str).unwrap_or_default(),
        app,
    );
    match parts.as_slice() {
        ["api","health"] => {
            let status = app.hub.status().unwrap_or_else(|_|json!({}));
            json_response(json!({"status":"ok","hub":app.hub.dir,"project":app.project,"ahead":status["ahead"].as_u64().unwrap_or(0),"behind":status["behind"].as_u64().unwrap_or(0)}))
        }
        ["api","projects"] => json_response(index.projects.values().map(|p| {
            let notes = index.project_issues(&p.name,true);
            let count = |state: &str| notes.iter().filter(|n|match &n.data {
                NoteData::Issue(d) => {
                    let terminal=p.workflow.is_terminal(d.metadata.status.as_bytes());
                    match state {"closed"=>terminal,"in_progress"=>!terminal && d.metadata.status=="in_progress",_=>!terminal && d.metadata.status!="in_progress"}
                },_=>false,
            }).count();
            json!({"name":String::from_utf8_lossy(&p.name),"prefix":if p.config.prefix.is_empty() {String::from_utf8_lossy(&p.name).into_owned()} else {p.config.prefix.to_string()},"counts":{"open":count("open"),"in_progress":count("in_progress"),"closed":count("closed")},"workflow":p.workflow})
        }).collect::<Vec<_>>()),
        ["api","projects",p,kind @ ("issues" | "ready" | "plans" | "requests")] => {
            if *p != "_all" && !index.projects.contains_key(p.as_bytes()) { return error(404,"not_found","project not found"); }
            let p = project(p,app);
            if *kind=="requests" {
                if q.get("status").is_some_and(|v| !v.is_empty() && !crate::domain::request::valid_status(v)) {return error(400,"validation_error","invalid request status");}
                if q.get("priority").is_some_and(|v|v.parse::<i64>().map_or(true,|v|!(0..=4).contains(&v))) {return error(400,"validation_error","invalid priority");}
            }
            if *kind=="plans" && q.get("status").is_some_and(|v|!v.is_empty() && !crate::domain::plan::valid_status(v)) {return error(400,"validation_error","invalid plan status");}
            let mut notes = match *kind {
                "ready" => index.ready(p,false),
                "plans" => index.project_plans(p),
                "requests" => index.project_requests(&RequestFilter { project:p.into(),status:q.get("status").cloned().unwrap_or_default().into_bytes(),label:q.get("label").cloned().unwrap_or_default().into_bytes(),priority:q.get("priority").and_then(|s|s.parse().ok()),query:q.get("q").cloned().unwrap_or_default().into_bytes(),terminal:q.get("terminal").is_some_and(|v|v=="true") }),
                _ => index.project_issues(p,q.get("archived").is_some_and(|v|v=="true")),
            };
            if *kind=="issues" && !q.get("archived").is_some_and(|v|v=="true") && q.get("status").is_none_or(|v|v.is_empty()) {
                notes.retain(|n| !matches!(&n.data,NoteData::Issue(d) if index.workflow_for(&n.project).is_terminal(d.metadata.status.as_bytes())));
            }
            json_response(notes.into_iter().map(|n|wire::note(&index,n,false)).filter(|v|["status","type"].iter().all(|k|q.get(*k).is_none_or(|expected|v[*k]==*expected)) && q.get("label").is_none_or(|l|v["labels"].as_array().is_some_and(|a|a.contains(&json!(l)))) && (*kind=="requests" || q.get("q").is_none_or(|s|v.to_string().to_lowercase().contains(&s.to_lowercase())))).collect::<Vec<_>>())
        }
        ["api",kind @ ("issues" | "requests" | "plans"),id] => {
            let kind = match *kind {"issues"=>NoteKind::Issue,"requests"=>NoteKind::Request,_=>NoteKind::Plan};
            match index.note_by_id(kind,id.as_bytes()) { Some(n)=>{
                let mut value=wire::note(&index,n,true);
                if q.get("include_archived_handoffs").is_some_and(|v|v=="true") {value["backlinks"]=wire::backlinks(&index,n,true);}
                json_response(value)
            },None=>error(404,"not_found","record not found") }
        }
        ["api","graph"] => {
            let graph = index.dependency_graph(if q.get("all").is_some_and(|v|v=="true") {b""} else {qp},false);
            json_response(json!({"nodes":graph.nodes.iter().map(|n|json!({"id":n.id,"title":n.title,"status":n.status,"priority":n.priority,"type":n.kind,"project":n.project,"archived":n.archived})).collect::<Vec<_>>(),"edges":graph.edges.iter().map(|e|json!({"from":e.from,"to":e.to,"kind":e.kind})).collect::<Vec<_>>()}))
        },
        ["api","search"] => json_response(index.search(q.get("q").map(String::as_bytes).unwrap_or_default(),&SearchOptions {kinds:q.get("kind").map(|s|s.split(',').map(Into::into).collect()).unwrap_or_default(),include_archived_handoffs:q.get("include_archived_handoffs").is_some_and(|v|v=="true")}).into_iter().filter(|h|qp.is_empty() || h.project.is_empty() || h.project.as_bytes()==qp).map(|h|json!({"kind":h.kind,"id":h.id,"basename":h.basename,"title":h.title,"project":h.project,"path":h.path,"score":h.score})).collect::<Vec<_>>()),
        ["api","docs","tree"] => json_response(json!({"docs":index.ordered_notes().iter().filter(|n|n.graph.kind==NoteKind::Doc && (qp.is_empty() || n.project.is_empty() || n.project==qp)).map(|n|json!({"path":String::from_utf8_lossy(&n.graph.path),"title":String::from_utf8_lossy(&n.title),"project":String::from_utf8_lossy(&n.project)})).collect::<Vec<_>>()})),
        ["api",kind @ ("docs" | "assets"),tail @ ..] => {
            let mut path = tail.join("/");
            if !files::valid(&path) { return error(400,"invalid_path","invalid public path"); }
            if *kind == "assets" {
                if !index.assets.contains(path.as_bytes()) { return error(404,"not_found","asset not found"); }
                return match files::read(&app.hub.dir,&path) {Ok(bytes)=>files::hub_asset_response(&path,bytes),Err(_)=>error(404,"not_found","asset unavailable")};
            }
            if !path.ends_with(".md") { path.push_str(".md"); }
            if let Some(note) = index.note_by_path(path.as_bytes()).filter(|n| matches!(n.graph.kind, NoteKind::Doc | NoteKind::Memory | NoteKind::Handoff)) {
                if files::read(&app.hub.dir,&path).is_err() { return error(404,"not_found","doc unavailable"); }
                json_response(wire::note(&index,note,true))
            } else { error(404,"not_found","doc not found") }
        }
        _ => error(404,"not_found","API route not found"),
    }
}
