use beans::vault::{Index, LinkRef};
use serde_json::{Value, json};
use std::{
    ffi::OsString,
    os::unix::{
        ffi::{OsStrExt, OsStringExt},
        fs::symlink,
    },
    path::{Path, PathBuf},
};
fn path(value: &Value) -> PathBuf {
    OsString::from_vec(serde_json::from_value::<Vec<u8>>(value.clone()).unwrap()).into()
}
fn links(links: &[LinkRef]) -> Value {
    json!(
        links
            .iter()
            .map(|l| json!({"from":l.from,"to":l.to,"kind":l.kind}))
            .collect::<Vec<_>>()
    )
}
fn snapshot(ix: &Index, root: &Path) -> Value {
    let notes: Vec<_> = ix.ordered_notes().iter().map(|n| json!({"kind":n.graph.kind,"path":n.graph.path,"basename":n.graph.basename,"project":n.project,"title":n.title,"tags":n.tags,"id":n.graph.id,"aliases":n.graph.aliases,"description":n.description,"body":n.body,"raw":n.graph.raw_out.iter().map(|l| json!({"target":l.target,"kind":l.kind})).collect::<Vec<_>>(),"outlinks":links(&n.graph.outlinks)})).collect();
    let mut names: Vec<_> = ix
        .ordered_notes()
        .iter()
        .map(|n| n.graph.basename.clone())
        .collect();
    names.sort();
    names.dedup();
    let owners: Vec<_> = names
        .iter()
        .map(|name| json!({"basename":name,"path":ix.graph.by_basename(name).unwrap().path}))
        .collect();
    let mut aliases: Vec<_> = ix.graph.aliases().iter().collect();
    aliases.sort_by(|a, b| a.0.cmp(b.0));
    let aliases: Vec<_> = aliases
        .iter()
        .map(|(k, v)| json!({"alias":k,"basename":v}))
        .collect();
    let mut backlinks: Vec<_> = ix.graph.backlinks().iter().collect();
    backlinks.sort_by(|a, b| a.0.cmp(b.0));
    let backlinks: Vec<_> = backlinks
        .iter()
        .map(|(k, v)| json!({"basename":k,"links":links(v)}))
        .collect();
    let warnings: Vec<_> = ix
        .graph
        .warnings()
        .iter()
        .map(|w| json!({"path":w.path,"error":w.error.replace(root.to_str().unwrap(),"{ROOT}")}))
        .collect();
    let projects: Vec<_> = ix.projects.iter().map(|(name,p)| json!({"name":name,"prefix":p.config.prefix.as_bytes(),"workflow":p.workflow})).collect();
    json!({"notes":notes,"owners":owners,"aliases":aliases,"backlinks":backlinks,"warnings":warnings,"assets":ix.assets,"projects":projects,"workflow":ix.workflow})
}
pub(super) fn write(root: &Path, entries: &Value) {
    for entry in entries.as_array().unwrap() {
        let full = root.join(path(&entry["Path"]));
        match entry["Kind"].as_str().unwrap() {
            "directory" => std::fs::create_dir_all(&full).unwrap(),
            "delete" => {
                let _ = std::fs::remove_dir_all(&full);
                let _ = std::fs::remove_file(&full);
            }
            kind => {
                std::fs::create_dir_all(full.parent().unwrap()).unwrap();
                let bytes: Vec<u8> = serde_json::from_value(entry["Data"].clone()).unwrap();
                if kind == "symlink" {
                    symlink(OsString::from_vec(bytes), full).unwrap();
                } else {
                    std::fs::write(full, bytes).unwrap();
                }
            }
        }
    }
}
fn files(root: &Path) -> Value {
    fn walk(root: &Path, at: &Path, out: &mut Vec<Value>) {
        let mut entries: Vec<_> = std::fs::read_dir(at).unwrap().map(Result::unwrap).collect();
        entries.sort_by_key(|e| e.file_name());
        for entry in entries {
            let full = entry.path();
            let ty = entry.file_type().unwrap();
            let (kind, data) = if ty.is_dir() {
                ("directory", vec![])
            } else if ty.is_symlink() {
                (
                    "symlink",
                    std::fs::read_link(&full)
                        .unwrap()
                        .as_os_str()
                        .as_bytes()
                        .to_vec(),
                )
            } else {
                ("file", std::fs::read(&full).unwrap())
            };
            out.push(json!({"Path":full.strip_prefix(root).unwrap().as_os_str().as_bytes(),"Kind":kind,"Data":data}));
            if ty.is_dir() {
                walk(root, &full, out);
            }
        }
    }
    let mut result = vec![];
    walk(root, root, &mut result);
    json!(result)
}
#[test]
fn disk_index_loading_recovery_and_reload_match_fixed_go() {
    let corpus: Value = serde_json::from_str(include_str!("../contract/index.json")).unwrap();
    let base = std::env::temp_dir().join(format!("beans-index-{}", std::process::id()));
    std::fs::create_dir(&base).unwrap();
    struct Cleanup(PathBuf);
    impl Drop for Cleanup {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }
    let _cleanup = Cleanup(base.clone());
    let mut failures = vec![];
    for (i, case) in corpus["cases"].as_array().unwrap().iter().enumerate() {
        let root = base
            .join(i.to_string())
            .join(case["root"].as_str().unwrap());
        std::fs::create_dir_all(&root).unwrap();
        write(&root, &case["initial"]);
        let result = Index::load(&root);
        let mut error = result
            .as_ref()
            .err()
            .map_or(String::new(), ToString::to_string)
            .replace(root.to_str().unwrap(), "{ROOT}");
        let mut index = result.ok();
        for (stage, expected) in case["stages"].as_array().unwrap().iter().enumerate() {
            if stage > 0 {
                let step = &case["steps"][stage - 1];
                write(&root, &step["changes"]);
                let ix = index.as_mut().unwrap();
                let paths: Vec<_> = step["paths"].as_array().unwrap().iter().map(path).collect();
                let result = if step["all"].as_bool().unwrap() {
                    ix.reload_all()
                } else {
                    ix.reload(&paths)
                };
                error = result
                    .err()
                    .map_or(String::new(), |e| e.to_string())
                    .replace(root.to_str().unwrap(), "{ROOT}");
            }
            let actual = json!({"error":error,"index":index.as_ref().map(|ix| snapshot(ix,&root)),"files":files(&root)});
            if actual != *expected {
                std::fs::create_dir_all(".compat/index-failures").unwrap();
                std::fs::write(
                    format!(".compat/index-failures/{i}-{stage}.json"),
                    serde_json::to_vec_pretty(&json!({"actual":actual,"expected":expected}))
                        .unwrap(),
                )
                .unwrap();
                failures.push((i, stage));
            }
        }
    }
    assert!(
        failures.is_empty(),
        "index mismatches {failures:?}; see .compat/index-failures"
    );
}

pub(super) struct TestHub(pub(super) PathBuf);
impl Drop for TestHub {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}
pub(super) fn hub(name: &str, fixture: bool) -> TestHub {
    let root = std::env::temp_dir().join(format!("beans-index-{}-{name}", std::process::id()));
    std::fs::create_dir(&root).unwrap();
    fn copy(source: &Path, target: &Path) {
        for entry in std::fs::read_dir(source).unwrap().map(Result::unwrap) {
            let destination = target.join(entry.file_name());
            if entry.file_type().unwrap().is_dir() {
                std::fs::create_dir(&destination).unwrap();
                copy(&entry.path(), &destination);
            } else {
                std::fs::copy(entry.path(), destination).unwrap();
            }
        }
    }
    if fixture {
        copy(
            Path::new("tests/fixtures/go-baseline/vault/testdata/hub"),
            &root,
        );
    } else {
        std::fs::create_dir_all(root.join("projects/p")).unwrap();
        std::fs::write(
            root.join("projects/p/beans.toml"),
            b"name='p'\nprefix='p'\n",
        )
        .unwrap();
    }
    TestHub(root)
}
#[test]
fn index_original_load_basics() {
    use beans::vault::NoteKind;
    let root = hub("basics", true);
    let ix = Index::load(&root.0).unwrap();
    assert!(ix.projects.contains_key(b"a".as_slice()) && ix.projects.contains_key(b"b".as_slice()));
    for id in [
        "a-epic001",
        "a-child001",
        "b-child001",
        "a-open001",
        "b-blocked001",
        "a-archived001",
    ] {
        assert!(
            ix.note_by_id(NoteKind::Issue, id.as_bytes()).is_some(),
            "{id}"
        );
    }
    assert!(ix.note_by_id(NoteKind::Issue, b"a-broken001").is_none());
    assert_eq!(ix.lookup(b"parity").unwrap().graph.kind, NoteKind::Doc);
    assert_eq!(
        ix.lookup(b"prod-schema").unwrap().graph.kind,
        NoteKind::Memory
    );
    assert!(ix.assets.contains(b"docs/img/screenshot.png".as_slice()));
}
#[test]
fn index_original_load_warnings() {
    let root = hub("warnings", true);
    let ix = Index::load(&root.0).unwrap();
    let warnings = ix.graph.warnings();
    assert_eq!(warnings.len(), 2);
    assert!(
        warnings
            .iter()
            .any(|w| w.path == b"projects/a/issues/broken.md")
    );
    assert!(
        warnings
            .iter()
            .any(|w| w.error.contains("unresolved link [[missing-page]]"))
    );
}
#[test]
fn index_original_lookup() {
    use beans::vault::NoteData;
    let root = hub("lookup", true);
    let ix = Index::load(&root.0).unwrap();
    for target in [b"a-open001".as_slice(), b"shared-schema", b"A-OPEN001"] {
        assert_eq!(ix.lookup(target).unwrap().graph.basename, b"a-open001");
    }
    match &ix.lookup(b"a-open001").unwrap().data {
        NoteData::Issue(d) => assert_eq!(d.metadata.id, "a-open001"),
        _ => panic!("not issue"),
    }
    assert!(ix.lookup(b"does-not-exist").is_none());
}
#[test]
fn index_original_backlinks() {
    let root = hub("backlinks", true);
    let ix = Index::load(&root.0).unwrap();
    assert!(
        ix.graph.backlinks()[b"a-open001".as_slice()]
            .iter()
            .any(|r| r.from == b"parity")
    );
}
#[test]
fn index_original_incomplete_plan_warning() {
    let root = hub("incomplete", false);
    std::fs::create_dir_all(root.0.join("projects/p/plans/incomplete")).unwrap();
    let ix = Index::load(&root.0).unwrap();
    assert!(
        ix.graph.warnings().iter().any(
            |w| w.path == b"projects/p/plans/incomplete" && w.error.contains("missing plan.md")
        )
    );
}
fn scaffold(root: &Path) {
    use beans::domain::{issue::Timestamp, plan};
    std::fs::create_dir_all(root.parent().unwrap()).unwrap();
    plan::write_scaffold(
        root.to_str().unwrap(),
        "p-plan-a3f2",
        "test",
        &Timestamp {
            seconds: 1767225600,
            nanoseconds: 0,
            offset_seconds: 0,
        },
    )
    .unwrap();
}
#[test]
fn index_original_load_recovers_interrupted_plan_tree() {
    use beans::vault::NoteKind;
    let root = hub("recover-load", false);
    let plans = root.0.join("projects/p/plans");
    scaffold(&plans.join(".p-plan-a3f2-test.backup"));
    let ix = Index::load(&root.0).unwrap();
    assert!(ix.note_by_id(NoteKind::Plan, b"p-plan-a3f2").is_some());
    assert!(plans.join("p-plan-a3f2-test/plan.md").is_file());
}
#[test]
fn index_original_reload_recovers_interrupted_plan_tree() {
    use beans::vault::NoteKind;
    let root = hub("recover-reload", false);
    let plan = root.0.join("projects/p/plans/p-plan-a3f2-test");
    scaffold(&plan);
    let mut ix = Index::load(&root.0).unwrap();
    std::fs::rename(
        &plan,
        plan.parent().unwrap().join(".p-plan-a3f2-test.backup"),
    )
    .unwrap();
    ix.reload(&[plan.join("plan.md")]).unwrap();
    assert!(ix.note_by_id(NoteKind::Plan, b"p-plan-a3f2").is_some());
    assert!(plan.join("plan.md").is_file());
}
#[test]
fn index_original_reload_section_keeps_last_valid_plan() {
    use beans::vault::{NoteData, NoteKind};
    let root = hub("section", false);
    let plan = root.0.join("projects/p/plans/p-plan-a3f2-test");
    scaffold(&plan);
    let manifest = std::fs::read_to_string(plan.join("plan.md"))
        .unwrap()
        .replacen("updated:", "sections:\n  - sections/one.md\nupdated:", 1);
    std::fs::write(plan.join("plan.md"), manifest).unwrap();
    std::fs::create_dir(plan.join("sections")).unwrap();
    let section = plan.join("sections/one.md");
    std::fs::write(&section, b"# Original\n").unwrap();
    let mut ix = Index::load(&root.0).unwrap();
    let body = |ix: &Index| match &ix.note_by_id(NoteKind::Plan, b"p-plan-a3f2").unwrap().data {
        NoteData::Plan(p) => p.section_bodies[0].markdown.clone(),
        _ => panic!("not plan"),
    };
    assert_eq!(body(&ix), "# Original\n");
    std::fs::write(&section, b"# Changed\n").unwrap();
    ix.reload(std::slice::from_ref(&section)).unwrap();
    assert_eq!(body(&ix), "# Changed\n");
    std::fs::remove_file(&section).unwrap();
    ix.reload(&[section]).unwrap();
    assert_eq!(body(&ix), "# Changed\n");
    assert!(!ix.graph.warnings().is_empty());
}

#[test]
fn duplicate_basename_keeps_every_issue_original_regression() {
    use beans::vault::{NoteData, NoteKind};
    let root = hub("duplicate-basename-regression", true);
    let path = root.0.join("projects/b/issues/a-open001.md");
    std::fs::write(&path, b"---\nid: b-dup001\ntitle: Duplicate basename\ntype: task\nstatus: open\npriority: 2\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\nbody\n").unwrap();
    std::fs::create_dir_all(root.0.join("projects/b/docs")).unwrap();
    std::fs::write(root.0.join("projects/b/docs/parity.md"), b"# B parity\n").unwrap();
    let mut ix = Index::load(&root.0).unwrap();
    assert!(ix.note_by_id(NoteKind::Issue, b"b-dup001").is_some());
    assert_eq!(ix.lookup(b"a-open001").unwrap().project, b"a");
    for target in [b"projects/b/docs/parity.md".as_slice(), b"b/docs/parity"] {
        assert_eq!(
            ix.lookup(target).unwrap().graph.path,
            b"projects/b/docs/parity.md"
        );
    }
    assert_eq!(
        ix.graph
            .warnings()
            .iter()
            .filter(|w| w.error.contains("duplicate note basename"))
            .count(),
        2
    );
    let winner = root.0.join("projects/a/issues/a-open001.md");
    std::fs::remove_file(&winner).unwrap();
    ix.reload(&[winner]).unwrap();
    let promoted = ix.lookup(b"a-open001").unwrap();
    assert_eq!(promoted.project, b"b");
    match &promoted.data {
        NoteData::Issue(d) => assert_eq!(d.metadata.id, "b-dup001"),
        _ => panic!("not issue"),
    }
    assert!(ix.note_by_id(NoteKind::Issue, b"a-open001").is_none());
}
#[test]
fn reload_project_config_refreshes_workflow_original_regression() {
    let root = hub("reload-workflow-regression", true);
    let mut ix = Index::load(&root.0).unwrap();
    assert!(!ix.workflow_for(b"a").is_active(b"blocked"));
    let path = root.0.join("projects/a/beans.toml");
    let mut data = std::fs::read(&path).unwrap();
    data.extend_from_slice(b"\n[workflow]\nactive = [\"open\", \"blocked\"]\n");
    std::fs::write(&path, data).unwrap();
    ix.reload(&[path]).unwrap();
    assert!(ix.workflow_for(b"a").is_active(b"blocked"));
    assert!(!ix.ordered_notes().is_empty());
    assert!(
        ix.ordered_notes()
            .iter()
            .any(|n| n.graph.kind == beans::vault::NoteKind::Issue)
    );
}
#[test]
fn reload_rejects_paths_outside_hub_original_regression() {
    let root = hub("reload-outside-regression", true);
    let mut ix = Index::load(&root.0).unwrap();
    let before = snapshot(&ix, &root.0);
    let outside = hub("reload-external-regression", false);
    for path in [
        outside.0.join("elsewhere.md"),
        PathBuf::from("../escape.md"),
    ] {
        assert!(ix.reload(&[path]).is_err());
        assert_eq!(snapshot(&ix, &root.0), before);
    }
}
