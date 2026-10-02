use beans::domain::plan::YamlString;
use beans::vault::{Index, Note, NoteKind, RequestFilter, SearchOptions};
use serde_json::{Value, json};
fn bytes(v: &Value) -> Vec<u8> {
    serde_json::from_value(v.clone()).unwrap()
}
fn ids(notes: Vec<&Note>) -> Value {
    json!(
        notes
            .iter()
            .map(|n| String::from_utf8(n.graph.id.clone().unwrap()).unwrap())
            .collect::<Vec<_>>()
    )
}
fn links(v: Vec<beans::vault::LinkRef>) -> Value {
    json!(
        v.iter()
            .map(|l| json!({"from":l.from,"to":l.to,"kind":l.kind}))
            .collect::<Vec<_>>()
    )
}
fn compare(ix: &Index, expected: &Value) {
    assert!(ix.plan_execution(b"missing").is_none());
    for e in expected["executions"].as_array().unwrap() {
        let report = ix
            .plan_execution(e["id"].as_str().unwrap().as_bytes())
            .unwrap();
        assert_eq!(json!(report), e["report"], "execution {}", e["id"]);
        assert_eq!(
            json!(report),
            json!(
                ix.plan_execution(e["id"].as_str().unwrap().as_bytes())
                    .unwrap()
            )
        );
    }
    for e in expected["scopes"].as_array().unwrap() {
        let p = bytes(&e["project"]);
        let all = e["all"].as_bool().unwrap();
        assert_eq!(ids(ix.ready(&p, all)), e["ready"], "ready {p:?} {all}");
        let blocked:Vec<_>=ix.blocked(&p,all).iter().map(|b|json!({"id":String::from_utf8(b.issue.graph.id.clone().unwrap()).unwrap(),"blockers":b.blockers})).collect();
        assert_eq!(json!(blocked), e["blocked"], "blocked {p:?}");
        let graph = ix.dependency_graph(&p, all);
        assert_eq!(json!(graph.nodes), e["nodes"], "nodes {p:?}");
        assert_eq!(json!(graph.edges), e["edges"], "edges {p:?}");
    }
    for e in expected["projects"].as_array().unwrap() {
        let p = bytes(&e["project"]);
        let archived = e["archived"].as_bool().unwrap();
        assert_eq!(ids(ix.project_issues(&p, archived)), e["issues"]);
        assert_eq!(ids(ix.project_handoffs(&p, archived)), e["handoffs"]);
        assert_eq!(ids(ix.project_plans(&p)), e["plans"]);
    }
    for e in expected["refs"].as_array().unwrap() {
        let r = bytes(&e["ref"]);
        let (target, n) = ix.resolve_issue_ref(&r);
        assert_eq!(json!(target), e["target"]);
        assert_eq!(
            json!(
                n.map(|n| String::from_utf8(n.graph.id.clone().unwrap()).unwrap())
                    .unwrap_or_default()
            ),
            e["issue"]
        );
        assert_eq!(ids(ix.children(&r)), e["children"]);
        assert_eq!(ids(ix.parents(&r)), e["parents"]);
        assert_eq!(links(ix.issue_backlinks(&r, false)), e["backlinks"]);
        assert_eq!(links(ix.issue_backlinks(&r, true)), e["all_backlinks"]);
    }
    for e in expected["blockers"].as_array().unwrap() {
        let n = ix
            .note_by_id(NoteKind::Issue, e["id"].as_str().unwrap().as_bytes())
            .unwrap();
        let (resolved, missing) = ix.blockers(n);
        assert_eq!(ids(resolved), e["resolved"]);
        assert_eq!(json!(missing), e["unresolved"]);
    }
    for e in expected["searches"].as_array().unwrap() {
        let opts = SearchOptions {
            kinds: e["kinds"]
                .as_array()
                .map(|v| v.iter().map(|s| s.as_str().unwrap().into()).collect())
                .unwrap_or_default(),
            include_archived_handoffs: e["archived"].as_bool().unwrap(),
        };
        let hits:Vec<_>=ix.search(&bytes(&e["q"]),&opts).iter().map(|h|json!({"kind":h.kind,"id":h.id.as_bytes(),"basename":h.basename.as_bytes(),"title":h.title.as_bytes(),"project":h.project.as_bytes(),"path":h.path.as_bytes(),"score":h.score})).collect();
        assert_eq!(json!(hits), e["hits"], "search {:?}", e["q"]);
    }
    for e in expected["requests"].as_array().unwrap() {
        let f = &e["filter"];
        let filter = RequestFilter {
            project: f["Project"].as_str().unwrap().as_bytes().into(),
            status: f["Status"].as_str().unwrap().as_bytes().into(),
            label: f["Label"].as_str().unwrap().as_bytes().into(),
            priority: f["Priority"].as_i64(),
            query: f["Query"].as_str().unwrap().as_bytes().into(),
            terminal: f["Terminal"].as_bool().unwrap(),
        };
        assert_eq!(ids(ix.project_requests(&filter)), e["ids"], "request {f}");
    }
    assert_eq!(json!(ix.cycles()), expected["cycles"]);
}
#[test]
fn queries_match_committed_contract_on_loaded_files_and_reload() {
    let corpus: Value = serde_json::from_str(include_str!("../contract/query.json")).unwrap();
    for (i, case) in corpus["cases"].as_array().unwrap().iter().enumerate() {
        let hub = super::index::hub(&format!("query-{i}"), false);
        // The standalone fixture owns project configuration; clear the helper seed.
        std::fs::remove_dir_all(hub.0.join("projects")).unwrap();
        super::index::write(&hub.0, &case["initial"]);
        let mut ix = Index::load(&hub.0).unwrap();
        for (stage, expected) in case["stages"].as_array().unwrap().iter().enumerate() {
            if stage > 0 {
                super::index::write(&hub.0, &case["changes"]);
                let paths: Vec<_> = case["paths"]
                    .as_array()
                    .unwrap()
                    .iter()
                    .map(|p| std::path::PathBuf::from(p.as_str().unwrap()))
                    .collect();
                ix.reload(&paths).unwrap();
            }
            compare(&ix, expected);
        }
    }
}
#[test]
fn query_original_ready_excludes_blocked() {
    let root = super::index::hub("query-ready", true);
    let ix = Index::load(&root.0).unwrap();
    let ids = ids(ix.ready(b"b", false));
    assert!(!ids.as_array().unwrap().contains(&json!("b-blocked001")));
    assert!(ids.as_array().unwrap().contains(&json!("b-child001")));
}
#[test]
fn query_original_ready_excludes_epic_with_children() {
    let root = super::index::hub("query-epic", true);
    let ix = Index::load(&root.0).unwrap();
    assert!(
        !ids(ix.ready(b"a", false))
            .as_array()
            .unwrap()
            .contains(&json!("a-epic001"))
    );
}
#[test]
fn query_original_children_across_projects() {
    let root = super::index::hub("query-children", true);
    let ix = Index::load(&root.0).unwrap();
    assert_eq!(
        ids(ix.children(b"a-epic001")),
        json!(["a-child001", "b-child001"])
    );
}
#[test]
fn query_original_parents() {
    let root = super::index::hub("query-parents", true);
    let ix = Index::load(&root.0).unwrap();
    assert_eq!(ids(ix.parents(b"b-child001")), json!(["a-epic001"]));
}
#[test]
fn query_original_blockers() {
    let root = super::index::hub("query-blockers", true);
    let ix = Index::load(&root.0).unwrap();
    let (r, u) = ix.blockers(ix.note_by_id(NoteKind::Issue, b"b-blocked001").unwrap());
    assert_eq!(ids(r), json!(["a-open001"]));
    assert!(u.is_empty());
}
#[test]
fn query_original_blocked() {
    let root = super::index::hub("query-blocked", true);
    let ix = Index::load(&root.0).unwrap();
    let all = ix.blocked(b"", true);
    let found = all
        .iter()
        .find(|b| b.issue.graph.id.as_deref() == Some(b"b-blocked001"))
        .unwrap();
    assert_eq!(found.blockers, vec![YamlString::from("a-open001")]);
}
#[test]
fn query_original_graph_cross_project_edge() {
    let root = super::index::hub("query-graph", true);
    let ix = Index::load(&root.0).unwrap();
    let g = ix.dependency_graph(b"b", false);
    assert!(g.nodes.iter().any(|n| n.id.as_bytes() == b"a-open001"));
    assert!(g.edges.iter().any(|e| e.from.as_bytes() == b"b-blocked001"
        && e.to.as_bytes() == b"a-open001"
        && e.kind.as_bytes() == b"blocks"));
}
#[test]
fn query_original_cycles_empty_on_fixture() {
    let root = super::index::hub("query-no-cycle", true);
    assert!(Index::load(&root.0).unwrap().cycles().is_empty());
}
#[test]
fn query_original_cycles_detects_cross_project_cycle() {
    let root = super::index::hub("query-cycle", true);
    let p = root.0.join("projects/a/issues/a-open001.md");
    let s = std::fs::read_to_string(&p).unwrap();
    let edited = s.replacen(
        "updated: 2026-01-01T00:00:00Z\n---",
        "updated: 2026-01-01T00:00:00Z\nblocked_by:\n  - \"[[b-blocked001]]\"\n---",
        1,
    );
    assert_ne!(s, edited);
    std::fs::write(p, edited).unwrap();
    assert_eq!(
        Index::load(&root.0).unwrap().cycles(),
        vec![vec!["a-open001".to_string(), "b-blocked001".to_string()]]
    );
}
#[test]
fn query_original_search_parity() {
    let root = super::index::hub("query-search", true);
    let ix = Index::load(&root.0).unwrap();
    let hits = ix.search(b"parity", &SearchOptions::default());
    assert!(hits.len() >= 2);
    assert!(hits.iter().any(|h| h.basename.as_bytes() == b"parity"));
    assert!(hits.iter().any(|h| h.id.as_bytes() == b"a-open001"));
    assert!(hits.iter().all(|h| h.score >= 3));
}
#[test]
fn query_original_plan_section_search() {
    use beans::domain::{issue::Timestamp, plan};
    let root = super::index::hub("query-plan-search", true);
    let dir = root.0.join("projects/a/plans/a-plan-a123-test");
    std::fs::create_dir_all(dir.parent().unwrap()).unwrap();
    plan::write_scaffold(
        dir.to_str().unwrap(),
        "a-plan-a123",
        "test",
        &Timestamp {
            seconds: 1767225600,
            nanoseconds: 0,
            offset_seconds: 0,
        },
    )
    .unwrap();
    let p = dir.join("plan.md");
    let s = std::fs::read_to_string(&p).unwrap().replacen(
        "created:",
        "sections: [sections/one.md]\ncreated:",
        1,
    );
    std::fs::write(p, s).unwrap();
    std::fs::create_dir(dir.join("sections")).unwrap();
    std::fs::write(dir.join("sections/one.md"), b"needle-only-in-section\n").unwrap();
    let ix = Index::load(&root.0).unwrap();
    assert_eq!(ix.project_plans(b"a").len(), 1);
    let hits = ix.search(b"needle-only-in-section", &SearchOptions::default());
    assert_eq!(hits.len(), 1);
    assert_eq!(hits[0].kind, NoteKind::Plan);
    assert_eq!(hits[0].id.as_bytes(), b"a-plan-a123");
    assert_eq!(hits[0].score, 1);
}
#[test]
fn query_original_reload_flips_ready() {
    let root = super::index::hub("query-reload-ready", true);
    let mut ix = Index::load(&root.0).unwrap();
    assert!(
        !ids(ix.ready(b"b", false))
            .as_array()
            .unwrap()
            .contains(&json!("b-blocked001"))
    );
    let p = root.0.join("projects/a/issues/a-open001.md");
    let s = std::fs::read_to_string(&p).unwrap();
    let edited = s.replacen("status: open", "status: closed", 1);
    assert_ne!(s, edited);
    std::fs::write(&p, edited).unwrap();
    ix.reload(&[p]).unwrap();
    assert!(
        ids(ix.ready(b"b", false))
            .as_array()
            .unwrap()
            .contains(&json!("b-blocked001"))
    );
}
#[test]
fn query_original_handoffs_archived_opt_in() {
    let root = super::index::hub("query-handoffs", true);
    for (p, id, title, body) in [
        (
            "projects/a/handoffs/a-handoff1-live.md",
            "a-handoff1",
            "Live handoff",
            "unique live continuation",
        ),
        (
            "projects/a/handoffs/archive/2026/a-handoff2-old.md",
            "a-handoff2",
            "Archived handoff",
            "unique archived continuation",
        ),
    ] {
        let p = root.0.join(p);
        std::fs::create_dir_all(p.parent().unwrap()).unwrap();
        std::fs::write(p,format!("---\nid: {id}\ntitle: {title}\ncreated: 2026-09-10T00:00:00Z\nupdated: 2026-09-10T00:00:00Z\n---\n# {title}\n{body}\n")).unwrap();
    }
    let ix = Index::load(&root.0).unwrap();
    let beans::vault::NoteData::Handoff(h) = &ix
        .note_by_id(NoteKind::Handoff, b"a-handoff1")
        .unwrap()
        .data
    else {
        panic!()
    };
    assert!(!h.metadata.archived);
    assert!(
        ix.search(b"unique archived continuation", &SearchOptions::default())
            .is_empty()
    );
    let hits = ix.search(
        b"unique archived continuation",
        &SearchOptions {
            kinds: vec![],
            include_archived_handoffs: true,
        },
    );
    assert_eq!(hits.len(), 1);
    assert_eq!(hits[0].kind, NoteKind::Handoff);
    assert_eq!(ix.project_handoffs(b"a", false).len(), 1);
    assert_eq!(ix.project_handoffs(b"a", true).len(), 2);
}
#[test]
fn query_original_plan_execution_classifies_bindings_deterministically() {
    use beans::domain::{issue::Timestamp, plan};
    let root = super::index::hub("query-execution", true);
    let dir = root.0.join("projects/a/plans/a-plan-a1b2-execution");
    std::fs::create_dir_all(dir.parent().unwrap()).unwrap();
    plan::write_scaffold(
        dir.to_str().unwrap(),
        "a-plan-a1b2",
        "execution",
        &Timestamp {
            seconds: 1767225600,
            nanoseconds: 0,
            offset_seconds: 0,
        },
    )
    .unwrap();
    let p = dir.join("plan.md");
    let mut s = std::fs::read_to_string(&p)
        .unwrap()
        .replacen("status: draft", "status: ready", 1);
    for text in ["Outcome", "- Areas", "1. Execute", "- Risks"] {
        s = s.replacen("<!-- bn:todo -->", text, 1)
    }
    let mut nodes = "nodes:".to_string();
    for (id, r) in [
        ("none", ""),
        ("note", "README"),
        ("missing", "a-missing999"),
        ("open", "a-open001"),
        ("blocked", "b-blocked001"),
    ] {
        nodes.push_str(&format!(
            "\n  - id: {id}\n    label: {id}\n    kind: component"
        ));
        if !r.is_empty() {
            nodes.push_str(&format!("\n    ref: {r}"))
        }
    }
    s = s.replacen("nodes: []", &nodes, 1);
    std::fs::write(p, s).unwrap();
    let ix = Index::load(&root.0).unwrap();
    let a = ix.plan_execution(b"a-plan-a1b2").unwrap();
    let b = ix.plan_execution(b"a-plan-a1b2").unwrap();
    assert_eq!(
        serde_json::to_vec(&a).unwrap(),
        serde_json::to_vec(&b).unwrap()
    );
    assert_eq!(
        (
            a.counts.unlinked,
            a.counts.reference,
            a.counts.missing_issue,
            a.counts.runnable,
            a.counts.blocked
        ),
        (1, 1, 1, 1, 1)
    );
    assert_eq!(a.execution_state, "degraded");
    assert_eq!(a.nodes[4].binding, "issue");
    assert_eq!(a.nodes[4].work_state, "blocked");
    assert!(!a.nodes[4].blockers.as_ref().unwrap().is_empty());
    assert_eq!(a.lifecycle_status, "ready");
}
