use beans::domain::{
    issue::Timestamp,
    memory::{self, MemoryDocument, MemoryMetadata},
};
use proptest::prelude::*;
use serde_json::Value;
const SAMPLE: &str = r#"---
key: bean-counter-prod-schema
type: project
tags: [deploy, prod]
created: 2026-06-14T00:00:00Z
updated: 2026-09-10T00:00:00Z
# a user comment
custom: keep me
---
Body text here.

More body.
"#;

fn stamp(text: &str) -> Timestamp {
    let t =
        time::OffsetDateTime::parse(text, &time::format_description::well_known::Rfc3339).unwrap();
    Timestamp {
        seconds: t.unix_timestamp(),
        nanoseconds: t.nanosecond(),
        offset_seconds: t.offset().whole_seconds(),
    }
}
fn apply(m: &mut MemoryDocument, kind: &str) {
    match kind {
        "key" => m.metadata.key = "New Key 雪".into(),
        "key-empty" => m.metadata.key.clear(),
        "type" => m.metadata.kind = "reference".into(),
        "tags" => m.metadata.tags = vec!["true".into(), "snow 雪".into()],
        "created" => m.metadata.created = stamp("2027-01-02T03:04:05.123+05:30"),
        "updated" => m.metadata.updated = stamp("2027-01-02T03:04:05.123Z"),
        "clear-optionals" => {
            m.metadata.kind.clear();
            m.metadata.tags.clear();
            m.metadata.created = Timestamp::default();
            m.metadata.updated = Timestamp::default();
        }
        "body" => m.body = "replacement\n## Log\nopaque\n".into(),
        "same-instant" => {
            m.metadata.created.offset_seconds = 0;
            m.metadata.updated.offset_seconds = 0;
        }
        "all" => {
            for key in ["key", "type", "tags", "created", "updated", "body"] {
                apply(m, key);
            }
        }
        "new" | "new-extra" => {
            let mut new = MemoryDocument::new(m.metadata.clone());
            new.body = m.body.clone();
            if kind == "new-extra" {
                use beans::domain::authored_yaml::Node;
                new.new_extra =
                    Node::mapping([(Node::string("custom"), Node::string("雪: value"))]);
            }
            *m = new;
        }
        _ => panic!("unknown memory edit {kind}"),
    }
}

#[test]
fn round_trip() {
    let m = MemoryDocument::parse("memories/bean-counter-prod-schema.md", SAMPLE).unwrap();
    assert_eq!(m.metadata.key, "bean-counter-prod-schema");
    assert_eq!(m.metadata.kind, "project");
    assert_eq!(m.metadata.tags, ["deploy", "prod"]);
    assert_eq!(m.unknown_fields().next().unwrap().key, "custom");
    assert_eq!(m.encode().unwrap().bytes, SAMPLE.as_bytes());
}
#[test]
fn mutation_splices_only_changed_lines() {
    let mut m = MemoryDocument::parse("m.md", SAMPLE).unwrap();
    m.metadata.kind = "reference".into();
    m.metadata.tags.push("urgent".into());
    let out = m.encode().unwrap().bytes;
    assert_eq!(
        out,
        SAMPLE
            .replace("type: project", "type: reference")
            .replace("tags: [deploy, prod]", "tags: [deploy, prod, urgent]")
            .as_bytes()
    );
    let again = MemoryDocument::parse("m.md", std::str::from_utf8(&out).unwrap()).unwrap();
    assert_eq!(again.metadata.kind, "reference");
    assert_eq!(again.metadata.tags, ["deploy", "prod", "urgent"]);
}
#[test]
fn new_memory_encodes_in_key_order() {
    let mut m = MemoryDocument::new(MemoryMetadata {
        key: "new-memory-key".into(),
        kind: "user".into(),
        tags: vec!["a".into(), "b".into()],
        created: stamp("2026-01-01T00:00:00Z"),
        updated: stamp("2026-01-02T00:00:00Z"),
        ..MemoryMetadata::default()
    });
    m.body = "Some body text.\n".into();
    let out = m.encode().unwrap().bytes;
    assert_eq!(out,b"---\nkey: new-memory-key\ntype: user\ntags: [a, b]\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-02T00:00:00Z\n---\nSome body text.\n");
    let back =
        MemoryDocument::parse("new-memory-key.md", std::str::from_utf8(&out).unwrap()).unwrap();
    assert_eq!(back.metadata.key, m.metadata.key);
    assert_eq!(back.metadata.kind, m.metadata.kind);
}
#[test]
fn missing_key_is_error() {
    assert!(MemoryDocument::parse("no-key.md","---\ntype: user\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\nbody\n").is_err());
}
#[test]
fn valid_memory_key() {
    for key in ["bean-counter-prod-schema", "a", "a1-b2-c3"] {
        assert!(memory::valid_key(key));
    }
    for key in ["", "-leading-dash", "Upper", "has_underscore", "has space"] {
        assert!(!memory::valid_key(key));
    }
    assert!(!memory::valid_key(&"a".repeat(81)));
    assert!(memory::valid_key(&"a".repeat(80)));
}
#[test]
fn valid_memory_type() {
    for kind in ["user", "feedback", "project", "reference", ""] {
        assert!(memory::valid_type(kind));
    }
    for kind in ["bogus", "USER", "task"] {
        assert!(!memory::valid_type(kind));
    }
}
#[test]
fn rejects_crlf() {
    assert!(MemoryDocument::parse("crlf.md", "---\r\nkey: x\r\n---\r\nbody\r\n").is_err());
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(512))]
    #[test]
    fn edits_preserve_unknown_bytes(text in "[a-z雪☃ ]{0,80}", blanks in 0..5usize) {
        let source=SAMPLE.replace("custom: keep me",&format!("custom: {{雪: [one, two]}}\n{}# keep", "\n".repeat(blanks))).replace("Body text here.",&text);
        let mut m=MemoryDocument::parse("m.md",&source).unwrap();prop_assert_eq!(m.encode().unwrap().bytes,source.as_bytes());
        m.metadata.kind="reference".into();let out=m.encode().unwrap();let expected=source.replace("type: project","type: reference");prop_assert_eq!(&out.bytes,expected.as_bytes());
        for copy in out.copies { prop_assert_eq!(&source.as_bytes()[copy.source],&out.bytes[copy.destination]); }
    }
}

#[test]
fn reader_and_all_mutations_match_committed_contract() {
    let corpus: Value = serde_json::from_str(include_str!("../contract/memories.json")).unwrap();
    for c in corpus["cases"].as_array().unwrap() {
        let path = c["path"].as_str().unwrap();
        let source = c["input"].as_str().unwrap();
        let parsed = MemoryDocument::parse(path, source);
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
            memory::valid_key(&parsed.metadata.key),
            c["valid_key"].as_bool().unwrap()
        );
        assert_eq!(
            memory::valid_type(&parsed.metadata.kind),
            c["valid_type"].as_bool().unwrap()
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
        parsed.new_extra = beans::domain::authored_yaml::Node::mapping([(
            beans::domain::authored_yaml::Node::string("ignored"),
            beans::domain::authored_yaml::Node::string("changed"),
        )]);
        assert_eq!(
            parsed.encode().unwrap().bytes,
            c["encoded"].as_str().unwrap().as_bytes()
        );
        for edit in c["edits"].as_array().unwrap() {
            let mut m = MemoryDocument::parse(path, source).unwrap();
            apply(&mut m, edit["kind"].as_str().unwrap());
            let out = m.encode().unwrap();
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
            match MemoryDocument::parse(path, &input) {
                Ok(read) => {
                    export["expected_metadata"] = serde_json::to_value(read.metadata).unwrap();
                    export["expected_body"] = serde_json::json!(read.body);
                }
                Err(error) => export["expected_error"] = serde_json::json!(error.to_string()),
            }
        }
    }
}
