use serde_json::Value;
use std::{
    fs,
    path::PathBuf,
    process::{Command, Output},
};

struct Fixture {
    root: PathBuf,
    home: PathBuf,
}
impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.root);
    }
}
impl Fixture {
    fn new() -> Self {
        let mut random = [0; 8];
        getrandom::fill(&mut random).unwrap();
        let root = std::env::temp_dir().join(format!(
            "bn-channel-references-{}-{}",
            std::process::id(),
            u64::from_ne_bytes(random)
        ));
        fs::create_dir_all(&root).unwrap();
        let remote = root.join("remote.git");
        assert!(
            Command::new("git")
                .args(["init", "--bare", "--initial-branch=main"])
                .arg(&remote)
                .output()
                .unwrap()
                .status
                .success()
        );
        let home = root.join("home");
        fs::create_dir(&home).unwrap();
        let fixture = Self { root, home };
        fixture.ok(&["init", remote.to_str().unwrap()]);
        fixture
    }
    fn hub(&self) -> PathBuf {
        self.home.join(".beans/hub")
    }
    fn run(&self, args: &[&str]) -> Output {
        Command::new(env!("CARGO_BIN_EXE_bn"))
            .args(["--json", "--project", "test", "--actor", "coordinator"])
            .args(args)
            .current_dir(&self.home)
            .env("HOME", &self.home)
            .env("BEANS_HOME", self.home.join(".beans"))
            .env("BN_NO_UPDATE_CHECK", "1")
            .env("GIT_CONFIG_GLOBAL", "/dev/null")
            .env("GIT_CONFIG_NOSYSTEM", "1")
            .env("GIT_TERMINAL_PROMPT", "0")
            .env_remove("BEANS_HUB")
            .env_remove("BEANS_PROJECT")
            .env_remove("BN_ACTOR")
            .env_remove("BN_CONFIG")
            .output()
            .unwrap()
    }
    fn ok(&self, args: &[&str]) -> Value {
        let output = self.run(args);
        assert!(
            output.status.success(),
            "stdout={} stderr={}",
            String::from_utf8_lossy(&output.stdout),
            String::from_utf8_lossy(&output.stderr)
        );
        serde_json::from_slice(&output.stdout).unwrap()
    }
}

#[test]
fn immutable_channel_references_survive_issue_deletion_without_doctor_errors() {
    let f = Fixture::new();
    let epic = f.ok(&["create", "Coordination epic", "--type", "epic"]);
    let epic_id = epic["id"].as_str().unwrap();
    let channel = f.ok(&["channel", "create", "coordination", "--issue", epic_id]);
    let channel_id = channel["id"].as_str().unwrap();
    let reference = format!("[[{epic_id}]]");
    let body = format!("Historical work on {reference} and [[test-never-existed]].");
    let message = f.ok(&[
        "channel",
        "send",
        channel_id,
        "--key",
        "historical-reference",
        "--reference",
        &reference,
        "--reference",
        "[[test-unknown-reference]]",
        "--description",
        &body,
    ]);
    let message_id = message["id"].as_str().unwrap();
    let path = f.hub().join(format!(
        "projects/test/channels/{channel_id}/messages/{message_id}.md"
    ));
    let before = fs::read(&path).unwrap();
    let linked = f.ok(&["--no-fetch", "show", epic_id]);
    assert!(
        linked["backlinks"]
            .as_array()
            .unwrap()
            .iter()
            .any(|link| link["from"] == channel_id)
    );
    assert_eq!(f.ok(&["--no-fetch", "doctor"])["ok"], true);

    f.ok(&["delete", epic_id]);

    assert_eq!(f.ok(&["--no-fetch", "doctor"])["ok"], true);
    assert_eq!(fs::read(&path).unwrap(), before);
    let read = f.ok(&["--no-fetch", "channel", "read", channel_id]);
    let retained = read["messages"]
        .as_array()
        .unwrap()
        .iter()
        .find(|message| message["id"] == message_id)
        .unwrap();
    assert_eq!(retained["body"], body);
    assert_eq!(retained["references"][0], reference);
}

#[test]
fn ordinary_document_links_still_protect_deletion_and_report_dangling_targets() {
    let f = Fixture::new();
    let issue = f.ok(&["create", "Tracked work"]);
    let issue_id = issue["id"].as_str().unwrap();
    let docs = f.hub().join("projects/test/docs");
    fs::create_dir_all(&docs).unwrap();
    fs::write(docs.join("work.md"), format!("# Work\n\n[[{issue_id}]]\n")).unwrap();
    assert_eq!(f.ok(&["--no-fetch", "doctor"])["ok"], true);
    let refused = f.run(&["delete", issue_id]);
    assert!(!refused.status.success());
    assert!(String::from_utf8_lossy(&refused.stderr).contains("issue is referenced"));

    f.ok(&["delete", issue_id, "--force"]);

    let doctor = f.run(&["--no-fetch", "doctor"]);
    assert!(!doctor.status.success());
    let result: Value = serde_json::from_slice(&doctor.stdout).unwrap();
    assert_eq!(result["ok"], false);
    assert!(
        result["warnings"]
            .as_array()
            .unwrap()
            .iter()
            .any(|warning| warning["message"]
                .as_str()
                .unwrap()
                .contains("unresolved link"))
    );
}
