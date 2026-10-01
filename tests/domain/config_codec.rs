use beans::domain::{
    config::{
        ProjectConfig, UserConfig, UserFetchConfig, UserHubConfig, encode_project_config,
        encode_user_config,
    },
    plan::YamlString,
    workflow::{States, WorkflowFile},
};
use serde::Deserialize;
use serde_json::{Value, json};

fn bytes(value: &Value) -> YamlString {
    YamlString::from_bytes(serde_json::from_value(value.clone()).unwrap())
}
fn states(value: &Value) -> States {
    serde_json::from_value::<Option<Vec<Vec<u8>>>>(value.clone())
        .unwrap()
        .map(|v| v.into_iter().map(YamlString::from_bytes).collect())
}
#[derive(Deserialize)]
struct Transition {
    from: Vec<u8>,
    to: Option<Vec<Vec<u8>>>,
}
fn workflow(value: &Value) -> WorkflowFile {
    WorkflowFile {
        statuses: states(&value["statuses"]),
        default: bytes(&value["default"]),
        active: states(&value["active"]),
        terminal: states(&value["terminal"]),
        transitions: serde_json::from_value::<Option<Vec<Transition>>>(
            value["transitions"].clone(),
        )
        .unwrap()
        .map(|rows| {
            rows.into_iter()
                .map(|r| {
                    (
                        YamlString::from_bytes(r.from),
                        r.to.map(|v| v.into_iter().map(YamlString::from_bytes).collect()),
                    )
                })
                .collect()
        }),
    }
}

#[test]
fn configuration_output_matches_go_bytes() {
    let corpus: Value =
        serde_json::from_str(include_str!("../contract/config-codec.json")).unwrap();
    let mut exports = Vec::new();
    let rows = corpus["encodes"].as_array().unwrap();
    assert_eq!(rows.len(), 350);
    for row in rows {
        let input = &row["config"];
        let output = match row["kind"].as_str().unwrap() {
            "user" => {
                let cfg = UserConfig {
                    actor: bytes(&input["actor"]),
                    hub: UserHubConfig {
                        remote: bytes(&input["hub"]["remote"]),
                        branch: bytes(&input["hub"]["branch"]),
                    },
                    fetch: UserFetchConfig {
                        throttle: bytes(&input["fetch"]["throttle"]),
                    },
                };
                let before = cfg.clone();
                let output = encode_user_config(&cfg);
                assert_eq!(cfg, before);
                output
            }
            "project" => {
                let cfg = ProjectConfig {
                    name: bytes(&input["name"]),
                    prefix: bytes(&input["prefix"]),
                    remotes: states(&input["remotes"]),
                    workflow: workflow(&input["workflow"]),
                };
                let before = cfg.clone();
                let output = encode_project_config(&cfg);
                assert_eq!(cfg, before);
                output
            }
            other => panic!("unexpected config kind {other}"),
        };
        let expected: Vec<u8> = serde_json::from_value(row["output"].clone()).unwrap();
        assert_eq!(output, expected, "{}", row["name"]);
        assert_eq!(row["error"], "");
        exports.push(json!({"name":row["name"], "kind":row["kind"], "bytes":output}));
    }
    if let Ok(path) = std::env::var("BN_CONFIG_CODEC_RUST_OUTPUT") {
        std::fs::write(path, serde_json::to_vec_pretty(&exports).unwrap()).unwrap();
    }
}

#[test]
fn encode_project_config_without_workflow() {
    // Original issue/config_test.go TestEncodeProjectConfigWithoutWorkflow.
    let cfg = ProjectConfig {
        name: "p".into(),
        prefix: "p".into(),
        ..ProjectConfig::default()
    };
    let output = encode_project_config(&cfg);
    assert!(
        !output
            .windows(b"[workflow]".len())
            .any(|w| w == b"[workflow]")
    );
    assert_eq!(output, b"name = \"p\"\nprefix = \"p\"\nremotes = []\n");
    assert_eq!(cfg.remotes, None);
}
