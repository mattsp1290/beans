use beans::domain::{issue::Timestamp, log::LogEntry, request::RequestDocument, text::Link};
use proptest::prelude::*;
use serde_json::Value;
const SAMPLE: &str = r#"---
id: beans-r-a3f2
aliases: [beans-r-a3f2]
title: Durable requests
status: open
priority: 2
labels: [planning]
requested_by: matt
issues:
  - "[[beans-a1b2]]"
custom: preserve
created: 2026-09-11T15:51:49Z
updated: 2026-09-11T15:51:49Z
---
Context.

## Request
Do the thing.

## Log
- 2026-09-11T15:51:49Z matt: created

## Later
Keep this too.
"#;
const PATH: &str = "projects/beans/requests/beans-r-a3f2-durable-requests.md";

proptest! {
    #![proptest_config(ProptestConfig::with_cases(512))]
    #[test]
    fn request_edits_preserve_unknown_bytes(body in "[a-z雪☃ ]{0,80}", blanks in 0..5usize) {
        let source = SAMPLE.replace("custom: preserve\n", &format!("custom: {{雪: [one, two]}}\n{}# retain comment\n", "\n".repeat(blanks))).replace("Context.", &body);
        let mut request = RequestDocument::parse(PATH, &source).unwrap();
        prop_assert_eq!(request.encode().unwrap().bytes, source.as_bytes());
        request.metadata.status = "accepted".into();
        let output = request.encode().unwrap();
        let expected = source.replacen("status: open\n", "status: accepted\n", 1);
        prop_assert_eq!(&output.bytes, expected.as_bytes());
        for copy in output.copies { prop_assert_eq!(&source.as_bytes()[copy.source], &output.bytes[copy.destination]); }
    }
}

fn stamp(text: &str) -> Timestamp {
    let date =
        time::OffsetDateTime::parse(text, &time::format_description::well_known::Rfc3339).unwrap();
    Timestamp {
        seconds: date.unix_timestamp(),
        nanoseconds: date.nanosecond(),
        offset_seconds: date.offset().whole_seconds(),
    }
}
fn entry() -> LogEntry {
    LogEntry {
        at: stamp("2028-01-02T03:04:05Z"),
        actor: "matt".into(),
        event: "status open → accepted\ncontinued".into(),
        ..LogEntry::default()
    }
}
fn apply(document: &mut RequestDocument, kind: &str) {
    match kind {
        "id" => document.metadata.id = "foreign-r-b4c5.1".into(),
        "aliases" => document.metadata.aliases = vec!["external".into()],
        "title" => document.metadata.title = "雪: new title".into(),
        "status" => document.metadata.status = "accepted".into(),
        "priority" => document.metadata.priority = 4,
        "labels" => document.metadata.labels = vec!["true".into(), "snow 雪".into()],
        "requested_by" => document.metadata.requested_by = "New Actor".into(),
        "issues" => {
            document.metadata.issues = vec![
                Link::parse("[[other-abc#heading|alias]]"),
                Link::parse("bare"),
            ]
        }
        "created" => document.metadata.created = stamp("2027-01-02T03:04:05.123+05:30"),
        "updated" => document.metadata.updated = stamp("2027-01-02T03:04:05.123Z"),
        "body" => document.set_body("replacement\n\n"),
        "body-empty" => document.set_body(""),
        "append" => document.append_log(entry()),
        "opaque-log" => {
            if let Some(first) = document.log.first_mut() {
                first.raw = "- replaced original".into();
                first.event = "ignored".into();
            }
        }
        "all" => {
            for key in [
                "title",
                "status",
                "priority",
                "labels",
                "requested_by",
                "issues",
                "body",
                "append",
            ] {
                apply(document, key);
            }
        }
        "clear-optionals" => {
            document.metadata.labels.clear();
            document.metadata.requested_by.clear();
            document.metadata.issues.clear();
        }
        "invalid-status" => document.metadata.status = "closed".into(),
        "invalid-id" => document.metadata.id = "beans-a3f2".into(),
        "invalid-priority" => document.metadata.priority = 5,
        "blank-title" => document.metadata.title = " \t\n".into(),
        "zero-created" => document.metadata.created = Timestamp::default(),
        "equal-log-time" => document.append_log(LogEntry {
            at: document.metadata.updated.clone(),
            actor: "matt".into(),
            event: "equal time".into(),
            ..LogEntry::default()
        }),
        "same-instant" => {
            document.metadata.created.offset_seconds = 0;
            document.metadata.updated.offset_seconds = 0;
        }
        "new" | "new-extra" => {
            let mut new = RequestDocument::new(document.metadata.clone());
            new.body = document.body.clone();
            new.log = document.log.clone();
            if kind == "new-extra" {
                use beans::domain::authored_yaml::{FLOW, Node};
                let mut nested = Node::sequence([Node::string("true"), Node::string("other")]);
                nested.style = FLOW;
                new.new_extra = Node::mapping([
                    (Node::string("custom"), Node::string("雪: value")),
                    (Node::string("nested"), nested),
                ]);
            }
            *document = new;
        }
        _ => panic!("unknown request mutation {kind}"),
    }
}

#[test]
fn round_trip_and_mutation() {
    let mut r = RequestDocument::parse(PATH, SAMPLE).unwrap();
    assert_eq!(r.encode().unwrap().bytes, SAMPLE.as_bytes());
    r.metadata.status = "accepted".into();
    r.append_log(LogEntry {
        at: stamp("2026-09-12T00:00:00Z"),
        actor: "matt".into(),
        event: "status open → accepted".into(),
        ..LogEntry::default()
    });
    let out = String::from_utf8(r.encode().unwrap().bytes).unwrap();
    assert!(out.contains("status: accepted"));
    assert!(out.contains("## Later\nKeep this too.\n"));
    assert!(out.contains("- 2026-09-12T00:00:00Z matt: status open → accepted"));
}

#[test]
fn parsing_errors() {
    for (path, text) in [
        ("projects/beans/issues/beans-r-a3f2.md", SAMPLE.to_owned()),
        (PATH, SAMPLE.replacen("status: open", "status: later", 1)),
        (PATH, SAMPLE.replace('\n', "\r\n")),
        (PATH, SAMPLE.replacen("title: Durable requests\n", "", 1)),
    ] {
        assert!(RequestDocument::parse(path, &text).is_err());
    }
}

#[test]
fn set_request_body() {
    let mut r = RequestDocument::parse(PATH, SAMPLE).unwrap();
    r.set_body("replacement\n\n");
    let out = String::from_utf8(r.encode().unwrap().bytes).unwrap();
    assert!(out.contains("---\nreplacement\n## Log"));
    assert!(out.contains("## Later\nKeep this too."));
}

#[test]
fn round_trip_fixtures() {
    let corpus: Value = serde_json::from_str(include_str!("../contract/requests.json")).unwrap();
    let fixtures: Vec<_> = corpus["cases"]
        .as_array()
        .unwrap()
        .iter()
        .filter(|case| case["name"].as_str().unwrap().ends_with(".md"))
        .collect();
    assert!(fixtures.len() >= 5);
    for case in fixtures {
        let source = case["input"].as_str().unwrap();
        let parsed = RequestDocument::parse(case["path"].as_str().unwrap(), source);
        if case["name"].as_str().unwrap().starts_with("err_") {
            assert!(parsed.is_err());
        } else {
            assert_eq!(parsed.unwrap().encode().unwrap().bytes, source.as_bytes());
        }
    }
}

#[test]
fn reader_and_all_mutations_match_fixed_go() {
    let corpus: Value = serde_json::from_str(include_str!("../contract/requests.json")).unwrap();
    let mut exports = Vec::new();
    for case in corpus["cases"].as_array().unwrap() {
        let path = case["path"].as_str().unwrap();
        let source = case["input"].as_str().unwrap();
        let parsed = RequestDocument::parse(path, source);
        if let Some(error) = case.get("error") {
            assert_eq!(
                parsed.unwrap_err().to_string(),
                error.as_str().unwrap(),
                "{}",
                case["name"]
            );
            continue;
        }
        let mut parsed = parsed.unwrap_or_else(|error| panic!("{}: {error}", case["name"]));
        assert_eq!(
            serde_json::to_value(&parsed.metadata).unwrap(),
            case["metadata"],
            "{} metadata",
            case["name"]
        );
        assert_eq!(
            parsed.body.as_bytes(),
            case["body"].as_str().unwrap().as_bytes()
        );
        assert_eq!(serde_json::to_value(&parsed.log).unwrap(), case["log"]);
        assert_eq!(
            serde_json::to_value(parsed.unknown_fields().map(|f| &f.key).collect::<Vec<_>>())
                .unwrap(),
            case["unknown"]
        );
        assert_eq!(
            parsed.encode().unwrap().bytes,
            case["encoded"].as_str().unwrap().as_bytes()
        );
        parsed.new_extra = beans::domain::authored_yaml::Node::mapping([(
            beans::domain::authored_yaml::Node::string("must-be-ignored"),
            beans::domain::authored_yaml::Node::string("replacement"),
        )]);
        assert_eq!(
            parsed.encode().unwrap().bytes,
            case["encoded"].as_str().unwrap().as_bytes()
        );
        for edit in case["edits"].as_array().unwrap() {
            let mut r = RequestDocument::parse(path, source).unwrap();
            apply(&mut r, edit["kind"].as_str().unwrap());
            let encoded = r.encode();
            if let Some(error) = edit.get("error") {
                assert_eq!(encoded.unwrap_err().to_string(), error.as_str().unwrap());
                assert_eq!(r.original(), source);
                continue;
            }
            let encoded = encoded.unwrap();
            assert_eq!(
                encoded.bytes,
                edit["encoded"].as_str().unwrap().as_bytes(),
                "{} {}",
                case["name"],
                edit["kind"]
            );
            for copy in &encoded.copies {
                assert_eq!(
                    &source.as_bytes()[copy.source.clone()],
                    &encoded.bytes[copy.destination.clone()]
                );
            }
            let input = String::from_utf8(encoded.bytes).unwrap();
            let read = RequestDocument::parse(path, &input).unwrap();
            exports.push(serde_json::json!({"name":format!("{}:{}",case["name"].as_str().unwrap(),edit["kind"].as_str().unwrap()), "path":path,"input":input,"expected_metadata":read.metadata,"expected_body":read.body,"expected_log":read.log}));
        }
    }
    if let Ok(file) = std::env::var("BN_RUST_REQUEST_OUTPUT") {
        std::fs::write(file, serde_json::to_vec(&exports).unwrap()).unwrap();
    }
}
