use serde_json::Value;

#[test]
fn encode_new_with_extra_and_raw_log() {
    use beans::domain::{
        authored_yaml::Node,
        issue::{IssueDocument, IssueMetadata, Timestamp},
        log::LogEntry,
    };
    // Independent port of issue/coverage_test.go's named regression.
    let at = Timestamp {
        seconds: 1_767_225_600,
        nanoseconds: 0,
        offset_seconds: 0,
    };
    let mut issue = IssueDocument::new(IssueMetadata {
        id: "p-ab12".into(),
        title: "T".into(),
        kind: "task".into(),
        status: "open".into(),
        priority: 1,
        created: at.clone(),
        updated: at,
        ..IssueMetadata::default()
    });
    issue.description = "d\n".into();
    issue.new_extra = Node::mapping([(Node::string("custom"), Node::string("value"))]);
    issue.log = vec![LogEntry {
        raw: "- a hand-written log line".into(),
        ..LogEntry::default()
    }];
    let encoded = String::from_utf8(issue.encode().unwrap().bytes).unwrap();
    assert!(encoded.contains("custom: value"));
    assert!(encoded.contains("- a hand-written log line"));
}

#[test]
fn parsed_unknown_fields_keep_source_bytes_when_authored_extra_changes() {
    use beans::domain::{authored_yaml::Node, issue::IssueDocument};
    let corpus: Value =
        serde_json::from_str(include_str!("../contract/frontmatter-primitives.json")).unwrap();
    for case in corpus["cases"].as_array().unwrap() {
        let input = case["input"].as_str().unwrap();
        let Ok(mut issue) = IssueDocument::parse(case["path"].as_str().unwrap(), input) else {
            continue;
        };
        let before = issue.encode().unwrap().bytes;
        let snapshot: Vec<_> = issue.unknown_fields().cloned().collect();
        issue.new_extra = Node::mapping([(Node::string("custom"), Node::string("replacement"))]);
        assert_eq!(issue.encode().unwrap().bytes, before, "{}", case["name"]);
        assert_eq!(
            issue.unknown_fields().cloned().collect::<Vec<_>>(),
            snapshot
        );
        for field in issue.unknown_fields() {
            assert!(!input[field.start..field.end].is_empty());
        }
    }
}

#[test]
fn new_issue_files_match_go_encoding_and_reader_semantics() {
    use beans::domain::{
        issue::{IssueDocument, IssueMetadata},
        log::LogEntry,
    };
    let corpus: Value =
        serde_json::from_str(include_str!("../contract/frontmatter-primitives.json")).unwrap();
    let mut candidates = Vec::new();
    for case in corpus["new_issues"].as_array().unwrap() {
        let mut issue = IssueDocument::new(
            serde_json::from_value::<IssueMetadata>(case["metadata"].clone()).unwrap(),
        );
        issue.description = case["description"].as_str().unwrap().into();
        issue.body = case["body"].as_str().unwrap().into();
        issue.log = serde_json::from_value::<Vec<LogEntry>>(case["log"].clone()).unwrap();
        issue.new_extra = serde_json::from_value(case["new_extra"].clone()).unwrap();
        if let Some(error) = case.get("encode_error") {
            assert_eq!(
                issue.encode().unwrap_err().to_string(),
                error.as_str().unwrap(),
                "{}",
                case["name"]
            );
            continue;
        }
        let encoded = issue.encode().unwrap();
        assert!(encoded.copies.is_empty());
        assert_eq!(
            String::from_utf8(encoded.bytes.clone()).unwrap(),
            case["encoded"].as_str().unwrap(),
            "{}",
            case["name"]
        );
        let output = String::from_utf8(encoded.bytes).unwrap();
        candidates
            .push(serde_json::json!({"name":case["name"],"path":case["path"],"input":output}));
        let parsed = IssueDocument::parse(case["path"].as_str().unwrap(), &output);
        if let Some(error) = case.get("read_error") {
            assert_eq!(
                parsed.unwrap_err().to_string(),
                error.as_str().unwrap(),
                "{}",
                case["name"]
            );
        } else {
            let parsed = parsed.unwrap_or_else(|error| panic!("{}: {error}", case["name"]));
            assert_eq!(
                serde_json::to_value(&parsed.metadata).unwrap(),
                case["read_metadata"],
                "{}",
                case["name"]
            );
            assert_eq!(
                parsed.description.as_bytes(),
                case["read_description"].as_str().unwrap().as_bytes()
            );
            assert_eq!(
                parsed.body.as_bytes(),
                case["read_body"].as_str().unwrap().as_bytes()
            );
            assert_eq!(serde_json::to_value(&parsed.log).unwrap(), case["read_log"]);
            let unknown: Vec<_> = parsed
                .unknown_fields()
                .map(|field| serde_json::json!({"key":field.key,"value":field.value}))
                .collect();
            assert_eq!(
                serde_json::to_value(unknown).unwrap(),
                case["read_extra"],
                "{}",
                case["name"]
            );
        }
    }
    if let Some(path) = std::env::var_os("BN_RUST_ISSUE_EXPORT") {
        std::fs::write(path, serde_json::to_vec_pretty(&candidates).unwrap()).unwrap();
    }
}
