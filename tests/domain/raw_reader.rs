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
fn raw_yaml_reader_windows_errors_and_ignored_suffix_edits_match_fixed_go() {
    let corpus: Value = serde_json::from_str(include_str!("../contract/raw-reader.json")).unwrap();
    let mut failures = vec![];
    let capture = std::env::var_os("BN_RUST_RAW_READER_OUTPUT");
    let mut candidates = vec![];
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
            let error = result
                .as_ref()
                .err()
                .map(ToString::to_string)
                .unwrap_or_default();
            if error != expected["error"].as_str().unwrap() {
                failures.push(
                    json!({"case":i,"mode":mode,"actual":error,"expected":expected["error"]}),
                );
                continue;
            }
            if let Ok(output) = result {
                if capture.is_some() {
                    candidates.push(json!({"Kind":c["kind"],"Path":path,"ExpectedError":expected["reread_error"],"Bytes":output.bytes}));
                }
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
                if reread_error != expected["reread_error"].as_str().unwrap() {
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
    if let Some(path) = capture {
        std::fs::write(path, serde_json::to_vec(&candidates).unwrap()).unwrap();
    }
    if !failures.is_empty() {
        std::fs::create_dir_all(".compat").unwrap();
        std::fs::write(
            ".compat/raw-reader-failures.json",
            serde_json::to_vec_pretty(&failures).unwrap(),
        )
        .unwrap();
    }
    assert!(
        failures.is_empty(),
        "{} raw-reader mismatches; see .compat/raw-reader-failures.json",
        failures.len()
    );
}

#[test]
fn raw_reader_error_precedence_follows_tokens_and_preserves_ignored_bytes() {
    let path = "projects/p/issues/p-one.md";
    let head=b"id: p-one\ntitle: Title\ntype: task\nstatus: open\npriority: 2\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n";
    let prefix = "projects/p/issues/p-one.md: frontmatter: yaml: ";
    let build = |start: &[u8], padding: usize, ending: &[u8]| {
        let mut v = b"---\n".to_vec();
        v.extend_from_slice(start);
        v.resize(v.len() + padding, b'x');
        v.extend_from_slice(ending);
        v.extend_from_slice(b"---\nbody\xff\n");
        v
    };
    let far = build(b"bad: [}\npadding: '", 1024, b"\xff'\n");
    assert_eq!(
        IssueDocument::parse_bytes(path, &far)
            .unwrap_err()
            .to_string(),
        format!("{prefix}did not find expected node content")
    );
    let near = build(b"bad: [}\npadding: '", 4, b"\xc0\xaf'\n");
    assert_eq!(
        IssueDocument::parse_bytes(path, &near)
            .unwrap_err()
            .to_string(),
        format!("{prefix}invalid length of a UTF-8 sequence")
    );
    let mut comments = head.to_vec();
    comments.extend_from_slice(b"bad: [}\n# ");
    let raw = build(&comments, 1024, b"\xff\n");
    assert_eq!(
        IssueDocument::parse_bytes(path, &raw)
            .unwrap_err()
            .to_string(),
        format!("{prefix}invalid leading UTF-8 octet")
    );
    let mut ignored = head.to_vec();
    ignored.extend_from_slice(b"...\nkey: value\n# ");
    let raw = build(&ignored, 1024, b"\xff\n");
    let document = IssueDocument::parse_bytes(path, &raw).unwrap();
    assert_eq!(document.original_bytes(), raw);
    assert_eq!(document.encode().unwrap().bytes, raw);
    let hub = super::index::hub("raw-reader-index", false);
    let file = hub.0.join(path);
    std::fs::create_dir_all(file.parent().unwrap()).unwrap();
    std::fs::write(file, &raw).unwrap();
    let ix = beans::vault::Index::load(&hub.0).unwrap();
    assert!(ix.graph.warnings().is_empty());
    let note = ix
        .note_by_id(beans::vault::NoteKind::Issue, b"p-one")
        .unwrap();
    let beans::vault::NoteData::Issue(d) = &note.data else {
        panic!()
    };
    assert_eq!(d.encode().unwrap().bytes, raw);
}
