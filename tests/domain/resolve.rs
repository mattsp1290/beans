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
            error: Some(Error::new("ignored capture error".into())),
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
fn project_resolution_and_directory_listing_match_committed_contract_without_writes() {
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

struct OriginalHub {
    hub: PathBuf,
    git: Value,
    env: String,
}
impl OriginalHub {
    fn new() -> Self {
        use std::sync::atomic::{AtomicUsize, Ordering};
        static NEXT: AtomicUsize = AtomicUsize::new(0);
        let hub = std::env::temp_dir().join(format!(
            "beans-original-resolution-{}-{}",
            std::process::id(),
            NEXT.fetch_add(1, Ordering::Relaxed)
        ));
        std::fs::create_dir(&hub).unwrap();
        std::fs::create_dir(hub.join("projects")).unwrap();
        Self {
            hub,
            env: String::new(),
            git: serde_json::json!({"Root":"/code/exa","Remote":"git@github.com:o/exa.git","Head":"0123456789abcdef","Branch":"feature/x"}),
        }
    }
    fn add(&self, name: &str, config: &str) {
        let dir = self.hub.join("projects").join(name);
        std::fs::create_dir_all(&dir).unwrap();
        std::fs::write(dir.join("beans.toml"), config).unwrap();
    }
    fn resolve(&self, flag: &str, write: bool, all_projects: bool) -> Result<Resolved, Error> {
        let git = Fake {
            input: &self.git,
            calls: RefCell::new(Vec::new()),
        };
        let env = |_: &str| self.env.as_bytes().to_vec();
        resolve(
            &self.hub,
            ResolveOptions {
                cwd: Some(Path::new("/working")),
                flag_project: flag.as_bytes(),
                write,
                all_projects,
                git: Some(&git),
                env: Some(&env),
            },
        )
    }
}
impl Drop for OriginalHub {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.hub);
    }
}
#[test]
fn resolve_basename_match_original() {
    let hub = OriginalHub::new();
    hub.add("exa", "remotes=['https://github.com/o/exa']");
    let res = hub.resolve("", false, false).unwrap();
    assert_eq!(res.project, b"exa");
    assert!(!res.created);
    assert_eq!(res.repo_head, b"0123456");
    assert_eq!(res.repo_branch, b"feature/x");
    assert_eq!(res.repo_remote, b"https://github.com/o/exa");
}
#[test]
fn resolve_remote_fallback_original() {
    let hub = OriginalHub::new();
    hub.add("renamed", "remotes=['https://github.com/o/exa']");
    assert_eq!(hub.resolve("", false, false).unwrap().project, b"renamed");
}
#[test]
fn resolve_collision_errors_original() {
    let hub = OriginalHub::new();
    hub.add("exa", "remotes=['https://github.com/other/exa']");
    assert!(
        hub.resolve("", false, false)
            .unwrap_err()
            .to_string()
            .contains("projects/exa belongs to https://github.com/other/exa")
    );
}
#[test]
fn resolve_outside_git_original() {
    let mut hub = OriginalHub::new();
    hub.git["Root"] = Value::String(String::new());
    assert_eq!(
        hub.resolve("", false, false).unwrap_err().to_string(),
        beans::vault::OUTSIDE_REPO
    );
    assert!(hub.resolve("", false, true).unwrap().project.is_empty());
}
#[test]
fn resolve_overrides_original() {
    let mut hub = OriginalHub::new();
    hub.add("flagged", "name='flagged'");
    let res = hub.resolve("flagged", false, false).unwrap();
    assert_eq!(res.project, b"flagged");
    assert!(!res.created);
    hub.env = "flagged".into();
    assert_eq!(hub.resolve("", false, false).unwrap().project, b"flagged");
    hub.env.clear();
    assert!(hub.resolve("nope", false, false).is_err());
    let res = hub.resolve("nope", true, false).unwrap();
    assert!(res.created);
    assert_eq!(res.project, b"nope");
    assert!(!hub.hub.join("projects/nope").exists());
    assert!(hub.resolve("Bad Name", false, false).is_err());
}

#[test]
fn resolve_auto_create_on_write_not_read_original() {
    use beans::vault::{create_project_files, project_dirs};
    let mut hub = OriginalHub::new();
    hub.git["Root"] = Value::String("/code/My Repo".into());
    hub.git["Remote"] = Value::String("https://github.com/o/my-repo".into());
    let read = hub.resolve("", false, false).unwrap();
    assert!(!read.created);
    assert_eq!(read.project, b"my-repo");
    assert_eq!(read.notice, "project my-repo has no issues yet");
    assert!(!hub.hub.join("projects/my-repo").exists());
    let write = hub.resolve("", true, false).unwrap();
    assert!(write.created);
    assert_eq!(write.project, b"my-repo");
    assert!(!hub.hub.join("projects/my-repo").exists());
    let paths = create_project_files(&hub.hub, &write.project, &write.repo_remote)
        .unwrap()
        .unwrap();
    assert_eq!(paths.len(), 8);
    assert_eq!(paths[0], b"projects/my-repo/beans.toml");
    assert_eq!(paths[5], b"projects/my-repo/requests/.gitkeep");
    assert_eq!(paths[7], b"projects/my-repo/handoffs/archive/.gitkeep");
    let cfg =
        beans::domain::config::load_project_config(&hub.hub.join("projects/my-repo/beans.toml"))
            .unwrap();
    assert_eq!(cfg.prefix.as_bytes(), b"my-repo");
    assert_eq!(
        cfg.remotes,
        Some(vec!["https://github.com/o/my-repo".into()])
    );
    assert!(
        create_project_files(&hub.hub, &write.project, &write.repo_remote)
            .unwrap()
            .is_none()
    );
    assert_eq!(
        project_dirs(&hub.hub).unwrap(),
        Some(vec![b"my-repo".to_vec()])
    );
}
