use beans::domain::plan::{Plan, set_node_ref};
use serde_json::Value;
fn view(p: Option<&Plan>) -> Value {
    p.map_or(Value::Null,|p|serde_json::json!({"path":p.path,"status":p.status,"body":p.body,"summary":p.summary,"graph":p.graph}))
}
#[test]
fn reference_edits_match_go_bytes_errors_and_failure_state() {
    let fixture: Value = serde_json::from_str(include_str!("../contract/plan-ref.json")).unwrap();
    let mut mismatches = Vec::new();
    let mut exports = Vec::new();
    for row in fixture["refs"].as_array().unwrap() {
        let mut p: Option<Plan> = serde_json::from_value(row["plan"].clone()).unwrap();
        let before = p.as_ref().map(|p| p.body.clone());
        let result = set_node_ref(
            p.as_mut(),
            row["id"].as_str().unwrap(),
            row["ref"].as_str().unwrap(),
        );
        let error = result
            .as_ref()
            .err()
            .map(ToString::to_string)
            .unwrap_or_default();
        if error != row["error"] || view(p.as_ref()) != row["after"] {
            mismatches.push(format!(
                "{}: {error:?} {}; expected {} {}",
                row["name"],
                view(p.as_ref()),
                row["error"],
                row["after"]
            ));
        }
        if let Ok(edit) = result {
            let p = p.as_ref().unwrap();
            let now = beans::domain::issue::Timestamp {
                seconds: 1789128000,
                nanoseconds: 0,
                offset_seconds: 0,
            };
            let bytes = beans::domain::plan::scaffold("beans-plan-a3f2", "x", &now).unwrap();
            let mut complete = beans::domain::plan::parse("plan.md", &bytes).unwrap();
            complete.body = p.body.clone();
            complete.status = p.status.clone();
            let encoded = beans::domain::plan::encode(Some(&complete)).unwrap();
            let parsed = beans::domain::plan::parse("plan.md", &encoded).unwrap();
            assert_eq!(view(Some(&parsed)), view(Some(p)));
            exports.push(
                serde_json::json!({"name":row["name"],"input":String::from_utf8(encoded).unwrap()}),
            );
            let source = before.unwrap();
            for copy in edit.copies {
                assert_eq!(
                    &source.as_bytes()[copy.source],
                    &edit.bytes[copy.destination]
                );
            }
        }
    }
    if let Ok(path) = std::env::var("BN_PLAN_REF_RUST_OUTPUT") {
        std::fs::write(path, serde_json::to_vec_pretty(&exports).unwrap()).unwrap();
    }
    assert!(mismatches.is_empty(), "{}", mismatches.join("\n"));
}

#[test]
fn set_node_ref_preserves_plan_and_canonicalizes_fence() {
    // Original plan/plan_test.go TestSetNodeRefPreservesPlanAndCanonicalizesFence.
    let now = beans::domain::issue::Timestamp {
        seconds: 1789128000,
        nanoseconds: 0,
        offset_seconds: 0,
    };
    let bytes = beans::domain::plan::scaffold("beans-plan-a3f2", "x", &now).unwrap();
    let data = String::from_utf8(bytes).unwrap().replacen(
        "nodes: []",
        "nodes:\n  - id: model\n    label: Model\n    kind: component",
        1,
    );
    let mut p = beans::domain::plan::parse("plan.md", data.as_bytes()).unwrap();
    let outcome = p.summary.outcome.clone();
    set_node_ref(Some(&mut p), "model", "[[beans-a1b2|work]]").unwrap();
    assert_eq!(
        p.graph.nodes.as_ref().unwrap()[0].reference,
        "[[beans-a1b2|work]]"
    );
    assert_eq!(p.summary.outcome, outcome);
    let encoded = beans::domain::plan::encode(Some(&p)).unwrap();
    beans::domain::plan::parse("plan.md", &encoded).unwrap();
    assert!(set_node_ref(Some(&mut p), "missing", "x").is_err());
}

#[test]
fn unicode_prose_survives_production_graph_splices() {
    let fixture: Value = serde_json::from_str(include_str!("../contract/plan-ref.json")).unwrap();
    let original: Plan = serde_json::from_value(fixture["refs"][2]["plan"].clone()).unwrap();
    for i in 0..512 {
        let mut p = original.clone();
        let prefix = format!("雪🌱 café {}\n", "🦀".repeat(i));
        let suffix = format!("\n## Notes\n{}\n", "αβ雪".repeat(511 - i));
        p.body = format!("{prefix}{}{suffix}", p.body);
        let before = p.body.clone();
        let start = before.find("```bn-change-graph\n").unwrap() + "```bn-change-graph\n".len();
        let end = start + before[start..].find("\n```").unwrap();
        let result = set_node_ref(Some(&mut p), "model", "[[beans-a1b2|雪🌱]]").unwrap();
        assert_eq!(&p.body[..start], &before[..start]);
        assert!(p.body.ends_with(&before[end..]));
        assert_eq!(result.copies.len(), 2);
        for copy in result.copies {
            assert_eq!(
                &before.as_bytes()[copy.source],
                &result.bytes[copy.destination]
            );
        }
        let canonical = p.body.clone();
        set_node_ref(Some(&mut p), "model", "[[beans-a1b2|雪🌱]]").unwrap();
        assert_eq!(p.body, canonical);
    }
}
