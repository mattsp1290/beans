use beans::domain::plan::{Plan, encode};
use serde_json::Value;

#[test]
fn plan_encoding_matches_committed_contract_bytes_and_errors() {
    let fixture: Value =
        serde_json::from_str(include_str!("../contract/plan-encode.json")).unwrap();
    let mut mismatches = Vec::new();
    for row in fixture["encodes"].as_array().unwrap() {
        let mut value = row["plan"].clone();
        if !value.is_null() {
            for key in ["aliases", "sections"] {
                if value[key].is_null() {
                    value[key] = serde_json::json!([]);
                }
            }
        }
        let p: Option<Plan> = serde_json::from_value(value).unwrap();
        let (output, error) = match encode(p.as_ref()) {
            Ok(x) => (String::from_utf8(x).unwrap(), String::new()),
            Err(e) => (String::new(), e.to_string()),
        };
        if error.is_empty() {}
        if output != row["output"] || error != row["error"] {
            mismatches.push(format!(
                "{}: {output:?} {error:?}; expected {} {}",
                row["name"], row["output"], row["error"]
            ));
        }
    }
    assert!(mismatches.is_empty(), "{}", mismatches.join("\n"));
}
