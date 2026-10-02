use beans::domain::{handoff::HandoffDocument, issue::Timestamp, text::Link};
use proptest::prelude::*;
use serde_json::Value;
const PATH: &str = "projects/alpha/handoffs/alpha-a1b2-continue-work.md";
const SAMPLE: &str = r##"---
id: alpha-a1b2
aliases: [alpha-a1b2]
title: Continue work
issue: "[[alpha-c3d4-fix-it]]"
created: 2026-09-10T21:44:02Z
updated: 2026-09-10T21:44:02Z
custom: keep me
---
# Context

Body.
"##;

fn stamp(text: &str) -> Timestamp {
    let t =
        time::OffsetDateTime::parse(text, &time::format_description::well_known::Rfc3339).unwrap();
    Timestamp {
        seconds: t.unix_timestamp(),
        nanoseconds: t.nanosecond(),
        offset_seconds: t.offset().whole_seconds(),
    }
}
fn apply(h: &mut HandoffDocument, kind: &str) {
    match kind {
        "id" => h.metadata.id = "foreign-a4b5.1".into(),
        "id-empty" => h.metadata.id.clear(),
        "aliases" => h.metadata.aliases = vec!["external".into()],
        "aliases-empty" => h.metadata.aliases.clear(),
        "title" => h.metadata.title = "雪: new title".into(),
        "title-empty" => h.metadata.title.clear(),
        "issue" => h.metadata.issue = Link::parse("[[other-abc#heading|alias]]"),
        "issue-target-only" => {
            h.metadata.issue = Link {
                target: "other-abc".into(),
                ..Link::default()
            }
        }
        "detach" => h.metadata.issue = Link::default(),
        "created" => h.metadata.created = stamp("2027-01-02T03:04:05.123+05:30"),
        "updated" => h.metadata.updated = stamp("2027-01-02T03:04:05.123Z"),
        "zero-time" => h.metadata.created = Timestamp::default(),
        "body" => h.body = "replacement\n## Log\nopaque\n".into(),
        "same-instant" => {
            h.metadata.created.offset_seconds = 0;
            h.metadata.updated.offset_seconds = 0;
        }
        "all" => {
            for key in ["aliases", "title", "issue", "created", "updated", "body"] {
                apply(h, key);
            }
        }
        "new" | "new-extra" | "new-extra-scalar" | "new-extra-sequence" => {
            let mut new = HandoffDocument::new(h.metadata.clone());
            new.body = h.body.clone();
            if kind != "new" {
                use beans::domain::authored_yaml::{Kind, Node};
                new.new_extra =
                    Node::mapping([(Node::string("custom"), Node::string("雪: value"))]);
                if kind == "new-extra-scalar" {
                    new.new_extra.kind = Kind::Scalar;
                }
                if kind == "new-extra-sequence" {
                    new.new_extra.kind = Kind::Sequence;
                }
            }
            *h = new;
        }
        _ => panic!("unknown handoff mutation {kind}"),
    }
}
#[test]
fn round_trip_and_minimal_attachment_edit() {
    let mut h = HandoffDocument::parse(PATH, SAMPLE).unwrap();
    assert_eq!(h.encode().unwrap().bytes, SAMPLE.as_bytes());
    h.metadata.issue = Link::default();
    h.metadata.updated.seconds += 1;
    let out = String::from_utf8(h.encode().unwrap().bytes).unwrap();
    assert!(!out.contains("issue:"));
    assert!(out.contains("custom: keep me"));
    assert!(out.contains("# Context"));
}
#[test]
fn rejects_bad_input() {
    for input in [
        SAMPLE.replacen("id: alpha-a1b2", "id: bad", 1),
        SAMPLE.replacen("updated: 2026-09-10T21:44:02Z", "updated: nope", 1),
        SAMPLE.replacen(
            "title: Continue work",
            "title: Continue work\ntitle: duplicate",
            1,
        ),
        SAMPLE.replacen('\n', "\r\n", 1),
    ] {
        assert!(
            HandoffDocument::parse("projects/alpha/handoffs/alpha-a1b2-title.md", &input).is_err()
        );
    }
}
#[test]
fn rejects_misleading_path() {
    for path in [
        "projects/alpha/handoffs/archive/not-a-year/alpha-a1b2-title.md",
        "projects/alpha/handoffs/archive/abcd/alpha-a1b2-title.md",
        "projects/alpha/handoffs/nested/alpha-a1b2-title.md",
        "docs/alpha-a1b2-title.md",
    ] {
        assert!(HandoffDocument::parse(path, SAMPLE).is_err());
    }
}
proptest! {
    #![proptest_config(ProptestConfig::with_cases(512))]
    #[test]
    fn edits_preserve_unknown_bytes(body in "[a-z雪☃ ]{0,80}",blanks in 0..5usize) {
        let source=SAMPLE.replace("custom: keep me",&format!("custom: {{雪: [one, two]}}\n{}# keep", "\n".repeat(blanks))).replace("Body.",&body);
        let mut h=HandoffDocument::parse(PATH,&source).unwrap();prop_assert_eq!(h.encode().unwrap().bytes,source.as_bytes());
        h.metadata.title="New title".into();let out=h.encode().unwrap();let expected=source.replace("title: Continue work","title: New title");prop_assert_eq!(&out.bytes,expected.as_bytes());
        for copy in out.copies { prop_assert_eq!(&source.as_bytes()[copy.source],&out.bytes[copy.destination]); }
    }
}
#[test]
fn reader_and_all_mutations_match_committed_contract() {
    let corpus: Value =
        serde_json::from_str(include_str!("../fixtures/expected/handoffs.json")).unwrap();
    for c in corpus["cases"].as_array().unwrap() {
        let path = c["path"].as_str().unwrap();
        let source = c["input"].as_str().unwrap();
        let parsed = HandoffDocument::parse(path, source);
        if let Some(error) = c.get("error") {
            assert_eq!(
                parsed.unwrap_err().to_string(),
                error.as_str().unwrap(),
                "{}",
                c["name"]
            );
            continue;
        }
        let mut parsed = parsed.unwrap();
        assert_eq!(
            serde_json::to_value(&parsed.metadata).unwrap(),
            c["metadata"],
            "{}",
            c["name"]
        );
        assert_eq!(
            parsed.body.as_bytes(),
            c["body"].as_str().unwrap().as_bytes()
        );
        assert_eq!(
            serde_json::to_value(parsed.unknown_fields().map(|f| &f.key).collect::<Vec<_>>())
                .unwrap(),
            c["unknown"]
        );
        assert_eq!(
            parsed.encode().unwrap().bytes,
            c["encoded"].as_str().unwrap().as_bytes()
        );
        parsed.new_extra = beans::domain::authored_yaml::Node::string("ignored");
        assert_eq!(
            parsed.encode().unwrap().bytes,
            c["encoded"].as_str().unwrap().as_bytes()
        );
        for edit in c["edits"].as_array().unwrap() {
            let mut h = HandoffDocument::parse(path, source).unwrap();
            apply(&mut h, edit["kind"].as_str().unwrap());
            let out = h.encode().unwrap();
            assert_eq!(
                out.bytes,
                edit["encoded"].as_str().unwrap().as_bytes(),
                "{} {}",
                c["name"],
                edit["kind"]
            );
            for copy in &out.copies {
                assert_eq!(
                    &source.as_bytes()[copy.source.clone()],
                    &out.bytes[copy.destination.clone()]
                );
            }
            let input = String::from_utf8(out.bytes).unwrap();
            let mut export = serde_json::json!({"name":format!("{}:{}",c["name"].as_str().unwrap(),edit["kind"].as_str().unwrap()),"path":path,"input":input});
            match HandoffDocument::parse(path, &input) {
                Ok(read) => {
                    export["expected_metadata"] = serde_json::to_value(read.metadata).unwrap();
                    export["expected_body"] = serde_json::json!(read.body);
                }
                Err(error) => export["expected_error"] = serde_json::json!(error.to_string()),
            }
        }
    }
}
