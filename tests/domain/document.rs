use beans::vault::{YamlValue, doc_metadata, split_doc_frontmatter};
use serde_json::{Value, json};
fn value(v: &YamlValue) -> Value {
    match v {
        YamlValue::Null => json!({"kind":"null"}),
        YamlValue::String(bytes) => json!({"kind":"string", "bytes":bytes}),
        YamlValue::Bool(v) => json!({"kind":"bool", "value":v}),
        YamlValue::Int(v) => json!({"kind":"int", "value":v}),
        YamlValue::Uint(v) => json!({"kind":"uint", "value":v}),
        YamlValue::Float(v) => json!({"kind":"float", "bits":format!("{:016x}", v.to_bits())}),
        YamlValue::Timestamp(v) => {
            json!({"kind":"timestamp", "seconds":v.seconds, "nanoseconds":v.nanoseconds, "offset":v.offset_seconds})
        }
        YamlValue::Sequence(items) => {
            json!({"kind":"sequence", "items":items.iter().map(value).collect::<Vec<_>>()})
        }
        YamlValue::StringMap(map) => {
            json!({"kind":"string_map", "entries":map.iter().map(|(key,v)| json!({"key":{"kind":"string", "bytes":key},"value":value(v)})).collect::<Vec<_>>()})
        }
        YamlValue::Map(map) => {
            let mut entries: Vec<_> = map
                .iter()
                .map(|(key, v)| {
                    (
                        value(key).to_string(),
                        json!({"key":value(key), "value":value(v)}),
                    )
                })
                .collect();
            entries.sort_by(|a, b| a.0.cmp(&b.0));
            json!({"kind":"map", "entries":entries.into_iter().map(|(_,v)| v).collect::<Vec<_>>()})
        }
    }
}
#[test]
fn doc_metadata_matches_committed_contract_forgiving_frontmatter_and_generic_values() {
    let corpus: Value = serde_json::from_str(include_str!("../contract/doc.json")).unwrap();
    let mut failures = Vec::new();
    for (i, case) in corpus["cases"].as_array().unwrap().iter().enumerate() {
        let input: Vec<u8> = serde_json::from_value(case["input"].clone()).unwrap();
        let (fm, _) = split_doc_frontmatter(&input);
        let doc = doc_metadata(b"fallback", &input);
        let actual = json!({"frontmatter_bytes":fm, "body":doc.body, "title":doc.title,"tags":doc.tags,"metadata":doc.frontmatter.map(|m| value(&YamlValue::StringMap(m)))});
        let expected = json!({"frontmatter_bytes":case["frontmatter_bytes"],"body":case["body"],"title":case["title"],"tags":case["tags"],"metadata":case["metadata"]});
        if actual != expected {
            if failures.len() < 20 {
                eprintln!(
                    "case{i} {:?}\nactual{actual}\nexpected{expected}",
                    String::from_utf8_lossy(&input)
                );
            }
            failures.push(i);
        }
    }
    assert!(
        failures.is_empty(),
        "{} mismatches: {failures:?}",
        failures.len()
    );
}

#[test]
fn doc_titles_tags_and_partial_errors_retain_stored_format_rules() {
    let source = b"---\ntitle: 42\ntags: [one, null, 2, false, '']\n---\n```\n# Heading in code\n```\nraw\xff";
    let doc = doc_metadata(b"fallback", source);
    assert_eq!(doc.title, b"Heading in code");
    assert_eq!(doc.tags, Some(vec![b"one".to_vec(), vec![]]));
    assert_eq!(doc.body, b"```\n# Heading in code\n```\nraw\xff");
    assert_eq!(
        doc.frontmatter.as_ref().unwrap().get(b"title".as_slice()),
        Some(&YamlValue::Int(42))
    );

    let doc = doc_metadata(
        b"fallback",
        b"---\ntitle: Earlier\nextra: !!binary '!!!'\ntags: Later\n---\nbody",
    );
    assert_eq!(doc.title, b"Earlier");
    assert_eq!(doc.tags, None);
    assert_eq!(doc.frontmatter.unwrap().len(), 1);

    assert_eq!(
        doc_metadata(b"fallback", b"---\ntags: []\n---\n").tags,
        Some(vec![])
    );
    assert_eq!(
        doc_metadata(b"fallback", b"---\ntags: ''\n---\n").tags,
        None
    );
    let source = b"---\r\ntitle: Other\r\n---\r\nbody";
    assert_eq!(split_doc_frontmatter(source), (None, source.as_slice()));
}
