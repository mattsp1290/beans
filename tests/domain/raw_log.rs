use beans::domain::{
    issue::{IssueDocument, Timestamp},
    log::{LogEntry, YamlString},
    request::RequestDocument,
};
use serde_json::{Value, json};
fn bytes(value: &Value) -> Vec<u8> {
    serde_json::from_value(value.clone()).unwrap()
}
fn entry(c: &Value) -> LogEntry {
    let mut e = LogEntry {
        at: Timestamp {
            seconds: 1798761600,
            nanoseconds: 0,
            offset_seconds: 0,
        },
        actor: "actor".into(),
        repo: "repo".into(),
        sha: "abcdef".into(),
        branch: "branch".into(),
        event: "event".into(),
        ..Default::default()
    };
    let mut value = b"A ".to_vec();
    value.extend_from_slice(&bytes(&c["sequence"]));
    value.extend_from_slice(b" Z");
    let value = YamlString::from_bytes(value);
    match c["field"].as_str().unwrap() {
        "actor" => e.actor = value,
        "repo" => e.repo = value,
        "sha" => e.sha = value,
        "branch" => e.branch = value,
        "event" => e.event = value,
        "raw" => e.raw = value,
        _ => unreachable!(),
    }
    e
}
fn input(kind: &str) -> (&'static str, Vec<u8>) {
    let (path, head) = if kind == "issue" {
        (
            "projects/p/issues/p-one.md",
            "id: p-one\ntitle: Title\ntype: task\nstatus: open\npriority: 2\n",
        )
    } else {
        (
            "projects/p/requests/p-r-one.md",
            "id: p-r-one\naliases: [p-r-one]\ntitle: Title\nstatus: open\npriority: 2\n",
        )
    };
    let mut text =
        format!("---\n{head}created: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n")
            .into_bytes();
    text.extend_from_slice(b"prefix\xff\n\n## Log\n- opaque\xff\n\n## Tail\ntail\xff\n");
    (path, text)
}
#[test]
fn raw_log_format_parse_and_append_match_fixed_go() {
    let corpus: Value = serde_json::from_str(include_str!("../contract/raw-log.json")).unwrap();
    let capture = std::env::var_os("BN_RUST_RAW_LOG_OUTPUT");
    let mut candidates = Vec::new();
    for (index, c) in corpus["cases"].as_array().unwrap().iter().enumerate() {
        let e = entry(c);
        assert_eq!(
            e.format_bytes().unwrap(),
            bytes(&c["formatted"]),
            "format {index}"
        );
        let line = e.line_bytes().unwrap();
        assert_eq!(line, bytes(&c["line"]), "line {index}");
        let parsed = LogEntry::parse_bytes(line.strip_suffix(b"\n").unwrap());
        assert_eq!(
            parsed.is_some(),
            c["parsed"]["ok"].as_bool().unwrap(),
            "parse {index}"
        );
        if let Some(parsed) = parsed {
            for (key, value) in [
                ("actor", parsed.actor),
                ("repo", parsed.repo),
                ("sha", parsed.sha),
                ("branch", parsed.branch),
                ("event", parsed.event),
            ] {
                assert_eq!(
                    value.as_bytes(),
                    bytes(&c["parsed"][key]),
                    "capture {index} {key}"
                );
            }
        }
        for (label, expected) in c["outputs"].as_object().unwrap() {
            let (kind, body_kind) = label.split_once(':').unwrap();
            let path = corpus["inputs"][label]["path"].as_str().unwrap();
            let raw = bytes(&corpus["inputs"][label]["bytes"]);
            let output = if kind == "issue" {
                let mut d = IssueDocument::parse_bytes(path, &raw).unwrap();
                if body_kind == "log" {
                    assert_eq!(d.log[0].raw.as_bytes(), b"- opaque\xff");
                }
                if body_kind == "new" {
                    d = IssueDocument::new(d.metadata);
                }
                d.append_log(e.clone());
                d.encode().unwrap()
            } else {
                let mut d = RequestDocument::parse_bytes(path, &raw).unwrap();
                if body_kind == "log" {
                    assert_eq!(d.log[0].raw.as_bytes(), b"- opaque\xff");
                }
                if body_kind == "new" {
                    d = RequestDocument::new(d.metadata);
                }
                d.append_log(e.clone());
                d.encode().unwrap()
            };
            assert_eq!(
                output.bytes,
                bytes(&expected["bytes"]),
                "append {index} {kind}"
            );
            for copy in output.copies {
                assert_eq!(
                    &output.bytes[copy.destination], &raw[copy.source],
                    "copies {index}"
                );
            }
            let reread = if kind == "issue" {
                IssueDocument::parse_bytes(path, &output.bytes).and_then(|d| d.encode())
            } else {
                RequestDocument::parse_bytes(path, &output.bytes).and_then(|mut d| d.encode())
            };
            let error = reread
                .as_ref()
                .err()
                .map(ToString::to_string)
                .unwrap_or_default();
            assert_eq!(
                error,
                expected["reread_error"].as_str().unwrap(),
                "reread {index} {kind}"
            );
            if let Ok(reread) = reread {
                assert_eq!(reread.bytes, output.bytes);
            }
            candidates
                .push(json!({"Kind":kind,"Path":path,"ExpectedError":error,"Bytes":output.bytes}));
        }
    }
    if let Some(path) = capture {
        std::fs::write(path, serde_json::to_vec(&candidates).unwrap()).unwrap();
    }
}

fn original_entry(actor: &str, repo: &str, sha: &str, branch: &str, event: &str) -> LogEntry {
    LogEntry {
        at: Timestamp {
            seconds: 1789027260,
            nanoseconds: 0,
            offset_seconds: 0,
        },
        actor: actor.into(),
        repo: repo.into(),
        sha: sha.into(),
        branch: branch.into(),
        event: event.into(),
        ..Default::default()
    }
}
#[test]
fn format_parse_log_entry_round_trip_original_regression() {
    for e in [
        original_entry(
            "matt",
            "exa",
            "a1b2c3d",
            "feature/x",
            "status open → in_progress",
        ),
        original_entry("claude", "", "", "", "closed — parity gate passes"),
        original_entry("matt", "exa", "a1b2c3d", "", "note — deployed"),
        original_entry("m", "", "", "", "line1\nline2"),
        original_entry("m", "", "", "", "first\nsecond\nthird"),
    ] {
        assert_eq!(LogEntry::parse(&e.format().unwrap()), Some(e));
    }
}
#[test]
fn format_log_entry_omits_parens_without_repo_info_original_regression() {
    let e = original_entry("claude", "", "", "", "note — hi");
    assert_eq!(
        e.format().unwrap(),
        "- 2026-09-10T08:01:00Z claude: note — hi"
    );
    let mut incomplete = e.clone();
    incomplete.repo = "repo".into();
    assert_eq!(incomplete.format().unwrap(), e.format().unwrap());
    incomplete.repo = "".into();
    incomplete.sha = "abcdef".into();
    assert_eq!(incomplete.format().unwrap(), e.format().unwrap());
}
#[test]
fn format_log_entry_repo_and_sha_no_branch_original_regression() {
    let e = original_entry("matt", "exa", "a1b2c3d", "", "note — x");
    assert_eq!(
        e.format().unwrap(),
        "- 2026-09-10T08:01:00Z matt (exa@a1b2c3d): note — x"
    );
}
#[test]
fn parse_log_entry_minute_precision_original_regression() {
    assert_eq!(
        LogEntry::parse("- 2026-09-10T08:01Z matt: created"),
        Some(original_entry("matt", "", "", "", "created"))
    );
}
#[test]
fn parse_log_entry_non_matching_lines_original_regression() {
    for text in [
        "not a log line at all",
        "- missing colon and format",
        "- 2026-09-10T08:01:00Z noactorcolon",
        "",
        "just some text: with a colon",
    ] {
        assert!(LogEntry::parse(text).is_none());
    }
}
#[test]
fn parse_log_entry_multi_line_continuation_original_regression() {
    assert_eq!(
        LogEntry::parse(
            "- 2026-09-10T08:01:00Z matt: note — first line\n  second line\n  third line"
        ),
        Some(original_entry(
            "matt",
            "",
            "",
            "",
            "note — first line\nsecond line\nthird line"
        ))
    );
}

#[test]
fn byte_logs_retain_captures_opaque_items_and_original_history_in_index() {
    let raw = b"## Log\n- 2027-01-01T00:00:00Z A\xff (repo\xff@abcdef branch\xff): event\xff\n  more\xff\n- opaque\xff\n  raw\xff\n";
    let entries = beans::domain::log::parse_section_bytes(raw);
    assert_eq!(entries.len(), 2);
    assert_eq!(entries[0].actor.as_bytes(), b"A\xff");
    assert_eq!(entries[0].repo.as_bytes(), b"repo\xff");
    assert_eq!(entries[0].branch.as_bytes(), b"branch\xff");
    assert_eq!(entries[0].event.as_bytes(), b"event\xff\nmore\xff");
    assert_eq!(entries[1].raw.as_bytes(), b"- opaque\xff\n  raw\xff");
    assert!(entries[0].format().is_err());
    assert!(entries[1].line().is_err());
    assert_eq!(
        entries[0].line_bytes().unwrap(),
        raw[7..raw
            .windows(10)
            .position(|v| v == b"- opaque\xff\n")
            .unwrap()]
    );
    let (path, base) = input("issue");
    let mut d = IssueDocument::parse_bytes(path, &base).unwrap();
    d.append_log(entries[0].clone());
    let encoded = d.encode().unwrap().bytes;
    let mut reread = IssueDocument::parse_bytes(path, &encoded).unwrap();
    assert_eq!(reread.log[1], entries[0]);
    reread.log[0].raw = "edited history ignored".into();
    reread.log[1].event = "edited history ignored".into();
    assert_eq!(reread.encode().unwrap().bytes, encoded);
    let hub = super::index::hub("raw-log-index", false);
    let file = hub.0.join(path);
    std::fs::create_dir_all(file.parent().unwrap()).unwrap();
    std::fs::write(file, &encoded).unwrap();
    let ix = beans::vault::Index::load(&hub.0).unwrap();
    assert!(ix.graph.warnings().is_empty());
    let note = ix
        .note_by_id(beans::vault::NoteKind::Issue, b"p-one")
        .unwrap();
    let beans::vault::NoteData::Issue(d) = &note.data else {
        panic!()
    };
    assert_eq!(d.log[1], entries[0]);
    assert_eq!(d.encode().unwrap().bytes, encoded);
}
