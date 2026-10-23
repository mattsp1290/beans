//! Public-CLI prompt journeys across real Git publication and raw Markdown boundaries.
use super::*;
use std::{
    process::{Child, Command, Output, Stdio},
    thread,
    time::{Duration, Instant},
};

fn init_fixture() -> (Sandbox, PathBuf) {
    let sandbox = Sandbox::new();
    let remote = sandbox.remote(false);
    sandbox.ok(&["init", remote.to_str().unwrap()]);
    // Materialize the project before cloning a competing writer.
    sandbox.ok(&["create", "Git journey seed", "-d", "seed"]);
    (sandbox, remote)
}

fn prompt_create(sandbox: &Sandbox, title: &str, body: &str) -> String {
    sandbox
        .ok(&[
            "prompt",
            "create",
            title,
            "--to",
            "queue",
            "--description",
            body,
        ])
        .trim()
        .to_owned()
}

fn prompt_path(sandbox: &Sandbox, id: &str) -> PathBuf {
    let output = sandbox.ok(&["--json", "prompt", "show", id]);
    let detail: serde_json::Value = serde_json::from_str(&output).unwrap();
    sandbox
        .path("home/hub")
        .join(detail["path"].as_str().unwrap())
}

fn cli_child(sandbox: &Sandbox, args: &[&str]) -> Child {
    Command::new(env!("CARGO_BIN_EXE_bn"))
        .args(args)
        .env("BEANS_HOME", sandbox.path("home"))
        .env("BEANS_PROJECT", "demo")
        .env("BN_ACTOR", "Native Tester")
        .env_remove("BEANS_HUB")
        .env_remove("BN_CONFIG")
        .current_dir(&sandbox.0)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap()
}

fn wait_for_file(path: &Path) {
    let deadline = Instant::now() + Duration::from_secs(15);
    while !path.exists() {
        assert!(
            Instant::now() < deadline,
            "timed out waiting for {}",
            path.display()
        );
        thread::sleep(Duration::from_millis(10));
    }
}

fn finish(child: Child, args: &[&str]) -> Output {
    let output = child.wait_with_output().unwrap();
    assert!(
        output.status.success(),
        "{args:?}: stdout={} stderr={}",
        String::from_utf8_lossy(&output.stdout),
        String::from_utf8_lossy(&output.stderr)
    );
    output
}

/// Pause every push after its local commit has been made. The hook has its own
/// bounded deadline, so a failed test cannot leave the CLI child hanging.
fn install_push_gate(sandbox: &Sandbox, name: &str) -> (PathBuf, PathBuf) {
    let ready = sandbox.path(&format!("{name}.ready"));
    let release = sandbox.path(&format!("{name}.release"));
    let hub = sandbox.path("home/hub");
    hook(
        &Hub::new(
            hub,
            sandbox.path("gate-cache"),
            "main".into(),
            "Native Tester".into(),
            false,
            Duration::ZERO,
        )
        .unwrap(),
        "pre-push",
        &format!(
            "printf ready > '{}'
for _n in $(seq 1 300); do
  test -f '{}' && exit 0
  sleep 0.05
done
echo 'test push gate timed out' >&2
exit 97",
            ready.display(),
            release.display()
        ),
    );
    (ready, release)
}

fn count(haystack: &[u8], needle: &[u8]) -> usize {
    haystack
        .windows(needle.len())
        .filter(|window| *window == needle)
        .count()
}

fn audit_lines(raw: &[u8]) -> Vec<&[u8]> {
    raw.split(|byte| *byte == b'\n')
        .filter(|line| line.starts_with(b"- "))
        .collect()
}

fn assert_audit_line(line: &[u8], event: &str) {
    let text = std::str::from_utf8(line).unwrap();
    assert_eq!(&text[..2], "- ");
    assert_eq!(&text[6..7], "-");
    assert_eq!(&text[9..10], "-");
    assert_eq!(&text[12..13], "T");
    assert_eq!(&text[15..16], ":");
    assert_eq!(&text[18..19], ":");
    assert_eq!(&text[21..23], "Z ");
    assert!(
        text.ends_with(&format!(": {event}")),
        "unexpected audit line: {text:?}"
    );
    assert!(
        text.contains(" Native-Tester"),
        "unexpected audit actor: {text:?}"
    );
}

#[test]
fn prompt_public_cli_create_and_update_replay_non_fast_forward_exactly_once() {
    let (sandbox, remote) = init_fixture();
    let worker_b = sandbox.path("worker-b");
    git(
        &sandbox.0,
        &[
            "clone",
            remote.to_str().unwrap(),
            worker_b.to_str().unwrap(),
        ],
    );

    let (ready, release) = install_push_gate(&sandbox, "create");
    let args = [
        "prompt",
        "create",
        "Raced public create",
        "--to",
        "queue",
        "--description",
        "CREATE BODY EXACT\n",
    ];
    let child = cli_child(&sandbox, &args);
    wait_for_file(&ready);
    let hand_path = worker_b.join("projects/demo/worker-b-create.txt");
    let hand_bytes = b"worker B exact create-race bytes\nsecond line\n";
    fs::write(&hand_path, hand_bytes).unwrap();
    git(&worker_b, &["add", "-A"]);
    git(&worker_b, &["commit", "-m", "worker B create race"]);
    git(&worker_b, &["push", "origin", "main"]);
    fs::write(&release, b"go").unwrap();
    let output = finish(child, &args);
    let id = String::from_utf8(output.stdout).unwrap().trim().to_owned();
    assert!(id.starts_with("demo-p-"));

    let verify = sandbox.path("verify-create");
    git(
        &sandbox.0,
        &["clone", remote.to_str().unwrap(), verify.to_str().unwrap()],
    );
    assert_eq!(
        fs::read(verify.join("projects/demo/worker-b-create.txt")).unwrap(),
        hand_bytes
    );
    let paths = git(
        &verify,
        &["ls-files", &format!("projects/demo/prompts/{id}*.md")],
    );
    assert_eq!(
        paths.lines().count(),
        1,
        "created prompt path count: {paths:?}"
    );
    let created = fs::read(verify.join(paths.trim())).unwrap();
    assert_eq!(count(&created, b"CREATE BODY EXACT\n"), 1);
    assert_eq!(count(&created, b": prompt created"), 1);
    assert_eq!(
        git(
            &verify,
            &[
                "log",
                "--format=%s",
                "--grep",
                &format!("^bn: record {id}$")
            ]
        )
        .lines()
        .count(),
        1
    );

    // Race an update to that same prompt. B adds exact authored bytes in an
    // unknown field while A appends a supplied note and changes status.
    git(&worker_b, &["pull", "--ff-only"]);
    let (ready, release) = install_push_gate(&sandbox, "update");
    let args = [
        "prompt",
        "update",
        id.as_str(),
        "--status",
        "declined",
        "--note",
        "A replay note exact",
    ];
    let child = cli_child(&sandbox, &args);
    wait_for_file(&ready);
    let worker_prompt = worker_b.join(paths.trim());
    let before = fs::read(&worker_prompt).unwrap();
    let marker = b"from: Native Tester\n";
    let offset = before
        .windows(marker.len())
        .position(|window| window == marker)
        .unwrap()
        + marker.len();
    let mut hand_edited = before[..offset].to_vec();
    hand_edited.extend_from_slice(b"x-worker-b: 'EXACT # value' # authored comment\n");
    hand_edited.extend_from_slice(&before[offset..]);
    fs::write(&worker_prompt, &hand_edited).unwrap();
    git(&worker_b, &["add", "-A"]);
    git(&worker_b, &["commit", "-m", "worker B update race"]);
    git(&worker_b, &["push", "origin", "main"]);
    fs::write(&release, b"go").unwrap();
    finish(child, &args);

    let final_bytes = fs::read(prompt_path(&sandbox, &id)).unwrap();
    assert!(
        final_bytes
            .windows(b"x-worker-b: 'EXACT # value' # authored comment\n".len())
            .any(|w| w == b"x-worker-b: 'EXACT # value' # authored comment\n")
    );
    assert!(
        final_bytes
            .windows(b"status: declined\n".len())
            .any(|w| w == b"status: declined\n")
    );
    assert_eq!(count(&final_bytes, b": A replay note exact"), 1);
    assert_eq!(count(&final_bytes, b": prompt status: declined"), 0);
    assert_eq!(
        git(
            &sandbox.path("home/hub"),
            &[
                "log",
                "--format=%s",
                "--grep",
                &format!("^bn: record {id}$")
            ]
        )
        .lines()
        .count(),
        2,
        "one create commit and one update commit"
    );
}

#[test]
fn prompt_public_cli_raw_audit_bytes_obey_single_entry_and_noop_contract() {
    let (sandbox, _remote) = init_fixture();
    let id = prompt_create(&sandbox, "Raw audit", "raw body\n");
    let path = prompt_path(&sandbox, &id);
    let created = fs::read(&path).unwrap();
    let created_lines = audit_lines(&created);
    assert_eq!(created_lines.len(), 1);
    assert_audit_line(created_lines[0], "prompt created");

    sandbox.ok(&["prompt", "update", &id, "--note", "sole note exact"]);
    let note_only = fs::read(&path).unwrap();
    assert_eq!(count(&note_only, b": sole note exact"), 1);
    assert_eq!(audit_lines(&note_only).len(), 2);
    assert_eq!(audit_lines(&note_only)[0], created_lines[0]);
    assert_audit_line(audit_lines(&note_only)[1], "sole note exact");

    sandbox.ok(&[
        "prompt",
        "update",
        &id,
        "--status",
        "declined",
        "--note",
        "combined note exact",
    ]);
    let combined = fs::read(&path).unwrap();
    assert_eq!(count(&combined, b": combined note exact"), 1);
    assert_eq!(count(&combined, b": prompt status: declined"), 0);
    assert_eq!(audit_lines(&combined).len(), 3);
    assert_eq!(audit_lines(&combined)[..2], audit_lines(&note_only));
    assert_audit_line(audit_lines(&combined)[2], "combined note exact");

    let head = git(&sandbox.path("home/hub"), &["rev-parse", "HEAD"]);
    let rejected = sandbox.cli(&["prompt", "update", &id, "--note", " \t "]);
    assert!(!rejected.status.success());
    assert_eq!(
        String::from_utf8(rejected.stderr).unwrap(),
        "bn: prompt note must not be blank\n"
    );
    assert_eq!(fs::read(&path).unwrap(), combined);
    assert_eq!(git(&sandbox.path("home/hub"), &["rev-parse", "HEAD"]), head);

    let claim_id = prompt_create(&sandbox, "Idempotent raw claim", "claim body");
    let claim_path = prompt_path(&sandbox, &claim_id);
    sandbox.ok(&["prompt", "update", &claim_id, "--claim"]);
    let claimed = fs::read(&claim_path).unwrap();
    let claim_head = git(&sandbox.path("home/hub"), &["rev-parse", "HEAD"]);
    assert_eq!(count(&claimed, b": prompt claimed"), 1);
    sandbox.ok(&["prompt", "update", &claim_id, "--claim"]);
    assert_eq!(fs::read(&claim_path).unwrap(), claimed);
    assert_eq!(
        git(&sandbox.path("home/hub"), &["rev-parse", "HEAD"]),
        claim_head
    );
}

fn assert_authored_bytes(raw: &[u8]) {
    for exact in [
        b"title: Linked preservation # authored title comment\n".as_slice(),
        b"x-human: 'unknown # scalar' # exact unknown comment\n".as_slice(),
        b"<!-- exact authored body comment -->\nBODY  trailing spaces  \n".as_slice(),
        b"- 2001-02-03T04:05:06Z human: opaque prior audit\n".as_slice(),
    ] {
        assert!(
            raw.windows(exact.len()).any(|window| window == exact),
            "missing exact bytes: {:?}",
            String::from_utf8_lossy(exact)
        );
        assert_eq!(count(raw, exact), 1);
    }
}

#[test]
fn prompt_link_unlink_relink_preserves_authored_bytes_and_forced_issue_delete_is_nonstructural() {
    let (sandbox, _remote) = init_fixture();
    let dependency_baseline = sandbox.ok(&["dep", "tree", "--all-projects"]);
    let ready_baseline = sandbox.ok(&["ready", "--all-projects"]);
    let plan_baseline = sandbox.ok(&["plan", "list"]);
    let issue = sandbox
        .ok(&["create", "Linked target", "-d", "target"])
        .trim()
        .to_owned();
    let prompt = prompt_create(&sandbox, "Linked preservation", "temporary body");
    let path = prompt_path(&sandbox, &prompt);
    let original = fs::read(&path).unwrap();
    let mut edited = String::from_utf8(original).unwrap();
    edited = edited.replace(
        "title: Linked preservation\n",
        "title: Linked preservation # authored title comment\n",
    );
    edited = edited.replace(
        "from: Native Tester\n",
        "from: Native Tester\nx-human: 'unknown # scalar' # exact unknown comment\n",
    );
    // Rebuild safely from frontmatter rather than depending on template bytes.
    let end_frontmatter = edited[4..].find("---\n").unwrap() + 8;
    let existing_log = edited[edited.find("## Log\n").unwrap()..].to_owned();
    edited = format!(
        "{}<!-- exact authored body comment -->\nBODY  trailing spaces  \n\n{}- 2001-02-03T04:05:06Z human: opaque prior audit\n",
        &edited[..end_frontmatter],
        existing_log
    );
    fs::write(&path, edited.as_bytes()).unwrap();
    git(&sandbox.path("home/hub"), &["add", "-A"]);
    git(
        &sandbox.path("home/hub"),
        &["commit", "-m", "authored prompt bytes"],
    );
    git(&sandbox.path("home/hub"), &["push", "origin", "main"]);
    assert_authored_bytes(&fs::read(&path).unwrap());

    sandbox.ok(&["prompt", "link", &prompt, &issue]);
    assert_authored_bytes(&fs::read(&path).unwrap());
    sandbox.ok(&["prompt", "unlink", &prompt]);
    assert_authored_bytes(&fs::read(&path).unwrap());
    sandbox.ok(&["prompt", "link", &prompt, &issue]);
    let linked = fs::read(&path).unwrap();
    assert_authored_bytes(&linked);
    assert_eq!(
        count(&linked, format!("issue: \"[[{issue}]]\"").as_bytes()),
        1
    );
    assert_eq!(count(&linked, b": prompt linked: "), 2);
    assert_eq!(count(&linked, b": prompt unlinked"), 1);

    let dependencies_before_delete = sandbox.ok(&["dep", "tree", "--all-projects"]);
    let ready_before_delete = sandbox.ok(&["ready", "--all-projects"]);
    let plans_before_delete = sandbox.ok(&["plan", "list"]);
    assert_ne!(dependencies_before_delete, dependency_baseline);
    assert_ne!(ready_before_delete, ready_baseline);
    assert_eq!(plans_before_delete, plan_baseline);
    sandbox.ok(&["delete", &issue, "--force"]);

    assert_eq!(
        fs::read(&path).unwrap(),
        linked,
        "issue deletion rewrote prompt bytes"
    );
    assert_eq!(sandbox.ok(&["prompt", "show", &prompt]).as_bytes(), linked);
    let detail: serde_json::Value =
        serde_json::from_str(&sandbox.ok(&["--json", "prompt", "show", &prompt])).unwrap();
    assert_eq!(detail["issue"]["raw"], format!("[[{issue}]]"));
    assert_eq!(detail["issue"]["target"], issue);

    let doctor = sandbox.cli(&["--json", "doctor"]);
    assert!(!doctor.status.success());
    let diagnosis: serde_json::Value = serde_json::from_slice(&doctor.stdout).unwrap();
    assert_eq!(diagnosis["ok"], false);
    assert!(
        diagnosis["warnings"]
            .as_array()
            .unwrap()
            .iter()
            .any(|warning| {
                warning["path"]
                    .as_str()
                    .is_some_and(|path_value| path_value.contains(&prompt))
                    && warning["message"].as_str().is_some_and(|message| {
                        message.contains("unresolved link") && message.contains(&issue)
                    })
            }),
        "doctor did not diagnose missing prompt issue: {diagnosis:#}"
    );

    assert_eq!(
        sandbox.ok(&["dep", "tree", "--all-projects"]),
        dependency_baseline
    );
    assert_eq!(sandbox.ok(&["ready", "--all-projects"]), ready_baseline);
    assert_eq!(sandbox.ok(&["plan", "list"]), plan_baseline);
}
