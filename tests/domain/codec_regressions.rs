//! Independent ports of issue/codec_test.go at the immutable Go baseline.
use beans::domain::{
    issue::{IssueDocument, IssueMetadata, Timestamp},
    log::LogEntry,
    text::Link,
};

const SAMPLE: &str = r#"---
id: exa-a3f2
aliases: [exa-a3f2]
title: Migrate the live infra-host deploy
type: task
status: open
priority: 2
labels: [deploy, infra]
assignee: matt
parent: "[[exa-mkg1-deploy-bean-counter]]"
blocked_by:
  - "[[exa-ued1-schema-parity]]"
url: https://example.invalid/ticket/1
created: 2026-06-15T10:22:00Z
updated: 2026-09-10T08:01:00Z
# a user comment
custom: keep me
---
Description paragraphs. Free markdown.

## Acceptance
- [ ] parity gate passes

## Log
- 2026-09-10T08:01:00Z matt (exa@a1b2c3d feature/x): status open → in_progress
- 2026-09-11T10:00:00Z claude: closed — parity gate passes
"#;

fn timestamp(value: &str) -> Timestamp {
    let at =
        time::OffsetDateTime::parse(value, &time::format_description::well_known::Rfc3339).unwrap();
    Timestamp {
        seconds: at.unix_timestamp(),
        nanoseconds: at.nanosecond(),
        offset_seconds: at.offset().whole_seconds(),
    }
}

#[test]
fn round_trip() {
    let issue = IssueDocument::parse("projects/exa/issues/exa-a3f2-migrate.md", SAMPLE).unwrap();
    assert_eq!(issue.encode().unwrap().bytes, SAMPLE.as_bytes());
    assert_eq!(issue.metadata.project, "exa");
    assert!(!issue.metadata.archived);
    assert_eq!(issue.metadata.priority, 2);
    assert_eq!(issue.metadata.parent.target, "exa-mkg1-deploy-bean-counter");
    assert_eq!(issue.log.len(), 2);
    assert_eq!(issue.log[0].sha, "a1b2c3d");
    assert_eq!(issue.log[1].event, "closed — parity gate passes");
    assert_eq!(issue.unknown_fields().next().unwrap().key, "custom");
}

#[test]
fn mutations_touch_only_their_lines() {
    let mut issue = IssueDocument::parse("x.md", SAMPLE).unwrap();
    issue.metadata.status = "in_progress".into();
    issue.metadata.blocked_by.push(Link::new("exa-zzzz"));
    issue.metadata.assignee.clear();
    issue.metadata.updated = timestamp("2026-09-12T00:00:00Z");
    issue.append_log(LogEntry {
        at: issue.metadata.updated.clone(),
        actor: "claude".into(),
        event: "status open → in_progress".into(),
        ..LogEntry::default()
    });
    issue.set_description("New description\nwith two lines");
    let expected = r#"---
id: exa-a3f2
aliases: [exa-a3f2]
title: Migrate the live infra-host deploy
type: task
status: in_progress
priority: 2
labels: [deploy, infra]
parent: "[[exa-mkg1-deploy-bean-counter]]"
blocked_by:
  - "[[exa-ued1-schema-parity]]"
  - "[[exa-zzzz]]"
url: https://example.invalid/ticket/1
created: 2026-06-15T10:22:00Z
updated: 2026-09-12T00:00:00Z
# a user comment
custom: keep me
---
New description
with two lines

## Acceptance
- [ ] parity gate passes

## Log
- 2026-09-10T08:01:00Z matt (exa@a1b2c3d feature/x): status open → in_progress
- 2026-09-11T10:00:00Z claude: closed — parity gate passes
- 2026-09-12T00:00:00Z claude: status open → in_progress
"#;
    let encoded = issue.encode().unwrap().bytes;
    assert_eq!(encoded, expected.as_bytes());
    let again = IssueDocument::parse("x.md", std::str::from_utf8(&encoded).unwrap()).unwrap();
    assert_eq!(again.encode().unwrap().bytes, encoded);
}

#[test]
fn new_issue_encode_and_log_creation() {
    let at = timestamp("2026-01-01T00:00:00Z");
    let mut issue = IssueDocument::new(IssueMetadata {
        id: "p-ab12".into(),
        title: "Title: with colon".into(),
        kind: "task".into(),
        status: "open".into(),
        priority: 2,
        created: at.clone(),
        updated: at.clone(),
        ..IssueMetadata::default()
    });
    issue.description = "desc\n".into();
    issue.body = "\n## Acceptance\n- [ ] x\n".into();
    issue.append_log(LogEntry {
        at: at.clone(),
        actor: "matt".into(),
        event: "created".into(),
        ..LogEntry::default()
    });
    let expected = r#"---
id: p-ab12
aliases: [p-ab12]
title: 'Title: with colon'
type: task
status: open
priority: 2
created: 2026-01-01T00:00:00Z
updated: 2026-01-01T00:00:00Z
---
desc

## Acceptance
- [ ] x

## Log
- 2026-01-01T00:00:00Z matt: created
"#;
    let encoded = issue.encode().unwrap().bytes;
    assert_eq!(encoded, expected.as_bytes());
    let back = IssueDocument::parse(
        "projects/p/issues/p-ab12-title.md",
        std::str::from_utf8(&encoded).unwrap(),
    )
    .unwrap();
    assert_eq!(back.metadata.title, "Title: with colon");
    assert_eq!(back.log[0].event, "created");
    let no_log = "---\nid: a-1\ntitle: t\ntype: task\nstatus: open\npriority: 1\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\nbody text\n";
    let mut issue = IssueDocument::parse("a.md", no_log).unwrap();
    issue.append_log(LogEntry {
        at,
        actor: "m".into(),
        event: "note — hi\nsecond line".into(),
        ..LogEntry::default()
    });
    let encoded = String::from_utf8(issue.encode().unwrap().bytes).unwrap();
    assert!(
        encoded
            .ends_with("body text\n\n## Log\n- 2026-01-01T00:00:00Z m: note — hi\n  second line\n")
    );
    assert!(encoded.contains("updated: 2026-01-01T00:00:00Z\n---"));
}

#[test]
fn parse_errors_name_the_file() {
    for (name, input) in [
        ("crlf", "---\r\nid: x\r\n---\r\n"),
        ("no fence", "id: x\n"),
        ("no close", "---\nid: x\n"),
        ("no id", "---\ntitle: x\n---\n"),
        (
            "bad prio",
            "---\nid: x\ntitle: t\ntype: task\nstatus: open\npriority: high\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n",
        ),
        (
            "bad time",
            "---\nid: x\ntitle: t\ntype: task\nstatus: open\npriority: 1\ncreated: yesterday\nupdated: 2026-01-01T00:00:00Z\n---\n",
        ),
        ("not a map", "---\n- a\n---\n"),
        ("empty", "---\n---\n"),
        ("dup key", "---\nid: x\nid: y\n---\n"),
        ("list title", "---\nid: x\ntitle: [a]\n---\n"),
    ] {
        let path = format!("{name}.md");
        let error = IssueDocument::parse(&path, input).unwrap_err();
        assert!(error.to_string().contains(&path), "{name}: {error}");
    }
}
