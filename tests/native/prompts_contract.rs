//! Focused acceptance matrices for the public prompt discovery and lifecycle contract.
use super::*;
use std::collections::BTreeMap;

#[derive(Clone)]
struct SeedPrompt {
    project: &'static str,
    id: String,
    title: String,
    status: &'static str,
    priority: i64,
    from: &'static str,
    to: &'static str,
    claimed_by: Option<&'static str>,
    labels: &'static [&'static str],
    created: String,
    body: String,
}

fn seed_prompt(seed: &Path, prompt: &SeedPrompt) {
    let labels = prompt.labels.join(", ");
    let claimed = prompt
        .claimed_by
        .map_or_else(String::new, |actor| format!("claimed_by: {actor}\n"));
    let text = format!(
        "---\nid: {}\naliases: []\ntitle: {}\nstatus: {}\npriority: {}\nlabels: [{}]\nfrom: {}\nto: {}\n{}created: {}\nupdated: {}\n---\n{}\n",
        prompt.id,
        prompt.title,
        prompt.status,
        prompt.priority,
        labels,
        prompt.from,
        prompt.to,
        claimed,
        prompt.created,
        prompt.created,
        prompt.body,
    );
    let path = seed
        .join("projects")
        .join(prompt.project)
        .join("prompts")
        .join(format!("{}-seed.md", prompt.id));
    fs::create_dir_all(path.parent().unwrap()).unwrap();
    fs::write(path, text).unwrap();
}

fn seeded_fixture(prompts: &[SeedPrompt]) -> Sandbox {
    let s = Sandbox::new();
    let remote = s.remote(true);
    let seed = s.path("seed");
    for project in prompts
        .iter()
        .map(|prompt| prompt.project)
        .collect::<std::collections::BTreeSet<_>>()
    {
        let path = seed.join("projects").join(project).join("beans.toml");
        fs::create_dir_all(path.parent().unwrap()).unwrap();
        fs::write(
            path,
            format!("[project]\nname = {project:?}\nprefix = {project:?}\n"),
        )
        .unwrap();
    }
    for prompt in prompts {
        seed_prompt(&seed, prompt);
    }
    git(&seed, &["add", "-A"]);
    git(&seed, &["commit", "-m", "seed prompt contract matrix"]);
    git(&seed, &["push", "origin", "main"]);
    s.ok(&["init", remote.to_str().unwrap()]);
    s
}

fn plain(s: &Sandbox, args: &[&str]) -> String {
    let output = s.cli(args);
    assert!(
        output.status.success(),
        "{args:?}: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    String::from_utf8(output.stdout).unwrap()
}

fn lines(items: &[&SeedPrompt]) -> String {
    items
        .iter()
        .map(|prompt| format!("{}\t{}\n", prompt.id, prompt.title))
        .collect()
}

macro_rules! prompt {
    ($project:expr, $id:expr, $title:expr, $status:expr, $priority:expr, $from:expr,
     $to:expr, $claimed_by:expr, $labels:expr, $second:expr, $body:expr $(,)?) => {
        SeedPrompt {
            project: $project,
            id: $id.to_owned(),
            title: $title.to_owned(),
            status: $status,
            priority: $priority,
            from: $from,
            to: $to,
            claimed_by: $claimed_by,
            labels: $labels,
            created: format!("2026-01-01T00:00:{:02}Z", $second),
            body: $body.to_owned(),
        }
    };
}

#[test]
fn prompt_discovery_order_filters_projects_and_limits_are_exact() {
    let mut records = vec![
        prompt!(
            "demo",
            "demo-p-a003",
            "priority zero",
            "open",
            0,
            "alice",
            "queue",
            None,
            &["red"],
            9,
            "needle zero",
        ),
        prompt!(
            "demo",
            "demo-p-a002",
            "tie second",
            "open",
            1,
            "alice",
            "queue",
            None,
            &["blue"],
            2,
            "needle tie",
        ),
        prompt!(
            "demo",
            "demo-p-a001",
            "tie first",
            "open",
            1,
            "bob",
            "queue",
            None,
            &["red"],
            2,
            "needle tie",
        ),
        prompt!(
            "demo",
            "demo-p-a004",
            "claimed elsewhere",
            "in_progress",
            2,
            "alice",
            "elsewhere",
            Some("Native Tester"),
            &["claimed"],
            3,
            "claimed body",
        ),
        prompt!(
            "demo",
            "demo-p-a005",
            "completed item",
            "completed",
            3,
            "alice",
            "queue",
            Some("worker"),
            &["terminal"],
            4,
            "completed body",
        ),
        prompt!(
            "demo",
            "demo-p-a006",
            "declined item",
            "declined",
            3,
            "alice",
            "queue",
            None,
            &["terminal"],
            5,
            "declined body",
        ),
        prompt!(
            "demo",
            "demo-p-a007",
            "cancelled item",
            "cancelled",
            4,
            "alice",
            "queue",
            None,
            &["terminal"],
            6,
            "cancelled body",
        ),
        prompt!(
            "other",
            "other-p-b001",
            "other project",
            "open",
            1,
            "carol",
            "queue",
            None,
            &["red"],
            1,
            "cross project needle",
        ),
    ];
    // More than fifty addressed records lock both the default and explicit limit behavior.
    for n in 0..52 {
        records.push(prompt!(
            "demo",
            &format!("demo-p-z{n:03}"),
            &format!("bulk {n:02}"),
            "open",
            4,
            "bulk-sender",
            "Native Tester",
            None,
            &["bulk"],
            7,
            "bulk body",
        ));
    }
    let s = seeded_fixture(&records);
    let by_id = |id: &str| records.iter().find(|item| item.id == id).unwrap();

    let active = [&records[0], &records[2], &records[1], &records[3]];
    assert_eq!(
        plain(&s, &["prompt", "list"]),
        lines(&active) + &lines(&records[8..].iter().collect::<Vec<_>>())
    );
    assert_eq!(
        plain(&s, &["prompt", "list", "--all-projects"]),
        lines(&[
            &records[0],
            &records[7],
            &records[2],
            &records[1],
            &records[3]
        ]) + &lines(&records[8..].iter().collect::<Vec<_>>())
    );
    assert_eq!(
        plain(&s, &["prompt", "list", "--from", "bob"]),
        lines(&[by_id("demo-p-a001")])
    );
    assert_eq!(
        plain(&s, &["prompt", "list", "--to", "elsewhere"]),
        lines(&[by_id("demo-p-a004")])
    );
    assert_eq!(
        plain(&s, &["prompt", "list", "--claimed-by", "Native Tester"]),
        lines(&[by_id("demo-p-a004")])
    );
    assert_eq!(
        plain(&s, &["prompt", "list", "--label", "blue"]),
        lines(&[by_id("demo-p-a002")])
    );
    assert_eq!(
        plain(&s, &["prompt", "list", "--priority", "0"]),
        lines(&[by_id("demo-p-a003")])
    );
    assert_eq!(
        plain(&s, &["prompt", "list", "--query", "needle zero"]),
        lines(&[by_id("demo-p-a003")])
    );
    assert_eq!(
        plain(&s, &["prompt", "list", "--status", "completed"]),
        lines(&[by_id("demo-p-a005")])
    );
    assert_eq!(
        plain(
            &s,
            &[
                "prompt",
                "list",
                "--status",
                "completed",
                "--terminal=false"
            ]
        ),
        lines(&[by_id("demo-p-a005")])
    );
    assert_eq!(
        plain(&s, &["prompt", "list", "--terminal"]),
        lines(&[
            by_id("demo-p-a003"),
            by_id("demo-p-a001"),
            by_id("demo-p-a002"),
            by_id("demo-p-a004"),
            by_id("demo-p-a005"),
            by_id("demo-p-a006"),
            by_id("demo-p-a007"),
        ]) + &lines(&records[8..].iter().collect::<Vec<_>>())
    );

    assert_eq!(
        plain(&s, &["prompt", "inbox", "--to", "queue", "--limit", "0"]),
        lines(&[
            by_id("demo-p-a003"),
            by_id("demo-p-a001"),
            by_id("demo-p-a002")
        ])
    );
    assert_eq!(
        plain(
            &s,
            &[
                "prompt",
                "inbox",
                "--to",
                "queue",
                "--terminal",
                "--limit",
                "0"
            ]
        ),
        lines(&[
            by_id("demo-p-a003"),
            by_id("demo-p-a001"),
            by_id("demo-p-a002"),
            by_id("demo-p-a005"),
            by_id("demo-p-a006"),
            by_id("demo-p-a007"),
        ])
    );
    assert_eq!(
        plain(&s, &["prompt", "inbox", "--claimed", "--limit", "0"]),
        lines(&[by_id("demo-p-a004")]) + &lines(&records[8..].iter().collect::<Vec<_>>())
    );
    let bulk: Vec<_> = records[8..].iter().collect();
    assert_eq!(
        plain(&s, &["prompt", "inbox", "--limit", "2"]),
        lines(&bulk[..2])
    );
    assert_eq!(plain(&s, &["prompt", "inbox"]), lines(&bulk[..50]));
    assert_eq!(
        plain(&s, &["prompt", "inbox", "--limit", "0"]),
        lines(&bulk)
    );
    assert_eq!(
        plain(
            &s,
            &[
                "prompt",
                "inbox",
                "--to",
                "queue",
                "--all-projects",
                "--limit",
                "0"
            ]
        ),
        lines(&[
            by_id("demo-p-a003"),
            by_id("other-p-b001"),
            by_id("demo-p-a001"),
            by_id("demo-p-a002")
        ])
    );
}

fn created_prompt(s: &Sandbox, title: &str) -> String {
    plain(
        s,
        &[
            "prompt",
            "create",
            title,
            "--to",
            "queue",
            "--description",
            "body",
        ],
    )
    .trim()
    .to_owned()
}

fn prompt_path(s: &Sandbox, id: &str) -> PathBuf {
    let value: serde_json::Value =
        serde_json::from_str(&plain(s, &["--json", "prompt", "show", id])).unwrap();
    s.path("home/hub").join(value["path"].as_str().unwrap())
}

fn reject_unchanged(s: &Sandbox, id: &str, extra: &[&str], diagnostic: &str) {
    let hub = s.path("home/hub");
    let path = prompt_path(s, id);
    let head = git(&hub, &["rev-parse", "HEAD"]);
    let bytes = fs::read(&path).unwrap();
    let mut args = vec!["--json", "prompt", "update", id];
    args.extend_from_slice(extra);
    let output = s.cli(&args);
    assert!(!output.status.success(), "unexpected success for {args:?}");
    assert!(
        output.stdout.is_empty(),
        "failure emitted success JSON: {:?}",
        output.stdout
    );
    assert_eq!(
        String::from_utf8(output.stderr).unwrap(),
        format!("bn: {diagnostic}\n")
    );
    assert_eq!(git(&hub, &["rev-parse", "HEAD"]), head);
    assert_eq!(git(&hub, &["status", "--porcelain"]), "");
    assert_eq!(fs::read(path).unwrap(), bytes);
}

fn lifecycle_fixture() -> Sandbox {
    let s = Sandbox::new();
    let remote = s.remote(false);
    s.ok(&["init", remote.to_str().unwrap()]);
    s.ok(&["create", "seed"]);
    s
}

#[test]
fn prompt_lifecycle_reassignment_and_failure_atomicity_matrix() {
    let s = lifecycle_fixture();
    let open = created_prompt(&s, "open matrix");
    reject_unchanged(
        &s,
        &open,
        &["--status", "in_progress"],
        "in_progress status requires the invocation actor to own the claim",
    );
    reject_unchanged(
        &s,
        &open,
        &["--status", "completed"],
        "invalid prompt status transition \"open\" -> \"completed\"",
    );
    reject_unchanged(
        &s,
        &open,
        &["--status", "completed", "--force"],
        "completed prompt requires a claimant",
    );

    for force in [false, true] {
        let mut claim = vec!["--to", "new", "--claim"];
        let mut status = vec!["--to", "new", "--status", "declined"];
        if force {
            claim.push("--force");
            status.push("--force");
        }
        reject_unchanged(
            &s,
            &open,
            &claim,
            "prompt recipient cannot be combined with claim or status",
        );
        reject_unchanged(
            &s,
            &open,
            &status,
            "prompt recipient cannot be combined with claim or status",
        );
    }

    let claimed = created_prompt(&s, "claimed matrix");
    plain(&s, &["prompt", "update", &claimed, "--claim"]);
    for force in [false, true] {
        let mut claim = vec!["--to", "new", "--claim"];
        let mut status = vec!["--to", "new", "--status", "completed"];
        if force {
            claim.push("--force");
            status.push("--force");
        }
        reject_unchanged(
            &s,
            &claimed,
            &claim,
            "prompt recipient cannot be combined with claim or status",
        );
        reject_unchanged(
            &s,
            &claimed,
            &status,
            "prompt recipient cannot be combined with claim or status",
        );
    }
    plain(&s, &["prompt", "update", &claimed, "--status", "completed"]);
    let completed: serde_json::Value =
        serde_json::from_str(&plain(&s, &["--json", "prompt", "show", &claimed])).unwrap();
    assert_eq!(completed["status"], "completed");
    assert_eq!(completed["claimed_by"], "Native Tester");
    for force in [false, true] {
        let mut claim = vec!["--to", "new", "--claim"];
        let mut status = vec!["--to", "new", "--status", "open"];
        if force {
            claim.push("--force");
            status.push("--force");
        }
        reject_unchanged(
            &s,
            &claimed,
            &claim,
            "prompt recipient cannot be combined with claim or status",
        );
        reject_unchanged(
            &s,
            &claimed,
            &status,
            "prompt recipient cannot be combined with claim or status",
        );
    }
    reject_unchanged(
        &s,
        &claimed,
        &["--to", "new"],
        "prompt recipient can only be changed while open and unclaimed",
    );
    plain(
        &s,
        &["prompt", "update", &claimed, "--to", "new", "--force"],
    );
    let reset: serde_json::Value =
        serde_json::from_str(&plain(&s, &["--json", "prompt", "show", &claimed])).unwrap();
    assert_eq!(
        (
            reset["status"].as_str(),
            reset["claimed_by"].as_str(),
            reset["to"].as_str()
        ),
        (Some("open"), None, Some("new"))
    );

    let declined = created_prompt(&s, "declined matrix");
    plain(&s, &["prompt", "update", &declined, "--status", "declined"]);
    plain(
        &s,
        &["prompt", "update", &declined, "--status", "open", "--force"],
    );
    let cancelled = created_prompt(&s, "cancelled matrix");
    plain(
        &s,
        &["prompt", "update", &cancelled, "--status", "cancelled"],
    );
    plain(
        &s,
        &[
            "prompt", "update", &cancelled, "--status", "declined", "--force",
        ],
    );
    plain(
        &s,
        &["prompt", "update", &cancelled, "--to", "reset", "--force"],
    );
    let reset: serde_json::Value =
        serde_json::from_str(&plain(&s, &["--json", "prompt", "show", &cancelled])).unwrap();
    assert_eq!(
        (reset["status"].as_str(), reset["claimed_by"].as_str()),
        (Some("open"), None)
    );
}

#[derive(Debug, PartialEq, Eq)]
struct ArgContract {
    id: String,
    index: Option<usize>,
    required: bool,
    long: Option<String>,
    short: Option<char>,
    values: Vec<String>,
    defaults: Vec<String>,
    action: String,
    num_args: String,
}

fn arg_contract(arg: &clap::Arg) -> ArgContract {
    ArgContract {
        id: arg.get_id().to_string(),
        index: arg.get_index(),
        required: arg.is_required_set(),
        long: arg.get_long().map(str::to_owned),
        short: arg.get_short(),
        values: arg
            .get_value_names()
            .map_or_else(Vec::new, |v| v.iter().map(ToString::to_string).collect()),
        defaults: arg
            .get_default_values()
            .iter()
            .map(|v| v.to_string_lossy().into_owned())
            .collect(),
        action: format!("{:?}", arg.get_action()),
        num_args: format!("{:?}", arg.get_num_args())
            .strip_prefix("Some(")
            .and_then(|value| value.strip_suffix(')'))
            .unwrap_or("None")
            .to_owned(),
    }
}

type ExpectedArg = (
    &'static str,
    Option<usize>,
    bool,
    Option<&'static str>,
    Option<char>,
    &'static str,
    &'static str,
);

#[test]
fn prompt_clap_metadata_and_parser_ranges_are_complete() {
    let mut root = beans::cli::command();
    root.build();
    let prompt = root.find_subcommand("prompt").unwrap();
    assert_eq!(
        prompt
            .get_subcommands()
            .map(|c| c.get_name())
            .filter(|name| *name != "help")
            .collect::<Vec<_>>(),
        [
            "create", "list", "inbox", "show", "update", "link", "unlink"
        ]
    );
    let expected: BTreeMap<&str, Vec<ExpectedArg>> = BTreeMap::from([
        (
            "create",
            vec![
                ("title", Some(1), true, None, None, "Set", "1"),
                ("to", None, true, Some("to"), None, "Set", "1"),
                ("from", None, false, Some("from"), None, "Set", "1"),
                ("issue", None, false, Some("issue"), None, "Set", "1"),
                (
                    "label",
                    None,
                    false,
                    Some("label"),
                    Some('l'),
                    "Append",
                    "1",
                ),
                (
                    "priority",
                    None,
                    false,
                    Some("priority"),
                    Some('p'),
                    "Set",
                    "1",
                ),
                (
                    "description",
                    None,
                    false,
                    Some("description"),
                    Some('d'),
                    "Set",
                    "1",
                ),
                (
                    "body-file",
                    None,
                    false,
                    Some("body-file"),
                    None,
                    "Set",
                    "1",
                ),
                (
                    "stdin",
                    None,
                    false,
                    Some("stdin"),
                    None,
                    "SetTrue",
                    "0..=1",
                ),
            ],
        ),
        (
            "list",
            vec![
                (
                    "all-projects",
                    None,
                    false,
                    Some("all-projects"),
                    None,
                    "SetTrue",
                    "0..=1",
                ),
                ("from", None, false, Some("from"), None, "Set", "1"),
                ("to", None, false, Some("to"), None, "Set", "1"),
                (
                    "claimed-by",
                    None,
                    false,
                    Some("claimed-by"),
                    None,
                    "Set",
                    "1",
                ),
                ("status", None, false, Some("status"), None, "Set", "1"),
                ("label", None, false, Some("label"), None, "Set", "1"),
                (
                    "priority",
                    None,
                    false,
                    Some("priority"),
                    Some('p'),
                    "Set",
                    "1",
                ),
                ("query", None, false, Some("query"), None, "Set", "1"),
                (
                    "terminal",
                    None,
                    false,
                    Some("terminal"),
                    None,
                    "SetTrue",
                    "0..=1",
                ),
            ],
        ),
        (
            "inbox",
            vec![
                ("to", None, false, Some("to"), None, "Set", "1"),
                (
                    "all-projects",
                    None,
                    false,
                    Some("all-projects"),
                    None,
                    "SetTrue",
                    "0..=1",
                ),
                (
                    "claimed",
                    None,
                    false,
                    Some("claimed"),
                    None,
                    "SetTrue",
                    "0..=1",
                ),
                (
                    "terminal",
                    None,
                    false,
                    Some("terminal"),
                    None,
                    "SetTrue",
                    "0..=1",
                ),
                ("limit", None, false, Some("limit"), None, "Set", "1"),
            ],
        ),
        ("show", vec![("id", Some(1), true, None, None, "Set", "1")]),
        (
            "update",
            vec![
                ("id", Some(1), true, None, None, "Set", "1"),
                (
                    "claim",
                    None,
                    false,
                    Some("claim"),
                    None,
                    "SetTrue",
                    "0..=1",
                ),
                ("status", None, false, Some("status"), None, "Set", "1"),
                (
                    "force",
                    None,
                    false,
                    Some("force"),
                    None,
                    "SetTrue",
                    "0..=1",
                ),
                ("title", None, false, Some("title"), None, "Set", "1"),
                ("to", None, false, Some("to"), None, "Set", "1"),
                ("priority", None, false, Some("priority"), None, "Set", "1"),
                ("label", None, false, Some("label"), None, "Append", "1"),
                ("unlabel", None, false, Some("unlabel"), None, "Append", "1"),
                ("note", None, false, Some("note"), None, "Set", "1"),
                (
                    "description",
                    None,
                    false,
                    Some("description"),
                    Some('d'),
                    "Set",
                    "1",
                ),
                (
                    "body-file",
                    None,
                    false,
                    Some("body-file"),
                    None,
                    "Set",
                    "1",
                ),
                (
                    "stdin",
                    None,
                    false,
                    Some("stdin"),
                    None,
                    "SetTrue",
                    "0..=1",
                ),
            ],
        ),
        (
            "link",
            vec![
                ("id", Some(1), true, None, None, "Set", "1"),
                ("issue-id", Some(2), true, None, None, "Set", "1"),
            ],
        ),
        (
            "unlink",
            vec![("id", Some(1), true, None, None, "Set", "1")],
        ),
    ]);
    for (name, wanted) in expected {
        let command = prompt.find_subcommand(name).unwrap();
        let got: Vec<_> = command
            .get_arguments()
            .filter(|arg| arg.get_id() != "help")
            .filter(|a| !a.is_global_set())
            .map(arg_contract)
            .collect();
        assert_eq!(
            got.len(),
            wanted.len(),
            "prompt {name} has extra/missing args: {got:#?}"
        );
        for (actual, (id, index, required, long, short, action, num_args)) in got.iter().zip(wanted)
        {
            assert_eq!(
                (
                    &*actual.id,
                    actual.index,
                    actual.required,
                    actual.long.as_deref(),
                    actual.short,
                    actual.action.as_str(),
                    actual.num_args.as_str()
                ),
                (id, index, required, long, short, action, num_args),
                "prompt {name}"
            );
            let expected_defaults: &[&str] = match (name, id) {
                ("create", "priority") => &["2"],
                ("inbox", "limit") => &["50"],
                (_, "all-projects" | "claimed" | "claim" | "force" | "stdin" | "terminal") => {
                    &["false"]
                }
                _ => &[],
            };
            assert_eq!(
                actual.defaults, expected_defaults,
                "prompt {name} {id} defaults"
            );
            assert!(
                actual.values.is_empty(),
                "prompt {name} {id} unexpected explicit value names"
            );
        }
    }
    for args in [
        ["prompt", "create", "x", "--to", "q", "--priority", "5"].as_slice(),
        ["prompt", "list", "--priority", "-1"].as_slice(),
        ["prompt", "inbox", "--limit", "-1"].as_slice(),
    ] {
        assert!(
            beans::cli::command()
                .try_get_matches_from(std::iter::once("bn").chain(args.iter().copied()))
                .is_err(),
            "parser accepted {args:?}"
        );
    }

    for (args, expected) in [
        (
            &["prompt", "--help"][..],
            include_str!("../fixtures/expected/prompt-help.txt"),
        ),
        (
            &["prompt", "list", "--help"][..],
            include_str!("../fixtures/expected/prompt-list-help.txt"),
        ),
        (
            &["prompt", "inbox", "--help"][..],
            include_str!("../fixtures/expected/prompt-inbox-help.txt"),
        ),
        (
            &["prompt", "update", "--help"][..],
            include_str!("../fixtures/expected/prompt-update-help.txt"),
        ),
    ] {
        let output = Sandbox::new().cli(args);
        assert!(output.status.success());
        let actual = String::from_utf8(output.stdout).unwrap();
        let normalized = actual
            .lines()
            .map(str::trim_end)
            .collect::<Vec<_>>()
            .join("\n")
            + "\n";
        assert_eq!(normalized, expected);
    }

    let root = beans::cli::command();
    let prompt = root.find_subcommand("prompt").unwrap();
    let mut man = Vec::new();
    clap_mangen::Man::new(prompt.clone())
        .render(&mut man)
        .unwrap();
    for subcommand in prompt.get_subcommands() {
        clap_mangen::Man::new(subcommand.clone())
            .render(&mut man)
            .unwrap();
    }
    let man = String::from_utf8(man).unwrap();
    for required in [
        "Create and manage agent prompts",
        "Create a prompt",
        "List prompts addressed to you",
        "Link a prompt to an issue",
        "Unlink a prompt from its issue",
        "\\-\\-all\\-projects",
        "\\-\\-claimed\\-by",
        "\\-\\-body\\-file",
        "\\-\\-terminal",
        "\\-\\-force",
    ] {
        assert!(man.contains(required), "generated man omitted {required}");
    }
}
