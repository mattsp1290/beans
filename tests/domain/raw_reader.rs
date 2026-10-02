use beans::domain::{
    handoff::HandoffDocument,
    issue::{IssueDocument, Timestamp},
    memory::MemoryDocument,
    request::RequestDocument,
};
use serde_json::{Value, json};
fn bytes(v: &Value) -> Vec<u8> {
    serde_json::from_value(v.clone()).unwrap()
}
fn source(c: &Value) -> Vec<u8> {
    let mut raw = b"---\n".to_vec();
    raw.extend_from_slice(c["prefix"].as_str().unwrap().as_bytes());
    raw.resize(raw.len() + c["padding"].as_u64().unwrap() as usize, b'x');
    raw.extend_from_slice(&bytes(&c["sequence"]));
    raw.extend_from_slice(c["suffix"].as_str().unwrap().as_bytes());
    raw.extend_from_slice(b"---\nbody\xff\n");
    raw
}
#[test]
fn native_yaml_encoding_rejection_and_valid_raw_edits_preserve_bytes() {
    let corpus: Value =
        serde_json::from_str(include_str!("../fixtures/expected/raw-reader.json")).unwrap();
    let mut failures = vec![];
    for (i, c) in corpus["cases"].as_array().unwrap().iter().enumerate() {
        let raw = source(c);
        let path = c["path"].as_str().unwrap();
        for (mode, expected) in c["outputs"].as_object().unwrap() {
            let now = Timestamp {
                seconds: 1798761600,
                nanoseconds: 0,
                offset_seconds: 0,
            };
            let result = match c["kind"].as_str().unwrap() {
                "issue" => IssueDocument::parse_bytes(path, &raw).and_then(|mut d| {
                    if mode == "first" {
                        d.metadata.title = "Changed".into()
                    };
                    if mode == "last" {
                        d.metadata.updated = now
                    };
                    d.encode()
                }),
                "request" => RequestDocument::parse_bytes(path, &raw).and_then(|mut d| {
                    if mode == "first" {
                        d.metadata.title = "Changed".into()
                    };
                    if mode == "last" {
                        d.metadata.updated = now
                    };
                    d.encode()
                }),
                "memory" => MemoryDocument::parse_bytes(path, &raw).and_then(|mut d| {
                    if mode == "first" {
                        d.metadata.kind = "feedback".into()
                    };
                    if mode == "last" {
                        d.metadata.updated = now
                    };
                    d.encode()
                }),
                "handoff" => HandoffDocument::parse_bytes(path, &raw).and_then(|mut d| {
                    if mode == "first" {
                        d.metadata.title = "Changed".into()
                    };
                    if mode == "last" {
                        d.metadata.updated = now
                    };
                    d.encode()
                }),
                _ => unreachable!(),
            };
            if let Some(reason) = super::diagnostics::encoding_error(&raw) {
                let error = result.expect_err("malformed frontmatter must never produce an edit");
                assert_eq!(
                    error.to_string(),
                    format!("{path}: frontmatter: {reason}"),
                    "case{i} {mode}"
                );
                continue;
            }
            let error = result
                .as_ref()
                .err()
                .map(ToString::to_string)
                .unwrap_or_default();
            if super::diagnostics::text(&error)
                != super::diagnostics::text(expected["error"].as_str().unwrap())
            {
                failures.push(
                    json!({"case":i,"mode":mode,"actual":error,"expected":expected["error"]}),
                );
                continue;
            }
            if let Ok(output) = result {
                let wanted = if mode == "noop" {
                    raw.clone()
                } else {
                    bytes(&expected["bytes"])
                };
                if output.bytes != wanted {
                    failures.push(json!({"case":i,"mode":mode,"actual_bytes":output.bytes,"expected_bytes":wanted}));
                }
                let reread =
                    match c["kind"].as_str().unwrap() {
                        "issue" => {
                            IssueDocument::parse_bytes(path, &output.bytes).and_then(|d| d.encode())
                        }
                        "request" => RequestDocument::parse_bytes(path, &output.bytes)
                            .and_then(|mut d| d.encode()),
                        "memory" => MemoryDocument::parse_bytes(path, &output.bytes)
                            .and_then(|d| d.encode()),
                        "handoff" => HandoffDocument::parse_bytes(path, &output.bytes)
                            .and_then(|d| d.encode()),
                        _ => unreachable!(),
                    };
                let reread_error = reread
                    .as_ref()
                    .err()
                    .map(ToString::to_string)
                    .unwrap_or_default();
                if super::diagnostics::text(&reread_error)
                    != super::diagnostics::text(expected["reread_error"].as_str().unwrap())
                {
                    failures.push(json!({"case":i,"mode":mode,"phase":"reread","actual":reread_error,"expected":expected["reread_error"]}));
                }
                if let Ok(reread) = reread {
                    assert_eq!(reread.bytes, output.bytes, "reread {i} {mode}");
                }
                for copy in output.copies {
                    assert_eq!(
                        &output.bytes[copy.destination], &raw[copy.source],
                        "{i} {mode}"
                    );
                }
            }
        }
    }
    if !failures.is_empty() {
        std::fs::create_dir_all(".verification").unwrap();
        std::fs::write(
            ".verification/raw-reader-failures.json",
            serde_json::to_vec_pretty(&failures).unwrap(),
        )
        .unwrap();
    }
    assert!(
        failures.is_empty(),
        "{} raw-reader mismatches; see .verification/raw-reader-failures.json",
        failures.len()
    );
}

#[test]
fn malformed_frontmatter_is_rejected_across_document_end_and_reader_boundaries() {
    let path = "projects/p/issues/p-one.md";
    let head=b"id: p-one\ntitle: Title\ntype: task\nstatus: open\npriority: 2\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n";
    for (prefix, padding, ending) in [
        (
            b"bad: [}\npadding: '".as_slice(),
            1024,
            b"\xff'\n".as_slice(),
        ),
        (
            b"bad: [}\npadding: '".as_slice(),
            4,
            b"\xc0\xaf'\n".as_slice(),
        ),
        (
            b"...\nkey: value\n# ".as_slice(),
            1024,
            b"\xff\n".as_slice(),
        ),
    ] {
        let mut raw = b"---\n".to_vec();
        raw.extend_from_slice(head);
        raw.extend_from_slice(prefix);
        raw.resize(raw.len() + padding, b'x');
        raw.extend_from_slice(ending);
        raw.extend_from_slice(b"---\nbody\xff\n");
        assert!(
            IssueDocument::parse_bytes(path, &raw)
                .unwrap_err()
                .to_string()
                .contains("invalid UTF-8 encoding")
        );
        let hub = super::index::hub("native-invalid-frontmatter", false);
        let file = hub.0.join(path);
        std::fs::create_dir_all(file.parent().unwrap()).unwrap();
        std::fs::write(&file, &raw).unwrap();
        let ix = beans::vault::Index::load(&hub.0).unwrap();
        assert!(
            ix.note_by_id(beans::vault::NoteKind::Issue, b"p-one")
                .is_none()
        );
        assert_eq!(ix.graph.warnings().len(), 1);
        assert_eq!(std::fs::read(file).unwrap(), raw);
    }
    let mut valid = b"---\n".to_vec();
    valid.extend_from_slice(head);
    valid.extend_from_slice(b"...\nkey: value\n---\nbody\xff\n");
    assert_eq!(
        IssueDocument::parse_bytes(path, &valid)
            .unwrap()
            .encode()
            .unwrap()
            .bytes,
        valid
    );
}
