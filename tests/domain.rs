use beans::domain::frontmatter::Frontmatter;
use beans::domain::text::{Link, split_issue_body, split_request_body};
use proptest::prelude::*;
use serde_json::Value;

#[test]
fn frontmatter_nodes_and_byte_spans_match_fixed_go_for_every_roundtrip_fixture() {
    let corpus: Value = serde_json::from_str(include_str!("contract/frontmatter-primitives.json"))
        .expect("committed Go corpus");
    let cases = corpus["cases"].as_array().unwrap();
    assert_eq!(cases.len(), 68);
    for case in cases {
        let name = case["name"].as_str().unwrap();
        let input = case["input"].as_str().unwrap();
        let parsed = Frontmatter::parse(case["path"].as_str().unwrap(), input);
        if let Some(expected) = case.get("error") {
            assert_eq!(
                parsed.unwrap_err().to_string(),
                expected.as_str().unwrap(),
                "{name}"
            );
            continue;
        }
        let parsed = parsed.unwrap_or_else(|error| panic!("{name}: {error}"));
        assert_eq!(
            parsed.yaml_text(),
            case["yaml"].as_str().unwrap_or_default(),
            "{name}"
        );
        assert_eq!(
            parsed.body(),
            case["body"].as_str().unwrap_or_default(),
            "{name}"
        );
        assert_eq!(
            serde_json::to_value(split_issue_body(parsed.body())).unwrap(),
            case["issue_body"],
            "{name}"
        );
        assert_eq!(
            serde_json::to_value(split_request_body(parsed.body())).unwrap(),
            case["request_body"],
            "{name}"
        );
        assert_eq!(
            serde_json::to_value(parsed.fields()).unwrap(),
            case["fields"],
            "{name}"
        );
        assert_eq!(
            parsed.replace_fields(&[]).unwrap().bytes,
            input.as_bytes(),
            "{name}"
        );
    }
}

#[test]
fn link_parsing_and_creation_match_go_without_normalizing_bare_ids() {
    let corpus: Value =
        serde_json::from_str(include_str!("contract/frontmatter-primitives.json")).unwrap();
    for case in corpus["links"].as_array().unwrap() {
        let input = case["input"].as_str().unwrap();
        let parsed = Link::parse(input);
        assert_eq!(
            serde_json::to_value(&parsed).unwrap(),
            case["parsed"],
            "{input:?}"
        );
        assert_eq!(
            parsed.is_zero(),
            case["zero"].as_bool().unwrap(),
            "{input:?}"
        );
        assert_eq!(
            serde_json::to_value(Link::new(input)).unwrap(),
            case["new"],
            "{input:?}"
        );
    }
}

#[test]
fn production_edits_use_parsed_byte_ranges_and_verified_copy_geometry() {
    let source = "---\ntitle: 雪\n# before status\nstatus: open\n\n# keep this\nextra:\n  nested: [a, b]\n---\nBody: ☃\n\n## Log\n- opaque entry\n";
    let document = Frontmatter::parse("x.md", source).unwrap();
    let status = document
        .fields()
        .iter()
        .position(|field| field.key == "status")
        .unwrap();
    let edit = document
        .replace_fields(&[(status, b"status: in_progress\n")])
        .unwrap();
    let expected = source.replace("status: open\n", "status: in_progress\n");
    assert_eq!(edit.bytes, expected.as_bytes());
    assert_eq!(edit.copies.len(), 2);
    let parsed_range = &document.fields()[status];
    assert_eq!(edit.copies[0].source, 0..parsed_range.start);
    assert_eq!(edit.copies[1].source, parsed_range.end..source.len());
    for copy in &edit.copies {
        assert_eq!(copy.source.len(), copy.destination.len());
        assert_eq!(
            &source.as_bytes()[copy.source.clone()],
            &edit.bytes[copy.destination.clone()]
        );
    }
    assert_eq!(document.original(), source);
}

#[test]
fn invalid_edit_requests_leave_the_original_document_untouched() {
    let source = "---\nid: x\nstatus: open\n---\nBody\n";
    let parsed = Frontmatter::parse("x.md", source).unwrap();
    assert!(parsed.replace_fields(&[(usize::MAX, b"bad")]).is_err());
    assert!(parsed.replace_fields(&[(0, b"one"), (0, b"two")]).is_err());
    assert_eq!(parsed.original(), source);
}

#[test]
fn scalar_and_list_coercion_distinguish_null_and_aliases_like_go() {
    let source = "---\nnull: null\nquoted: 'null'\nlist: [null, ~, true, 12]\noriginal: &s value\nalias: *s\n---\n";
    let parsed = Frontmatter::parse("x.md", source).unwrap();
    let fields = parsed.fields();
    assert_eq!(fields[0].value.scalar("title").unwrap(), "");
    assert_eq!(fields[1].value.scalar("title").unwrap(), "null");
    assert_eq!(
        fields[2].value.string_list("labels").unwrap(),
        ["null", "~", "true", "12"]
    );
    assert_eq!(
        fields[4].value.scalar("title").unwrap_err().to_string(),
        "title must be a string"
    );
    assert_eq!(
        fields[4]
            .value
            .string_list("labels")
            .unwrap_err()
            .to_string(),
        "labels must be a list of strings"
    );
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(512))]

    #[test]
    fn parsed_byte_edits_preserve_all_unowned_text(
        title in prop::collection::vec(any::<char>().prop_filter("printable YAML scalar", |ch| !ch.is_control() && !matches!(ch, '\u{fffe}' | '\u{ffff}')), 0..40),
        body in prop::collection::vec(any::<char>(), 0..100),
        blank_lines in 0usize..5,
    ) {
        let title: String = title.into_iter().collect();
        let title = serde_json::to_string(&title).unwrap();
        let body: String = body.into_iter().collect();
        let source = format!("---\ntitle: {title}\n# preceding status\nstatus: open\n{}# untouched trailing comment\nextra: {{nested: [a, b]}}\n---\n{body}", "\n".repeat(blank_lines));
        // CRLF input is rejected by contract even when it occurs in the body.
        if source.contains("\r\n") {
            prop_assert!(Frontmatter::parse("x.md", &source).is_err());
            return Ok(());
        }
        let parsed = Frontmatter::parse("x.md", &source).unwrap();
        let index = parsed.fields().iter().position(|field| field.key == "status").unwrap();
        let output = parsed.replace_fields(&[(index, b"status: in_progress\n")]).unwrap();
        let expected = source.replacen("status: open\n", "status: in_progress\n", 1);
        prop_assert_eq!(&output.bytes, expected.as_bytes());
        for copy in output.copies {
            prop_assert_eq!(&source.as_bytes()[copy.source], &output.bytes[copy.destination]);
        }
        prop_assert_eq!(parsed.replace_fields(&[]).unwrap().bytes, source.as_bytes());
    }
}

#[test]
fn typed_issue_metadata_and_validation_match_fixed_go() {
    use beans::domain::issue::IssueDocument;
    let corpus: Value =
        serde_json::from_str(include_str!("contract/frontmatter-primitives.json")).unwrap();
    for case in corpus["cases"].as_array().unwrap() {
        let input = case["input"].as_str().unwrap();
        let result = IssueDocument::parse(case["path"].as_str().unwrap(), input);
        if let Some(error) = case.get("issue_error") {
            assert_eq!(
                result.unwrap_err().to_string(),
                error.as_str().unwrap(),
                "{}",
                case["name"]
            );
        } else {
            let parsed = result.unwrap();
            assert_eq!(
                serde_json::to_value(&parsed.metadata).unwrap(),
                case["metadata"],
                "{}",
                case["name"]
            );
            assert_eq!(parsed.original(), input);
            assert_eq!(
                serde_json::to_value(parsed.body()).unwrap(),
                case["issue_body"]
            );
        }
    }
}
