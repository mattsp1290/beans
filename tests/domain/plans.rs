use beans::domain::plan::{self, ChangeGraph, GraphNode, Plan, Summary, ValidationError};
use serde_json::Value;

// Independent port of plan/plan_test.go TestReadyRequiresStructuredSummary.
#[test]
fn ready_requires_structured_summary() {
    let mut p = Plan {
        status: plan::READY.into(),
        summary: Summary {
            outcome: "outcome".into(),
            affected_areas: "- area".into(),
            execution_order: "1. step".into(),
            risks: "- None.".into(),
            ..Summary::default()
        },
        graph: ChangeGraph {
            version: 1,
            nodes: Some(vec![GraphNode {
                id: "model".into(),
                label: "Model".into(),
                kind: "component".into(),
                ..GraphNode::default()
            }]),
            ..ChangeGraph::default()
        },
        ..Plan::default()
    };
    assert!(plan::validate(Some(&p)).is_ok());
    p.summary.execution_order = "prose".into();
    assert!(plan::validate(Some(&p)).is_err());
}

fn corpus() -> Value {
    serde_json::from_str(include_str!("../contract/plan-foundation.json")).unwrap()
}

#[test]
fn lifecycle_matches_fixed_go_errors_and_precedence() {
    for case in corpus()["lifecycle"].as_array().unwrap() {
        let mut p = Plan::default();
        if case.get("nil").is_none() {
            p.status = case["status"].as_str().unwrap().into();
            assert_eq!(
                plan::valid_status(&p.status),
                case["valid_status"].as_bool().unwrap()
            );
            p.summary = Summary {
                outcome: case["outcome"].as_str().unwrap().into(),
                affected_areas: case["areas"].as_str().unwrap().into(),
                execution_order: case["order"].as_str().unwrap().into(),
                risks: case["risks"].as_str().unwrap().into(),
                ..Summary::default()
            };
            let count = case["nodes"].as_u64().unwrap() as usize;
            if count > 0 {
                p.graph.nodes = Some(vec![GraphNode::default(); count]);
            }
        }
        let result = plan::validate(if case.get("nil").is_some() {
            None
        } else {
            Some(&p)
        });
        assert_eq!(
            result.err().map(|e| e.to_string()).unwrap_or_default(),
            case["error"].as_str().unwrap(),
            "{case}"
        );
    }
}

#[test]
fn identifiers_slugs_and_collision_traces_match_fixed_go() {
    let corpus = corpus();
    for case in corpus["ids"].as_array().unwrap() {
        assert_eq!(
            plan::id::valid_id(
                case["prefix"].as_str().unwrap(),
                case["id"].as_str().unwrap()
            ),
            case["valid"].as_bool().unwrap(),
            "{case}"
        );
    }
    for case in corpus["slugs"].as_array().unwrap() {
        let slug = plan::id::slug(case["title"].as_str().unwrap());
        assert_eq!(slug, case["slug"].as_str().unwrap());
        assert_eq!(
            plan::id::valid_slug(&slug),
            case["valid"].as_bool().unwrap()
        );
        assert_eq!(
            plan::id::directory_name("beans-plan-abc", &slug),
            case["directory"].as_str().unwrap()
        );
    }
    for case in corpus["slug_grammar"].as_array().unwrap() {
        assert_eq!(
            plan::id::valid_slug(case["slug"].as_str().unwrap()),
            case["valid"].as_bool().unwrap()
        );
    }
    for case in corpus["generation"].as_array().unwrap() {
        let prefix = case["prefix"].as_str().unwrap();
        let collisions = case["collisions"].as_u64().unwrap() as usize;
        let mut attempts = Vec::new();
        let mut exists = |id: &str| {
            assert!(id.starts_with(&format!("{prefix}-plan-")));
            attempts.push(id.len() - prefix.len() - 6);
            attempts.len() <= collisions
        };
        let id = plan::id::new_id(
            prefix,
            Some(&mut exists),
            case["length"].as_i64().unwrap() as isize,
        );
        assert_eq!(serde_json::to_value(attempts).unwrap(), case["attempts"]);
        assert_eq!(
            plan::id::valid_id(prefix, &id),
            case["valid"].as_bool().unwrap()
        );
    }
}

#[test]
fn graph_and_validation_models_match_go_serialization() {
    let corpus = corpus();
    for case in corpus["graph_models"].as_array().unwrap() {
        let graph: ChangeGraph = serde_json::from_value(case.clone()).unwrap();
        assert_eq!(serde_json::to_value(graph).unwrap(), *case);
    }
    for case in corpus["validation_errors"].as_array().unwrap() {
        let issues = if case["issues"].is_null() {
            Vec::new()
        } else {
            serde_json::from_value(case["issues"].clone()).unwrap()
        };
        let e = ValidationError { issues };
        assert_eq!(e.to_string(), case["error"].as_str().unwrap());
        if !case["issues"].is_null() {
            assert_eq!(serde_json::to_value(e.issues).unwrap(), case["issues"]);
        }
    }
}
