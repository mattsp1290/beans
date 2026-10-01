use beans::{
    domain::frontmatter::Error,
    vault::{GitCapture, GitResolver, ResolveOptions, Resolved, project_dirs, resolve},
};
use serde_json::Value;
use std::{
    cell::RefCell,
    collections::BTreeMap,
    os::unix::ffi::OsStrExt,
    path::{Path, PathBuf},
};
struct Fake<'a> {
    input: &'a Value,
    calls: RefCell<Vec<&'static str>>,
}
impl Fake<'_> {
    fn cap(&self, field: &'static str, key: &str) -> GitCapture {
        self.calls.borrow_mut().push(field);
        let value = self.input[key].as_str().unwrap().as_bytes().to_vec();
        GitCapture {
            found: !value.is_empty(),
            value,
            error: Some(Error("ignored capture error".into())),
        }
    }
}
impl GitResolver for Fake<'_> {
    fn toplevel(&self, _: &Path) -> GitCapture {
        self.cap("toplevel", "Root")
    }
    fn remote_url(&self, _: &Path) -> GitCapture {
        self.cap("remote", "Remote")
    }
    fn head_commit(&self, _: &Path) -> GitCapture {
        self.cap("head", "Head")
    }
    fn branch(&self, _: &Path) -> GitCapture {
        self.cap("branch", "Branch")
    }
}
fn snapshot(root: &Path) -> BTreeMap<PathBuf, Vec<u8>> {
    fn walk(root: &Path, dir: &Path, out: &mut BTreeMap<PathBuf, Vec<u8>>) {
        for entry in std::fs::read_dir(dir).unwrap() {
            let entry = entry.unwrap();
            let path = entry.path();
            let kind = entry.file_type().unwrap();
            let data = if kind.is_symlink() {
                std::fs::read_link(&path)
                    .unwrap()
                    .as_os_str()
                    .as_bytes()
                    .to_vec()
            } else if kind.is_dir() {
                walk(root, &path, out);
                b"directory".to_vec()
            } else {
                std::fs::read(&path).unwrap()
            };
            out.insert(path.strip_prefix(root).unwrap().into(), data);
        }
    }
    let mut out = BTreeMap::new();
    walk(root, root, &mut out);
    out
}
#[test]
fn project_resolution_and_directory_listing_match_fixed_go_without_writes() {
    let corpus: Value = serde_json::from_str(include_str!("../contract/resolve.json")).unwrap();
    let base = std::env::temp_dir().join(format!("beans-resolve-{}", std::process::id()));
    std::fs::create_dir(&base).unwrap();
    struct Cleanup(PathBuf);
    impl Drop for Cleanup {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }
    let _cleanup = Cleanup(base.clone());
    for (i, case) in corpus["cases"].as_array().unwrap().iter().enumerate() {
        let hub = base.join(i.to_string());
        std::fs::create_dir(&hub).unwrap();
        let input = &case["Input"];
        if let Some(dirs) = input["Dirs"].as_array() {
            for dir in dirs {
                std::fs::create_dir_all(hub.join(dir.as_str().unwrap())).unwrap();
            }
        }
        if let Some(files) = input["Files"].as_object() {
            for (name, contents) in files {
                let path = hub.join(name);
                std::fs::create_dir_all(path.parent().unwrap()).unwrap();
                std::fs::write(path, contents.as_str().unwrap()).unwrap();
            }
        }
        if let Some(links) = input["Links"].as_object() {
            for (name, target) in links {
                std::os::unix::fs::symlink(target.as_str().unwrap(), hub.join(name)).unwrap();
            }
        }
        let before = snapshot(&hub);
        let git = Fake {
            input,
            calls: RefCell::new(Vec::new()),
        };
        let env = |_: &str| input["Env"].as_str().unwrap().as_bytes().to_vec();
        let actual = resolve(
            &hub,
            ResolveOptions {
                cwd: Some(Path::new("/working")),
                flag_project: input["Flag"].as_str().unwrap().as_bytes(),
                write: input["Write"].as_bool().unwrap(),
                all_projects: input["All"].as_bool().unwrap(),
                git: Some(&git),
                env: Some(&env),
            },
        );
        let normalize = |s: String| s.replace(hub.to_str().unwrap(), "/oracle/hub");
        let result = if case["Error"] == "" {
            actual.unwrap_or_else(|e| panic!("case{i}: {e}"))
        } else {
            assert_eq!(
                normalize(actual.unwrap_err().to_string()),
                case["Error"].as_str().unwrap(),
                "case{i}"
            );
            Resolved::default()
        };
        let fields = [
            (
                "HubDir",
                normalize(result.hub_dir.to_string_lossy().into()).into_bytes(),
            ),
            ("Project", result.project),
            (
                "ProjectDir",
                normalize(result.project_dir.to_string_lossy().into()).into_bytes(),
            ),
            ("RepoRoot", result.repo_root),
            ("RepoRemote", result.repo_remote),
            ("RepoHead", result.repo_head),
            ("RepoBranch", result.repo_branch),
            ("Candidate", result.candidate),
        ];
        for (key, value) in fields {
            let expected: Vec<u8> = serde_json::from_value(case["Fields"][key].clone()).unwrap();
            assert_eq!(value, expected, "case{i} {key}");
        }
        assert_eq!(result.created, case["Created"].as_bool().unwrap());
        assert_eq!(result.notice, case["Notice"].as_str().unwrap());
        let calls: Vec<String> = serde_json::from_value(case["Calls"].clone()).unwrap();
        assert_eq!(*git.calls.borrow(), calls);
        let names = project_dirs(&hub);
        if case["DirsError"] == "" {
            let expected: Option<Vec<String>> =
                serde_json::from_value(case["Names"].clone()).unwrap();
            assert_eq!(
                names.unwrap().map(|v| v
                    .into_iter()
                    .map(|n| String::from_utf8(n).unwrap())
                    .collect::<Vec<_>>()),
                expected,
                "case{i}"
            );
        } else {
            assert_eq!(
                normalize(names.unwrap_err().to_string()),
                case["DirsError"].as_str().unwrap(),
                "case{i}"
            );
        }
        assert_eq!(
            snapshot(&hub),
            before,
            "read or write resolution changed case{i}"
        );
    }
}

#[test]
fn system_git_reads_real_repository_and_detached_head() {
    use beans::vault::SystemGit;
    let root = std::env::temp_dir().join(format!("beans-systemgit-{}", std::process::id()));
    std::fs::create_dir(&root).unwrap();
    struct Cleanup(PathBuf);
    impl Drop for Cleanup {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }
    let _cleanup = Cleanup(root.clone());
    let run = |args: &[&str]| {
        let output = std::process::Command::new("git")
            .args(args)
            .current_dir(&root)
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        output.stdout
    };
    run(&["init", "-b", "feature/x"]);
    assert!(!SystemGit.head_commit(&root).found);
    assert!(!SystemGit.remote_url(&root).found);
    run(&["config", "remote.origin.url", "git@github.com:o/exa.git"]);
    run(&[
        "-c",
        "user.name=Fixture",
        "-c",
        "user.email=fixture@example.test",
        "commit",
        "--allow-empty",
        "-m",
        "fixture",
    ]);
    assert_eq!(SystemGit.toplevel(&root).value, root.as_os_str().as_bytes());
    assert_eq!(
        SystemGit.remote_url(&root).value,
        b"git@github.com:o/exa.git"
    );
    let head = SystemGit.head_commit(&root);
    assert!(head.found);
    assert_eq!(head.value.len(), 40);
    assert_eq!(SystemGit.branch(&root).value, b"feature/x");
    run(&["checkout", "--detach"]);
    assert!(!SystemGit.branch(&root).found);
    assert!(!SystemGit.toplevel(&root.join("missing")).found);
}
