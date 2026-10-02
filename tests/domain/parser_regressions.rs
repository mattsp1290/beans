//! Independent ports of issue/coverage_test.go and review_regression_test.go.
use beans::domain::{
    config::{ProjectConfig, decode_project_config, encode_project_config},
    issue::{IssueDocument, Timestamp},
    log::LogEntry,
    memory::MemoryDocument,
    workflow::WorkflowFile,
};
const HEAD: &str = "---\nid: x\ntitle: t\ntype: task\nstatus: open\npriority: 1\n";
const TIMES: &str = "created: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n";
fn source(extra: &str, body: &str) -> String {
    format!("{HEAD}{extra}{TIMES}---\n{body}")
}
fn parse(extra: &str) -> IssueDocument {
    IssueDocument::parse("x.md", &source(extra, "")).unwrap()
}
fn later_log(event: &str) -> LogEntry {
    LogEntry {
        at: Timestamp {
            seconds: 1767312000,
            nanoseconds: 0,
            offset_seconds: 0,
        },
        actor: "m".into(),
        event: event.into(),
        ..LogEntry::default()
    }
}
#[test]
fn additional_owned_and_yaml_errors() {
    let base =
        |extra| format!("---\nid: x\ntitle: t\ntype: task\nstatus: open\n{extra}{TIMES}---\n");
    let cases = [
        ("priority not scalar", base("priority:\n  - 1\n  - 2\n")),
        ("parent not scalar", base("priority: 1\nparent:\n  - a\n  - b\n")),
        ("blocked_by item not scalar", base("priority: 1\nblocked_by:\n  - \"[[a]]\"\n  - [nested]\n")),
        ("created not scalar", "---\nid: x\ntitle: t\ntype: task\nstatus: open\npriority: 1\ncreated:\n  - a\nupdated: 2026-01-01T00:00:00Z\n---\n".into()),
        ("labels item not scalar", base("priority: 1\nlabels:\n  - a\n  - [nested]\n")),
        ("labels not list or scalar", base("priority: 1\nlabels:\n  a: b\n")),
        ("non-scalar top-level key", format!("---\n? [a, b]\n: v\nid: x\ntitle: t\ntype: task\nstatus: open\npriority: 1\n{TIMES}---\n")),
        ("invalid yaml syntax", "---\nid: [unterminated\n---\n".into()),
    ];
    for (name, raw) in cases {
        assert!(
            IssueDocument::parse(&format!("{name}.md"), &raw).is_err(),
            "{name}"
        );
    }
}
#[test]
fn closing_fence_without_trailing_newline() {
    let raw = format!("{HEAD}{TIMES}---");
    let d = IssueDocument::parse("no-trailing.md", &raw).unwrap();
    assert!(d.description.is_empty() && d.body.is_empty());
    assert_eq!(d.encode().unwrap().bytes, raw.as_bytes());
}
#[test]
fn empty_body_after_fence() {
    let raw = source("", "");
    let d = IssueDocument::parse("empty-body.md", &raw).unwrap();
    assert!(d.description.is_empty() && d.body.is_empty() && d.log.is_empty());
    assert_eq!(d.encode().unwrap().bytes, raw.as_bytes());
}
#[test]
fn null_scalar() {
    assert!(parse("assignee: null\n").metadata.assignee.is_empty());
}
#[test]
fn null_string_list() {
    assert!(parse("labels: null\n").metadata.labels.is_empty());
}
#[test]
fn blocker_list_skips_empty_elements() {
    let d = parse("blocked_by: [\"[[a]]\", \"\"]\n");
    assert_eq!(d.metadata.blocked_by.len(), 1);
    assert_eq!(d.metadata.blocked_by[0].target, "a");
}
#[test]
fn link_alias_and_heading_targets() {
    let d = parse("parent: \"[[proj-x|Some Alias]]\"\nblocked_by:\n  - \"[[proj-y#section]]\"\n");
    assert_eq!(d.metadata.parent.target, "proj-x");
    assert_eq!(d.metadata.blocked_by.len(), 1);
    assert_eq!(d.metadata.blocked_by[0].target, "proj-y");
}
#[test]
fn new_document_section_separators() {
    for (body, suffix) in [
        ("", "---\n## Log\n- x\n"),
        (
            "no trailing newline",
            "no trailing newline\n\n## Log\n- x\n",
        ),
        ("para\n\n", "para\n\n## Log\n- x\n"),
    ] {
        let mut d = IssueDocument::new(parse("").metadata);
        d.body = body.into();
        d.log.push(LogEntry {
            raw: "- x".into(),
            ..LogEntry::default()
        });
        let out = String::from_utf8(d.encode().unwrap().bytes).unwrap();
        assert!(out.ends_with(suffix), "{out}");
        assert!(!out.contains("para\n\n\n## Log\n"));
    }
}
#[test]
fn fenced_headings_are_not_sections() {
    let body = "Description documenting the format:\n\n```\n## Log\n- fake entry inside code fence\n```\n\nMore description text after the fence.\n\n## Acceptance\n- [ ] real section\n";
    let raw = source("", body);
    let mut d = IssueDocument::parse("f.md", &raw).unwrap();
    assert!(d.log.is_empty());
    assert!(
        d.description
            .as_bytes()
            .ends_with(b"More description text after the fence.\n\n")
    );
    assert!(d.body.as_bytes().starts_with(b"## Acceptance"));
    d.append_log(later_log("created"));
    let expected = raw.replace(
        "updated: 2026-01-01T00:00:00Z\n",
        "updated: 2026-01-02T00:00:00Z\n",
    ) + "\n## Log\n- 2026-01-02T00:00:00Z m: created\n";
    assert_eq!(d.encode().unwrap().bytes, expected.as_bytes());
}
#[test]
fn log_tokens_round_trip() {
    let cases = [
        (
            "Matt Spurlin",
            "",
            "",
            "",
            "created",
            "Matt-Spurlin",
            "",
            "",
        ),
        (
            "matt",
            "my repo",
            "abc1234",
            "feature/fix(bug)",
            "status open → in_progress",
            "matt",
            "my-repo",
            "feature/fix(bug)",
        ),
        (
            "matt",
            "r",
            "abc1234",
            "weird)): name",
            "note — x): y",
            "matt",
            "r",
            "weird)):-name",
        ),
        ("", "", "", "", "reopened", "-", "", ""),
    ];
    for (actor, repo, sha, branch, event, want_actor, want_repo, want_branch) in cases {
        let d = LogEntry {
            at: Timestamp {
                seconds: 1767225600,
                nanoseconds: 0,
                offset_seconds: 0,
            },
            actor: actor.into(),
            repo: repo.into(),
            sha: sha.into(),
            branch: branch.into(),
            event: event.into(),
            ..LogEntry::default()
        };
        let line = d.format().unwrap();
        let back = LogEntry::parse(&line).unwrap();
        assert_eq!(back.event.as_bytes(), event.as_bytes());
        assert_eq!(back.sha.as_bytes(), sha.as_bytes());
        assert_eq!(back.actor.as_bytes(), want_actor.as_bytes());
        assert_eq!(back.repo.as_bytes(), want_repo.as_bytes());
        assert_eq!(back.branch.as_bytes(), want_branch.as_bytes());
    }
}
#[test]
fn project_config_control_characters() {
    let d = ProjectConfig {
        name: "bell\x07char".into(),
        prefix: "p\x0bq".into(),
        remotes: Some(vec!["https://x/y \"z\"".into()]),
        ..ProjectConfig::default()
    };
    let bytes = encode_project_config(&d);
    let back = decode_project_config(&bytes).unwrap();
    assert_eq!(back, d);
    assert!(!String::from_utf8(bytes).unwrap().contains("[workflow]"));
    let d = ProjectConfig {
        name: "a".into(),
        prefix: "a".into(),
        workflow: WorkflowFile {
            default: "in_progress".into(),
            ..WorkflowFile::default()
        },
        ..ProjectConfig::default()
    };
    let bytes = encode_project_config(&d);
    let out = String::from_utf8(bytes).unwrap();
    assert!(out.contains("[workflow]") && out.contains("default = \"in_progress\""));
}
#[test]
fn memory_duplicate_owned_key() {
    let err = MemoryDocument::parse("m.md", "---\nkey: a\nkey: b\n---\n").unwrap_err();
    assert!(err.to_string().contains("duplicate frontmatter key"));
}
#[test]
fn unclosed_fence_does_not_hide_headings() {
    let raw = source(
        "",
        "Description with an unclosed fence:\n\n```\nsome code\n\n## Log\n- 2026-01-01T00:00:00Z matt: created\n",
    );
    let mut d = IssueDocument::parse("u.md", &raw).unwrap();
    assert_eq!(d.log.len(), 1);
    assert_eq!(d.log[0].event.as_bytes(), b"created");
    assert_eq!(d.encode().unwrap().bytes, raw.as_bytes());
    d.append_log(later_log("reopened"));
    let expected = raw.replace(
        "updated: 2026-01-01T00:00:00Z\n",
        "updated: 2026-01-02T00:00:00Z\n",
    ) + "- 2026-01-02T00:00:00Z m: reopened\n";
    assert_eq!(d.encode().unwrap().bytes, expected.as_bytes());
    assert_eq!(expected.matches("## Log").count(), 1);
}
