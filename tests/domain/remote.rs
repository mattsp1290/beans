use beans::{
    domain::frontmatter::Error,
    vault::{normalize_remote_url, remote_host, validate_remote_url},
};
use serde_json::Value;
fn assert_result(actual: Result<Vec<u8>, Error>, case: &Value, value: &str, error: &str) {
    let expected = case[error].as_str().unwrap();
    if expected.is_empty() {
        let bytes: Vec<u8> = serde_json::from_value(case[value].clone()).unwrap();
        assert_eq!(actual.unwrap(), bytes, "{case}");
    } else {
        assert_eq!(actual.unwrap_err().to_string(), expected, "{case}");
    }
}
#[test]
fn remote_validation_normalization_host_and_second_pass_match_committed_contract() {
    let corpus: Value = serde_json::from_str(include_str!("../contract/remote.json")).unwrap();
    for case in corpus["cases"].as_array().unwrap() {
        let input: Vec<u8> = serde_json::from_value(case["Input"].clone()).unwrap();
        let error = case["ValidateError"].as_str().unwrap();
        let actual = validate_remote_url(&input);
        if error.is_empty() {
            actual.unwrap();
        } else {
            assert_eq!(actual.unwrap_err().to_string(), error, "{case}");
        }
        assert_result(
            normalize_remote_url(&input),
            case,
            "Normalized",
            "NormalizeError",
        );
        match remote_host(&input) {
            Ok(Some(host)) => {
                assert!(case["HostOK"].as_bool().unwrap(), "{case}");
                assert_result(Ok(host), case, "Host", "HostError");
            }
            Ok(None) => {
                assert!(!case["HostOK"].as_bool().unwrap(), "{case}");
                assert_eq!(case["HostError"], "");
            }
            Err(e) => assert_eq!(e.to_string(), case["HostError"].as_str().unwrap(), "{case}"),
        }
        if case["NormalizeError"] == "" {
            let first: Vec<u8> = serde_json::from_value(case["Normalized"].clone()).unwrap();
            assert_result(normalize_remote_url(&first), case, "Second", "SecondError");
        }
    }
}

#[test]
fn original_remote_transport_equivalence_and_idempotence() {
    let inputs = [
        "git@github.com:alice/app.git",
        "ssh://git@github.com/alice/app.git",
        "https://github.com/alice/app.git",
        "https://github.com/alice/app.git/",
        "file:///tmp/repo.git",
        "/tmp/repo.git",
        "ssh://git@git.corp.example.com:2222/alice/app.git",
    ];
    for (i, input) in inputs.iter().enumerate() {
        let first = normalize_remote_url(input.as_bytes()).unwrap();
        if i < 3 {
            assert_eq!(first, b"https://github.com/alice/app");
        }
        assert_eq!(normalize_remote_url(&first).unwrap(), first);
    }
    assert_eq!(
        normalize_remote_url(b"").unwrap_err().to_string(),
        beans::vault::NO_REMOTE
    );
}
