use beans::domain::{
    handoff::HandoffDocument,
    issue::{IssueDocument, Timestamp},
    log::LogEntry,
    memory::MemoryDocument,
    request::RequestDocument,
};
use serde_json::Value;
fn bytes(v: &Value) -> Vec<u8> {
    serde_json::from_value(v.clone()).unwrap()
}
#[test]
fn original_body_bytes_and_mutations_match_fixed_go() {
    let corpus: Value = serde_json::from_str(include_str!("../contract/raw-codec.json")).unwrap();
    let mut candidates = Vec::new();
    for c in corpus["cases"].as_array().unwrap() {
        let raw = bytes(&c["input"]);
        let path = c["path"].as_str().unwrap();
        for (mutation, e) in c["outputs"].as_object().unwrap() {
            let entry = LogEntry {
                at: Timestamp {
                    seconds: 1769904000,
                    nanoseconds: 0,
                    offset_seconds: 0,
                },
                actor: "actor".into(),
                event: "event".into(),
                ..Default::default()
            };
            let result = match c["kind"].as_str().unwrap() {
                "issue" => IssueDocument::parse_bytes(path, &raw).and_then(|mut d| {
                    assert_eq!(d.original_bytes(), raw);
                    match mutation.as_str() {
                        "metadata" => d.metadata.title = "Changed".into(),
                        "body" => d.set_description("replacement\nline"),
                        "body_raw" => d.set_description_bytes(&bytes(&c["replacement"])),
                        "append" => d.append_log(entry),
                        _ => (),
                    };
                    d.encode()
                }),
                "request" => RequestDocument::parse_bytes(path, &raw).and_then(|mut d| {
                    assert_eq!(d.original_bytes(), raw);
                    match mutation.as_str() {
                        "metadata" => d.metadata.title = "Changed".into(),
                        "body" => d.set_body("replacement\nline"),
                        "body_raw" => d.set_body_bytes(&bytes(&c["replacement"])),
                        "append" => d.append_log(entry),
                        _ => (),
                    };
                    d.encode()
                }),
                "memory" => MemoryDocument::parse_bytes(path, &raw).and_then(|mut d| {
                    assert_eq!(d.original_bytes(), raw);
                    match mutation.as_str() {
                        "metadata" => d.metadata.kind = "feedback".into(),
                        "body" => d.body = "replacement\nline\n".into(),
                        "body_raw" => {
                            d.body = beans::domain::plan::YamlString::from_bytes(bytes(
                                &c["replacement"],
                            ))
                        }
                        _ => (),
                    };
                    d.encode()
                }),
                "handoff" => HandoffDocument::parse_bytes(path, &raw).and_then(|mut d| {
                    assert_eq!(d.original_bytes(), raw);
                    match mutation.as_str() {
                        "metadata" => d.metadata.title = "Changed".into(),
                        "body" => d.body = "replacement\nline\n".into(),
                        "body_raw" => {
                            d.body = beans::domain::plan::YamlString::from_bytes(bytes(
                                &c["replacement"],
                            ))
                        }
                        _ => (),
                    };
                    d.encode()
                }),
                _ => unreachable!(),
            };
            assert_eq!(
                result
                    .as_ref()
                    .err()
                    .map(ToString::to_string)
                    .unwrap_or_default(),
                e["error"].as_str().unwrap(),
                "{} {mutation}",
                c["name"]
            );
            if let Ok(output) = result {
                candidates.push(serde_json::json!({"name":format!("{}-{mutation}",c["name"].as_str().unwrap()),"kind":c["kind"],"path":path,"bytes":output.bytes}));
                assert_eq!(output.bytes, bytes(&e["bytes"]), "{} {mutation}", c["name"]);
                for copy in output.copies {
                    assert_eq!(output.bytes[copy.destination], raw[copy.source]);
                }
            }
        }
    }
    if let Some(path) = std::env::var_os("BN_RUST_RAW_CODEC_OUTPUT") {
        std::fs::write(path, serde_json::to_vec(&candidates).unwrap()).unwrap();
    }
}

fn input(kind: &str, body: &[u8]) -> (String, Vec<u8>) {
    let (path, fields) = match kind {
        "issue" => (
            "projects/p/issues/p-one.md",
            "id: p-one\ntitle: Title\ntype: task\nstatus: open\npriority: 2\n",
        ),
        "request" => (
            "projects/p/requests/p-r-one.md",
            "id: p-r-one\naliases: [p-r-one]\ntitle: Title\nstatus: open\npriority: 2\n",
        ),
        "memory" => ("projects/p/memories/key.md", "key: key\ntype: reference\n"),
        "handoff" => (
            "projects/p/handoffs/p-one-context.md",
            "id: p-one\ntitle: Title\n",
        ),
        _ => unreachable!(),
    };
    let mut source=format!("---\n{fields}created: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\nunknown: keep # unchanged\n---\n").into_bytes();
    source.extend_from_slice(body);
    (path.into(), source)
}
proptest::proptest! {
    #[test]
    fn generated_raw_noop_and_metadata_edit_preserve_body(body in proptest::collection::vec(proptest::prelude::any::<u8>(),0..1024)) {
        proptest::prop_assume!(!body.windows(2).any(|p|p==b"\r\n"));
        for kind in ["issue","request","memory","handoff"] {
            let (path,source)=input(kind,&body);
            let (noop,edited)=match kind {
                "issue"=>{let mut d=IssueDocument::parse_bytes(&path,&source).unwrap();let noop=d.encode().unwrap();d.metadata.title="Changed".into();(noop,d.encode().unwrap())},
                "request"=>{let mut d=RequestDocument::parse_bytes(&path,&source).unwrap();let noop=d.encode().unwrap();d.metadata.title="Changed".into();(noop,d.encode().unwrap())},
                "memory"=>{let mut d=MemoryDocument::parse_bytes(&path,&source).unwrap();let noop=d.encode().unwrap();d.metadata.kind="feedback".into();(noop,d.encode().unwrap())},
                "handoff"=>{let mut d=HandoffDocument::parse_bytes(&path,&source).unwrap();let noop=d.encode().unwrap();d.metadata.title="Changed".into();(noop,d.encode().unwrap())},_=>unreachable!()
            };
            proptest::prop_assert_eq!(noop.bytes.as_slice(),source.as_slice());
            proptest::prop_assert!(edited.bytes.ends_with(&body));
            proptest::prop_assert!(edited.bytes.windows(b"unknown: keep # unchanged\n".len()).any(|v|v==b"unknown: keep # unchanged\n"));
            for result in [noop,edited] {for copy in result.copies {proptest::prop_assert_eq!(&result.bytes[copy.destination],&source[copy.source]);}}
        }
    }
}
#[test]
fn disk_index_payloads_are_lossless_mutation_sources_for_raw_bodies() {
    let hub = super::index::hub("raw-codec-index", false);
    let body = b"desc\xff\n## Body\nbody\xe2\x82\n## Log\n- opaque\xff\n\n## Tail\ntail\xff\n";
    for kind in ["issue", "request", "memory", "handoff"] {
        let (path, source) = input(kind, body);
        let path = hub.0.join(path);
        std::fs::create_dir_all(path.parent().unwrap()).unwrap();
        std::fs::write(path, source).unwrap();
    }
    let ix = beans::vault::Index::load(&hub.0).unwrap();
    assert_eq!(ix.ordered_notes().len(), 4);
    assert!(ix.graph.warnings().is_empty());
    for note in ix.ordered_notes() {
        let output = match &note.data {
            beans::vault::NoteData::Issue(d) => d.encode().unwrap(),
            beans::vault::NoteData::Request(d) => d.clone().encode().unwrap(),
            beans::vault::NoteData::Memory(d) => d.encode().unwrap(),
            beans::vault::NoteData::Handoff(d) => d.encode().unwrap(),
            _ => unreachable!(),
        };
        assert_eq!(output.bytes, note.source);
    }
    let (path, raw) = input("memory", b"raw\xff\n");
    let mut d = MemoryDocument::parse_bytes(&path, &raw).unwrap();
    d.body = "raw�\n".into();
    let out = d.encode().unwrap();
    assert_ne!(out.bytes, raw);
    assert!(out.bytes.ends_with("raw�\n".as_bytes()));
}
