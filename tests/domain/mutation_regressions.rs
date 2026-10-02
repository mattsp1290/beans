//! Independent ports of the original issue mutation and roundtrip tests.
use beans::domain::{
    issue::{IssueDocument, Timestamp},
    log::LogEntry,
    text::Link,
};

const BASE: &str = "---\nid: mut-a1b2\naliases: [mut-a1b2]\ntitle: Mutation base\ntype: task\nstatus: open\npriority: 2\nparent: \"[[mut-parent1]]\"\nblocked_by:\n  - \"[[mut-block1]]\"\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\ncustom_after: keep me\n---\nOriginal description.\n\n## Acceptance\n- [ ] something\n\n## Log\n- 2026-01-01T00:00:00Z matt: created\n";
fn base() -> IssueDocument {
    IssueDocument::parse("mut.md", BASE).unwrap()
}
fn entry(event: &str) -> LogEntry {
    LogEntry {
        at: Timestamp {
            seconds: 1767312000,
            nanoseconds: 0,
            offset_seconds: 0,
        },
        actor: "claude".into(),
        event: event.into(),
        ..LogEntry::default()
    }
}
fn assert_output(document: &IssueDocument, expected: &str) -> IssueDocument {
    let bytes = document.encode().unwrap().bytes;
    assert_eq!(bytes, expected.as_bytes());
    let again = IssueDocument::parse_bytes("mut.md", &bytes).unwrap();
    assert_eq!(again.encode().unwrap().bytes, bytes);
    again
}
#[test]
fn set_status() {
    let mut d = base();
    d.metadata.status = "in_progress".into();
    let again = assert_output(&d, &BASE.replace("status: open\n", "status: in_progress\n"));
    assert_eq!(again.metadata.status, "in_progress");
}
#[test]
fn append_log() {
    let mut d = base();
    let e = entry("note — did a thing");
    let at = e.at.clone();
    d.append_log(e);
    let expected = BASE.replace(
        "updated: 2026-01-01T00:00:00Z\n",
        "updated: 2026-01-02T00:00:00Z\n",
    ) + "- 2026-01-02T00:00:00Z claude: note — did a thing\n";
    let again = assert_output(&d, &expected);
    assert_eq!(again.log.len(), 2);
    assert_eq!(again.log[1].actor.as_bytes(), b"claude");
    assert_eq!(
        again.log[1].event.as_bytes(),
        "note — did a thing".as_bytes()
    );
    assert_eq!(again.metadata.updated, at);
}
#[test]
fn set_description() {
    let mut d = base();
    d.set_description("New description text.\nSecond line.");
    let again = assert_output(
        &d,
        &BASE.replace(
            "Original description.",
            "New description text.\nSecond line.",
        ),
    );
    assert_eq!(
        again.description.as_bytes(),
        b"New description text.\nSecond line.\n\n"
    );
}
#[test]
fn add_blocker() {
    let mut d = base();
    d.metadata.blocked_by.push(Link::new("mut-block2"));
    let again = assert_output(
        &d,
        &BASE.replace(
            "  - \"[[mut-block1]]\"\n",
            "  - \"[[mut-block1]]\"\n  - \"[[mut-block2]]\"\n",
        ),
    );
    assert_eq!(again.metadata.blocked_by.len(), 2);
    assert_eq!(again.metadata.blocked_by[1].target, "mut-block2");
}
#[test]
fn remove_blocker_to_zero() {
    let mut d = base();
    d.metadata.blocked_by.clear();
    let again = assert_output(
        &d,
        &BASE.replace("blocked_by:\n  - \"[[mut-block1]]\"\n", ""),
    );
    assert!(again.metadata.blocked_by.is_empty());
}
#[test]
fn set_absent_assignee() {
    let mut d = base();
    assert!(d.metadata.assignee.is_empty());
    d.metadata.assignee = "matt".into();
    let again = assert_output(
        &d,
        &BASE.replace(
            "custom_after: keep me\n",
            "assignee: matt\ncustom_after: keep me\n",
        ),
    );
    assert_eq!(again.metadata.assignee, "matt");
}
#[test]
fn clear_parent() {
    let mut d = base();
    d.metadata.parent = Link::default();
    let again = assert_output(&d, &BASE.replace("parent: \"[[mut-parent1]]\"\n", ""));
    assert!(again.metadata.parent.target.is_empty());
}
#[test]
fn append_log_when_log_not_last() {
    let raw =
        include_str!("../fixtures/native-baseline/issue/testdata/roundtrip/g_log_not_last.md");
    let mut d = IssueDocument::parse("g.md", raw).unwrap();
    d.append_log(entry("note — after"));
    let expected = raw
        .replace(
            "updated: 2026-01-01T00:00:00Z\n",
            "updated: 2026-01-02T00:00:00Z\n",
        )
        .replace(
            "\n\n## Notes\n",
            "\n- 2026-01-02T00:00:00Z claude: note — after\n\n## Notes\n",
        );
    let again = assert_output(&d, &expected);
    assert_eq!(again.log.len(), 2);
    assert!(expected.ends_with("- 2026-01-01T00:00:00Z matt: created\n- 2026-01-02T00:00:00Z claude: note — after\n\n## Notes\nSome trailing notes after the log section.\n"));
}
#[test]
fn keeps_inline_comment_on_rewritten_scalar() {
    let raw = include_str!("../fixtures/native-baseline/issue/testdata/roundtrip/c_comments.md");
    let mut d = IssueDocument::parse("c.md", raw).unwrap();
    d.metadata.status = "closed".into();
    assert_output(&d, &raw.replace("status: open  #", "status: closed #"));
}
#[test]
fn round_trip_fixtures() {
    let directory = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fixtures/native-baseline/issue/testdata/roundtrip");
    let mut files: Vec<_> = std::fs::read_dir(directory)
        .unwrap()
        .map(|f| f.unwrap().path())
        .filter(|p| p.extension().is_some_and(|e| e == "md"))
        .collect();
    files.sort();
    assert!(files.len() >= 12);
    for file in files {
        let name = file.file_name().unwrap().to_str().unwrap();
        let raw = std::fs::read(&file).unwrap();
        let path = if name == "archived.md" {
            "projects/p/archive/2026/p-x1.md".into()
        } else {
            format!("projects/roundtrip/issues/{name}")
        };
        let result = IssueDocument::parse_bytes(&path, &raw);
        if name.starts_with("err_") {
            assert!(result.is_err(), "{name}");
            continue;
        }
        let d = result.unwrap();
        assert_eq!(d.encode().unwrap().bytes, raw, "{name}");
        if name == "archived.md" {
            assert!(d.metadata.archived);
            assert_eq!(d.metadata.project, "p");
        }
        if name == "n_log_mixed.md" {
            assert_eq!(d.log.len(), 3);
            assert_eq!(d.log[0].event.as_bytes(), b"created");
            assert_eq!(
                d.log[1].raw.as_bytes(),
                b"- hand-written note without proper format"
            );
            assert_eq!(
                d.log[2].event.as_bytes(),
                "note — multi-line event\ncontinuation of the note\nanother continuation line"
                    .as_bytes()
            );
        }
        let targets = match name {
            "d_flow_blocked_by.md" => Some(vec!["proj-d001-a", "proj-d001-b"]),
            "k_blocked_by_no_indent.md" => Some(vec!["proj-k001-a", "proj-k001-b"]),
            "m_single_scalar_blocked_by.md" => Some(vec!["proj-m001-a"]),
            _ => None,
        };
        if let Some(targets) = targets {
            assert_eq!(
                d.metadata
                    .blocked_by
                    .iter()
                    .map(|l| l.target.as_str())
                    .collect::<Vec<_>>(),
                targets,
                "{name}"
            );
        }
    }
}
