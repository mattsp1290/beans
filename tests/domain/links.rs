use beans::markdown::{Link, links};
use serde_json::{Value, json};
fn value(link: &Link) -> Value {
    json!({"Target":link.target,"Fragment":link.fragment,"Alias":link.alias,"Embed":link.embed})
}
#[test]
fn markdown_links_match_committed_contract_fields_context_and_deduplication() {
    let corpus: Value = serde_json::from_str(include_str!("../contract/links.json")).unwrap();
    let mut mismatches = Vec::new();
    for (i, case) in corpus["cases"].as_array().unwrap().iter().enumerate() {
        let input: Vec<u8> = serde_json::from_value(case["Input"].clone()).unwrap();
        let actual = Value::Array(links(&input).iter().map(value).collect());
        let expected = if case["Links"].is_null() {
            json!([])
        } else {
            case["Links"].clone()
        };
        if actual != expected {
            if mismatches.len() < 25 {
                eprintln!(
                    "case{i} {:?}\nactual{actual}\nexpected{expected}",
                    String::from_utf8_lossy(&input)
                );
            }
            mismatches.push(i);
        }
    }
    assert!(
        mismatches.is_empty(),
        "{} mismatches: {mismatches:?}",
        mismatches.len()
    );
}
#[test]
fn links_original_code_and_duplicate_regression() {
    let source=b"See [[note]] and [[note]] again.\n\nEmbed: ![[note]].\n\nHeading link: [[note#Section]].\n\nInline code with `[[not a link]]` should not count.\n\n```\n[[also not a link]]\n```\n";
    assert_eq!(
        links(source),
        vec![
            Link {
                target: b"note".to_vec(),
                fragment: vec![],
                alias: vec![],
                embed: false
            },
            Link {
                target: b"note".to_vec(),
                fragment: vec![],
                alias: vec![],
                embed: true
            },
            Link {
                target: b"note".to_vec(),
                fragment: b"Section".to_vec(),
                alias: vec![],
                embed: false
            }
        ]
    );
}
#[test]
fn links_original_alias_regression() {
    assert_eq!(
        links(b"[[note|Custom Label]]"),
        vec![Link {
            target: b"note".to_vec(),
            fragment: vec![],
            alias: b"Custom Label".to_vec(),
            embed: false
        }]
    );
}
