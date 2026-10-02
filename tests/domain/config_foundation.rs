use beans::domain::{
    config::{DEFAULT_FETCH_THROTTLE, TypesConfig, UserConfig},
    duration::parse_duration,
    plan::YamlString,
    workflow::{States, WorkflowConfig, WorkflowFile},
};
use proptest::prelude::*;
use serde::Deserialize;
use serde_json::{Value, json};
use std::collections::BTreeMap;

#[derive(Deserialize)]
struct Input {
    statuses: Option<Vec<Vec<u8>>>,
    default: Vec<u8>,
    active: Option<Vec<Vec<u8>>>,
    terminal: Option<Vec<Vec<u8>>>,
    transitions: Option<Vec<Transition>>,
}
#[derive(Deserialize)]
struct Transition {
    from: Vec<u8>,
    to: Option<Vec<Vec<u8>>>,
}
fn states(values: Option<Vec<Vec<u8>>>) -> States {
    values.map(|xs| xs.into_iter().map(YamlString::from_bytes).collect())
}
fn input(value: &Value) -> WorkflowConfig {
    let w: Input = serde_json::from_value(value.clone()).unwrap();
    WorkflowConfig {
        statuses: states(w.statuses),
        default: YamlString::from_bytes(w.default),
        active: states(w.active),
        terminal: states(w.terminal),
        transitions: w.transitions.map(|rows| {
            rows.into_iter()
                .map(|r| (YamlString::from_bytes(r.from), states(r.to)))
                .collect()
        }),
    }
}
fn corpus() -> Value {
    serde_json::from_str(include_str!("../contract/config-foundation.json")).unwrap()
}
#[test]
fn workflow_validation_classification_and_merges_match_immutable_go() {
    let corpus = corpus();
    for row in corpus["workflows"].as_array().unwrap() {
        let w = input(&row["input"]);
        assert_eq!(
            serde_json::to_value(&w).unwrap(),
            row["config"],
            "{}",
            row["name"]
        );
        assert_eq!(
            w.validate()
                .err()
                .map(|e| e.to_string())
                .unwrap_or_default(),
            row["error"],
            "{}",
            row["name"]
        );
        assert_eq!(
            serde_json::to_value(w.default_state()).unwrap(),
            row["default_state"]
        );
        assert_eq!(
            serde_json::to_value(w.status_names()).unwrap(),
            row["status_names"]
        );
        for class in row["classes"].as_array().unwrap() {
            let status: Vec<u8> = serde_json::from_value(class["status"].clone()).unwrap();
            assert_eq!(
                json!({"valid":w.is_valid(&status),"active":w.is_active(&status),"terminal":w.is_terminal(&status),"hold":w.is_hold(&status)}),
                json!({"valid":class["valid"],"active":class["active"],"terminal":class["terminal"],"hold":class["hold"]}),
                "{} {:?}",
                row["name"],
                status
            );
        }
    }
    for row in corpus["merges"].as_array().unwrap() {
        let base = input(&row["base"]);
        let w = input(&row["file"]);
        let file = WorkflowFile {
            statuses: w.statuses,
            default: w.default,
            active: w.active,
            terminal: w.terminal,
            transitions: w.transitions,
        };
        let before = base.clone();
        let file_before = file.clone();
        let result = base.merge(&file);
        assert_eq!(file.is_empty(), row["empty"].as_bool().unwrap());
        assert_eq!(
            serde_json::to_value(&result).unwrap(),
            row["result"],
            "{}",
            row["name"]
        );
        assert_eq!(
            result
                .validate()
                .err()
                .map(|e| e.to_string())
                .unwrap_or_default(),
            row["error"],
            "{}",
            row["name"]
        );
        assert_eq!(base, before);
        assert_eq!(file, file_before);
    }
}
#[test]
fn types_duration_and_throttle_match_go_including_raw_bytes() {
    let corpus = corpus();
    for row in corpus["types"].as_array().unwrap() {
        let names: Option<Vec<Vec<u8>>> = serde_json::from_value(row["names"].clone()).unwrap();
        let types = TypesConfig {
            names: states(names),
        };
        for check in row["checks"].as_array().unwrap() {
            let name: Vec<u8> = serde_json::from_value(check["name"].clone()).unwrap();
            assert_eq!(types.valid_type(&name), check["valid"].as_bool().unwrap());
        }
    }
    for row in corpus["durations"].as_array().unwrap() {
        let bytes: Vec<u8> = serde_json::from_value(row["input"].clone()).unwrap();
        let (value, error) = match parse_duration(&bytes) {
            Ok(d) => (d, String::new()),
            Err(e) => (0, e.to_string()),
        };
        assert_eq!(value, row["nanoseconds"], "{}", row["name"]);
        assert_eq!(error, row["error"], "{}", row["name"]);
        let mut cfg = UserConfig::default();
        cfg.fetch.throttle = YamlString::from_bytes(bytes);
        assert_eq!(cfg.throttle_duration(), row["throttle"], "{}", row["name"]);
    }
}

#[test]
fn default_workflow_config_buckets() {
    // Original issue/workflow_test.go TestDefaultWorkflowConfigBuckets.
    let w = WorkflowConfig::built_in();
    let expected = [
        "open",
        "in_progress",
        "ready_for_review",
        "ready_for_validation",
        "ready_for_merge",
        "blocked",
        "closed",
        "done",
    ];
    assert_eq!(
        w.statuses.as_ref().unwrap(),
        &expected.map(YamlString::from)
    );
    assert_eq!(w.default_state(), YamlString::from("open"));
    for state in [
        "ready_for_review",
        "ready_for_validation",
        "ready_for_merge",
    ] {
        assert!(w.is_valid(state.as_bytes()));
        assert!(!w.is_active(state.as_bytes()));
        assert!(!w.is_terminal(state.as_bytes()));
        assert!(w.is_hold(state.as_bytes()));
    }
    assert!(w.is_active(b"open"));
    assert!(!w.is_hold(b"open"));
    for state in [b"closed".as_slice(), b"done".as_slice()] {
        assert!(w.is_terminal(state));
        assert!(!w.is_hold(state));
    }
}
#[test]
fn workflow_config_unknown_status_not_hold() {
    // Original TestWorkflowConfigUnknownStatusNotHold.
    let w = WorkflowConfig::built_in();
    assert!(!w.is_valid(b"mystery"));
    assert!(!w.is_active(b"mystery"));
    assert!(!w.is_terminal(b"mystery"));
    assert!(!w.is_hold(b"mystery"));
}
#[test]
fn workflow_config_default_state_fallback() {
    assert_eq!(
        WorkflowConfig::default().default_state(),
        YamlString::from("open")
    );
}
#[test]
fn workflow_config_status_names() {
    // Original TestWorkflowConfigStatusNames, with independent-copy assertion.
    let w = WorkflowConfig::built_in();
    let expected = [
        "open",
        "in_progress",
        "ready_for_review",
        "ready_for_validation",
        "ready_for_merge",
        "blocked",
        "closed",
        "done",
    ]
    .map(YamlString::from);
    let mut names = w.status_names().unwrap();
    assert_eq!(names, expected);
    names[0] = "changed".into();
    assert_eq!(w.statuses.as_ref().unwrap()[0], YamlString::from("open"));
}
fn named(values: &[&str]) -> States {
    Some(values.iter().map(|v| (*v).into()).collect())
}
#[test]
fn workflow_config_validate() {
    // All original TestWorkflowConfigValidate table cases, independent of corpus.
    let cfg = |statuses: &[&str], default: &str| WorkflowConfig {
        statuses: named(statuses),
        default: default.into(),
        ..WorkflowConfig::default()
    };
    let cases = [
        (WorkflowConfig::built_in(), false),
        (
            WorkflowConfig {
                default: "open".into(),
                ..WorkflowConfig::default()
            },
            true,
        ),
        (cfg(&["open"], "closed"), true),
        (
            WorkflowConfig {
                active: named(&["weird"]),
                ..cfg(&["open"], "open")
            },
            true,
        ),
        (
            WorkflowConfig {
                terminal: named(&["weird"]),
                ..cfg(&["open"], "open")
            },
            true,
        ),
        (
            WorkflowConfig {
                active: named(&["open", "closed"]),
                terminal: named(&["closed"]),
                ..cfg(&["open", "closed"], "open")
            },
            true,
        ),
        (cfg(&["open", "open"], "open"), true),
        (
            WorkflowConfig {
                transitions: Some(BTreeMap::from([("open".into(), named(&["ghost"]))])),
                ..cfg(&["open", "closed"], "open")
            },
            true,
        ),
        (
            WorkflowConfig {
                transitions: Some(BTreeMap::from([("ghost".into(), named(&["open"]))])),
                ..cfg(&["open", "closed"], "open")
            },
            true,
        ),
        (
            WorkflowConfig {
                active: named(&["open"]),
                terminal: named(&["closed"]),
                ..cfg(&["open", "qa", "closed"], "open")
            },
            false,
        ),
    ];
    for (w, error) in cases {
        assert_eq!(w.validate().is_err(), error, "{w:?}");
    }
}
#[test]
fn types_config_valid_type() {
    // Original issue/types_test.go TestTypesConfigValidType.
    let defaults = TypesConfig::built_in();
    for kind in ["task", "bug", "feature", "epic", "chore"] {
        assert!(defaults.valid_type(kind.as_bytes()));
    }
    assert!(!defaults.valid_type(b"story"));
    let custom = TypesConfig {
        names: named(&["story"]),
    };
    assert!(custom.valid_type(b"story"));
    assert!(!custom.valid_type(b"task"));
    assert!(TypesConfig::default().valid_type(b"bug"));
}
#[test]
fn user_config_throttle_duration() {
    // Original issue/config_test.go TestUserConfigThrottleDuration.
    let mut cfg = UserConfig::default();
    assert_eq!(DEFAULT_FETCH_THROTTLE, 60_000_000_000);
    assert_eq!(cfg.throttle_duration(), DEFAULT_FETCH_THROTTLE);
    cfg.fetch.throttle = "5m".into();
    assert_eq!(cfg.throttle_duration(), 300_000_000_000);
    cfg.fetch.throttle = "not-a-duration".into();
    assert_eq!(cfg.throttle_duration(), DEFAULT_FETCH_THROTTLE);
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(512))]
    #[test]
    fn valid_workflow_partitions_known_states_and_excludes_unknowns(
        names in prop::collection::btree_set("[a-z]{1,16}",1..32),
        choices in prop::collection::vec(0u8..3,32),
        unknown in prop::collection::vec(any::<u8>(),0..32),
    ) {
        let names:Vec<_>=names.into_iter().map(YamlString::from).collect();let mut active=Vec::new();let mut terminal=Vec::new();
        for (i,name) in names.iter().enumerate(){match choices[i%choices.len()]{0=>active.push(name.clone()),1=>terminal.push(name.clone()),_=>{}}}
        let w=WorkflowConfig{default:names[0].clone(),statuses:Some(names.clone()),active:Some(active),terminal:Some(terminal),transitions:None};
        prop_assert!(w.validate().is_ok());
        for name in &names {let status=name.as_bytes();let buckets=usize::from(w.is_active(status))+usize::from(w.is_terminal(status))+usize::from(w.is_hold(status));prop_assert_eq!(buckets,1);}
        if !names.iter().any(|n|n.as_bytes()==unknown){prop_assert!(!w.is_active(&unknown)&&!w.is_terminal(&unknown)&&!w.is_hold(&unknown)&&!w.is_valid(&unknown));}
        prop_assert_eq!(w.merge(&WorkflowFile::default()),w);
    }
    #[test]
    fn composed_duration_matches_wider_integer_arithmetic(hours in 0u64..1_000_000, minutes in 0u64..60, nanos in 0u64..1_000_000_000,negative in any::<bool>()) {
        let input=format!("{}{}h{}m{}ns",if negative{"-"}else{""},hours,minutes,nanos);
        let expected=(hours as i128*3_600_000_000_000+minutes as i128*60_000_000_000+nanos as i128)*if negative{-1}else{1};
        prop_assert_eq!(parse_duration(input.as_bytes()).unwrap() as i128,expected);
    }
}
