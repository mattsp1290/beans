//! Public prompt command, JSON, actor, and diagnostic contracts.
use super::*;

#[test]
fn native_prompt_cli_contract_and_journey() {
    let mut root = beans::cli::command();
    root.build();
    let prompt = root
        .find_subcommand("prompt")
        .expect("the public prompt command is registered");
    for command in [
        "create", "list", "inbox", "show", "update", "link", "unlink",
    ] {
        assert!(
            prompt.find_subcommand(command).is_some(),
            "missing prompt {command}"
        );
    }

    let s = fixture();
    let issue = s
        .ok(&["create", "prompt issue", "--silent"])
        .trim()
        .to_owned();
    let created = json(
        &s,
        &[
            "--json",
            "--actor",
            "  sender  ",
            "prompt",
            "create",
            "Review CLI",
            "--to",
            "  queue  ",
            "--description",
            "Exact body\n\n",
            "--priority",
            "1",
            "--label",
            "cli",
            "--issue",
            &issue,
        ],
    );
    assert_eq!(
        created
            .as_object()
            .unwrap()
            .keys()
            .cloned()
            .collect::<std::collections::BTreeSet<_>>(),
        ["commit", "id", "key", "message", "pushed", "sha"]
            .into_iter()
            .map(str::to_owned)
            .collect()
    );
    let id = created["id"].as_str().unwrap().to_owned();
    assert_eq!(created["key"], id);
    assert_eq!(created["commit"], created["sha"]);
    assert_eq!(created["pushed"], true);

    let detail = json(&s, &["--json", "prompt", "show", &id]);
    assert_eq!(
        detail
            .as_object()
            .unwrap()
            .keys()
            .cloned()
            .collect::<std::collections::BTreeSet<_>>(),
        [
            "aliases",
            "body",
            "claimed_by",
            "created",
            "from",
            "id",
            "issue",
            "labels",
            "log",
            "path",
            "priority",
            "project",
            "status",
            "title",
            "to",
            "updated",
        ]
        .into_iter()
        .map(str::to_owned)
        .collect()
    );
    assert_eq!(detail["from"], "sender");
    assert_eq!(detail["to"], "queue");
    assert_eq!(detail["claimed_by"], serde_json::Value::Null);
    assert_eq!(
        detail["issue"],
        serde_json::json!({"raw": format!("[[{issue}]]"), "target": issue})
    );
    assert_eq!(detail["body"], "Exact body\n\n");
    for timestamp in ["created", "updated"] {
        assert_eq!(
            detail[timestamp]
                .as_object()
                .unwrap()
                .keys()
                .cloned()
                .collect::<std::collections::BTreeSet<_>>(),
            ["nanoseconds", "offset_seconds", "seconds"]
                .into_iter()
                .map(str::to_owned)
                .collect()
        );
    }
    assert_eq!(
        detail["log"][0]
            .as_object()
            .unwrap()
            .keys()
            .cloned()
            .collect::<std::collections::BTreeSet<_>>(),
        ["actor", "at", "branch", "event", "raw", "repo", "sha"]
            .into_iter()
            .map(str::to_owned)
            .collect()
    );
    let path = s.path("home/hub").join(detail["path"].as_str().unwrap());
    assert!(path.ends_with(format!("{id}-review-cli.md")));
    assert_eq!(
        s.ok(&["prompt", "show", &id]).as_bytes(),
        fs::read(&path).unwrap()
    );

    let summaries = json(
        &s,
        &[
            "--json",
            "prompt",
            "list",
            "--from",
            "sender",
            "--to",
            "queue",
            "--label",
            "cli",
            "--priority",
            "1",
            "--query",
            "exact body",
        ],
    );
    assert_eq!(summaries.as_array().unwrap().len(), 1);
    assert_eq!(summaries[0].as_object().unwrap().len(), 14);
    assert_eq!(
        json(&s, &["--json", "prompt", "inbox", "--to", " queue "])[0]["id"],
        id
    );
    let identities = fixture();
    assert!(
        identities
            .cli(&[
                "--actor", " ", "prompt", "inbox", "--to", "queue", "--limit", "1",
            ])
            .status
            .success()
    );
    let explicit_sender = identities.cli(&[
        "--actor",
        " ",
        "prompt",
        "create",
        "Explicit sender",
        "--to",
        "queue",
        "--from",
        "sender",
    ]);
    assert!(explicit_sender.status.success());
    let explicit_id = String::from_utf8(explicit_sender.stdout).unwrap();
    let missing_claimant = identities.cli(&[
        "--actor",
        " ",
        "prompt",
        "update",
        explicit_id.trim(),
        "--claim",
    ]);
    assert!(!missing_claimant.status.success());
    assert!(
        String::from_utf8_lossy(&missing_claimant.stderr)
            .contains("set global --actor or BN_ACTOR")
    );
    let missing_sender = s.cli(&[
        "--actor",
        " ",
        "prompt",
        "create",
        "Missing sender",
        "--to",
        "queue",
    ]);
    assert!(!missing_sender.status.success());
    assert!(
        String::from_utf8_lossy(&missing_sender.stderr).contains("set global --actor or BN_ACTOR")
    );

    s.ok(&["--actor", " worker ", "prompt", "update", &id, "--claim"]);
    let claimed = json(&s, &["--json", "prompt", "show", &id]);
    assert_eq!(claimed["status"], "in_progress");
    assert_eq!(claimed["claimed_by"], "worker");
    let claimed_bytes = fs::read(&path).unwrap();
    let head = git(&s.path("home/hub"), &["rev-parse", "HEAD"]);
    let replay = json(
        &s,
        &[
            "--json", "--actor", "worker", "prompt", "update", &id, "--claim",
        ],
    );
    assert_eq!(replay["sha"], "");
    assert_eq!(replay["commit"], "");
    assert_eq!(git(&s.path("home/hub"), &["rev-parse", "HEAD"]), head);
    let conflict = s.cli(&["--actor", "other", "prompt", "update", &id, "--claim"]);
    assert!(!conflict.status.success());
    assert_eq!(fs::read(&path).unwrap(), claimed_bytes);

    s.ok(&[
        "--actor",
        "worker",
        "prompt",
        "update",
        &id,
        "--status",
        "completed",
        "--note",
        "completion evidence",
    ]);
    assert!(
        json(&s, &["--json", "prompt", "list"])
            .as_array()
            .unwrap()
            .is_empty()
    );
    assert_eq!(
        json(&s, &["--json", "prompt", "list", "--terminal"])[0]["id"],
        id
    );
    s.ok(&["prompt", "update", &id, "--to", "next", "--force"]);
    let reset = json(&s, &["--json", "prompt", "show", &id]);
    assert_eq!(reset["status"], "open");
    assert_eq!(reset["claimed_by"], serde_json::Value::Null);
    assert_eq!(reset["to"], "next");

    let before_name = path.clone();
    s.ok(&["prompt", "update", &id, "--title", "Renamed"]);
    assert!(before_name.exists());
    assert!(
        !s.path("home/hub/projects/demo/prompts")
            .join(format!("{id}-renamed.md"))
            .exists()
    );
    let noop = json(
        &s,
        &["--json", "prompt", "update", &id, "--title", "Renamed"],
    );
    assert_eq!(noop["sha"], "");
    assert!(!s.cli(&["prompt", "update", &id]).status.success());
    assert!(
        !s.cli(&["prompt", "update", &id, "--force"])
            .status
            .success()
    );
    assert!(
        !s.cli(&["prompt", "update", &id, "--to", "x", "--claim", "--force"])
            .status
            .success()
    );
    assert!(
        !s.cli(&[
            "prompt", "update", &id, "--to", "x", "--status", "open", "--force",
        ])
        .status
        .success()
    );

    s.ok(&["prompt", "unlink", &id]);
    assert_eq!(
        json(&s, &["--json", "prompt", "show", &id])["issue"],
        serde_json::Value::Null
    );
    s.ok(&["prompt", "link", &id, &issue]);
    assert_eq!(
        json(&s, &["--json", "prompt", "show", &id])["issue"]["target"],
        issue
    );
    assert!(
        json(&s, &["--json", "search", "Renamed", "--kind", "prompt"])
            .as_array()
            .unwrap()
            .iter()
            .any(|hit| hit["ID"] == id)
    );
}

#[test]
fn prompt_help_and_generated_man_are_an_exact_public_contract() {
    let mut root = beans::cli::command();
    root.build();
    let prompt = root.find_subcommand("prompt").unwrap();
    type FlagContract<'a> = (&'a str, Option<char>, Option<&'a str>);
    let expected: &[(&str, &[FlagContract<'_>])] = &[
        (
            "create",
            &[
                ("to", None, None),
                ("from", None, None),
                ("issue", None, None),
                ("label", Some('l'), None),
                ("priority", Some('p'), Some("2")),
                ("description", Some('d'), None),
                ("body-file", None, None),
                ("stdin", None, None),
            ],
        ),
        (
            "list",
            &[
                ("all-projects", None, None),
                ("from", None, None),
                ("to", None, None),
                ("claimed-by", None, None),
                ("status", None, None),
                ("label", None, None),
                ("priority", Some('p'), None),
                ("query", None, None),
                ("terminal", None, None),
            ],
        ),
        (
            "inbox",
            &[
                ("to", None, None),
                ("all-projects", None, None),
                ("claimed", None, None),
                ("terminal", None, None),
                ("limit", None, Some("50")),
            ],
        ),
        ("show", &[]),
        (
            "update",
            &[
                ("claim", None, None),
                ("status", None, None),
                ("force", None, None),
                ("title", None, None),
                ("to", None, None),
                ("priority", None, None),
                ("label", None, None),
                ("unlabel", None, None),
                ("note", None, None),
                ("description", Some('d'), None),
                ("body-file", None, None),
                ("stdin", None, None),
            ],
        ),
        ("link", &[]),
        ("unlink", &[]),
    ];
    for (name, flags) in expected {
        let command = prompt.find_subcommand(name).unwrap();
        for (long, short, default) in *flags {
            let arg = command
                .get_arguments()
                .find(|arg| arg.get_long() == Some(*long))
                .unwrap_or_else(|| panic!("prompt {name} missing --{long}"));
            assert_eq!(arg.get_short(), *short, "prompt {name} --{long}");
            assert!(
                arg.get_help().is_some(),
                "prompt {name} --{long} lacks usage"
            );
            let expected_default = default.or_else(|| {
                [
                    "all-projects",
                    "claimed",
                    "claim",
                    "force",
                    "stdin",
                    "terminal",
                ]
                .contains(long)
                .then_some("false")
            });
            assert_eq!(
                arg.get_default_values().first().and_then(|v| v.to_str()),
                expected_default,
                "prompt {name} --{long} default",
            );
        }
        let mut copy = command.clone();
        let help = copy.render_long_help().to_string();
        assert!(help.contains("Usage:"));
        for (long, _, _) in *flags {
            assert!(help.contains(&format!("--{long}")), "{help}");
        }
    }
    let man = String::from_utf8(fixture().cli(&["man"]).stdout).unwrap();
    for command in [
        "create", "list", "inbox", "show", "update", "link", "unlink",
    ] {
        assert!(man.contains(&format!(".SS bn prompt {command}")));
    }
    for flag in [
        "all\\-projects",
        "body\\-file",
        "claim",
        "claimed",
        "claimed\\-by",
        "description",
        "force",
        "from",
        "issue",
        "label",
        "limit",
        "note",
        "priority",
        "query",
        "status",
        "stdin",
        "terminal",
        "title",
        "to",
        "unlabel",
    ] {
        assert!(man.contains(&format!("\\-\\-{flag}")), "man lacks --{flag}");
    }
    assert!(man.contains("[default: 50]"));
    assert!(man.contains("[default: 2]"));
    assert!(man.contains("\\-d, \\-\\-description"));
    assert!(man.contains("\\-l, \\-\\-label"));
    assert!(man.contains("\\-p, \\-\\-priority"));
}

#[test]
fn prompt_actor_resolution_chain_is_real_trimmed_case_preserving_and_never_broadens() {
    let s = fixture();
    fs::create_dir_all(s.path("identity-home")).unwrap();
    let cases = [
        ("flag", Some("  Flag Case  "), None, None, None, "Flag Case"),
        ("env", None, Some("  Env Case  "), None, None, "Env Case"),
        (
            "config",
            None,
            None,
            Some("  Config Case  "),
            None,
            "Config Case",
        ),
        ("git", None, None, None, Some("  Git Case  "), "Git Case"),
        ("user", None, None, None, None, "User Case"),
    ];
    for (tag, flag_actor, env_actor, configured, git_actor, expected) in cases {
        fs::write(
            s.path("home/config.toml"),
            configured.map_or_else(String::new, |actor| format!("actor = {actor:?}\n")),
        )
        .unwrap();
        fs::write(
            s.path("identity-home/.gitconfig"),
            git_actor.map_or_else(String::new, |actor| format!("[user]\n\tname = {actor}\n")),
        )
        .unwrap();
        let mut prefix = Vec::new();
        if let Some(actor) = flag_actor {
            prefix.extend(["--actor", actor]);
        }
        let envs = [
            ("BN_ACTOR", env_actor.unwrap_or("")),
            ("USER", if tag == "user" { "  User Case  " } else { "" }),
        ];
        let title = format!("actor {tag}");
        let mut create = prefix.clone();
        create.extend(["--json", "prompt", "create", &title, "--to", expected]);
        let output = s.cli_env(&create, &envs);
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        let created: serde_json::Value = serde_json::from_slice(&output.stdout).unwrap();
        let id = created["id"].as_str().unwrap();
        let mut show = prefix.clone();
        show.extend(["--json", "prompt", "show", id]);
        let detail: serde_json::Value =
            serde_json::from_slice(&s.cli_env(&show, &envs).stdout).unwrap();
        assert_eq!(detail["from"], expected);
        let mut inbox = prefix.clone();
        inbox.extend(["--json", "prompt", "inbox"]);
        let items: serde_json::Value =
            serde_json::from_slice(&s.cli_env(&inbox, &envs).stdout).unwrap();
        assert!(
            items
                .as_array()
                .unwrap()
                .iter()
                .any(|item| item["id"] == id)
        );
        let mut claim = prefix.clone();
        claim.extend(["prompt", "update", id, "--claim"]);
        let claimed = s.cli_env(&claim, &envs);
        assert!(
            claimed.status.success(),
            "{}",
            String::from_utf8_lossy(&claimed.stderr)
        );
        let detail: serde_json::Value =
            serde_json::from_slice(&s.cli_env(&show, &envs).stdout).unwrap();
        assert_eq!(detail["claimed_by"], expected);
    }
    fs::write(s.path("home/config.toml"), "actor = '   '\n").unwrap();
    fs::write(s.path("identity-home/.gitconfig"), "[user]\n\tname =    \n").unwrap();
    for args in [
        vec!["prompt", "inbox"],
        vec!["prompt", "create", "missing", "--to", "queue"],
    ] {
        let out = s.cli_env(&args, &[("BN_ACTOR", " "), ("USER", " ")]);
        assert!(!out.status.success());
        assert!(String::from_utf8_lossy(&out.stderr).contains("set global --actor or BN_ACTOR"));
        assert!(out.stdout.is_empty());
    }
    let explicit = s.cli_env(
        &["--json", "prompt", "inbox", "--to", "nobody"],
        &[("BN_ACTOR", " "), ("USER", " ")],
    );
    assert!(explicit.status.success());
    assert_eq!(
        serde_json::from_slice::<serde_json::Value>(&explicit.stdout).unwrap(),
        serde_json::json!([])
    );
}

#[test]
fn prompt_json_fixture_and_failure_atomicity_are_exact() {
    fn normalize(value: &mut serde_json::Value, id: &str, second_id: &str, issue: &str) {
        match value {
            serde_json::Value::Array(values) => {
                for value in values {
                    normalize(value, id, second_id, issue);
                }
            }
            serde_json::Value::Object(object) => {
                if object
                    .keys()
                    .any(|key| matches!(key.as_str(), "seconds" | "nanoseconds" | "offset_seconds"))
                    && object.len() == 3
                {
                    object.insert("seconds".into(), serde_json::json!(0));
                    object.insert("nanoseconds".into(), serde_json::json!(0));
                    object.insert("offset_seconds".into(), serde_json::json!(0));
                    return;
                }
                let is_log = object.contains_key("event");
                for (key, value) in object.iter_mut() {
                    if key == "repo" {
                        *value = serde_json::json!("<REPO>");
                    } else if key == "branch" {
                        *value = serde_json::json!("<BRANCH>");
                    } else if key == "sha" && is_log {
                        *value = serde_json::json!("<LOG_SHA>");
                    } else if matches!(key.as_str(), "sha" | "commit")
                        && value.as_str().is_some_and(|v| !v.is_empty())
                    {
                        *value = serde_json::json!("<SHA>");
                    } else {
                        normalize(value, id, second_id, issue);
                    }
                }
            }
            serde_json::Value::String(text) => {
                *text = text
                    .replace(id, "<ID>")
                    .replace(second_id, "<SECOND_ID>")
                    .replace(issue, "<ISSUE>");
            }
            _ => {}
        }
    }
    let expected: serde_json::Value =
        serde_json::from_str(include_str!("../../fixtures/expected/prompts-cli.json")).unwrap();
    let s = fixture();
    let issue = s
        .ok(&["create", "fixture issue", "--silent"])
        .trim()
        .to_owned();
    let mut actual = serde_json::Map::new();
    let created = json(
        &s,
        &[
            "--json",
            "prompt",
            "create",
            "Fixture Prompt",
            "--to",
            "Native Tester",
            "--description",
            "fixture body\n",
            "--priority",
            "1",
            "--label",
            "fixture",
        ],
    );
    let id = created["id"].as_str().unwrap().to_owned();
    actual.insert("create".into(), created);
    let second = json(
        &s,
        &[
            "--json",
            "prompt",
            "create",
            "Fixture Earlier",
            "--to",
            "Native Tester",
            "--description",
            "second fixture body\n",
            "--priority",
            "0",
            "--label",
            "fixture",
        ],
    );
    let second_id = second["id"].as_str().unwrap().to_owned();
    actual.insert(
        "link".into(),
        json(&s, &["--json", "prompt", "link", &id, &issue]),
    );
    actual.insert(
        "list".into(),
        json(&s, &["--json", "prompt", "list", "--label", "fixture"]),
    );
    actual.insert("inbox".into(), json(&s, &["--json", "prompt", "inbox"]));
    actual.insert("show".into(), json(&s, &["--json", "prompt", "show", &id]));
    actual.insert(
        "update".into(),
        json(
            &s,
            &["--json", "prompt", "update", &id, "--title", "Updated"],
        ),
    );
    actual.insert(
        "unlink".into(),
        json(&s, &["--json", "prompt", "unlink", &id]),
    );
    actual.insert(
        "same_title_noop".into(),
        json(
            &s,
            &["--json", "prompt", "update", &id, "--title", "Updated"],
        ),
    );
    let mut actual = serde_json::Value::Object(actual);
    normalize(&mut actual, &id, &second_id, &issue);
    assert_eq!(actual, expected);

    let path = s
        .path("home/hub/projects/demo/prompts")
        .join(format!("{id}-fixture-prompt.md"));
    for args in [
        vec![
            "--json", "prompt", "create", "bad", "--to", "queue", "--bogus",
        ],
        vec!["--json", "prompt", "update", &id, "--note", "   "],
        vec!["--json", "prompt", "show", "missing-p-ab12"],
    ] {
        let head = git(&s.path("home/hub"), &["rev-parse", "HEAD"]);
        let bytes = fs::read(&path).unwrap();
        let output = s.cli(&args);
        assert!(!output.status.success());
        assert!(!output.stderr.is_empty());
        assert!(serde_json::from_slice::<serde_json::Value>(&output.stdout).is_err());
        assert_eq!(git(&s.path("home/hub"), &["rev-parse", "HEAD"]), head);
        assert_eq!(fs::read(&path).unwrap(), bytes);
    }
}

#[test]
fn prompt_native_cli_creation_query_transition_noop_and_diagnostic_matrices() {
    let s = fixture();
    let body = s.path("prompt-body.md");
    fs::write(&body, b"body-file bytes\n\n").unwrap();
    let from_file = json(
        &s,
        &[
            "--json",
            "prompt",
            "create",
            "Body File",
            "--to",
            "queue",
            "--body-file",
            body.to_str().unwrap(),
            "--priority",
            "3",
            "--label",
            "dupe",
        ],
    );
    let file_id = from_file["id"].as_str().unwrap().to_owned();
    assert_eq!(
        json(&s, &["--json", "prompt", "show", &file_id])["body"],
        "body-file bytes\n\n"
    );
    let fallback = json(
        &s,
        &[
            "--json",
            "prompt",
            "create",
            "!!!",
            "--to",
            "queue",
            "--description",
            "",
        ],
    );
    let fallback_id = fallback["id"].as_str().unwrap();
    let fallback_detail = json(&s, &["--json", "prompt", "show", fallback_id]);
    assert_eq!(fallback_detail["body"], "");
    assert!(
        s.path("home/hub")
            .join(fallback_detail["path"].as_str().unwrap())
            .ends_with(format!("{fallback_id}.md"))
    );
    let templated = json(
        &s,
        &[
            "--json",
            "prompt",
            "create",
            "Templated",
            "--to",
            "queue",
            "--priority",
            "0",
        ],
    );
    let templated_id = templated["id"].as_str().unwrap();
    assert!(
        json(&s, &["--json", "prompt", "show", templated_id])["body"]
            .as_str()
            .unwrap()
            .as_bytes()
            .starts_with(beans::domain::template::default_prompt_template())
    );

    let ordered = json(&s, &["--json", "prompt", "list"]);
    assert_eq!(ordered[0]["id"], templated_id);
    assert_eq!(
        json(
            &s,
            &["--json", "prompt", "inbox", "--to", "queue", "--limit", "0"]
        )
        .as_array()
        .unwrap()
        .len(),
        3
    );
    assert_eq!(
        json(
            &s,
            &[
                "--json",
                "prompt",
                "list",
                "--priority",
                "3",
                "--label",
                "dupe",
                "--query",
                "body-file"
            ]
        )[0]["id"],
        file_id
    );

    let path = s.path("home/hub").join(
        json(&s, &["--json", "prompt", "show", &file_id])["path"]
            .as_str()
            .unwrap(),
    );
    for edit in [
        vec!["--title", "Body File"],
        vec!["--status", "open"],
        vec!["--label", "dupe"],
        vec!["--unlabel", "absent"],
    ] {
        let head = git(&s.path("home/hub"), &["rev-parse", "HEAD"]);
        let bytes = fs::read(&path).unwrap();
        let mut args = vec!["--json", "prompt", "update", &file_id];
        args.extend(edit);
        let result: serde_json::Value = serde_json::from_slice(&s.cli(&args).stdout).unwrap();
        assert_eq!(result["sha"], "");
        assert_eq!(git(&s.path("home/hub"), &["rev-parse", "HEAD"]), head);
        assert_eq!(fs::read(&path).unwrap(), bytes);
    }
    for args in [
        vec!["prompt", "update", &file_id, "--status", "in_progress"],
        vec!["prompt", "update", &file_id, "--status", "completed"],
        vec![
            "prompt",
            "update",
            &file_id,
            "--status",
            "completed",
            "--force",
        ],
    ] {
        assert!(!s.cli(&args).status.success());
    }
    for extra in [
        vec!["--to", "new", "--claim"],
        vec!["--to", "new", "--claim", "--force"],
        vec!["--to", "new", "--status", "open"],
        vec!["--to", "new", "--status", "open", "--force"],
    ] {
        let mut args = vec!["prompt", "update", &file_id];
        args.extend(extra);
        assert!(!s.cli(&args).status.success());
    }

    let before = json(&s, &["--json", "prompt", "show", &file_id])["log"]
        .as_array()
        .unwrap()
        .len();
    s.ok(&["prompt", "update", &file_id, "--note", "exact note"]);
    let detail = json(&s, &["--json", "prompt", "show", &file_id]);
    assert_eq!(detail["log"].as_array().unwrap().len(), before + 1);
    assert_eq!(
        detail["log"].as_array().unwrap().last().unwrap()["event"],
        "exact note"
    );

    let malformed = s.path("home/hub/projects/demo/prompts/malformed.md");
    fs::write(&malformed, b"---\nid: bad\nkind: prompt\n---\n").unwrap();
    let doctor = s.cli(&["--json", "doctor"]);
    assert!(!doctor.status.success());
    let report: serde_json::Value = serde_json::from_slice(&doctor.stdout).unwrap();
    assert!(
        report["warnings"]
            .as_array()
            .unwrap()
            .iter()
            .any(|warning| warning.to_string().contains("malformed"))
    );
}
