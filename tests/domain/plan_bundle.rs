use beans::domain::{
    issue::Timestamp,
    plan::{self, Bundle, BundleSnapshot, Section, YamlString},
};
use serde_json::{Value, json};
use std::os::unix::{
    ffi::{OsStrExt, OsStringExt},
    fs::{PermissionsExt, symlink},
};
use std::{
    collections::BTreeMap,
    fs,
    io::Write,
    path::{Path, PathBuf},
    process::{Command, Stdio},
};

struct Scratch(PathBuf);
impl Scratch {
    fn new() -> Self {
        let mut nonce = [0; 16];
        getrandom::fill(&mut nonce).unwrap();
        let path =
            std::env::temp_dir().join(format!("beans-bundle-{:x}", u128::from_ne_bytes(nonce)));
        fs::create_dir(&path).unwrap();
        Self(path)
    }
    fn root(&self) -> PathBuf {
        self.0.join("bundle")
    }
}
impl Drop for Scratch {
    fn drop(&mut self) {
        fs::remove_dir_all(&self.0).unwrap();
    }
}
fn fixture() -> Value {
    serde_json::from_str(include_str!("../contract/plan-bundle.json")).unwrap()
}
fn now() -> Timestamp {
    Timestamp {
        seconds: 1789128000,
        nanoseconds: 0,
        offset_seconds: 0,
    }
}
fn digest(bytes: &[u8]) -> Value {
    // CI and product qualification are Linux-only; coreutils supplies the
    // test hash without adding a hashing library to the product or lockfile.
    let mut child = Command::new("sha256sum")
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .unwrap();
    child.stdin.take().unwrap().write_all(bytes).unwrap();
    let output = child.wait_with_output().unwrap();
    assert!(output.status.success());
    let output = String::from_utf8(output.stdout).unwrap();
    json!({"size":bytes.len(),"sha256":output.split_whitespace().next().unwrap()})
}
fn sections(xs: &[Section]) -> Value {
    json!(
        xs.iter()
            .map(|s| json!({"path":s.path.as_bytes(),"data":digest(s.markdown.as_bytes())}))
            .collect::<Vec<_>>()
    )
}
fn files(snapshot: &BundleSnapshot) -> Value {
    json!(
        snapshot
            .paths()
            .iter()
            .map(|p| json!({"path":p.as_bytes(),"data":digest(&snapshot.files[p])}))
            .collect::<Vec<_>>()
    )
}
fn view(bundle: &Bundle, prefix: &str) -> Value {
    let p = bundle.plan.as_ref().unwrap();
    json!({"root":bundle.root.strip_prefix(prefix).unwrap_or(&bundle.root),"id":p.id,"title":p.title,"slug":p.slug,"status":p.status,"path":p.path.strip_prefix(prefix).unwrap_or(&p.path),"body":digest(p.body.as_bytes()),"sections":sections(&bundle.sections),"section_bodies":sections(&p.section_bodies),"snapshot":files(&bundle.snapshot())})
}
fn materialize(row: &Value, base: &str) -> BundleSnapshot {
    let mut snapshot = BundleSnapshot::default();
    for file in row["files"].as_array().into_iter().flatten() {
        let name: Vec<u8> = serde_json::from_value(file["path"].clone()).unwrap();
        let bytes = if let Some(s) = file["text"].as_str() {
            s.as_bytes().to_vec()
        } else if let Some(size) = file["repeat"].as_u64() {
            let mut bytes = vec![b'x'; size as usize];
            if let Some(last) = bytes.last_mut() {
                *last = b'\n';
            }
            bytes
        } else if let Some(size) = file["plan_size"].as_u64() {
            let mut bytes = format!("{base}\n## Additional\n").into_bytes();
            bytes.resize(size as usize - 1, b'x');
            bytes.push(b'\n');
            bytes
        } else {
            serde_json::from_value(file["bytes"].clone()).unwrap()
        };
        snapshot.files.insert(YamlString::from_bytes(name), bytes);
    }
    snapshot
}
#[test]
fn snapshot_loading_matches_go_boundaries_paths_and_order() {
    let corpus = fixture();
    let mut exports = Vec::new();
    for row in corpus["loads"].as_array().unwrap() {
        let snapshot = materialize(row, corpus["scaffold"].as_str().unwrap());
        let before = snapshot.clone();
        let result = plan::load_snapshot(row["root"].as_str().unwrap(), &snapshot);
        match result {
            Ok(bundle) => {
                assert_eq!(row["error"], "");
                assert_eq!(view(&bundle, ""), row["bundle"], "{}", row["name"]);
                let snapshot = bundle.snapshot();
                let exported_files = snapshot.files.iter().map(|(name,bytes)|json!({"path":name.as_bytes(),"input":std::str::from_utf8(bytes).unwrap()})).collect::<Vec<_>>();
                exports.push(json!({"name":row["name"],"root":row["root"],"files":exported_files}));
                match plan::load_snapshot(row["root"].as_str().unwrap(), &snapshot) {
                    Ok(reread) => {
                        assert_eq!(row["read_error"], "");
                        assert_eq!(view(&reread, ""), row["read_bundle"]);
                    }
                    Err(e) => assert_eq!(e.to_string(), row["read_error"]),
                }
            }
            Err(error) => {
                assert!(row["bundle"].is_null());
                assert_eq!(error.to_string(), row["error"], "{}", row["name"]);
            }
        }
        assert_eq!(snapshot, before, "loader mutated input: {}", row["name"]);
    }
    if let Ok(path) = std::env::var("BN_PLAN_BUNDLE_RUST_OUTPUT") {
        fs::write(path, serde_json::to_vec(&exports).unwrap()).unwrap();
    }
    for row in corpus["paths"].as_array().unwrap() {
        let bytes: Vec<u8> = serde_json::from_value(row["bytes"].clone()).unwrap();
        assert_eq!(
            plan::valid_section_path(&bytes),
            row["valid"].as_bool().unwrap(),
            "{}",
            row["name"]
        );
    }
}

fn tree(path: &Path) -> BTreeMap<Vec<u8>, (u32, Vec<u8>)> {
    fn walk(path: &Path, out: &mut BTreeMap<Vec<u8>, (u32, Vec<u8>)>) {
        let Ok(meta) = fs::symlink_metadata(path) else {
            return;
        };
        let data = if meta.file_type().is_symlink() {
            fs::read_link(path).unwrap().as_os_str().as_bytes().to_vec()
        } else if meta.is_file() {
            fs::read(path).unwrap()
        } else {
            Vec::new()
        };
        out.insert(
            path.as_os_str().as_bytes().to_vec(),
            (meta.permissions().mode(), data),
        );
        if meta.is_dir() {
            for child in fs::read_dir(path).unwrap() {
                walk(&child.unwrap().path(), out);
            }
        }
    }
    let mut out = BTreeMap::new();
    walk(path, &mut out);
    out
}
fn make_fs(kind: &str, root: &Path, base: &[u8]) {
    if kind != "missing" {
        fs::create_dir(root).unwrap();
        fs::write(root.join("plan.md"), base).unwrap();
    }
    match kind {
        "plan-missing" => fs::remove_file(root.join("plan.md")).unwrap(),
        "plan-dir" => {
            fs::remove_file(root.join("plan.md")).unwrap();
            fs::create_dir(root.join("plan.md")).unwrap();
        }
        "plan-link" => {
            fs::remove_file(root.join("plan.md")).unwrap();
            symlink("absent", root.join("plan.md")).unwrap();
        }
        "section-link" => {
            fs::create_dir(root.join("sections")).unwrap();
            symlink("../plan.md", root.join("sections/x.md")).unwrap();
        }
        "root-file" => {
            fs::remove_dir_all(root).unwrap();
            fs::write(root, b"root").unwrap();
        }
        "root-link" => {
            let target = root.with_file_name("bundle-target");
            fs::rename(root, target).unwrap();
            symlink("bundle-target", root).unwrap();
        }
        "extra-dir" => fs::create_dir(root.join("other")).unwrap(),
        "nested-dir" => fs::create_dir_all(root.join("sections/nested")).unwrap(),
        "extra-file" => fs::write(root.join("extra.txt"), b"x").unwrap(),
        "section-extension" => {
            fs::create_dir(root.join("sections")).unwrap();
            fs::write(root.join("sections/x.txt"), b"x").unwrap();
        }
        "symlink" => symlink("plan.md", root.join("link.md")).unwrap(),
        "dangling-link" => symlink("absent", root.join("link.md")).unwrap(),
        "fifo" => assert!(
            Command::new("mkfifo")
                .args(["-m", "600"])
                .arg(root.join("pipe.md"))
                .status()
                .unwrap()
                .success()
        ),
        "oversize" => fs::write(root.join("plan.md"), vec![b'x'; plan::MAX_FILE_SIZE + 1]).unwrap(),
        "unlisted" => {
            fs::create_dir(root.join("sections")).unwrap();
            fs::write(root.join("sections/x.md"), b"x\n").unwrap();
        }
        "empty-sections" => fs::create_dir(root.join("sections")).unwrap(),
        "lexical-first" => {
            fs::write(root.join("a.txt"), b"x").unwrap();
            fs::create_dir(root.join("z")).unwrap();
        }
        "missing" | "draft" => {}
        _ => panic!("unknown filesystem fixture {kind}"),
    }
}
#[test]
fn linux_filesystem_loading_matches_go_and_never_mutates_files() {
    let corpus = fixture();
    for row in corpus["filesystem"].as_array().unwrap() {
        let scratch = Scratch::new();
        let root = scratch.root();
        make_fs(
            row["kind"].as_str().unwrap(),
            &root,
            corpus["scaffold"].as_str().unwrap().as_bytes(),
        );
        let before = tree(&scratch.0);
        let prefix = format!("{}/", scratch.0.display());
        match plan::load(root.to_str().unwrap()) {
            Ok(bundle) => {
                assert_eq!(row["error"], "");
                assert_eq!(view(&bundle, &prefix), row["bundle"], "{}", row["kind"]);
            }
            Err(error) => assert_eq!(
                error.to_string().replace(&prefix, ""),
                row["error"],
                "{}",
                row["kind"]
            ),
        }
        assert_eq!(tree(&scratch.0), before);
    }
}
#[test]
fn exclusive_scaffold_writes_match_go_and_preserve_existing_destinations() {
    let corpus = fixture();
    for row in corpus["writes"].as_array().unwrap() {
        let scratch = Scratch::new();
        let mut root = scratch.root();
        let mut title = "x";
        match row["kind"].as_str().unwrap() {
            "existing-dir" => {
                fs::create_dir(&root).unwrap();
                fs::write(root.join("sentinel"), b"keep").unwrap();
            }
            "existing-file" => fs::write(&root, b"keep").unwrap(),
            "existing-link" => symlink("absent", &root).unwrap(),
            "parent-missing" => root = root.join("child"),
            "special-title" => title = "Ship: phase #1 \"quoted\"",
            "new" => {}
            _ => unreachable!(),
        }
        let before = tree(&scratch.0);
        let prefix = format!("{}/", scratch.0.display());
        let result = plan::write_scaffold(root.to_str().unwrap(), "beans-plan-a3f2", title, &now());
        let error = result
            .as_ref()
            .err()
            .map(ToString::to_string)
            .unwrap_or_default()
            .replace(&prefix, "");
        assert_eq!(error, row["error"], "{}", row["kind"]);
        assert_eq!(
            String::from_utf8(fs::read(root.join("plan.md")).unwrap_or_default()).unwrap(),
            row["plan"]
        );
        assert_eq!(
            String::from_utf8(fs::read(root.join("sentinel")).unwrap_or_default()).unwrap(),
            row["sentinel"]
        );
        if result.is_err() {
            assert_eq!(tree(&scratch.0), before);
        }
    }
}

#[test]
fn scaffold_loads_as_draft() {
    // Original TestScaffoldLoadsAsDraft.
    let scratch = Scratch::new();
    let root = scratch.root();
    plan::write_scaffold(
        root.to_str().unwrap(),
        "beans-plan-a3f2",
        "Add plan artifacts",
        &now(),
    )
    .unwrap();
    let b = plan::load(root.to_str().unwrap()).unwrap();
    let p = b.plan.unwrap();
    assert_eq!(p.id, "beans-plan-a3f2");
    assert_eq!(p.slug, "add-plan-artifacts");
    assert_eq!(p.status, plan::DRAFT);
}
#[test]
fn scaffold_safely_encodes_special_title() {
    // Original TestScaffoldSafelyEncodesSpecialTitle.
    let scratch = Scratch::new();
    let root = scratch.root();
    let title = "Ship: phase #1 \"quoted\"";
    plan::write_scaffold(root.to_str().unwrap(), "beans-plan-a3f2", title, &now()).unwrap();
    assert_eq!(
        plan::load(root.to_str().unwrap())
            .unwrap()
            .plan
            .unwrap()
            .title,
        title
    );
}
#[test]
fn load_retains_ordered_section_bodies() {
    // Original TestLoadRetainsOrderedSectionBodies.
    let scratch = Scratch::new();
    let root = scratch.root();
    plan::write_scaffold(root.to_str().unwrap(), "beans-plan-a3f2", "x", &now()).unwrap();
    let file = root.join("plan.md");
    let data = fs::read_to_string(&file).unwrap().replacen(
        "updated:",
        "sections:\n  - sections/02.md\n  - sections/01.md\nupdated:",
        1,
    );
    fs::write(file, data).unwrap();
    fs::create_dir(root.join("sections")).unwrap();
    fs::write(root.join("sections/01.md"), "# First\n").unwrap();
    fs::write(root.join("sections/02.md"), "# Second\n").unwrap();
    let b = plan::load(root.to_str().unwrap()).unwrap();
    let sections = b.plan.unwrap().section_bodies;
    assert_eq!(sections.len(), 2);
    assert_eq!(sections[0].path, YamlString::from("sections/02.md"));
    assert_eq!(sections[1].markdown, "# First\n");
}
#[test]
fn load_rejects_unlisted_section() {
    // Original TestLoadRejectsUnlistedSection.
    let scratch = Scratch::new();
    let root = scratch.root();
    plan::write_scaffold(root.to_str().unwrap(), "beans-plan-a3f2", "x", &now()).unwrap();
    fs::create_dir(root.join("sections")).unwrap();
    fs::write(root.join("sections/extra.md"), "# extra\n").unwrap();
    assert!(plan::load(root.to_str().unwrap()).is_err());
}

#[test]
fn accepted_snapshot_raw_names_can_be_loaded_from_linux() {
    let corpus = fixture();
    let row = corpus["loads"]
        .as_array()
        .unwrap()
        .iter()
        .find(|r| r["name"] == "binary-path")
        .unwrap();
    let snapshot = materialize(row, corpus["scaffold"].as_str().unwrap());
    let scratch = Scratch::new();
    let root = scratch.root();
    fs::create_dir(&root).unwrap();
    fs::create_dir(root.join("sections")).unwrap();
    for (name, bytes) in &snapshot.files {
        fs::write(
            root.join(std::ffi::OsString::from_vec(name.as_bytes().into())),
            bytes,
        )
        .unwrap();
    }
    let before = tree(&root);
    let b = plan::load(root.to_str().unwrap()).unwrap();
    let prefix = format!("{}/", scratch.0.display());
    assert_eq!(view(&b, &prefix), row["bundle"]);
    assert_eq!(tree(&root), before);
}

#[test]
fn filesystem_size_boundaries_match_go_and_leave_all_bytes_unchanged() {
    let corpus = fixture();
    for row in corpus["filesystem_bounds"].as_array().unwrap() {
        let scratch = Scratch::new();
        let root = scratch.root();
        fs::create_dir(&root).unwrap();
        let snapshot = materialize(row, corpus["scaffold"].as_str().unwrap());
        for (name, bytes) in &snapshot.files {
            let full = root.join(std::ffi::OsString::from_vec(name.as_bytes().into()));
            fs::create_dir_all(full.parent().unwrap()).unwrap();
            fs::write(full, bytes).unwrap();
        }
        let before = tree(&root);
        let prefix = format!("{}/", scratch.0.display());
        match plan::load(root.to_str().unwrap()) {
            Ok(bundle) => {
                assert_eq!(row["error"], "");
                assert_eq!(view(&bundle, &prefix), row["bundle"], "{}", row["name"]);
            }
            Err(e) => assert_eq!(
                e.to_string().replace(&prefix, ""),
                row["error"],
                "{}",
                row["name"]
            ),
        }
        assert_eq!(tree(&root), before);
    }
}

#[test]
fn snapshot_ignores_encode_errors_and_keeps_last_duplicate_path() {
    let corpus = fixture();
    for row in corpus["snapshots"].as_array().unwrap() {
        let p = if row["status"] == "nil" {
            None
        } else {
            let mut p =
                plan::parse("plan.md", corpus["scaffold"].as_str().unwrap().as_bytes()).unwrap();
            p.status = row["status"].as_str().unwrap().into();
            Some(p)
        };
        let sections = row["sections"]
            .as_array()
            .unwrap()
            .iter()
            .map(|s| Section {
                path: YamlString::from_bytes(serde_json::from_value(s["path"].clone()).unwrap()),
                markdown: s["text"].as_str().unwrap().into(),
            })
            .collect();
        let bundle = Bundle {
            plan: p,
            sections,
            root: "unused".into(),
        };
        assert_eq!(files(&bundle.snapshot()), row["files"]);
    }
}
