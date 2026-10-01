use beans::vault::create_project_files;
use serde_json::{Value, json};
use std::{
    os::unix::{
        ffi::OsStrExt,
        fs::{DirBuilderExt, OpenOptionsExt, PermissionsExt},
    },
    path::{Path, PathBuf},
};
fn bytes(v: &Value) -> Vec<u8> {
    serde_json::from_value(v.clone()).unwrap()
}
pub(super) fn snapshot(root: &Path) -> Value {
    fn walk(root: &Path, dir: &Path, nodes: &mut Vec<Value>) {
        let mut entries: Vec<_> = std::fs::read_dir(dir)
            .unwrap()
            .map(Result::unwrap)
            .collect();
        entries.sort_by_key(|e| e.file_name());
        for e in entries {
            let path = e.path();
            let meta = std::fs::symlink_metadata(&path).unwrap();
            let (kind, data) = if meta.is_symlink() {
                (
                    "symlink",
                    std::fs::read_link(&path)
                        .unwrap()
                        .as_os_str()
                        .as_bytes()
                        .to_vec(),
                )
            } else if meta.is_dir() {
                ("directory", Vec::new())
            } else {
                ("file", std::fs::read(&path).unwrap())
            };
            nodes.push(json!({"Path":path.strip_prefix(root).unwrap().as_os_str().as_bytes(), "Kind":kind, "Data":data, "Mode":meta.permissions().mode() & 0o777}));
            if meta.is_dir() {
                walk(root, &path, nodes);
            }
        }
    }
    let mut nodes = Vec::new();
    walk(root, root, &mut nodes);
    Value::Array(nodes)
}
struct Cleanup(PathBuf);
impl Drop for Cleanup {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}
#[test]
fn project_file_effects_and_repeated_creation_match_fixed_go() {
    let fixture: Value =
        serde_json::from_str(include_str!("../contract/project-files.json")).unwrap();
    let base = std::env::temp_dir().join(format!("beans-project-files-{}", std::process::id()));
    std::fs::create_dir(&base).unwrap();
    let _clean = Cleanup(base.clone());
    let cases = fixture["cases"].as_array().unwrap();
    assert_eq!(cases.len(), 90);
    for (i, case) in cases.iter().enumerate() {
        let root = base.join(i.to_string());
        std::fs::create_dir(&root).unwrap();
        let input = &case["Input"];
        let layout = &input["Layout"];
        if let Some(dirs) = layout["Dirs"].as_array() {
            for dir in dirs {
                std::fs::DirBuilder::new()
                    .recursive(true)
                    .mode(0o755)
                    .create(root.join(dir.as_str().unwrap()))
                    .unwrap();
            }
        }
        if let Some(files) = layout["Files"].as_object() {
            for (path, data) in files {
                let path = root.join(path);
                std::fs::DirBuilder::new()
                    .recursive(true)
                    .mode(0o755)
                    .create(path.parent().unwrap())
                    .unwrap();
                use std::io::Write;
                std::fs::OpenOptions::new()
                    .write(true)
                    .create(true)
                    .truncate(true)
                    .mode(0o644)
                    .open(path)
                    .unwrap()
                    .write_all(data.as_str().unwrap().as_bytes())
                    .unwrap();
            }
        }
        if let Some(links) = layout["Links"].as_object() {
            for (path, target) in links {
                let path = root.join(path);
                std::fs::DirBuilder::new()
                    .recursive(true)
                    .mode(0o755)
                    .create(path.parent().unwrap())
                    .unwrap();
                std::os::unix::fs::symlink(target.as_str().unwrap(), path).unwrap();
            }
        }
        for stage in case["Stages"].as_array().unwrap() {
            let result =
                create_project_files(&root, &bytes(&input["Name"]), &bytes(&input["Remote"]));
            let (paths, error) = match result {
                Ok(paths) => (paths, String::new()),
                Err(e) => (
                    None,
                    e.to_string().replace(&format!("{}/", root.display()), ""),
                ),
            };
            assert_eq!(json!(paths), stage["Paths"], "case{i} paths");
            assert_eq!(error, stage["Error"].as_str().unwrap(), "case{i} error");
            assert_eq!(snapshot(&root), stage["Tree"], "case{i} filesystem effects");
        }
    }
}
