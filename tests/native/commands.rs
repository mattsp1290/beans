//! End-to-end retained CLI contracts over the same disposable real-Git sandbox.
use super::*;
fn json(s: &Sandbox, args: &[&str]) -> serde_json::Value {
    serde_json::from_str(&s.ok(args)).unwrap()
}
fn fixture() -> Sandbox {
    let s = Sandbox::new();
    let r = s.remote(false);
    s.ok(&["init", r.to_str().unwrap()]);
    s.ok(&["create", "seed"]);
    s
}
fn issue_path(s: &Sandbox, id: &str) -> PathBuf {
    let v = json(s, &["--json", "show", id]);
    s.path("home/hub").join(v["path"].as_str().unwrap())
}
#[test]
fn native_retained_relationships_archive_reopen_and_force_delete() {
    let s = fixture();
    let a = s.ok(&["create", "parent", "--silent"]).trim().to_owned();
    let b = s
        .ok(&[
            "create",
            "child",
            "--parent",
            &a,
            "--blocked-by",
            &a,
            "--label",
            "first",
            "--label",
            "second",
            "--assignee",
            "owner",
            "--url",
            "https://example.org",
            "--silent",
        ])
        .trim()
        .to_owned();
    assert_eq!(json(&s, &["--json", "children", &a])[0]["id"], b);
    assert!(
        json(&s, &["--json", "blocked"])
            .as_array()
            .unwrap()
            .iter()
            .any(|v| v["issue"]["id"] == b)
    );
    let cycle = s.cli(&["dep", "add", &a, &b]);
    assert!(!cycle.status.success());
    assert!(String::from_utf8_lossy(&cycle.stderr).contains("cycle"));
    s.ok(&["sync"]);
    assert_eq!(
        json(&s, &["--json", "dep", "cycles"]),
        serde_json::json!([])
    );
    let tree = json(&s, &["--json", "dep", "tree", &b]);
    assert_eq!(tree["nodes"].as_array().unwrap().len(), 2);
    assert!(!s.cli(&["close", &a]).status.success());
    s.ok(&["sync"]);
    s.ok(&["close", &a, "--force"]);
    s.ok(&[
        "update",
        &b,
        "--unlabel",
        "first",
        "--label",
        "third",
        "--description",
        "new description",
        "--type",
        "bug",
        "--note",
        "retained note",
    ]);
    let changed = json(&s, &["--json", "show", &b]);
    assert_eq!(changed["labels"], serde_json::json!(["second", "third"]));
    assert_eq!(
        changed["description"].as_str().unwrap().trim(),
        "new description"
    );
    assert!(
        changed["log"]
            .as_array()
            .unwrap()
            .iter()
            .any(|l| l["event"] == "retained note")
    );
    s.ok(&["close", &b, "-r", "done"]);
    let before_archive = issue_path(&s, &b);
    let raw = fs::read_to_string(&before_archive)
        .unwrap()
        .replace("2026-", "2025-");
    fs::write(&before_archive, raw).unwrap();
    s.ok(&["archive", "--older-than", "30d"]);
    let archived = issue_path(&s, &b);
    assert!(archived.to_string_lossy().contains("/archive/2025/"));
    assert!(!before_archive.exists());
    let raw = fs::read_to_string(&archived).unwrap().replacen(
        "title:",
        "vendor: {keep: [one, two]} # authored\ntitle:",
        1,
    );
    fs::write(&archived, &raw).unwrap();
    s.ok(&["reopen", &b]);
    let restored = issue_path(&s, &b);
    assert!(restored.to_string_lossy().contains("/issues/"));
    assert!(!archived.exists());
    assert!(
        fs::read_to_string(&restored)
            .unwrap()
            .contains("vendor: {keep: [one, two]} # authored")
    );
    assert!(!s.cli(&["delete", &a]).status.success());
    s.ok(&["sync"]);
    s.ok(&["delete", &a, "--force"]);
    let child = json(&s, &["--json", "show", &b]);
    assert_eq!(child["parent"]["target"], "");
    assert!(child["blocked_by"].as_array().unwrap().is_empty());
}
#[test]
fn native_request_and_continuation_keep_unknown_authored_bytes() {
    let s = fixture();
    let issue = s
        .ok(&["create", "implement request", "--silent"])
        .trim()
        .to_owned();
    let body = s.path("body.md");
    fs::write(&body, "Requested outcome\n\nKeep this prose.\n").unwrap();
    let request = json(
        &s,
        &[
            "--json",
            "request",
            "create",
            "User request",
            "--body-file",
            body.to_str().unwrap(),
            "--issue",
            &issue,
            "--label",
            "milestone",
            "--requested-by",
            "human",
        ],
    )["id"]
        .as_str()
        .unwrap()
        .to_owned();
    let before = json(&s, &["--json", "request", "show", &request]);
    assert!(
        before["body"]
            .as_str()
            .unwrap()
            .contains("Keep this prose.")
    );
    let path = s.path("home/hub").join(before["path"].as_str().unwrap());
    let bytes = fs::read_to_string(&path).unwrap().replacen(
        "title:",
        "unknown: 'verbatim value' # keep comment\ntitle:",
        1,
    );
    fs::write(&path, &bytes).unwrap();
    assert!(
        !s.cli(&["request", "update", &request, "--status", "resolved"])
            .status
            .success()
    );
    s.ok(&["sync"]);
    for status in ["accepted", "in_progress", "resolved"] {
        s.ok(&["request", "update", &request, "--status", status]);
    }
    s.ok(&["request", "unlink", &request, &issue]);
    assert!(
        json(&s, &["--json", "request", "show", &request])["issues"]
            .as_array()
            .unwrap()
            .is_empty()
    );
    s.ok(&["request", "link", &request, &issue]);
    assert!(
        fs::read_to_string(&path)
            .unwrap()
            .contains("unknown: 'verbatim value' # keep comment")
    );
    let handoff = json(
        &s,
        &[
            "--json",
            "handoff",
            "create",
            "Next session",
            "--file",
            body.to_str().unwrap(),
            "--issue",
            &issue,
        ],
    )["id"]
        .as_str()
        .unwrap()
        .to_owned();
    let handoff_before = json(&s, &["--json", "handoff", "show", &handoff]);
    let path = s
        .path("home/hub")
        .join(handoff_before["path"].as_str().unwrap());
    fs::write(
        &path,
        fs::read_to_string(&path).unwrap().replacen(
            "title:",
            "unknown: [keep, this] # handoff\ntitle:",
            1,
        ),
    )
    .unwrap();
    s.ok(&["handoff", "detach", &handoff]);
    s.ok(&["handoff", "attach", &handoff, &issue]);
    s.ok(&["handoff", "archive", &handoff]);
    assert!(!path.exists());
    assert!(
        json(&s, &["--json", "handoff", "list"])
            .as_array()
            .unwrap()
            .is_empty()
    );
    s.ok(&["handoff", "restore", &handoff]);
    let raw = s.ok(&["handoff", "show", &handoff, "--raw"]);
    assert!(raw.contains("unknown: [keep, this] # handoff"));
    assert!(raw.ends_with("Requested outcome\n\nKeep this prose.\n"));
    let clone = s.clone_hub(&s.path("remote.git"), "observer");
    assert!(
        clone
            .dir
            .join("projects/demo/requests")
            .read_dir()
            .unwrap()
            .count()
            > 1
    );
    assert!(
        clone
            .dir
            .join("projects/demo/handoffs")
            .join(path.file_name().unwrap())
            .exists()
    );
}
#[test]
fn native_plan_bundle_stale_revision_roundtrip_and_node_workflows() {
    let s = fixture();
    let issue = s
        .ok(&["create", "plan implementation", "--silent"])
        .trim()
        .to_owned();
    let draft = s.path("draft");
    let plan = json(
        &s,
        &[
            "--json",
            "plan",
            "init",
            "Bounded plan",
            "--output",
            draft.to_str().unwrap(),
        ],
    )["id"]
        .as_str()
        .unwrap()
        .to_owned();
    let manifest = draft.join("plan.md");
    let raw = fs::read_to_string(&manifest)
        .unwrap()
        .replace("title:", "# authored frontmatter comment\ntitle:")
        .replace(
            "nodes: []",
            "nodes:\n  - id: implement\n    label: Implement work\n    kind: workflow",
        );
    fs::write(&manifest, raw).unwrap();
    s.ok(&["plan", "validate", draft.to_str().unwrap()]);
    s.ok(&["plan", "put", draft.to_str().unwrap()]);
    let stale = s.path("stale");
    s.ok(&["plan", "get", &plan, "--output", stale.to_str().unwrap()]);
    s.ok(&["plan", "link", &plan, "implement", &issue]);
    let stale_result = s.cli(&["plan", "put", stale.to_str().unwrap()]);
    assert!(!stale_result.status.success());
    assert!(String::from_utf8_lossy(&stale_result.stderr).contains("stale plan"));
    s.ok(&["sync"]);
    let detail = json(&s, &["--json", "plan", "show", &plan]);
    assert_eq!(detail["graph"]["nodes"][0]["ref"], issue);
    let published = s.path("home/hub").join(detail["path"].as_str().unwrap());
    assert!(
        fs::read_to_string(&published)
            .unwrap()
            .contains("# authored frontmatter comment")
    );
    s.ok(&["plan", "status", &plan]);
    s.ok(&["plan", "unlink", &plan, "implement", &issue]);
    let fresh = s.path("fresh");
    s.ok(&["plan", "get", &plan, "--output", fresh.to_str().unwrap()]);
    fs::create_dir(fresh.join("sections")).unwrap();
    fs::write(
        fresh.join("sections/design.md"),
        "# Design\n\nAuthored design.\n",
    )
    .unwrap();
    let raw = fs::read_to_string(fresh.join("plan.md")).unwrap().replacen(
        "---\n\n",
        "sections: [sections/design.md]\n---\n\n",
        1,
    );
    fs::write(fresh.join("plan.md"), raw).unwrap();
    s.ok(&["plan", "put", fresh.to_str().unwrap()]);
    let roundtrip = s.path("roundtrip");
    s.ok(&[
        "plan",
        "get",
        &plan,
        "--output",
        roundtrip.to_str().unwrap(),
    ]);
    assert_eq!(
        fs::read(roundtrip.join("sections/design.md")).unwrap(),
        b"# Design\n\nAuthored design.\n"
    );
    let raw = fs::read_to_string(roundtrip.join("plan.md"))
        .unwrap()
        .replace("sections: [sections/design.md]\n", "");
    fs::write(roundtrip.join("plan.md"), raw).unwrap();
    fs::remove_file(roundtrip.join("sections/design.md")).unwrap();
    s.ok(&["plan", "put", roundtrip.to_str().unwrap()]);
    assert!(
        !published
            .parent()
            .unwrap()
            .join("sections/design.md")
            .exists()
    );
    let head = git(&s.path("home/hub"), &["rev-parse", "HEAD"]);
    s.ok(&["plan", "put", roundtrip.to_str().unwrap()]);
    assert_eq!(git(&s.path("home/hub"), &["rev-parse", "HEAD"]), head);
    s.ok(&["project", "create", "another"]);
    assert!(
        !s.cli(&[
            "--project",
            "another",
            "plan",
            "put",
            roundtrip.to_str().unwrap()
        ])
        .status
        .success()
    );
}
#[test]
fn native_memory_docs_projects_import_diagnostics_cache_and_root() {
    let s = fixture();
    s.ok(&[
        "remember",
        "Project fact",
        "--key",
        "project-fact",
        "--tag",
        "docs",
        "--type",
        "project",
    ]);
    s.ok(&[
        "remember",
        "Global fact",
        "--key",
        "global-fact",
        "--global",
    ]);
    assert_eq!(
        json(&s, &["--json", "memories", "--all"])
            .as_array()
            .unwrap()
            .len(),
        2
    );
    s.ok(&["forget", "global-fact", "--global"]);
    s.ok(&["doc", "new", "design/overview"]);
    assert!(s.ok(&["doc", "list"]).contains("overview"));
    assert!(s.ok(&["search", "Project fact"]).contains("memory"));
    s.ok(&["project", "create", "another"]);
    assert!(s.ok(&["project", "list"]).contains("another"));
    s.ok(&["project", "show", "another"]);
    let export = s.path("export.jsonl");
    fs::write(&export,"# bd export\n{bad}\n{\"id\":\"old-ab12\",\"title\":\"Imported\",\"status\":\"open\",\"issue_type\":\"task\",\"dependencies\":[{\"issue_id\":\"old-ab12\",\"depends_on_id\":\"missing-ab12\",\"type\":\"relates\"}]}\n{\"_type\":\"memory\",\"key\":\"bd-fact\",\"value\":\"Imported fact\"}\n").unwrap();
    let dry = json(
        &s,
        &[
            "--json",
            "import",
            "bd",
            export.to_str().unwrap(),
            "--dry-run",
        ],
    );
    assert_eq!(dry["report"]["issues"], 1);
    assert_eq!(dry["report"]["warnings"].as_array().unwrap().len(), 2);
    assert!(!s.cli(&["show", "old-ab12"]).status.success());
    s.ok(&["import", "bd", export.to_str().unwrap()]);
    assert_eq!(json(&s, &["--json", "show", "old-ab12"])["id"], "old-ab12");
    let malformed = s.path("home/hub/projects/demo/issues/bad.md");
    fs::write(&malformed, "---\nid: broken\n---\n").unwrap();
    let doctor = s.cli(&["--json", "doctor"]);
    assert!(!doctor.status.success());
    let diagnostics: serde_json::Value = serde_json::from_slice(&doctor.stdout).unwrap();
    assert!(!diagnostics["warnings"].as_array().unwrap().is_empty());
    let cache = s.path("home/cache");
    fs::write(cache.join("derived"), "cache").unwrap();
    fs::write(cache.join("op-journal.json"), "retained recovery evidence").unwrap();
    s.ok(&["cache", "clear"]);
    assert!(!cache.join("derived").exists());
    assert_eq!(
        fs::read_to_string(cache.join("op-journal.json")).unwrap(),
        "retained recovery evidence"
    );
    assert!(s.ok(&[]).contains("Usage:"));
    assert!(s.ok(&["--version"]).contains("bn"));
    assert!(s.ok(&["man"]).contains("bn"));
    assert_eq!(s.ok(&["prime"]), include_str!("../../docs/prime.md"));
}
#[test]
fn retained_command_and_flag_census_is_callable() {
    let baseline: serde_json::Value =
        serde_json::from_str(include_str!("../contract/commands.json")).unwrap();
    let mut root = beans::cli::command();
    root.build();
    for command in baseline["commands"].as_array().unwrap() {
        if command["retired"] == true {
            continue;
        }
        let path: Vec<_> = command["path"]
            .as_array()
            .unwrap()
            .iter()
            .map(|s| s.as_str().unwrap())
            .collect();
        if path.first().is_some_and(|s| matches!(*s, "serve" | "help")) {
            continue;
        }
        let mut actual = &root;
        for name in &path {
            actual = actual
                .find_subcommand(name)
                .unwrap_or_else(|| panic!("missing retained command {path:?}"));
        }
        for flag in command["local_flags"].as_array().unwrap() {
            let name = flag["name"].as_str().unwrap();
            if name == "help" {
                continue;
            }
            assert!(
                actual.get_arguments().any(|a| a.get_long() == Some(name)),
                "missing retained flag {path:?} --{name}"
            );
        }
    }
}
#[test]
fn stdin_bodies_and_atomic_validation_preserve_new_and_existing_projects() {
    use std::io::Write;
    use std::process::Stdio;
    let s = fixture();
    let issue = s
        .ok(&["create", "stdin issue", "--silent"])
        .trim()
        .to_owned();
    let stdin_run = |args: &[&str], body: &str| {
        let mut c = Command::new(env!("CARGO_BIN_EXE_bn"))
            .args(args)
            .env("BEANS_HOME", s.path("home"))
            .env("BEANS_PROJECT", "demo")
            .env_remove("BEANS_HUB")
            .env_remove("BN_CONFIG")
            .current_dir(&s.0)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .unwrap();
        c.stdin.take().unwrap().write_all(body.as_bytes()).unwrap();
        let o = c.wait_with_output().unwrap();
        assert!(o.status.success(), "{}", String::from_utf8_lossy(&o.stderr));
        serde_json::from_slice::<serde_json::Value>(&o.stdout).unwrap()
    };
    let handoff = stdin_run(
        &[
            "--json", "handoff", "create", "--file", "-", "--issue", &issue,
        ],
        "Verbatim continuation\n\n\n",
    );
    let id = handoff["id"].as_str().unwrap();
    assert_eq!(
        json(&s, &["--json", "handoff", "show", id])["body"],
        "Verbatim continuation\n\n\n"
    );
    let request = stdin_run(
        &["--json", "request", "create", "stdin request", "--stdin"],
        "stdin requested outcome\n",
    );
    let id = request["id"].as_str().unwrap();
    let before = json(&s, &["--json", "request", "show", id]);
    assert!(
        before["body"]
            .as_str()
            .unwrap()
            .contains("stdin requested outcome")
    );
    assert!(
        !s.cli(&["request", "link", id, &issue, "missing-ab12"])
            .status
            .success()
    );
    s.ok(&["sync"]);
    assert_eq!(
        json(&s, &["--json", "request", "show", id])["issues"],
        before["issues"]
    );
    assert!(
        !s.cli(&[
            "--project",
            "uncreated",
            "handoff",
            "create",
            "--file",
            "-",
            "--issue",
            "missing-ab12"
        ])
        .status
        .success()
    );
    assert!(!s.path("home/hub/projects/uncreated").exists());
    let explicit_empty = json(
        &s,
        &[
            "--json",
            "request",
            "create",
            "empty body",
            "--description",
            "",
        ],
    )["id"]
        .as_str()
        .unwrap()
        .to_owned();
    assert!(
        !json(&s, &["--json", "request", "show", &explicit_empty])["body"]
            .as_str()
            .unwrap()
            .contains("Outcome")
    );
}
#[test]
fn native_new_families_reject_symlink_destinations_without_external_writes() {
    use std::os::unix::fs::symlink;
    for family in ["requests", "handoffs", "memories", "docs", "plans"] {
        let s = fixture();
        let outside = s.path("authored-external");
        fs::create_dir(&outside).unwrap();
        fs::write(outside.join("keep.md"), "authored external bytes\n").unwrap();
        let destination = s.path(&format!("home/hub/projects/demo/{family}"));
        if destination.exists() {
            fs::remove_dir_all(&destination).unwrap();
        }
        symlink(&outside, &destination).unwrap();
        let body = s.path("body.md");
        fs::write(&body, "body\n").unwrap();
        let draft = s.path("draft");
        let output = match family {
            "requests" => s.cli(&["request", "create", "unsafe"]),
            "handoffs" => s.cli(&["handoff", "create", "--file", body.to_str().unwrap()]),
            "memories" => s.cli(&["remember", "unsafe", "--key", "unsafe"]),
            "docs" => s.cli(&["doc", "new", "unsafe"]),
            _ => {
                s.ok(&[
                    "plan",
                    "init",
                    "unsafe",
                    "--output",
                    draft.to_str().unwrap(),
                ]);
                s.cli(&["plan", "put", draft.to_str().unwrap()])
            }
        };
        assert!(!output.status.success(), "{family}");
        assert_eq!(
            fs::read_to_string(outside.join("keep.md")).unwrap(),
            "authored external bytes\n"
        );
        assert_eq!(fs::read_dir(&outside).unwrap().count(), 1);
    }
}
#[test]
fn native_alias_registry_closed_filters_and_partial_close_failure() {
    let s = fixture();
    let a = s.ok(&["create", "first", "--silent"]).trim().to_owned();
    let b = s
        .ok(&["create", "second", "--priority", "0", "--silent"])
        .trim()
        .to_owned();
    assert_eq!(json(&s, &["--json", "ready"])[0]["id"], b);
    let out = s.cli(&["close", &a, "missing-ab12", "-r", "finished"]);
    assert!(!out.status.success());
    s.ok(&["sync"]);
    assert_eq!(json(&s, &["--json", "show", &a])["status"], "closed");
    assert!(
        !s.ok(&["--json=false", "list", "--closed=false", "--limit", "-1"])
            .contains(&a)
    );
    let closed = json(&s, &["--json", "list", "--closed"]);
    assert!(closed.as_array().unwrap().iter().any(|i| i["id"] == a));
    assert!(!closed.as_array().unwrap().iter().any(|i| i["id"] == b));
    s.ok(&["reopen", &a]);
    let head = git(&s.path("home/hub"), &["rev-parse", "HEAD"]);
    s.ok(&["reopen", &a]);
    assert_eq!(git(&s.path("home/hub"), &["rev-parse", "HEAD"]), head);
    s.ok(&["doc", "new", "demo-collision", "--global"]);
    let hub = Hub {
        dir: s.path("home/hub"),
        cache: s.path("home/cache"),
        branch: "main".into(),
        actor: "Native Tester".into(),
        no_sync: false,
        throttle: Duration::ZERO,
    };
    let resolved = beans::vault::Resolved {
        project: b"demo".to_vec(),
        ..Default::default()
    };
    let mut op = beans::ops::IssueMutation::new(
        resolved.clone(),
        String::new(),
        beans::ops::IssueChange::Create {
            title: "collision".into(),
            kind: "task".into(),
            priority: 2,
            description: None,
        },
        "Native Tester".into(),
        None,
    );
    op.id = "demo-collision".into();
    assert!(hub.mutate(&mut op).is_err());
    assert!(
        fs::read_to_string(s.path("home/hub/docs/demo-collision.md"))
            .unwrap()
            .contains("# demo-collision")
    );
    let mut request = beans::ops::RecordMutation::new(
        resolved,
        String::new(),
        beans::ops::RecordChange::RequestCreate(beans::ops::RequestEdit {
            title: Some("collision".into()),
            ..Default::default()
        }),
        "Native Tester".into(),
        None,
    );
    request.id = "demo-collision".into();
    assert!(hub.mutate(&mut request).is_err());
}
#[test]
fn native_live_bd_export_force_rerun_is_no_change_and_no_hub_errors() {
    let empty = Sandbox::new();
    assert!(!empty.cli(&["ready"]).status.success());
    assert!(!empty.cli(&["request", "list"]).status.success());
    assert!(empty.ok(&["man"]).starts_with(".TH BN 1"));
    assert_eq!(empty.ok(&["prime"]), include_str!("../../docs/prime.md"));
    assert_eq!(empty.cli(&["create"]).status.code(), Some(2));
    let s = fixture();
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fixtures/native-baseline/cmd/bn/testdata/beans_export_2026-09-10.jsonl");
    let report = json(
        &s,
        &[
            "--json",
            "--project",
            "beans",
            "import",
            "bd",
            path.to_str().unwrap(),
        ],
    );
    assert!(report["report"]["issues"].as_u64().unwrap() > 10);
    assert!(report["report"]["rejected"].as_array().unwrap().is_empty());
    let hub = s.path("home/hub");
    let before = git(&hub, &["rev-parse", "HEAD"]);
    let repeated = json(
        &s,
        &[
            "--json",
            "--project",
            "beans",
            "import",
            "bd",
            path.to_str().unwrap(),
            "--force",
        ],
    );
    assert!(
        repeated["report"]["rejected"]
            .as_array()
            .unwrap()
            .is_empty()
    );
    assert_eq!(
        git(&hub, &["rev-parse", "HEAD"]),
        before,
        "{}",
        git(&hub, &["show", "--format=", "--"])
    );
}
#[test]
fn native_basename_parent_cycle_alias_registry_and_second_import_fixture() {
    let s = fixture();
    let a = s
        .ok(&["create", "alias owner", "--silent"])
        .trim()
        .to_owned();
    let b = s.ok(&["create", "dependent", "--silent"]).trim().to_owned();
    let path = issue_path(&s, &a);
    let basename = path.file_stem().unwrap().to_str().unwrap();
    s.ok(&["dep", "add", &b, basename]);
    s.ok(&["dep", "add", &b, basename, "--type", "parent-child"]);
    let cycle = s.cli(&["dep", "add", &a, &b, "--type", "parent-child"]);
    assert!(!cycle.status.success());
    assert!(String::from_utf8_lossy(&cycle.stderr).contains("cycle"));
    s.ok(&["sync"]);
    let source = fs::read(&path).unwrap();
    let mut authored = beans::domain::issue::IssueDocument::parse_bytes(
        "projects/demo/issues/authored.md",
        &source,
    )
    .unwrap();
    authored.metadata.aliases.push("demo-alias".into());
    fs::write(&path, authored.encode().unwrap().bytes).unwrap();
    let hub = Hub {
        dir: s.path("home/hub"),
        cache: s.path("home/cache"),
        branch: "main".into(),
        actor: "Native Tester".into(),
        no_sync: false,
        throttle: Duration::ZERO,
    };
    let mut op = beans::ops::IssueMutation::new(
        beans::vault::Resolved {
            project: b"demo".to_vec(),
            ..Default::default()
        },
        String::new(),
        beans::ops::IssueChange::Create {
            title: "collision".into(),
            kind: "task".into(),
            priority: 2,
            description: None,
        },
        "Native Tester".into(),
        None,
    );
    op.id = "demo-alias".into();
    assert!(hub.mutate(&mut op).is_err());
    assert!(fs::read_to_string(&path).unwrap().contains("demo-alias"));
    s.ok(&["sync"]);
    let export = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fixtures/native-baseline/cmd/bn/testdata/gastownhall_beads_export.jsonl");
    let report = json(
        &s,
        &[
            "--json",
            "--project",
            "gastownhall",
            "import",
            "bd",
            export.to_str().unwrap(),
        ],
    );
    assert!(report["report"]["issues"].as_u64().unwrap() > 0);
    assert!(report["report"]["rejected"].as_array().unwrap().is_empty());
    let ready = s.ok(&["create", "unblocked", "--silent"]).trim().to_owned();
    let blocker = s.ok(&["create", "blocker", "--silent"]).trim().to_owned();
    let waiting = s
        .ok(&["create", "waiting", "--blocked-by", &blocker, "--silent"])
        .trim()
        .to_owned();
    let result = json(
        &s,
        &["--json", "close", &blocker, "-r", "done", "--suggest-next"],
    );
    assert!(
        result["next_ready"]
            .as_array()
            .unwrap()
            .iter()
            .any(|v| v["id"] == waiting)
    );
    assert!(
        !result["next_ready"]
            .as_array()
            .unwrap()
            .iter()
            .any(|v| v["id"] == ready)
    );
}
