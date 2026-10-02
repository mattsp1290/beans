use super::*;
use beans::vault::{GitCapture, GitResolver, SystemGit};
use std::cell::RefCell;

#[test]
fn resolver_fake_records_exact_root_and_preserves_configured_capture() {
    struct Fake(RefCell<Vec<PathBuf>>, Vec<u8>);
    impl GitResolver for Fake {
        fn toplevel(&self, _: &Path) -> GitCapture {
            GitCapture::default()
        }
        fn remote_url(&self, _: &Path) -> GitCapture {
            GitCapture::default()
        }
        fn head_commit(&self, root: &Path) -> GitCapture {
            self.0.borrow_mut().push(root.into());
            GitCapture {
                value: self.1.clone(),
                found: true,
                error: None,
            }
        }
    }
    for configured in [
        b"0123456789abcdef0123456789abcdef01234567".as_slice(),
        b"HEAD",
    ] {
        let fake = Fake(RefCell::new(vec![]), configured.into());
        let root = Path::new("/repo/root");
        let result = fake.head_commit(root);
        assert!(result.found);
        assert_eq!(result.value, configured);
        assert!(result.error.is_none());
        assert_eq!(*fake.0.borrow(), [root]);
    }
}
fn assert_head(root: &Path, expected: &str) {
    let before = contents(root);
    let cap = SystemGit.head_commit(root);
    assert!(cap.found);
    assert_eq!(cap.value, expected.as_bytes());
    assert!(cap.error.is_none());
    assert_eq!(
        contents(root),
        before,
        "read-only Git resolver mutated checkout"
    );
}
fn commit(root: &Path, bytes: &str) -> String {
    fs::write(root.join("conflict.txt"), bytes).unwrap();
    git(root, &["add", "-A"]);
    git(root, &["commit", "-m", bytes]);
    git(root, &["rev-parse", "HEAD"]).trim().into()
}
#[test]
fn resolver_exact_head_in_dirty_detached_linked_and_submodule_repositories() {
    let s = Sandbox::new();
    let root = s.path("repo");
    fs::create_dir(&root).unwrap();
    git(&root, &["init", "-b", "main"]);
    let head = commit(&root, "base");
    assert_head(&root, &head);
    fs::write(root.join("conflict.txt"), "dirty").unwrap();
    assert_head(&root, &head);
    git(&root, &["checkout", "--detach", "HEAD"]);
    assert_head(&root, &head);
    let linked = s.path("linked");
    git(
        &root,
        &[
            "worktree",
            "add",
            "-b",
            "linked",
            linked.to_str().unwrap(),
            "HEAD",
        ],
    );
    assert_head(&linked, &head);
    let sub = s.path("sub");
    fs::create_dir(&sub).unwrap();
    git(&sub, &["init", "-b", "main"]);
    let subhead = commit(&sub, "sub");
    git(
        &root,
        &[
            "-c",
            "protocol.file.allow=always",
            "submodule",
            "add",
            sub.to_str().unwrap(),
            "deps/sub",
        ],
    );
    assert_head(&root.join("deps/sub"), &subhead);
}
#[test]
fn resolver_exact_head_during_real_merge_and_rebase_conflicts() {
    for mode in ["merge", "rebase"] {
        let s = Sandbox::new();
        let root = s.path("repo");
        fs::create_dir(&root).unwrap();
        git(&root, &["init", "-b", "main"]);
        commit(&root, "base");
        git(&root, &["checkout", "-b", "side"]);
        commit(&root, "side");
        git(&root, &["checkout", "main"]);
        let main = commit(&root, "main");
        if mode == "rebase" {
            git(&root, &["checkout", "side"]);
        }
        let output = Command::new("git")
            .current_dir(&root)
            .env("GIT_AUTHOR_NAME", "Tester")
            .env("GIT_AUTHOR_EMAIL", "test@example.com")
            .env("GIT_COMMITTER_NAME", "Tester")
            .env("GIT_COMMITTER_EMAIL", "test@example.com")
            .args([mode, if mode == "merge" { "side" } else { "main" }])
            .output()
            .unwrap();
        assert!(!output.status.success());
        assert_head(&root, &main);
        assert!(
            root.join(if mode == "merge" {
                ".git/MERGE_HEAD"
            } else {
                ".git/rebase-merge"
            })
            .exists()
        );
    }
}
#[test]
fn resolver_best_effort_missing_unborn_outside_and_permission_denied() {
    if std::env::var_os("BN_TEST_MISSING_GIT").is_some() {
        let result = SystemGit.head_commit(Path::new("/"));
        assert!(!result.found);
        assert!(result.value.is_empty());
        assert!(result.error.is_none());
        return;
    }
    let s = Sandbox::new();
    let unborn = s.path("unborn");
    fs::create_dir(&unborn).unwrap();
    git(&unborn, &["init", "-b", "main"]);
    let denied = s.path("denied");
    fs::create_dir(&denied).unwrap();
    use std::os::unix::fs::PermissionsExt;
    fs::set_permissions(&denied, fs::Permissions::from_mode(0o000)).unwrap();
    for path in [&s.0, &unborn, &s.path("missing"), &denied] {
        let result = SystemGit.head_commit(path);
        assert!(!result.found);
        assert!(result.value.is_empty());
        assert!(result.error.is_none());
    }
    fs::set_permissions(&denied, fs::Permissions::from_mode(0o700)).unwrap();
    // A child test process isolates PATH changes from parallel Rust tests.
    let output = Command::new(std::env::current_exe().unwrap())
        .args([
            "git_resolver::resolver_best_effort_missing_unborn_outside_and_permission_denied",
            "--exact",
        ])
        .env("BN_TEST_MISSING_GIT", "1")
        .env("PATH", s.path("missing-tools"))
        .output()
        .unwrap();
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
    assert!(String::from_utf8_lossy(&output.stdout).contains("1 passed; 0 failed"));
}
