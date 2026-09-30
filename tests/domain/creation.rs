use serde_json::Value;

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
        issue.description = case["description"].as_str().unwrap().to_owned();
        issue.body = case["body"].as_str().unwrap().to_owned();
        issue.log = serde_json::from_value::<Vec<LogEntry>>(case["log"].clone()).unwrap();
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
            let parsed = parsed.unwrap();
            assert_eq!(
                serde_json::to_value(&parsed.metadata).unwrap(),
                case["read_metadata"],
                "{}",
                case["name"]
            );
            assert_eq!(
                parsed.description,
                case["read_description"].as_str().unwrap()
            );
            assert_eq!(parsed.body, case["read_body"].as_str().unwrap());
            assert_eq!(serde_json::to_value(&parsed.log).unwrap(), case["read_log"]);
        }
    }
    if let Some(path) = std::env::var_os("BN_RUST_ISSUE_EXPORT") {
        std::fs::write(path, serde_json::to_vec_pretty(&candidates).unwrap()).unwrap();
    }
}
