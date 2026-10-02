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
fn scalar_comments_in_utf8_and_utf16_match_committed_contract() {
    let corpus: Value =
        serde_json::from_str(include_str!("../contract/scalar-comment.json")).unwrap();
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
    if !failures.is_empty() {
        std::fs::create_dir_all(".verification").unwrap();
        std::fs::write(
            ".verification/scalar-comment-failures.json",
            serde_json::to_vec_pretty(&failures).unwrap(),
        )
        .unwrap();
    }
    assert!(
        failures.is_empty(),
        "{} scalar-comment mismatches; see .verification/scalar-comment-failures.json",
        failures.len()
    );
}
