//! Reproductions of independently discovered CLI data-preservation and selection failures.
use super::*;

fn fixture() -> Sandbox {
    let s = Sandbox::new();
    let remote = s.remote(false);
    s.ok(&["init", remote.to_str().unwrap()]);
    s.ok(&["create", "seed"]);
    s
}
fn json(s: &Sandbox, args: &[&str]) -> serde_json::Value {
    serde_json::from_str(&s.ok(args)).unwrap()
}
fn cli_in(s: &Sandbox, cwd: &Path, args: &[&str], project: bool) -> Output {
    let mut command = Command::new(env!("CARGO_BIN_EXE_bn"));
    command
        .args(args)
        .env("BEANS_HOME", s.path("home"))
        .env("BN_ACTOR", "Native Tester")
        .env_remove("BEANS_HUB")
        .env_remove("BN_CONFIG")
        .current_dir(cwd);
    if project {
        command.env("BEANS_PROJECT", "demo");
    } else {
        command.env_remove("BEANS_PROJECT");
    }
    command.output().unwrap()
}
fn json_in(s: &Sandbox, cwd: &Path, args: &[&str]) -> serde_json::Value {
    let output = cli_in(s, cwd, args, false);
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
    serde_json::from_slice(&output.stdout).unwrap()
}
fn set_clock(path: &Path, handoff: bool, seconds: i64) {
    let source = fs::read(path).unwrap();
    let at = beans::domain::issue::Timestamp {
        seconds,
        ..Default::default()
    };
    let bytes = if handoff {
        let relative = format!(
            "projects/demo/handoffs/{}",
            path.file_name().unwrap().to_str().unwrap()
        );
        let mut document =
            beans::domain::handoff::HandoffDocument::parse_bytes(&relative, &source).unwrap();
        document.metadata.updated = at;
        document.encode().unwrap().bytes
    } else {
        let mut document =
            beans::domain::issue::IssueDocument::parse_bytes("record.md", &source).unwrap();
        document.metadata.updated = at.clone();
        // Encoding preserves authored log history; this fixture deliberately edits
        // the stored timestamps as an independent clone would.
        let mut text = String::from_utf8(document.encode().unwrap().bytes).unwrap();
        for entry in &document.log {
            let mut aged = entry.clone();
            aged.at = at.clone();
            text = text.replace(&entry.format().unwrap(), &aged.format().unwrap());
        }
        text.into_bytes()
    };
    fs::write(path, bytes).unwrap();
}
fn handoff(s: &Sandbox, title: &str) -> String {
    let body = s.path("handoff-body.md");
    fs::write(&body, "Authored continuation body.\n").unwrap();
    s.ok(&["handoff", "create", title, "--file", body.to_str().unwrap()])
        .trim()
        .to_owned()
}

#[test]
fn project_link_preserves_unknown_tables_comments_and_remote_array_trivia() {
    let s = fixture();
    let source = "# Authored project heading\nname = 'demo'\nprefix = 'demo'\nREMOTES = [ # retain array comment\n  'https://example.invalid/old', # retain item comment\n] # retain close comment\ncustom = { flag = true }\n[workflow]\nactive = ['open'] # retain workflow comment\n[vendor]\nimportant = 'keep me'\n[vendor.nested]\nsetting = 123\n";
    let path = s.path("home/hub/projects/demo/beans.toml");
    fs::write(&path, source).unwrap();
    s.ok(&["sync"]);
    let repo = s.path("consumer");
    fs::create_dir(&repo).unwrap();
    git(&repo, &["init", "--initial-branch=main"]);
    git(
        &repo,
        &["remote", "add", "origin", "https://example.invalid/current"],
    );
    for command in [
        vec!["project", "link", "demo"],
        vec!["project", "create", "demo", "--link"],
    ] {
        let output = cli_in(&s, &repo, &command, true);
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        let after = fs::read_to_string(&path).unwrap();
        assert!(after.starts_with("# Authored project heading\nname = 'demo'\nprefix = 'demo'\nREMOTES = [ # retain array comment\n  'https://example.invalid/old', # retain item comment\n"));
        assert!(after.ends_with("] # retain close comment\ncustom = { flag = true }\n[workflow]\nactive = ['open'] # retain workflow comment\n[vendor]\nimportant = 'keep me'\n[vendor.nested]\nsetting = 123\n"));
        let cfg = beans::domain::config::decode_project_config(after.as_bytes()).unwrap();
        assert_eq!(cfg.remotes.unwrap().len(), 2);
    }
    let head = git(&s.path("home/hub"), &["rev-parse", "HEAD"]);
    let bytes = fs::read(&path).unwrap();
    assert!(
        cli_in(&s, &repo, &["project", "link", "demo"], true)
            .status
            .success()
    );
    assert_eq!(fs::read(&path).unwrap(), bytes);
    assert_eq!(git(&s.path("home/hub"), &["rev-parse", "HEAD"]), head);
}

#[test]
fn doc_discovery_includes_globals_and_supports_unscoped_and_auto_resolved_reads() {
    let s = fixture();
    s.ok(&["doc", "new", "global-guide", "--global"]);
    s.ok(&["doc", "new", "demo-guide"]);
    s.ok(&["--project", "other", "doc", "new", "other-guide"]);
    let scoped = json(&s, &["--json", "doc", "list"]);
    assert_eq!(scoped.as_array().unwrap().len(), 2);
    let global = json(&s, &["--json", "doc", "list", "--global"]);
    assert_eq!(global.as_array().unwrap().len(), 1);
    let unscoped = json_in(&s, &s.0, &["--json", "doc", "list"]);
    assert_eq!(unscoped.as_array().unwrap().len(), 3);
    assert!(json_in(&s, &s.0, &["--json", "doc", "backlinks", "global-guide"]).is_array());
    let repo = s.path("consumer");
    fs::create_dir(&repo).unwrap();
    git(&repo, &["init", "--initial-branch=main"]);
    git(
        &repo,
        &[
            "remote",
            "add",
            "origin",
            "https://example.invalid/docs-consumer",
        ],
    );
    assert!(
        cli_in(&s, &repo, &["project", "link", "demo"], true)
            .status
            .success()
    );
    assert_eq!(
        json_in(&s, &repo, &["--json", "doc", "list"])
            .as_array()
            .unwrap()
            .len(),
        2
    );
}

#[test]
fn handoff_archive_dry_run_validates_batches_and_emits_json_without_effects() {
    let s = fixture();
    let id = handoff(&s, "continue");
    let head = git(&s.path("home/hub"), &["rev-parse", "HEAD"]);
    let result = json(&s, &["--json", "handoff", "archive", &id, "--dry-run"]);
    assert_eq!(result["dry_run"], true);
    assert_eq!(result["ids"], serde_json::json!([id]));
    for ids in [vec!["demo-h-missing"], vec![id.as_str(), "demo-h-missing"]] {
        let mut args = vec!["--json", "handoff", "archive"];
        args.extend(ids);
        args.push("--dry-run");
        assert!(!s.cli(&args).status.success());
    }
    assert_eq!(git(&s.path("home/hub"), &["rev-parse", "HEAD"]), head);
    assert!(git(&s.path("home/hub"), &["status", "--porcelain"]).is_empty());
    assert_eq!(
        json(
            &s,
            &[
                "--json",
                "handoff",
                "archive",
                "--older-than",
                "30d",
                "--dry-run"
            ]
        )["count"],
        0
    );
    s.ok(&["handoff", "archive", &id]);
    assert_eq!(
        json(&s, &["--json", "handoff", "archive", &id, "--dry-run"])["count"],
        0
    );
    assert_eq!(
        json(
            &s,
            &[
                "--json",
                "handoff",
                "archive",
                "--older-than",
                "30d",
                "--dry-run"
            ]
        )["ids"],
        serde_json::json!([])
    );
}

#[test]
fn age_archives_select_latest_two_clone_eligibility_for_issues_and_handoffs() {
    for is_handoff in [false, true] {
        let s = fixture();
        let (a, b) = if is_handoff {
            (
                handoff(&s, "old continuation"),
                handoff(&s, "fresh continuation"),
            )
        } else {
            let a = s.ok(&["create", "old issue"]).trim().to_owned();
            let b = s.ok(&["create", "fresh issue"]).trim().to_owned();
            s.ok(&["close", &a, &b, "-r", "finished"]);
            (a, b)
        };
        let a_info = if is_handoff {
            json(&s, &["--json", "handoff", "show", &a])
        } else {
            json(&s, &["--json", "show", &a])
        };
        let b_info = if is_handoff {
            json(&s, &["--json", "handoff", "show", &b])
        } else {
            json(&s, &["--json", "show", &b])
        };
        let a_path = PathBuf::from(a_info["path"].as_str().unwrap());
        let b_path = PathBuf::from(b_info["path"].as_str().unwrap());
        set_clock(&s.path("home/hub").join(&a_path), is_handoff, 1_577_836_800);
        s.ok(&["sync"]);
        let other = s.clone_hub(&s.path("remote.git"), "other");
        set_clock(
            &other.dir.join(&a_path),
            is_handoff,
            time::OffsetDateTime::now_utc().unix_timestamp(),
        );
        set_clock(&other.dir.join(&b_path), is_handoff, 1_577_836_800);
        git(&other.dir, &["add", "-A"]);
        git(
            &other.dir,
            &["commit", "-m", "refresh old record and age another"],
        );
        git(&other.dir, &["push", "origin", "main"]);
        if is_handoff {
            s.ok(&["handoff", "archive", "--older-than", "30d"]);
        } else {
            s.ok(&["archive", "--older-than", "30d"]);
        }
        assert!(
            s.path("home/hub").join(a_path).exists(),
            "recent remote record was archived"
        );
        assert!(
            !s.path("home/hub").join(b_path).exists(),
            "newly eligible remote record was omitted"
        );
        let b_after = if is_handoff {
            json(&s, &["--json", "handoff", "show", &b])
        } else {
            json(&s, &["--json", "show", &b])
        };
        assert!(b_after["path"].as_str().unwrap().contains("/archive/"));
        assert!(git(&s.path("home/hub"), &["status", "--porcelain"]).is_empty());
    }
}

#[test]
fn age_archive_rechecks_eligibility_after_push_race_and_rename_merge() {
    for is_handoff in [false, true] {
        let s = fixture();
        let (a, b) = if is_handoff {
            (
                handoff(&s, "old continuation"),
                handoff(&s, "fresh continuation"),
            )
        } else {
            let a = s.ok(&["create", "old issue"]).trim().to_owned();
            let b = s.ok(&["create", "fresh issue"]).trim().to_owned();
            s.ok(&["close", &a, &b, "-r", "finished"]);
            (a, b)
        };
        let show = |id: &str| {
            if is_handoff {
                json(&s, &["--json", "handoff", "show", id])
            } else {
                json(&s, &["--json", "show", id])
            }
        };
        let a_path = PathBuf::from(show(&a)["path"].as_str().unwrap());
        let b_path = PathBuf::from(show(&b)["path"].as_str().unwrap());
        set_clock(&s.path("home/hub").join(&a_path), is_handoff, 1_577_836_800);
        s.ok(&["sync"]);
        let other = s.clone_hub(&s.path("remote.git"), "other");
        set_clock(
            &other.dir.join(&a_path),
            is_handoff,
            time::OffsetDateTime::now_utc().unix_timestamp(),
        );
        set_clock(&other.dir.join(&b_path), is_handoff, 1_577_836_800);
        let refreshed = fs::read(other.dir.join(&a_path)).unwrap();
        git(&other.dir, &["add", "-A"]);
        git(
            &other.dir,
            &["commit", "-m", "race refresh and newly eligible archive"],
        );
        let hub = Hub {
            dir: s.path("home/hub"),
            cache: s.path("home/cache"),
            branch: "main".into(),
            actor: "Native Tester".into(),
            no_sync: false,
            throttle: Duration::ZERO,
        };
        let marker = s.path("raced");
        hook(
            &hub,
            "pre-push",
            &format!(
                "test ! -f '{}' || exit 0\ntouch '{}'\ngit -C '{}' push -q origin main",
                marker.display(),
                marker.display(),
                other.dir.display()
            ),
        );
        if is_handoff {
            s.ok(&["handoff", "archive", "--older-than", "30d"]);
        } else {
            s.ok(&["archive", "--older-than", "30d"]);
        }
        assert_eq!(
            fs::read(hub.dir.join(&a_path)).unwrap(),
            refreshed,
            "a competing fresh record must remain live with its exact bytes"
        );
        assert!(
            !hub.dir.join(&b_path).exists(),
            "a newly eligible competing record must be archived"
        );
        assert!(show(&b)["path"].as_str().unwrap().contains("/archive/"));
        assert!(git(&hub.dir, &["status", "--porcelain"]).is_empty());
        assert_eq!(
            git(&hub.dir, &["rev-parse", "HEAD"]),
            git(&hub.dir, &["rev-parse", "origin/main"])
        );
    }
}
