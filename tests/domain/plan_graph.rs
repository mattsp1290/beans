use beans::domain::plan::{parse_graph, parse_summary};

#[test]
fn graph_and_summary_match_committed_contract_results_errors_and_partial_values() {
    let corpus: serde_json::Value =
        serde_json::from_str(include_str!("../contract/plan-graph.json")).unwrap();
    let mut failures = Vec::new();
    for case in corpus["graphs"].as_array().unwrap() {
        let (graph, error) = match parse_graph("plan.md", case["input"].as_str().unwrap()) {
            Ok(graph) => (graph, String::new()),
            Err(error) => (error.graph.clone(), error.to_string()),
        };
        if serde_json::to_value(graph).unwrap() != case["graph"]
            || error != case["error"].as_str().unwrap()
        {
            failures.push(format!(
                "{}: {error:?}; expected {:?}",
                case["name"], case["error"]
            ));
        }
    }
    for case in corpus["summaries"].as_array().unwrap() {
        match parse_summary("plan.md", case["input"].as_str().unwrap()) {
            Ok((summary, graph)) => {
                assert_eq!(serde_json::to_value(summary).unwrap(), case["summary"]);
                assert_eq!(serde_json::to_value(graph).unwrap(), case["graph"]);
                assert_eq!(case["error"], "");
            }
            Err(error) => {
                if error.to_string() != case["error"].as_str().unwrap() {
                    failures.push(format!(
                        "{}: {error:?}; expected {:?}",
                        case["name"], case["error"]
                    ));
                }
            }
        }
    }
    assert!(failures.is_empty(), "{}", failures.join("\n"));
}

#[test]
fn summary_ignores_headings_in_fences() {
    let body = "## Summary\n\n### Outcome\n```text\n## not a section\n```\n\n### Affected areas\n- x\n\n### Execution order\n1. x\n\n### Risks\n- none\n\n### Change graph\n```bn-change-graph\nversion: 1\nnodes: []\nedges: []\n```\n\n## Next\n";
    assert!(parse_summary("plan.md", body).is_ok());
}
#[test]
fn graph_rejects_unknown_fields() {
    assert!(parse_graph("plan.md","```bn-change-graph\nversion: 1\nnodes:\n  - id: model\n    label: Model\n    kind: component\n    color: red\nedges: []\n```").is_err());
}
#[test]
fn graph_rejects_alias_and_custom_tag() {
    for text in [
        "```bn-change-graph\nversion: 1\nnodes: &nodes []\nedges: *nodes\n```",
        "```bn-change-graph\nversion: !beans 1\nnodes: []\nedges: []\n```",
    ] {
        assert!(parse_graph("plan.md", text).is_err());
    }
}
