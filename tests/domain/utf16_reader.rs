use beans::domain::{
    handoff::HandoffDocument,
    issue::{IssueDocument, Timestamp},
    memory::MemoryDocument,
    request::RequestDocument,
};
use serde_json::{Value, json};
fn bytes(v: &Value) -> Vec<u8> {
    v.as_array()
        .unwrap()
        .iter()
        .flat_map(|chunk| {
            if chunk.is_array() {
                serde_json::from_value::<Vec<u8>>(chunk.clone()).unwrap()
            } else {
                let pattern: Vec<u8> = serde_json::from_value(chunk["pattern"].clone()).unwrap();
                pattern.repeat(chunk["count"].as_u64().unwrap() as usize)
            }
        })
        .collect()
}
fn source(c: &Value) -> Vec<u8> {
    bytes(&c["bytes"])
}
#[test]
fn utf16_yaml_reader_errors_and_raw_lf_edits_match_fixed_go() {
    let corpus: Value =
        serde_json::from_str(include_str!("../contract/utf16-reader.json")).unwrap();
    let mut failures = vec![];
    let capture = std::env::var_os("BN_RUST_UTF16_READER_OUTPUT");
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
            ".compat/utf16-reader-failures.json",
            serde_json::to_vec_pretty(&failures).unwrap(),
        )
        .unwrap();
    }
    assert!(
        failures.is_empty(),
        "{} utf16-reader mismatches; see .compat/utf16-reader-failures.json",
        failures.len()
    );
}

#[test]
fn utf16_parser_view_keeps_physical_field_ranges_and_disk_index_bytes() {
    use beans::domain::frontmatter::Frontmatter;
    let path = "projects/p/issues/p-one.md";
    let text = "id: p-one\ntitle: α😀 # keep\ntype: task\nstatus: open\npriority: 2\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n...\n";
    for little in [true, false] {
        let mut raw = b"---\n".to_vec();
        raw.extend(if little { [255, 254] } else { [254, 255] });
        for unit in text.encode_utf16() {
            raw.extend(if little {
                unit.to_le_bytes()
            } else {
                unit.to_be_bytes()
            });
        }
        raw.extend_from_slice(b"\n\n---\nbody\xff\n");
        let doc = Frontmatter::parse_bytes(path, &raw).unwrap();
        let title = doc.fields().iter().find(|f| f.key == "title").unwrap();
        let start = 4 + raw[4..].iter().position(|b| *b == b'\n').unwrap() + 1;
        let end = start + raw[start..].iter().position(|b| *b == b'\n').unwrap() + 1;
        assert_eq!((title.start, title.end), (start, end));
        let mut issue = IssueDocument::parse_bytes(path, &raw).unwrap();
        assert_eq!(issue.metadata.title.as_bytes(), "α😀".as_bytes());
        assert_eq!(issue.encode().unwrap().bytes, raw);
        issue.metadata.title = "Changed".into();
        let edit = issue.encode().unwrap();
        let mut expected = raw[..start].to_vec();
        expected.extend_from_slice(b"title: Changed # keep\n");
        expected.extend_from_slice(&raw[end..]);
        assert_eq!(edit.bytes, expected);
        for copy in edit.copies {
            assert_eq!(&edit.bytes[copy.destination], &raw[copy.source]);
        }
        let hub = super::index::hub(
            if little {
                "utf16-le-index"
            } else {
                "utf16-be-index"
            },
            false,
        );
        let file = hub.0.join(path);
        std::fs::create_dir_all(file.parent().unwrap()).unwrap();
        std::fs::write(file, &raw).unwrap();
        let ix = beans::vault::Index::load(&hub.0).unwrap();
        assert!(ix.graph.warnings().is_empty());
        let note = ix
            .note_by_id(beans::vault::NoteKind::Issue, b"p-one")
            .unwrap();
        let beans::vault::NoteData::Issue(issue) = &note.data else {
            panic!()
        };
        assert_eq!(issue.metadata.title.as_bytes(), "α😀".as_bytes());
        assert_eq!(issue.encode().unwrap().bytes, raw);
    }
}
