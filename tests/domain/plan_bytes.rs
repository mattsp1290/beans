use beans::domain::{
    error::Error,
    issue::Timestamp,
    plan::{
        self, Bundle, BundleSnapshot, ChangeGraph, GraphNode, Plan, Section, ValidationError,
        ValidationIssue, YamlString,
    },
};
use serde_json::{Value, json};
use std::{
    ffi::{CString, OsString},
    io::Write,
    os::unix::{
        ffi::{OsStrExt, OsStringExt},
        fs::{DirBuilderExt, FileTypeExt, OpenOptionsExt, PermissionsExt},
    },
    path::{Path, PathBuf},
};
fn bytes(v: &Value) -> Vec<u8> {
    serde_json::from_value(v.clone()).unwrap()
}
fn normalize(raw: &[u8], root: &Path) -> Vec<u8> {
    let needle = root.as_os_str().as_bytes();
    let mut rest = raw;
    let mut out = Vec::new();
    while let Some(i) = rest.windows(needle.len()).position(|w| w == needle) {
        out.extend_from_slice(&rest[..i]);
        out.extend_from_slice(b"/oracle");
        rest = &rest[i + needle.len()..];
    }
    out.extend_from_slice(rest);
    out
}
fn sections(v: &[Section]) -> Value {
    json!(
        v.iter()
            .map(|s| json!({"path":s.path.as_bytes(),"markdown":s.markdown}))
            .collect::<Vec<_>>()
    )
}
fn plan_view(p: &Plan, root: Option<&Path>) -> Value {
    let path = root.map_or_else(
        || p.path.as_bytes().to_vec(),
        |r| normalize(p.path.as_bytes(), r),
    );
    json!({"path":path,"json_path":YamlString::from_bytes(path.clone()),"body":p.body,"graph":p.graph,"sections":p.sections.iter().map(YamlString::as_bytes).collect::<Vec<_>>(),"section_bodies":sections(&p.section_bodies)})
}
fn bundle_view(b: &Bundle, root: Option<&Path>) -> Value {
    let name = root.map_or_else(
        || b.root.as_bytes().to_vec(),
        |r| normalize(b.root.as_bytes(), r),
    );
    json!({"root":name,"plan":plan_view(b.plan.as_ref().unwrap(),root),"sections":sections(&b.sections)})
}
fn filesystem(root: &Path) -> Value {
    fn walk(root: &Path, dir: &Path, entries: &mut Vec<Value>) {
        let mut children = std::fs::read_dir(dir)
            .unwrap()
            .map(Result::unwrap)
            .collect::<Vec<_>>();
        children.sort_by_key(|e| e.file_name());
        for child in children {
            let full = child.path();
            let info = std::fs::symlink_metadata(&full).unwrap();
            let ty = info.file_type();
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
            } else if ty.is_fifo() {
                ("fifo", vec![])
            } else {
                ("file", std::fs::read(&full).unwrap())
            };
            entries.push(json!({"Path":full.strip_prefix(root).unwrap().as_os_str().as_bytes(),"Data":data,"Kind":kind,"Mode":info.permissions().mode() & 0o777}));
            if ty.is_dir() {
                walk(root, &full, entries)
            }
        }
    }
    let mut entries = Vec::new();
    walk(root, root, &mut entries);
    json!(entries)
}
struct Scratch(PathBuf);
impl Scratch {
    fn new() -> Self {
        let mut nonce = [0; 16];
        getrandom::fill(&mut nonce).unwrap();
        let path =
            std::env::temp_dir().join(format!("beans-plan-bytes-{:x}", u128::from_ne_bytes(nonce)));
        std::fs::create_dir(&path).unwrap();
        Self(path)
    }
}
impl Drop for Scratch {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}
fn setup(root: &Path, files: &Value) {
    for f in files.as_array().into_iter().flatten() {
        // Go's setup uses filepath.Join; the production load keeps the raw root.
        let relative = bytes(&f["Path"]);
        let mut full = root.to_path_buf();
        for part in relative.split(|&b| b == b'/') {
            match part {
                b"" | b"." => (),
                b".." => {
                    assert!(full.pop());
                }
                _ => full.push(OsString::from_vec(part.into())),
            }
        }
        std::fs::DirBuilder::new()
            .recursive(true)
            .mode(0o755)
            .create(full.parent().unwrap())
            .unwrap();
        match f["Kind"].as_str().unwrap() {
            "directory" => std::fs::DirBuilder::new()
                .recursive(true)
                .mode(0o755)
                .create(&full)
                .unwrap(),
            "symlink" => {
                std::os::unix::fs::symlink(OsString::from_vec(bytes(&f["Data"])), &full).unwrap()
            }
            "fifo" => {
                let name = CString::new(full.as_os_str().as_bytes()).unwrap(); // SAFETY: name is NUL-terminated and lives through the call.
                assert_eq!(unsafe { libc::mkfifo(name.as_ptr(), 0o600) }, 0);
            }
            _ => {
                let mut out = std::fs::OpenOptions::new()
                    .create(true)
                    .truncate(true)
                    .write(true)
                    .mode(0o644)
                    .open(&full)
                    .unwrap();
                out.write_all(&bytes(&f["Data"])).unwrap();
            }
        }
    }
}
// Fixture storage shares long values; expansion clones bytes without Unicode conversion.
fn expand(value: &Value, blobs: &[Value]) -> Value {
    match value {
        Value::Object(map) if map.len() == 1 && map.contains_key("blob") => {
            blobs[map["blob"].as_u64().unwrap() as usize].clone()
        }
        Value::Object(map) => Value::Object(
            map.iter()
                .map(|(k, v)| (k.clone(), expand(v, blobs)))
                .collect(),
        ),
        Value::Array(values) => Value::Array(values.iter().map(|v| expand(v, blobs)).collect()),
        _ => value.clone(),
    }
}
#[test]
fn canonical_plan_models_diagnostics_and_filesystems_match_committed_contract() {
    let fixture: Value = serde_json::from_str(include_str!("../contract/plan-bytes.json")).unwrap();
    let blobs = fixture["blobs"].as_array().unwrap();
    for (i, c) in fixture["cases"].as_array().unwrap().iter().enumerate() {
        let c = expand(c, blobs);
        let root = bytes(&c["Root"]);
        let data = bytes(&c["Data"]);
        let mut result = Value::Null;
        let mut scratch = None;
        let outcome: Result<(), Error> = match c["Operation"].as_str().unwrap() {
            "snapshot" => {
                let mut files = BundleSnapshot::default();
                for f in c["Files"].as_array().into_iter().flatten() {
                    let size = f["Size"].as_u64().unwrap();
                    let mut data = bytes(&f["Data"]);
                    if size > 0 {
                        data = vec![b'x'; size as usize];
                        *data.last_mut().unwrap() = b'\n'
                    }
                    files
                        .files
                        .insert(YamlString::from_bytes(bytes(&f["Path"])), data);
                }
                plan::load_snapshot_bytes(&root, &files).map(|b| result = bundle_view(&b, None))
            }
            "parse" => plan::parse_bytes(&root, &data).map(|p| result = plan_view(&p, None)),
            "graph" => match plan::parse_graph_bytes(&root, std::str::from_utf8(&data).unwrap()) {
                Ok(g) => {
                    result = json!(g);
                    Ok(())
                }
                Err(e) => {
                    result = json!(e.graph);
                    Err(e.diagnostic())
                }
            },
            op @ ("reference" | "reference_valid") => {
                let mut p = Plan {
                    path: YamlString::from_bytes(root.clone()),
                    status: plan::DRAFT.into(),
                    body: String::from_utf8(data).unwrap(),
                    graph: ChangeGraph {
                        version: 1,
                        nodes: Some(vec![GraphNode {
                            id: "node".into(),
                            label: "x".into(),
                            kind: if op == "reference_valid" {
                                "component"
                            } else {
                                "task"
                            }
                            .into(),
                            ..Default::default()
                        }]),
                        ..Default::default()
                    },
                    ..Default::default()
                };
                let outcome =
                    plan::set_node_ref(Some(&mut p), "node", "[[beans-a1b2]]").map(|_| ());
                result = plan_view(&p, None);
                outcome
            }
            "validation" => {
                let e = ValidationError {
                    issues: vec![ValidationIssue {
                        path: YamlString::from_bytes(root.clone()),
                        line: 3,
                        code: YamlString::from_bytes(bytes(&c["Name"])),
                        message: YamlString::from_bytes(data),
                    }],
                };
                result = json!(e.issues);
                Err(e.diagnostic())
            }
            op @ ("disk" | "scaffold") => {
                let s = Scratch::new();
                setup(&s.0, &c["Files"]);
                assert_eq!(filesystem(&s.0), c["Before"], "case{i} setup");
                let full = PathBuf::from(OsString::from_vec(
                    [s.0.as_os_str().as_bytes(), b"/", &root].concat(),
                ));
                let outcome = if op == "disk" {
                    plan::load_path(&full).map(|b| result = bundle_view(&b, Some(&s.0)))
                } else {
                    let now = Timestamp {
                        seconds: 1789128000,
                        nanoseconds: 0,
                        offset_seconds: 0,
                    };
                    plan::write_scaffold_path(&full, "beans-plan-a3f2", "x", &now)
                };
                assert_eq!(filesystem(&s.0), c["After"], "case{i} after");
                scratch = Some(s);
                outcome
            }
            _ => unreachable!(),
        };
        let error = outcome.err().map_or_else(Vec::new, |e| {
            scratch
                .as_ref()
                .map_or_else(|| e.as_bytes().to_vec(), |s| normalize(e.as_bytes(), &s.0))
        });
        assert_eq!(
            error,
            bytes(&c["Error"]),
            "case{i} {} diagnostic",
            c["Operation"]
        );
        assert_eq!(result, c["Result"], "case{i} {} model", c["Operation"]);
    }
}
