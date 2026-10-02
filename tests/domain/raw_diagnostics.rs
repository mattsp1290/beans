use beans::{
    domain::{
        config::{load_hub_config, load_project_config, load_user_config},
        workflow::load_workflow,
    },
    gitops::write_file,
};
use serde_json::Value;
use std::{
    ffi::OsString,
    io::Write,
    os::unix::{
        ffi::{OsStrExt, OsStringExt},
        fs::{DirBuilderExt, OpenOptionsExt},
    },
    path::PathBuf,
};
fn normalize(mut bytes: Vec<u8>, root: &[u8]) -> Vec<u8> {
    let prefix = [root, b"/"].concat();
    while let Some(i) = bytes.windows(prefix.len()).position(|w| w == prefix) {
        bytes.drain(i..i + prefix.len());
    }
    let pattern = b".bn-write-";
    let mut cursor = 0;
    while let Some(i) = bytes[cursor..]
        .windows(pattern.len())
        .position(|w| w == pattern)
    {
        let start = cursor + i;
        let mut end = start + pattern.len();
        while bytes.get(end).is_some_and(u8::is_ascii_digit) {
            end += 1;
        }
        if end > start + pattern.len() {
            bytes.splice(start..end, b".bn-write-TEMP".iter().copied());
        }
        cursor = start + pattern.len();
    }
    bytes
}
struct Cleanup(PathBuf);
impl Drop for Cleanup {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}
#[test]
fn canonical_diagnostic_bytes_and_write_effects_match_committed_contract() {
    let corpus: Value =
        serde_json::from_str(include_str!("../contract/raw-diagnostics.json")).unwrap();
    let cases = corpus["cases"].as_array().unwrap();
    assert_eq!(cases.len(), 4257);
    let base = std::env::temp_dir().join(format!("beans-raw-diagnostics-{}", std::process::id()));
    std::fs::create_dir(&base).unwrap();
    let _cleanup = Cleanup(base.clone());
    let mut mismatches = Vec::new();
    for (i, case) in cases.iter().enumerate() {
        let root = base.join(i.to_string());
        std::fs::create_dir(&root).unwrap();
        let input = &case["Input"];
        let name: Vec<u8> = serde_json::from_value(input["Name"].clone()).unwrap();
        let kind = input["Kind"].as_str().unwrap();
        let action = input["Action"].as_str().unwrap();
        let mut path = root.join(OsString::from_vec(name.clone()));
        if !name.contains(&0) {
            std::fs::DirBuilder::new()
                .recursive(true)
                .mode(0o755)
                .create(path.parent().unwrap())
                .unwrap();
            let write = |path: &std::path::Path, data: &[u8]| {
                std::fs::OpenOptions::new()
                    .write(true)
                    .create(true)
                    .truncate(true)
                    .mode(0o644)
                    .open(path)
                    .unwrap()
                    .write_all(data)
                    .unwrap();
            };
            match kind {
                "directory" => std::fs::DirBuilder::new()
                    .mode(0o755)
                    .create(&path)
                    .unwrap(),
                "malformed" => write(&path, b"name=["),
                "vocabulary" => write(&path, b"[workflow]\ndefault='absent'\n"),
                "existing" => write(&path, b"old user data"),
                "symlink" => {
                    write(&root.join("target"), b"unrelated target");
                    let target = if path.parent().unwrap() == root {
                        "target"
                    } else {
                        "../target"
                    };
                    std::os::unix::fs::symlink(target, &path).unwrap();
                }
                "parent-file" => {
                    std::fs::DirBuilder::new()
                        .recursive(true)
                        .mode(0o755)
                        .create(&path)
                        .unwrap();
                    write(&path.join("blocker"), b"user file");
                    path = path.join("blocker/leaf");
                }
                "hub-malformed" | "hub-directory" | "project-malformed" | "projects-file" => {
                    std::fs::DirBuilder::new()
                        .recursive(true)
                        .mode(0o755)
                        .create(&path)
                        .unwrap();
                    match kind {
                        "hub-directory" => std::fs::DirBuilder::new()
                            .mode(0o755)
                            .create(path.join("beans.toml"))
                            .unwrap(),
                        "projects-file" => write(&path.join("projects"), b"user file"),
                        "project-malformed" => {
                            let config = path.join("projects/p/beans.toml");
                            std::fs::DirBuilder::new()
                                .recursive(true)
                                .mode(0o755)
                                .create(config.parent().unwrap())
                                .unwrap();
                            write(&config, b"name=[");
                        }
                        _ => write(&path.join("beans.toml"), b"name=["),
                    }
                }
                "missing" => {}
                _ => panic!("unknown layout"),
            }
        }
        let before = super::project_files::snapshot(&root);
        let result = match action {
            "user" => load_user_config(&path).map(|_| ()),
            "project" => load_project_config(&path).map(|_| ()),
            "hub" => load_hub_config(&path).map(|_| ()),
            "workflow" => load_workflow(Some(&path), &[], &[]).map(|_| ()),
            "write" => write_file(&path, b"replacement\xff\n"),
            "index" => beans::vault::Index::load(&path).map(|_| ()),
            "dirs" => beans::vault::project_dirs(&path).map(|_| ()),
            "reload" => beans::vault::Index::load(&root.join("live"))
                .and_then(|mut ix| ix.reload(std::slice::from_ref(&path))),
            _ => panic!("unknown action"),
        };
        let bytes = result.err().map_or_else(Vec::new, |e| {
            normalize(e.as_bytes().into(), root.as_os_str().as_bytes())
        });
        let expected: Vec<u8> = serde_json::from_value(case["Error"].clone()).unwrap();
        if bytes != expected {
            mismatches.push(format!(
                "case{i} {action}/{kind} {:?}: actual {:?}, Go {:?}",
                name,
                String::from_utf8_lossy(&bytes),
                String::from_utf8_lossy(&expected)
            ));
        }
        let after = super::project_files::snapshot(&root);
        if action == "write" {
            assert_eq!(after, case["Tree"], "case{i} effects");
        } else {
            assert_eq!(after, before, "case{i} read changed filesystem");
        }
    }
    assert!(
        mismatches.is_empty(),
        "{} raw diagnostic mismatches:\n{}",
        mismatches.len(),
        mismatches.join("\n")
    );
}
