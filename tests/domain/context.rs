use beans::{
    cli::Actor,
    domain::workflow::load_workflow,
    ops::{OperationConfig, prefix_for},
};
use serde_json::Value;
use std::{
    cell::Cell,
    collections::BTreeMap,
    path::{Path, PathBuf},
};
#[test]
fn actor_precedence_caching_and_raw_bytes_match_fixed_go() {
    let fixture: Value = serde_json::from_str(include_str!("../contract/context.json")).unwrap();
    for case in fixture["actors"].as_array().unwrap() {
        let bytes = |key| serde_json::from_value::<Vec<u8>>(case[key].clone()).unwrap();
        let mut actor = Actor::new(&bytes("Flag"));
        let calls = Cell::new(0);
        let first = actor
            .resolve_with(
                &bytes("Config"),
                |key| bytes(if key == "BN_ACTOR" { "Env" } else { "User" }),
                || {
                    calls.set(calls.get() + 1);
                    if case["GitFail"].as_bool().unwrap() {
                        None
                    } else {
                        Some(bytes("Git"))
                    }
                },
            )
            .to_vec();
        assert_eq!(first, bytes("First"), "{case}");
        let second = actor
            .resolve_with(
                b"changed-config",
                |key| {
                    if key == "BN_ACTOR" {
                        b"next-env".to_vec()
                    } else {
                        bytes("User")
                    }
                },
                || panic!("cached or env actor must win"),
            )
            .to_vec();
        assert_eq!(second, bytes("Second"), "{case}");
        assert_eq!(calls.get(), case["Calls"].as_u64().unwrap(), "{case}");
    }
}
#[test]
fn resolve_actor_original_precedence_regression() {
    let mut actor = Actor::new(b"flag-actor");
    assert_eq!(
        actor.resolve_with(b"", |_| b"env-actor".to_vec(), || panic!("flag wins")),
        b"flag-actor"
    );
    let mut actor = Actor::default();
    assert_eq!(
        actor.resolve_with(b"", |_| b"env-actor".to_vec(), || panic!("env wins")),
        b"env-actor"
    );
}
fn snapshot(root: &Path) -> BTreeMap<PathBuf, Vec<u8>> {
    fn walk(root: &Path, path: &Path, out: &mut BTreeMap<PathBuf, Vec<u8>>) {
        for entry in std::fs::read_dir(path).unwrap() {
            let entry = entry.unwrap();
            let path = entry.path();
            if entry.file_type().unwrap().is_dir() {
                walk(root, &path, out);
            } else {
                out.insert(
                    path.strip_prefix(root).unwrap().into(),
                    std::fs::read(path).unwrap(),
                );
            }
        }
    }
    let mut out = BTreeMap::new();
    walk(root, root, &mut out);
    out
}
#[test]
fn operation_fallback_prefix_and_source_liveness_match_fixed_go() {
    let fixture: Value = serde_json::from_str(include_str!("../contract/context.json")).unwrap();
    let base = std::env::temp_dir().join(format!("beans-context-{}", std::process::id()));
    std::fs::create_dir(&base).unwrap();
    struct Cleanup(PathBuf);
    impl Drop for Cleanup {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }
    let _cleanup = Cleanup(base.clone());
    for (i, case) in fixture["operations"].as_array().unwrap().iter().enumerate() {
        let hub = base.join(i.to_string());
        std::fs::create_dir(&hub).unwrap();
        let files = case["Files"].as_object().unwrap();
        for (name, contents) in files {
            let path = hub.join(name);
            std::fs::create_dir_all(path.parent().unwrap()).unwrap();
            std::fs::write(path, contents.as_str().unwrap()).unwrap();
        }
        let explicit = case["Explicit"].as_str().unwrap();
        let path = (!explicit.is_empty()).then(|| hub.join(explicit));
        let env = path
            .as_ref()
            .map_or(String::new(), |p| format!(" {} ", p.display()));
        let before = snapshot(&hub);
        let ctx = OperationConfig::load(&hub, env.as_bytes());
        assert_eq!(
            serde_json::to_value(ctx.workflow_for(b"exa")).unwrap(),
            case["Workflow"],
            "case{i}"
        );
        assert_eq!(
            serde_json::to_value(&ctx.types.names).unwrap(),
            case["Types"],
            "case{i}"
        );
        assert_eq!(ctx.id_length, case["IDLength"].as_i64().unwrap(), "case{i}");
        let prefix: Vec<u8> = serde_json::from_value(case["Prefix"].clone()).unwrap();
        assert_eq!(prefix_for(&hub, b"exa"), prefix, "case{i}");
        let bytes = |key| {
            files
                .get(key)
                .map_or(b"".as_slice(), |v| v.as_str().unwrap().as_bytes())
        };
        let error = load_workflow(
            path.as_deref(),
            bytes("projects/exa/beans.toml"),
            bytes("beans.toml"),
        )
        .err()
        .map_or(String::new(), |e| {
            e.to_string().replace(hub.to_str().unwrap(), "/oracle/hub")
        });
        assert_eq!(error, case["ReadError"].as_str().unwrap(), "case{i}");
        assert_eq!(snapshot(&hub), before, "case{i} load wrote files");
        std::fs::write(hub.join("beans.toml"), b"[workflow]\ndefault='bogus'").unwrap();
        std::fs::create_dir_all(hub.join("projects/exa")).unwrap();
        std::fs::write(
            hub.join("projects/exa/beans.toml"),
            b"[workflow]\ndefault='closed'",
        )
        .unwrap();
        if let Some(path) = path.as_ref() {
            let contents = if explicit.ends_with(".yaml") {
                "workflow:\n  default: in_progress\n"
            } else {
                "[workflow]\ndefault='in_progress'"
            };
            std::fs::write(path, contents).unwrap();
        }
        assert_eq!(
            serde_json::to_value(ctx.workflow_for(b"exa")).unwrap(),
            case["After"],
            "case{i} snapshot/liveness"
        );
    }
}
