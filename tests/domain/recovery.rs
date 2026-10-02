use beans::gitops::{recover_plan_temp, recover_tree, recover_trees};
use serde_json::{Value, json};
use std::{
    ffi::OsString,
    os::unix::ffi::{OsStrExt, OsStringExt},
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
        let bytes = |v: &Value| {
            v.as_array()
                .unwrap()
                .iter()
                .map(|b| b.as_u64().unwrap() as u8)
                .collect::<Vec<_>>()
        };
        let raw = bytes(&case["RawName"]);
        let name = |text: &str| {
            let mut out = Vec::new();
            for (i, part) in text.split("__RAW__").enumerate() {
                if i != 0 {
                    out.extend_from_slice(&raw);
                }
                out.extend_from_slice(part.as_bytes());
            }
            PathBuf::from(OsString::from_vec(out))
        };
        let setup = !raw.contains(&0) && !raw.contains(&b'/');
        if setup {
            if let Some(dirs) = layout["Dirs"].as_array() {
                for dir in dirs {
                    std::fs::create_dir_all(root.join(name(dir.as_str().unwrap()))).unwrap();
                }
            }
            if let Some(files) = layout["Files"].as_object() {
                for (file, data) in files {
                    let path = root.join(name(file));
                    std::fs::create_dir_all(path.parent().unwrap()).unwrap();
                    std::fs::write(path, data.as_str().unwrap()).unwrap();
                }
            }
            if let Some(links) = layout["Links"].as_object() {
                for (file, to) in links {
                    let path = root.join(name(file));
                    std::fs::create_dir_all(path.parent().unwrap()).unwrap();
                    std::os::unix::fs::symlink(to.as_str().unwrap(), path).unwrap();
                }
            }
        }
        assert_eq!(snapshot(&root), case["Before"], "case{i} setup");
        let resolved = bytes(&case["ResolvedTargetBytes"]);
        let suffix = resolved.strip_prefix(b"/oracle").unwrap();
        let target = PathBuf::from(OsString::from_vec(
            [root.as_os_str().as_bytes(), suffix].concat(),
        ));
        let result = match case["Mode"].as_str().unwrap() {
            "tree" => recover_tree(&target),
            "trees" => recover_trees(&target),
            "temp" => recover_plan_temp(&target),
            _ => unreachable!(),
        };
        let error = result.err().map_or(Vec::new(), |e| {
            // Root normalization is a byte replacement, never a Unicode view.
            let needle = root.as_os_str().as_bytes();
            let mut rest = e.as_bytes();
            let mut out = Vec::new();
            while let Some(i) = rest.windows(needle.len()).position(|w| w == needle) {
                out.extend_from_slice(&rest[..i]);
                out.extend_from_slice(b"/oracle");
                rest = &rest[i + needle.len()..];
            }
            out.extend_from_slice(rest);
            out
        });
        assert_eq!(error, bytes(&case["ErrorBytes"]), "case{i}");
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
