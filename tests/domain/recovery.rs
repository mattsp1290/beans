use beans::gitops::{recover_plan_temp, recover_tree, recover_trees};
use serde_json::{Value, json};
use std::{
    os::unix::ffi::OsStrExt,
    path::{Path, PathBuf},
};
fn snapshot(root: &Path) -> Value {
    fn walk(root: &Path, dir: &Path, out: &mut Vec<Value>) {
        let mut entries = std::fs::read_dir(dir)
            .unwrap()
            .map(Result::unwrap)
            .collect::<Vec<_>>();
        entries.sort_by_key(|e| e.file_name());
        for entry in entries {
            let path = entry.path();
            let info = entry.file_type().unwrap();
            let (kind, data) = if info.is_dir() {
                ("directory", Vec::new())
            } else if info.is_symlink() {
                (
                    "symlink",
                    std::fs::read_link(&path)
                        .unwrap()
                        .as_os_str()
                        .as_bytes()
                        .to_vec(),
                )
            } else {
                ("file", std::fs::read(&path).unwrap())
            };
            out.push(json!({"Path":path.strip_prefix(root).unwrap().as_os_str().as_bytes(),"Kind":kind,"Data":data}));
            if info.is_dir() {
                walk(root, &path, out);
            }
        }
    }
    let mut out = Vec::new();
    walk(root, root, &mut out);
    Value::Array(out)
}
#[test]
fn recovery_matches_fixed_go_filesystem_results_and_errors() {
    let fixture: Value = serde_json::from_str(include_str!("../contract/recovery.json")).unwrap();
    let base = std::env::temp_dir().join(format!("beans-recovery-{}", std::process::id()));
    std::fs::create_dir(&base).unwrap();
    struct Cleanup(PathBuf);
    impl Drop for Cleanup {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }
    let _cleanup = Cleanup(base.clone());
    for (i, case) in fixture["cases"].as_array().unwrap().iter().enumerate() {
        let root = base.join(i.to_string());
        std::fs::create_dir(&root).unwrap();
        let layout = &case["Layout"];
        if let Some(dirs) = layout["Dirs"].as_array() {
            for dir in dirs {
                std::fs::create_dir_all(root.join(dir.as_str().unwrap())).unwrap();
            }
        }
        if let Some(files) = layout["Files"].as_object() {
            for (name, data) in files {
                let path = root.join(name);
                std::fs::create_dir_all(path.parent().unwrap()).unwrap();
                std::fs::write(path, data.as_str().unwrap()).unwrap();
            }
        }
        if let Some(links) = layout["Links"].as_object() {
            for (name, to) in links {
                let path = root.join(name);
                std::fs::create_dir_all(path.parent().unwrap()).unwrap();
                std::os::unix::fs::symlink(to.as_str().unwrap(), path).unwrap();
            }
        }
        assert_eq!(snapshot(&root), case["Before"], "case{i} setup");
        let target = PathBuf::from(
            case["ResolvedTarget"]
                .as_str()
                .unwrap()
                .replace("/oracle", root.to_str().unwrap()),
        );
        let result = match case["Mode"].as_str().unwrap() {
            "tree" => recover_tree(&target),
            "trees" => recover_trees(&target),
            "temp" => recover_plan_temp(&target),
            _ => unreachable!(),
        };
        let error = result.err().map_or(String::new(), |e| {
            e.to_string().replace(root.to_str().unwrap(), "/oracle")
        });
        assert_eq!(error, case["Error"].as_str().unwrap(), "case{i}");
        assert_eq!(snapshot(&root), case["After"], "case{i} after");
    }
}
#[test]
fn recover_trees_restores_interrupted_backup_original() {
    let parent =
        std::env::temp_dir().join(format!("beans-original-recovery-{}", std::process::id()));
    std::fs::create_dir(&parent).unwrap();
    let backup = parent.join(".bundle.backup");
    std::fs::create_dir(&backup).unwrap();
    std::fs::write(backup.join("plan.md"), b"old\n").unwrap();
    recover_trees(&parent).unwrap();
    assert_eq!(
        std::fs::read(parent.join("bundle/plan.md")).unwrap(),
        b"old\n"
    );
    assert!(!backup.exists());
    std::fs::remove_dir_all(parent).unwrap();
}
