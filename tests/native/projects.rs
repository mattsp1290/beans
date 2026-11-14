use super::*;
use serde_json::{Value, json};

fn fixture() -> Sandbox {
    let sandbox = Sandbox::new();
    let remote = sandbox.remote(false);
    sandbox.ok(&["init", remote.to_str().unwrap()]);
    sandbox
}
fn repo(id: &str, name: &str) -> Value {
    json!({"id":id,"full_name":format!("owner/{name}"),"html_url":format!("https://github.com/owner/{name}")})
}
fn request(operation: &str, revision: Option<&str>, change: Value) -> Value {
    json!({"schema_version":1,"project_id":"project-stable","operation_id":operation,"expected_revision":revision,"change":change})
}
fn invoke(s: &Sandbox, request: &Value) -> Output {
    let file = s.path("request.json");
    fs::write(&file, serde_json::to_vec(request).unwrap()).unwrap();
    s.cli(&[
        "--json",
        "project",
        "apply",
        "--request-file",
        file.to_str().unwrap(),
    ])
}
fn apply(s: &Sandbox, request: &Value) -> Value {
    let output = invoke(s, request);
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
    serde_json::from_slice(&output.stdout).unwrap()
}
fn rejected(s: &Sandbox, request: &Value, text: &str) {
    let before = git(&s.path("home/hub"), &["rev-parse", "HEAD"]);
    let output = invoke(s, request);
    assert!(!output.status.success());
    assert!(output.stdout.is_empty());
    assert!(
        String::from_utf8_lossy(&output.stderr).contains(text),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
    assert_eq!(git(&s.path("home/hub"), &["rev-parse", "HEAD"]), before);
}
fn inspect(s: &Sandbox) -> Value {
    serde_json::from_str(&s.ok(&["--json", "project", "inspect", "project-stable"])).unwrap()
}
#[test]
fn project_apply_cross_process_retry_conflict_and_default_selection_order() {
    let s = fixture();
    let create = request(
        "create-one",
        None,
        json!({"type":"create","name":null,"repositories":[repo("22","zebra"),repo("11","alpha"),repo("22","zebra")]}),
    );
    let first = apply(&s, &create);
    assert_eq!(first["project"]["name"], "zebra");
    assert_eq!(
        first["project"]["repositories"].as_array().unwrap().len(),
        2
    );
    assert_eq!(first["project"]["repositories"][0]["id"], "22");
    assert_eq!(first["pushed"], true);
    let head = git(&s.path("home/hub"), &["rev-parse", "HEAD"]);
    let retry = apply(&s, &create);
    assert_eq!(retry["replayed"], true);
    assert_eq!(retry["project"], first["project"]);
    assert_eq!(git(&s.path("home/hub"), &["rev-parse", "HEAD"]), head);
    let mut conflict = create;
    conflict["change"]["name"] = "Conflicting reuse".into();
    rejected(&s, &conflict, "conflicting request");
    assert_eq!(inspect(&s)["project"], first["project"]);
}
#[test]
fn project_edits_preserve_namespace_history_trivia_and_tombstone() {
    let s = fixture();
    let created = apply(
        &s,
        &request(
            "create",
            None,
            json!({"type":"create","name":"Custom name","repositories":[repo("1","first")]}),
        ),
    );
    assert_eq!(created["project"]["name"], "Custom name");
    let history = s.ok(&["--project", "project-stable", "create", "Historical issue"]);
    let issue = history.trim();
    let config = s.path("home/hub/projects/project-stable/beans.toml");
    let authored = format!(
        "# Keep heading\n{}\n[vendor]\nmeaning = 'keep me' # Keep tail\n",
        fs::read_to_string(&config).unwrap()
    );
    fs::write(&config, authored).unwrap();
    s.ok(&["sync"]);
    let current = inspect(&s);
    let revision = current["project"]["revision"].as_str().unwrap();
    let renamed = apply(
        &s,
        &request(
            "rename",
            Some(revision),
            json!({"type":"rename","name":"Renamed project"}),
        ),
    );
    assert_eq!(renamed["project"]["id"], "project-stable");
    let source = fs::read_to_string(&config).unwrap();
    assert!(source.starts_with("# Keep heading\n"));
    assert!(source.ends_with("[vendor]\nmeaning = 'keep me' # Keep tail\n"));
    rejected(
        &s,
        &request(
            "stale",
            Some(revision),
            json!({"type":"rename","name":"Stale"}),
        ),
        "revision conflict",
    );
    let added = apply(
        &s,
        &request(
            "add",
            renamed["project"]["revision"].as_str(),
            json!({"type":"add_repository","repository":repo("2","second")}),
        ),
    );
    let canonical = apply(
        &s,
        &request(
            "canonical",
            added["project"]["revision"].as_str(),
            json!({"type":"add_repository","repository":repo("2","renamed-repo")}),
        ),
    );
    assert_eq!(
        canonical["project"]["repositories"]
            .as_array()
            .unwrap()
            .len(),
        2
    );
    assert_eq!(canonical["project"]["name"], "Renamed project");
    let removed = apply(
        &s,
        &request(
            "remove",
            canonical["project"]["revision"].as_str(),
            json!({"type":"remove_repository","repository_id":"1"}),
        ),
    );
    rejected(
        &s,
        &request(
            "last",
            removed["project"]["revision"].as_str(),
            json!({"type":"remove_repository","repository_id":"2"}),
        ),
        "last project repository",
    );
    let archived = apply(
        &s,
        &request(
            "archive",
            removed["project"]["revision"].as_str(),
            json!({"type":"archive"}),
        ),
    );
    assert_eq!(archived["project"]["archived"], true);
    let catalog: Value = serde_json::from_str(&s.ok(&["--json", "project", "catalog"])).unwrap();
    assert!(catalog["projects"].as_array().unwrap().is_empty());
    assert_eq!(inspect(&s)["project"], archived["project"]);
    let note: Value =
        serde_json::from_str(&s.ok(&["--json", "--project", "project-stable", "show", issue]))
            .unwrap();
    assert_eq!(note["title"], "Historical issue");
    assert_eq!(note["project"], "project-stable");
    rejected(
        &s,
        &request(
            "after-archive",
            archived["project"]["revision"].as_str(),
            json!({"type":"rename","name":"No"}),
        ),
        "archived",
    );
    assert!(s.path("remote.git").is_dir());
}
#[test]
fn project_publication_failure_retains_receipt_and_retry_publishes_once() {
    let s = fixture();
    let remote = s.path("remote.git");
    let before = git(&remote, &["rev-parse", "HEAD"]);
    let hook = remote.join("hooks/pre-receive");
    fs::write(&hook, "#!/bin/sh\nexit 1\n").unwrap();
    fs::set_permissions(&hook, fs::Permissions::from_mode(0o755)).unwrap();
    let create = request(
        "lost-publication",
        None,
        json!({"type":"create","repositories":[repo("1","first")]}),
    );
    let output = invoke(&s, &create);
    assert!(!output.status.success());
    assert!(output.stdout.is_empty());
    assert!(String::from_utf8_lossy(&output.stderr).contains("publication failed"));
    assert_eq!(git(&remote, &["rev-parse", "HEAD"]), before);
    let local = git(&s.path("home/hub"), &["rev-parse", "HEAD"]);
    assert_ne!(local, before);
    assert!(
        inspect(&s)["project"]["revision"]
            .as_str()
            .unwrap()
            .starts_with("sha256:")
    );
    fs::remove_file(hook).unwrap();
    let retry = apply(&s, &create);
    assert_eq!(retry["replayed"], true);
    assert_eq!(retry["pushed"], true);
    assert_eq!(git(&s.path("home/hub"), &["rev-parse", "HEAD"]), local);
    assert_eq!(git(&remote, &["rev-parse", "HEAD"]), local);
    let observer = s.clone_hub(&remote, "observer");
    let snapshot = beans::ops::projects::inspect(&observer.dir, "project-stable").unwrap();
    assert_eq!(snapshot.repositories.len(), 1);
}
#[test]
fn project_rejects_empty_membership_unsafe_scope_and_untrusted_repository() {
    let s = fixture();
    rejected(
        &s,
        &request("empty", None, json!({"type":"create","repositories":[]})),
        "requires 1 to 100",
    );
    let mut unsafe_id = request(
        "unsafe",
        None,
        json!({"type":"create","repositories":[repo("1","first")]}),
    );
    unsafe_id["project_id"] = "../outside".into();
    rejected(&s, &unsafe_id, "invalid project identity");
    let mut untrusted = request(
        "url",
        None,
        json!({"type":"create","repositories":[repo("1","first")]}),
    );
    untrusted["change"]["repositories"][0]["html_url"] = "https://evil.invalid/owner/first".into();
    rejected(&s, &untrusted, "invalid GitHub repository");
}
#[test]
fn project_concurrent_same_create_is_one_authored_operation() {
    let s = fixture();
    let create = request(
        "concurrent-create",
        None,
        json!({"type":"create","repositories":[repo("1","first")]}),
    );
    let file = s.path("concurrent.json");
    fs::write(&file, serde_json::to_vec(&create).unwrap()).unwrap();
    let before: usize = git(&s.path("home/hub"), &["rev-list", "--count", "HEAD"])
        .parse()
        .unwrap();
    let start = || {
        Command::new(env!("CARGO_BIN_EXE_bn"))
            .env("BN_NO_UPDATE_CHECK", "1")
            .args([
                "--json",
                "project",
                "apply",
                "--request-file",
                file.to_str().unwrap(),
            ])
            .env("BEANS_HOME", s.path("home"))
            .env("BEANS_PROJECT", "ignored-project")
            .env("BN_ACTOR", "Native Tester")
            .env_remove("BEANS_HUB")
            .env_remove("BN_CONFIG")
            .current_dir(&s.0)
            .stdout(std::process::Stdio::piped())
            .stderr(std::process::Stdio::piped())
            .spawn()
            .unwrap()
    };
    let first = start();
    let second = start();
    let results: Vec<Value> = [first, second]
        .into_iter()
        .map(|child| {
            let output = child.wait_with_output().unwrap();
            assert!(
                output.status.success(),
                "{}",
                String::from_utf8_lossy(&output.stderr)
            );
            serde_json::from_slice(&output.stdout).unwrap()
        })
        .collect();
    assert_eq!(results[0]["project"], results[1]["project"]);
    assert_ne!(results[0]["replayed"], results[1]["replayed"]);
    let after: usize = git(&s.path("home/hub"), &["rev-list", "--count", "HEAD"])
        .parse()
        .unwrap();
    assert_eq!(after, before + 1);
}

#[test]
fn project_contended_publication_reports_rebased_commit_and_invocation_identity() {
    let s = fixture();
    let remote = s.path("remote.git");
    let competitor = s.clone_hub(&remote, "competitor");
    let hub = Hub::new(
        s.path("home/hub"),
        s.path("home/cache"),
        "main".into(),
        "Native Tester".into(),
        false,
        Duration::ZERO,
    )
    .unwrap();
    let mut competing = request(
        "other-create",
        None,
        json!({"type":"create","repositories":[repo("2","other")]}),
    );
    competing["project_id"] = "other-project".into();
    let competing_file = s.path("competing.json");
    fs::write(&competing_file, serde_json::to_vec(&competing).unwrap()).unwrap();
    let marker = s.path("push-raced");
    let before_rebase = s.path("before-rebase");
    hook(
        &hub,
        "pre-push",
        &format!(
            "test ! -e '{}' || exit 0\n: > '{}'\ngit rev-parse HEAD > '{}'\nBN_NO_UPDATE_CHECK=1 '{}' --hub '{}' --actor 'Competing owner' --json project apply --request-file '{}' > /dev/null",
            marker.display(),
            marker.display(),
            before_rebase.display(),
            env!("CARGO_BIN_EXE_bn"),
            competitor.dir.display(),
            competing_file.display()
        ),
    );
    let create = request(
        "contended-create",
        None,
        json!({"type":"create","repositories":[repo("1","first")]}),
    );
    let created = apply(&s, &create);
    let published = git(&remote, &["rev-parse", "HEAD"]);
    assert_eq!(created["pushed"], true);
    assert_eq!(created["replayed"], false);
    assert_eq!(created["commit"], published);
    assert_eq!(created["sha"], published);
    assert_ne!(fs::read_to_string(before_rebase).unwrap().trim(), published);
    assert_eq!(git(&hub.dir, &["rev-parse", "HEAD"]), published);
    let observer = s.clone_hub(&remote, "contended-observer");
    assert_eq!(
        beans::ops::projects::inspect(&observer.dir, "other-project")
            .unwrap()
            .name,
        "other"
    );
    let retry = apply(&s, &create);
    assert_eq!(retry["replayed"], true);
    assert_eq!(retry["commit"], "");
    assert_eq!(git(&remote, &["rev-parse", "HEAD"]), published);
}
