use beans::domain::workflow::{WorkflowConfig, WorkflowFile, decode_workflow_file, load_workflow};
use serde_json::Value;
use std::{
    path::PathBuf,
    sync::atomic::{AtomicUsize, Ordering},
};
fn corpus() -> Value {
    serde_json::from_str(include_str!("../contract/workflow-file.json")).unwrap()
}
fn bytes(value: &Value) -> Vec<u8> {
    serde_json::from_value(value.clone()).unwrap()
}
#[test]
fn strict_workflow_files_match_go() {
    let corpus = corpus();
    let mut mismatches = Vec::new();
    assert_eq!(corpus["decodes"].as_array().unwrap().len(), 371);
    for row in corpus["decodes"].as_array().unwrap() {
        let result = decode_workflow_file(row["file"].as_str().unwrap(), &bytes(&row["input"]));
        let (cfg, error) = match result {
            Ok(cfg) => (cfg, String::new()),
            Err(e) => (WorkflowFile::default(), e.to_string()),
        };
        let cfg = serde_json::to_value(cfg).unwrap();
        if cfg != row["file_config"] || error != row["error"] {
            mismatches.push(format!(
                "{}: config {cfg} != {}; error {error:?} != {}",
                row["name"], row["file_config"], row["error"]
            ));
        }
    }
    assert!(mismatches.is_empty(), "{}", mismatches.join("\n"));
}
struct TempDir(PathBuf);
impl TempDir {
    fn new() -> Self {
        static NEXT: AtomicUsize = AtomicUsize::new(0);
        let path = std::env::temp_dir().join(format!(
            "beans-workflow-{}-{}",
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
#[test]
fn workflow_source_precedence_and_explicit_files_match_go() {
    let corpus = corpus();
    let mut mismatches = Vec::new();
    assert_eq!(corpus["loads"].as_array().unwrap().len(), 131);
    for row in corpus["loads"].as_array().unwrap() {
        let work = TempDir::new();
        let path = row["explicit"].as_str().map(|name| work.0.join(name));
        if let Some(path) = &path {
            use std::os::unix::fs::symlink;
            if row.get("input").is_some() {
                std::fs::write(path, bytes(&row["input"])).unwrap();
            } else {
                match row["fs_kind"].as_str().unwrap() {
                    "missing" => {}
                    "directory" => std::fs::create_dir(path).unwrap(),
                    "parent-file" => std::fs::write(work.0.join("parent"), b"x").unwrap(),
                    "dangling-link" => symlink("absent.toml", path).unwrap(),
                    "symlink" => {
                        std::fs::write(
                            work.0.join("target.toml"),
                            b"[workflow]\ndefault=\"in_progress\"",
                        )
                        .unwrap();
                        symlink("target.toml", path).unwrap();
                    }
                    _ => panic!("unknown layout"),
                }
            }
        }
        let before = path.as_ref().and_then(|p| std::fs::read(p).ok());
        let result = load_workflow(
            path.as_deref(),
            &bytes(&row["project"]),
            &bytes(&row["hub"]),
        );
        let (cfg, error) = match result {
            Ok(cfg) => (cfg, String::new()),
            Err(e) => (
                WorkflowConfig::default(),
                e.to_string().replace(&format!("{}/", work.0.display()), ""),
            ),
        };
        let cfg = serde_json::to_value(cfg).unwrap();
        if cfg != row["config"] || error != row["error"] {
            mismatches.push(format!(
                "{}: config {cfg} != {}; error {error:?} != {}",
                row["name"], row["config"], row["error"]
            ));
        }
        assert_eq!(
            path.as_ref().and_then(|p| std::fs::read(p).ok()),
            before,
            "loader modified source"
        );
    }
    assert!(mismatches.is_empty(), "{}", mismatches.join("\n"));
}

#[test]
fn decode_workflow_file_toml_and_yaml() {
    let toml=b"\n[workflow]\nstatuses = [\"open\", \"in_progress\", \"qa\", \"closed\"]\ndefault = \"open\"\nactive = [\"open\"]\nterminal = [\"closed\"]\n";
    let yaml=b"\nworkflow:\n  statuses: [open, in_progress, qa, closed]\n  default: open\n  active: [open]\n  terminal: [closed]\n";
    let base = WorkflowConfig::built_in();
    let from_toml = base.merge(&decode_workflow_file("beans.toml", toml).unwrap());
    let from_yaml = base.merge(&decode_workflow_file("wf.yaml", yaml).unwrap());
    assert_eq!(from_toml.statuses.as_ref().unwrap().len(), 4);
    assert!(from_toml.is_valid(b"qa"));
    assert!(!from_toml.is_valid(b"done"));
    assert!(from_toml.is_terminal(b"closed"));
    assert_eq!(from_yaml, from_toml);
}
#[test]
fn merge_workflow_file_partial_inherits_base() {
    let file =
        decode_workflow_file("beans.toml", b"[workflow]\ndefault = \"in_progress\"\n").unwrap();
    let workflow = WorkflowConfig::built_in().merge(&file);
    assert_eq!(workflow.default_state(), "in_progress".into());
    assert!(workflow.is_valid(b"ready_for_review"));
    assert!(workflow.is_terminal(b"done"));
}
#[test]
fn decode_workflow_file_unknown_keys_fail_fast() {
    for (name, body) in [
        ("bad.toml", b"[workflow]\ndefaultx = \"open\"\n".as_slice()),
        ("bad.yaml", b"workflow:\n  defaultx: open\n".as_slice()),
    ] {
        assert!(decode_workflow_file(name, body).is_err());
    }
}
#[test]
fn decode_workflow_file_ignores_other_toml_tables() {
    let file = decode_workflow_file(
        "beans.toml",
        b"[workflow]\ndefault = \"open\"\n\n[types]\nnames = [\"task\"]\n",
    )
    .unwrap();
    assert_eq!(file.default, "open".into());
}
#[test]
fn decode_workflow_file_unsupported_extension() {
    assert!(decode_workflow_file("wf.json", b"{}").is_err());
}
#[test]
fn merged_invalid_bucket_fails_validate() {
    let file=decode_workflow_file("bad.toml",b"\n[workflow]\nstatuses = [\"open\", \"closed\"]\ndefault = \"open\"\nactive = [\"ghost\"]\nterminal = [\"closed\"]\n").unwrap();
    assert!(WorkflowConfig::built_in().merge(&file).validate().is_err());
}

proptest::proptest! {
    #![proptest_config(proptest::test_runner::Config::with_cases(512))]
    #[test]
    fn generated_vocabularies_agree_across_workflow_formats(names in proptest::collection::btree_set("[a-z]{1,16}",2..20)) {
        let names:Vec<_>=names.into_iter().collect();let default=&names[0];let terminal=names.last().unwrap();
        let quoted=|value:&str|serde_json::to_string(value).unwrap();
        let statuses=serde_json::to_string(&names).unwrap();
        let toml=format!("[workflow]\nstatuses={statuses}\ndefault={}\nactive=[{}]\nterminal=[{}]\n",quoted(default),quoted(default),quoted(terminal));
        let yaml=format!("workflow:\n  statuses: {statuses}\n  default: {}\n  active: [{}]\n  terminal: [{}]\n",quoted(default),quoted(default),quoted(terminal));
        let a=decode_workflow_file("wf.toml",toml.as_bytes()).unwrap();let b=decode_workflow_file("wf.yaml",yaml.as_bytes()).unwrap();
        proptest::prop_assert_eq!(&a,&b);
        proptest::prop_assert!(WorkflowConfig::built_in().merge(&a).validate().is_ok());
        proptest::prop_assert_eq!(load_workflow(None,toml.as_bytes(),&[]).unwrap(),WorkflowConfig::built_in().merge(&b));
    }
}

// Independent ports of every original issue/workflow_load_test.go scenario.
#[test]
fn explicit_workflow_wins_over_project_and_hub() {
    let work = TempDir::new();
    let path = work.0.join("explicit.toml");
    std::fs::write(&path, b"[workflow]\nstatuses = [\"draft\", \"live\"]\ndefault = \"draft\"\nactive = [\"draft\"]\nterminal = [\"live\"]\n").unwrap();
    let wf = load_workflow(
        Some(&path),
        b"[workflow]\ndefault = \"closed\"\n",
        b"[workflow]\nstatuses = [\"open\", \"closed\"]\ndefault = \"open\"\n",
    )
    .unwrap();
    assert_eq!(wf.statuses, Some(vec!["draft".into(), "live".into()]));
    assert_eq!(wf.default.as_bytes(), b"draft");
}
#[test]
fn project_workflow_overrides_hub_per_key() {
    let wf = load_workflow(None, b"[workflow]\ndefault = \"in_progress\"\n", b"[workflow]\nstatuses = [\"open\", \"in_progress\", \"closed\"]\ndefault = \"open\"\nactive = [\"open\", \"in_progress\"]\nterminal = [\"closed\"]\n").unwrap();
    assert_eq!(
        wf.statuses,
        Some(vec!["open".into(), "in_progress".into(), "closed".into()])
    );
    assert_eq!(wf.default.as_bytes(), b"in_progress");
    assert_eq!(wf.active, Some(vec!["open".into(), "in_progress".into()]));
    assert_eq!(wf.terminal, Some(vec!["closed".into()]));
}
#[test]
fn missing_explicit_workflow_errors() {
    let work = TempDir::new();
    assert!(load_workflow(Some(&work.0.join("does-not-exist.toml")), b"", b"").is_err());
}
#[test]
fn invalid_inherited_workflow_vocabulary_errors() {
    let err = load_workflow(
        None,
        b"[workflow]\ndefault = \"nonexistent_status\"\n",
        b"[workflow]\nstatuses = [\"open\", \"closed\"]\ndefault = \"open\"\n",
    )
    .unwrap_err();
    assert!(err.to_string().contains("not in statuses"));
}
#[test]
fn workflow_with_types_and_ids_tables_loads() {
    let wf = load_workflow(None, b"", b"[workflow]\nstatuses = [\"open\", \"closed\"]\ndefault = \"open\"\nactive = [\"open\"]\nterminal = [\"closed\"]\n\n[types]\nnames = [\"task\", \"bug\"]\n\n[ids]\nlength = 5\n").unwrap();
    assert_eq!(wf.statuses, Some(vec!["open".into(), "closed".into()]));
}
#[test]
fn workflow_without_config_uses_built_in_defaults() {
    assert_eq!(
        load_workflow(None, b"", b"").unwrap(),
        WorkflowConfig::built_in()
    );
}
