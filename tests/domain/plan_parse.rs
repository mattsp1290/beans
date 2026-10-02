use beans::domain::plan::parse;
use serde_json::Value;
#[test]
fn manifest_parsing_matches_committed_contract_models_and_errors() {
    let fixture: Value =
        serde_json::from_str(include_str!("../fixtures/expected/plan-parse.json")).unwrap();
    let mut mismatches = Vec::new();
    for row in fixture["parses"].as_array().unwrap() {
        let input: Vec<u8> = if row.get("input_bytes").is_some() {
            serde_json::from_value(row["input_bytes"].clone()).unwrap()
        } else {
            row["input"].as_str().unwrap().as_bytes().into()
        };
        if let Some(reason) = super::diagnostics::encoding_error(&input) {
            assert_eq!(
                parse("plan.md", &input).unwrap_err().to_string(),
                format!("plan.md: frontmatter: {reason}"),
                "{}",
                row["name"]
            );
            continue;
        }
        let (plan, error) = match parse("plan.md", &input) {
            Ok(p) => {
                let (encoded, error) = match beans::domain::plan::encode(Some(&p)) {
                    Ok(bytes) => (String::from_utf8(bytes).unwrap(), String::new()),
                    Err(e) => (String::new(), e.to_string()),
                };
                if encoded != row["encoded"] || error != row["encode_error"] {
                    mismatches.push(format!(
                        "{}: encoded {encoded:?} {error:?}; expected {} {}",
                        row["name"], row["encoded"], row["encode_error"]
                    ));
                }
                (serde_json::to_value(p).unwrap(), String::new())
            }
            Err(e) => (Value::Null, e.to_string()),
        };
        let mut expected = row["plan"].clone();
        if !expected.is_null() {
            for key in ["aliases", "sections"] {
                if expected[key].is_null() {
                    expected[key] = serde_json::json!([]);
                }
            }
            expected["section_bodies"] = serde_json::json!([]);
        }
        if plan != expected
            || super::diagnostics::text(&error)
                != super::diagnostics::text(row["error"].as_str().unwrap())
        {
            mismatches.push(format!(
                "{}: {error:?} {plan}; expected {} {}",
                row["name"], row["error"], expected
            ));
        }
    }
    assert!(mismatches.is_empty(), "{}", mismatches.join("\n"));
}

#[test]
fn scaffold_bytes_match_committed_contract_template_and_encoder() {
    let fixture: Value =
        serde_json::from_str(include_str!("../fixtures/expected/plan-parse.json")).unwrap();
    for row in fixture["scaffolds"].as_array().unwrap() {
        let now = serde_json::from_value(row["now"].clone()).unwrap();
        let output = beans::domain::plan::scaffold(
            row["id"].as_str().unwrap(),
            row["title"].as_str().unwrap(),
            &now,
        )
        .unwrap();
        assert_eq!(String::from_utf8(output).unwrap(), row["output"]);
    }
}

#[test]
fn parse_rejects_unknown_frontmatter() {
    // Original plan/plan_test.go TestParseRejectsUnknownFrontmatter.
    let now = beans::domain::issue::Timestamp {
        seconds: 1789128000,
        nanoseconds: 0,
        offset_seconds: 0,
    };
    let data = beans::domain::plan::scaffold("beans-plan-a3f2", "x", &now).unwrap();
    let data = String::from_utf8(data)
        .unwrap()
        .replacen("title:", "provenance: agent\ntitle:", 1);
    assert!(parse("plan.md", data.as_bytes()).is_err());
}
