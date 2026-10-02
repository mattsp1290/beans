//! Dynamic observations use the same production readers as the retained contracts.
use beans::domain::{
    frontmatter::Field, handoff::HandoffDocument, issue::IssueDocument, memory::MemoryDocument,
    plan, request::RequestDocument, workflow::decode_workflow_file,
};
use serde_json::{Value, json};
fn canonical(v: Value) -> Value {
    match v {
        Value::String(s) => json!(s.into_bytes()),
        Value::Array(a) => Value::Array(a.into_iter().map(canonical).collect()),
        Value::Object(m) => Value::Object(m.into_iter().map(|(k, v)| (k, canonical(v))).collect()),
        v => v,
    }
}
fn node(n: &beans::domain::frontmatter::Node) -> Value {
    let mut v = serde_json::to_value(n).unwrap();
    v["tag"] = json!(n.tag);
    v["anchor"] = json!(n.anchor_name);
    if !n.children.is_empty() {
        v["children"] = json!(n.children.iter().map(node).collect::<Vec<_>>())
    };
    v
}
fn states(s: &beans::domain::workflow::States) -> Value {
    match s {
        Some(v) => json!(v.iter().map(|s| s.as_bytes()).collect::<Vec<_>>()),
        None => Value::Null,
    }
}
fn workflow(w: &beans::domain::workflow::WorkflowFile) -> Value {
    json!({"statuses":states(&w.statuses),"default":w.default.as_bytes(),"active":states(&w.active),"terminal":states(&w.terminal),"transitions":w.transitions.as_ref().map(|m|m.iter().map(|(k,v)|json!({"key":k.as_bytes(),"value":states(v)})).collect::<Vec<_>>())})
}
fn unknown<'a>(fields: impl Iterator<Item = &'a Field>) -> Value {
    canonical(Value::Array(
        fields
            .map(|f| json!({"key":f.key,"value":node(&f.value)}))
            .collect(),
    ))
}
type Observation = Result<(Value, Option<Vec<u8>>), (&'static str, Vec<u8>)>;
fn graph_model(g: &plan::ChangeGraph) -> Value {
    let mut model = canonical(serde_json::to_value(g).unwrap());
    if let Some(nodes) = model["nodes"].as_array_mut() {
        for n in nodes {
            if n.get("ref").is_none() {
                n["ref"] = json!([])
            }
        }
    };
    if let Some(edges) = model["edges"].as_array_mut() {
        for e in edges {
            if e.get("label").is_none() {
                e["label"] = json!([])
            }
        }
    };
    model
}
fn graph_observe(path: &str, raw: &[u8]) -> Value {
    let (model, error) = match plan::parse_graph_raw(path.as_bytes(), raw) {
        Ok(g) => (graph_model(&g), Vec::new()),
        Err(e) => (graph_model(&e.graph), e.as_bytes().to_vec()),
    };
    json!({"model":model,"noop":null,"error":error,"parse_error":error,"encode_error":[]})
}
fn observe_once(c: &Value) -> Value {
    let raw: Vec<u8> = serde_json::from_value(c["Input"].clone()).unwrap();
    let path = c["Path"].as_str().unwrap();
    if c["Kind"] == "graph" {
        return graph_observe(path, &raw);
    }
    let result: Observation = (|| {
        macro_rules! doc {
            ($ty:ty, $extra:expr) => {{
                #[allow(unused_mut)]
                let mut d =
                    <$ty>::parse_bytes(path, &raw).map_err(|e| ("parse", e.as_bytes().to_vec()))?;
                let extra = $extra(&d);
                let mut m = canonical(serde_json::to_value(&d.metadata).unwrap());
                m.as_object_mut()
                    .unwrap()
                    .extend(extra.as_object().unwrap().clone());
                m["unknown"] = unknown(d.unknown_fields());
                m["path"] = json!(path.as_bytes());
                let out = d
                    .encode()
                    .map_err(|e| ("encode", e.as_bytes().to_vec()))?
                    .bytes;
                Ok((m, Some(out)))
            }};
        }
        match c["Kind"].as_str().unwrap() {
            "issue" => {
                let mut d = IssueDocument::parse_bytes(path, &raw)
                    .map_err(|e| ("parse", e.as_bytes().to_vec()))?;
                if c["Edit"] == true {
                    d.metadata.status = "in_progress".into();
                    let out = d.encode().map_err(|e| ("encode", e.as_bytes().to_vec()))?;
                    let old = b"status: open # retained\n";
                    let new = b"status: in_progress # retained\n";
                    let start = raw.windows(old.len()).position(|s| s == old).unwrap();
                    assert_eq!(&out.bytes[..start], &raw[..start]);
                    assert_eq!(&out.bytes[start + new.len()..], &raw[start + old.len()..]);
                    assert_eq!(&out.bytes[start..start + new.len()], new);
                    for copy in &out.copies {
                        assert_eq!(
                            &out.bytes[copy.destination.clone()],
                            &raw[copy.source.clone()]
                        );
                    }
                    d = IssueDocument::parse_bytes(path, &out.bytes)
                        .map_err(|e| ("parse", e.as_bytes().to_vec()))?;
                }
                let mut m = canonical(serde_json::to_value(&d.metadata).unwrap());
                m["description"] = json!(d.description.as_bytes());
                m["body"] = json!(d.body.as_bytes());
                m["log"] = canonical(serde_json::to_value(&d.log).unwrap());
                m["unknown"] = unknown(d.unknown_fields());
                m["path"] = json!(path.as_bytes());
                Ok((
                    m,
                    Some(
                        d.encode()
                            .map_err(|e| ("encode", e.as_bytes().to_vec()))?
                            .bytes,
                    ),
                ))
            }
            "request" => doc!(
                RequestDocument,
                |d: &RequestDocument| json!({"body":d.body.as_bytes(),"log":canonical(serde_json::to_value(&d.log).unwrap())})
            ),
            "memory" => doc!(
                MemoryDocument,
                |d: &MemoryDocument| json!({"body":d.body.as_bytes()})
            ),
            "handoff" => doc!(
                HandoffDocument,
                |d: &HandoffDocument| json!({"body":d.body.as_bytes()})
            ),
            "manifest" => {
                let d = plan::parse(path, &raw).map_err(|e| ("parse", e.as_bytes().to_vec()))?;
                let mut model = canonical(serde_json::to_value(&d).unwrap());
                model["aliases"] =
                    json!(d.aliases.iter().map(|s| s.as_bytes()).collect::<Vec<_>>());
                model["sections"] =
                    json!(d.sections.iter().map(|s| s.as_bytes()).collect::<Vec<_>>());
                model["path"] = json!(d.path.as_bytes());
                model["graph"] = graph_model(&d.graph);
                Ok((
                    model,
                    Some(plan::encode(Some(&d)).map_err(|e| ("encode", e.as_bytes().to_vec()))?),
                ))
            }
            "workflow" => {
                let d = decode_workflow_file(path, &raw)
                    .map_err(|e| ("parse", e.as_bytes().to_vec()))?;
                Ok((workflow(&d), None))
            }
            _ => unreachable!(),
        }
    })();
    match result {
        Ok((model, noop)) => {
            json!({"model":model,"noop":noop,"error":[],"parse_error":[],"encode_error":[]})
        }
        Err((stage, e)) => {
            let mut r =
                json!({"model":null,"noop":null,"error":e,"parse_error":[],"encode_error":[]});
            r[format!("{stage}_error")] = json!(e);
            r
        }
    }
}
#[test]
fn yaml_upstream_observations() {
    let Some(input) = std::env::var_os("BN_YAML_CORPUS") else {
        return;
    };
    let corpus: Value = serde_json::from_slice(&std::fs::read(input).unwrap()).unwrap();
    let mut rows = serde_json::Map::new();
    for c in corpus["cases"].as_array().unwrap() {
        rows.insert(c["ID"].as_str().unwrap().into(), observe(c));
    }
    std::fs::write(
        std::env::var_os("BN_YAML_RUST_OUTPUT").unwrap(),
        serde_json::to_vec(&rows).unwrap(),
    )
    .unwrap();
}

fn observe(c: &Value) -> Value {
    if c["EditField"].is_string() {
        return observe_edit(c);
    }

    let mut r = observe_once(c);
    if c["Edit"] == true {
        let mut initial = c.clone();
        initial["Edit"] = json!(false);
        r["initial_read"] = observe_once(&initial)
    }
    if r["noop"].is_array() {
        let mut reread = c.clone();
        reread["Input"] = r["noop"].clone();
        reread["Edit"] = json!(false);
        r["reread"] = observe_once(&reread)
    } else {
        r["reread"] = Value::Null
    };
    r
}

#[test]
fn anchored_unknown_issue_status_edit_preserves_semantics_and_all_other_bytes() {
    let c = json!({"Kind":"issue","Path":"projects/p/issues/p-one.md","Edit":true,"Input":b"---\nid: p-one\ntitle: Title\ntype: task\nstatus: open # retained\npriority: 2\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\ncustom: &settings {nested: [a, b]}\ncustom_alias: *settings\n# untouched\n---\nBody\n".to_vec()});
    let actual = observe(&c);
    assert_eq!(actual["initial_read"]["parse_error"], json!([]));
    assert_eq!(
        actual["initial_read"]["model"]["status"],
        json!(b"open".to_vec())
    );
    assert_eq!(actual["parse_error"], json!([]));
    assert_eq!(actual["encode_error"], json!([]));
    assert_eq!(actual["reread"]["parse_error"], json!([]));
    assert_eq!(actual["model"], actual["reread"]["model"]);
    assert_eq!(actual["noop"], actual["reread"]["noop"]);
    assert_eq!(actual["model"]["status"], json!(b"in_progress".to_vec()));
    assert_eq!(actual["model"]["unknown"].as_array().unwrap().len(), 2);
}

fn observe_edit(c: &Value) -> Value {
    let initial = observe_once(c);
    let mut result = json!({"initial_read":initial,"edit_bytes":null,"edit_error":[],"reread":null,"owned_range":null,"unchanged_ranges":null,"unknown_ranges":null});
    let raw: Vec<u8> = serde_json::from_value(c["Input"].clone()).unwrap();
    let path = c["Path"].as_str().unwrap();
    let Ok(mut d) = IssueDocument::parse_bytes(path, &raw) else {
        return result;
    };
    let fm = beans::domain::frontmatter::Frontmatter::parse_bytes(path, &raw).unwrap();
    let (start, end) = if c["EditField"] == "status" {
        let field = fm
            .fields()
            .iter()
            .find(|field| field.key == "status")
            .unwrap();
        (field.start, field.end)
    } else {
        let start = raw.len() - fm.body_bytes().len();
        (start, start + d.description.as_bytes().len())
    };
    let unknown_ranges: Vec<_> = d
        .unknown_fields()
        .map(|field| [field.start, field.end])
        .collect();
    result["owned_range"] = json!([start, end]);
    result["unknown_ranges"] = json!(unknown_ranges);
    if c["EditField"] == "status" {
        d.metadata.status = "in_progress".into();
    } else {
        d.set_description("Upstream qualification replacement.\nSecond line.");
    }
    match d.encode() {
        Err(e) => result["edit_error"] = json!(e.as_bytes()),
        Ok(output) => {
            for copy in &output.copies {
                assert_eq!(
                    &output.bytes[copy.destination.clone()],
                    &raw[copy.source.clone()]
                );
            }
            let replacement_end = output.bytes.len() - (raw.len() - end);
            let ranges = [
                [0, start, 0, start],
                [end, raw.len(), replacement_end, output.bytes.len()],
            ];
            result["unchanged_ranges"] = json!(ranges);
            if IssueDocument::parse_bytes(path, &output.bytes).is_ok() {
                for [from, to, dest, dest_end] in ranges {
                    assert_eq!(&raw[from..to], &output.bytes[dest..dest_end]);
                }
                for [from, to] in unknown_ranges {
                    assert!(to <= start || from >= end);
                }
            }
            let mut reread = c.clone();
            reread["Input"] = json!(output.bytes);
            result["edit_bytes"] = reread["Input"].clone();
            result["reread"] = observe_once(&reread);
        }
    }
    result
}

#[test]
fn yaml_upstream_shrunk_scanner_reader_and_edit_regressions_match_immutable_go() {
    let corpus: Value =
        serde_json::from_str(include_str!("../contract/yaml-upstream.json")).unwrap();
    let mut count = 0;
    for c in corpus["cases"].as_array().unwrap() {
        if c["ID"].as_str().unwrap().starts_with("regression/") {
            assert_eq!(observe(c), c["expected"], "{}", c["ID"]);
            count += 1;
        }
    }
    assert_eq!(count, 25);
    let invalid = corpus["cases"]
        .as_array()
        .unwrap()
        .iter()
        .find(|c| c["ID"] == "regression/owned-status-anchor/issue/retained-alias/edit-status")
        .unwrap();
    assert_eq!(invalid["expected"]["edit_error"], json!([]));
    assert!(
        !invalid["expected"]["reread"]["parse_error"]
            .as_array()
            .unwrap()
            .is_empty()
    );
    assert!(invalid["expected"]["edit_bytes"].is_array());
}
