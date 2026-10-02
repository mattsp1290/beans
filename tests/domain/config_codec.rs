use beans::domain::{
    config::{
        ProjectConfig, UserConfig, UserFetchConfig, UserHubConfig, encode_project_config,
        encode_user_config,
    },
    plan::YamlString,
    workflow::{States, WorkflowFile},
};
use serde::Deserialize;
use serde_json::Value;
use std::{
    path::{Path, PathBuf},
    sync::atomic::{AtomicUsize, Ordering},
};

struct TempDir(PathBuf);
impl TempDir {
    fn new() -> Self {
        static NEXT: AtomicUsize = AtomicUsize::new(0);
        let path = std::env::temp_dir().join(format!(
            "beans-config-codec-{}-{}",
            std::process::id(),
            NEXT.fetch_add(1, Ordering::Relaxed)
        ));
        std::fs::create_dir(&path).unwrap();
        Self(path)
    }
}
impl Drop for TempDir {
    fn drop(&mut self) {
        std::fs::remove_dir_all(&self.0).unwrap();
    }
}

fn load(kind: &str, path: &Path) -> (Value, String) {
    use beans::domain::config::*;
    fn view<T: serde::Serialize + Default>(
        result: Result<T, beans::domain::frontmatter::Error>,
    ) -> (Value, String) {
        match result {
            Ok(cfg) => (serde_json::to_value(cfg).unwrap(), String::new()),
            Err(e) => (serde_json::to_value(T::default()).unwrap(), e.to_string()),
        }
    }
    match kind {
        "user" => view(load_user_config(path)),
        "project" => view(load_project_config(path)),
        "hub" => view(load_hub_config(path)),
        _ => panic!("unknown kind"),
    }
}

#[test]
fn configuration_file_loads_match_committed_contract() {
    let corpus: Value =
        serde_json::from_str(include_str!("../fixtures/expected/config-codec.json")).unwrap();
    let mut mismatches = Vec::new();
    assert_eq!(corpus["reads"].as_array().unwrap().len(), 240);
    for (rows, encoded) in [(&corpus["reads"], false), (&corpus["encodes"], true)] {
        for row in rows.as_array().unwrap() {
            let work = TempDir::new();
            let mut path = work.0.join("config.toml");
            if encoded || row.get("input").is_some() {
                let data: Vec<u8> =
                    serde_json::from_value(row[if encoded { "output" } else { "input" }].clone())
                        .unwrap();
                std::fs::write(&path, &data).unwrap();
            } else {
                use std::os::unix::fs::symlink;
                match row["fs_kind"].as_str().unwrap() {
                    "missing" => {}
                    "directory" => std::fs::create_dir(&path).unwrap(),
                    "parent-file" => {
                        std::fs::write(work.0.join("parent"), b"x").unwrap();
                        path = work.0.join("parent/config.toml");
                    }
                    "symlink" => {
                        std::fs::write(work.0.join("target.toml"), b"").unwrap();
                        symlink("target.toml", &path).unwrap();
                    }
                    "dangling-link" => symlink("absent.toml", &path).unwrap(),
                    _ => panic!("unknown filesystem fixture"),
                }
            }
            let before = std::fs::read(&path).ok();
            let (cfg, error) = load(row["kind"].as_str().unwrap(), &path);
            let error = error.replace(&format!("{}/", work.0.display()), "");
            let expected_cfg = &row[if encoded { "read_config" } else { "config" }];
            let expected_error = &row[if encoded { "read_error" } else { "error" }];
            if cfg != *expected_cfg || error != *expected_error {
                mismatches.push(format!(
                    "{}: config {cfg} != {expected_cfg}; error {error:?} != {expected_error}",
                    row["name"]
                ));
            }
            assert_eq!(
                std::fs::read(&path).ok(),
                before,
                "{} loader mutated input",
                row["name"]
            );
        }
    }
    assert!(mismatches.is_empty(), "{}", mismatches.join("\n"));
}

#[test]
fn load_hub_config_missing_path_returns_defaults() {
    use beans::domain::config::load_hub_config;
    let work = TempDir::new();
    let cfg = load_hub_config(&work.0.join("missing.toml")).unwrap();
    assert_eq!(cfg.ids.length, 4);
    assert_eq!(
        cfg.types.names.unwrap(),
        ["task", "bug", "feature", "epic", "chore"].map(YamlString::from)
    );
}
#[test]
fn load_hub_config_malformed_toml_names_file_and_line() {
    use beans::domain::config::load_hub_config;
    let work = TempDir::new();
    let path = work.0.join("beans.toml");
    std::fs::write(
        &path,
        b"[workflow]\nstatuses = [\"open\"\ndefault = \"open\"\n",
    )
    .unwrap();
    let error = load_hub_config(&path).unwrap_err().to_string();
    assert!(error.contains(path.to_str().unwrap()));
    assert!(error.contains("line"));
}
#[test]
fn load_project_config_reads_name_prefix_remotes() {
    use beans::domain::config::load_project_config;
    let work = TempDir::new();
    let path = work.0.join("beans.toml");
    std::fs::write(&path,b"name = \"exampleA\"\nprefix = \"exampleA\"\nremotes = [\"https://github.com/mattsp1290/exampleA\"]\n").unwrap();
    let cfg = load_project_config(&path).unwrap();
    assert_eq!(cfg.name, YamlString::from("exampleA"));
    assert_eq!(cfg.prefix, cfg.name);
    assert_eq!(
        cfg.remotes.unwrap(),
        [YamlString::from("https://github.com/mattsp1290/exampleA")]
    );
}
#[test]
fn encode_project_config_round_trip() {
    use beans::domain::config::load_project_config;
    let cfg = ProjectConfig {
        name: "beanCounter".into(),
        prefix: "bean-counter".into(),
        remotes: Some(
            [
                "https://github.com/mattsp1290/beans",
                "git@github.com:mattsp1290/beans.git",
            ]
            .map(YamlString::from)
            .to_vec(),
        ),
        workflow: WorkflowFile {
            statuses: Some(["open", "closed"].map(YamlString::from).to_vec()),
            default: "open".into(),
            ..WorkflowFile::default()
        },
    };
    let work = TempDir::new();
    let path = work.0.join("beans.toml");
    std::fs::write(&path, encode_project_config(&cfg)).unwrap();
    assert_eq!(load_project_config(&path).unwrap(), cfg);
}
#[test]
fn load_user_config_missing_returns_defaults() {
    use beans::domain::config::{DEFAULT_FETCH_THROTTLE, load_user_config};
    let work = TempDir::new();
    let cfg = load_user_config(&work.0.join("missing.toml")).unwrap();
    assert_eq!(cfg.throttle_duration(), DEFAULT_FETCH_THROTTLE);
    assert_eq!(cfg.actor, YamlString::default());
}
#[test]
fn encode_user_config_decodes_back() {
    use beans::domain::config::load_user_config;
    let cfg = UserConfig {
        actor: "matt".into(),
        hub: UserHubConfig {
            remote: "git@github.com:owner/beans-hub.git".into(),
            branch: "main".into(),
        },
        fetch: UserFetchConfig {
            throttle: "90s".into(),
        },
    };
    let work = TempDir::new();
    let path = work.0.join("config.toml");
    std::fs::write(&path, encode_user_config(&cfg)).unwrap();
    let back = load_user_config(&path).unwrap();
    assert_eq!(back, cfg);
    assert_eq!(back.throttle_duration(), 90_000_000_000);
}

proptest::proptest! {
    #![proptest_config(proptest::test_runner::Config::with_cases(512))]
    #[test]
    fn valid_unicode_config_roundtrips_preserve_fields(
        actor in proptest::collection::vec(proptest::char::any(),0..100),
        remote in proptest::collection::vec(proptest::char::any(),0..100),
        branch in proptest::collection::vec(proptest::char::any(),0..100),
        throttle in proptest::collection::vec(proptest::char::any(),0..100),
    ) {
        let string=|v:Vec<char>|YamlString::from(v.into_iter().collect::<String>());
        let cfg=UserConfig {actor:string(actor),hub:UserHubConfig {remote:string(remote),branch:string(branch)},fetch:UserFetchConfig {throttle:string(throttle)}};
        let encoded=encode_user_config(&cfg);
        proptest::prop_assert_eq!(beans::domain::config::decode_user_config(&encoded).unwrap(),cfg);
    }
}

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
fn configuration_output_matches_committed_contract_bytes() {
    let corpus: Value =
        serde_json::from_str(include_str!("../fixtures/expected/config-codec.json")).unwrap();
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
