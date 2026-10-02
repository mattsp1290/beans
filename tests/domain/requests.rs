use beans::domain::request;

// Independent port of issue/request_test.go at the immutable Go baseline.
#[test]
fn ids_and_transitions() {
    assert!(request::valid_id("beans-r-a3f2"));
    assert!(!request::valid_id("beans-a3f2"));
    assert!(!request::valid_id("beans-r-"));
    let generated = request::new_id("beans", Some(&mut |_| false), 7);
    assert_eq!(generated.len(), "beans-r-".len() + 7);
    assert!(request::valid_id(&generated));

    let statuses = ["open", "accepted", "in_progress", "resolved", "declined"];
    for from in statuses {
        for to in statuses {
            let allowed = from == to
                || from == "open" && (to == "accepted" || to == "declined")
                || from == "accepted" && (to == "in_progress" || to == "declined")
                || from == "in_progress" && (to == "resolved" || to == "declined");
            assert_eq!(
                request::validate_transition(from, to).is_ok(),
                allowed,
                "{from} -> {to}"
            );
        }
    }
}

#[test]
fn lifecycle_and_namespace_match_committed_contract() {
    let corpus: serde_json::Value =
        serde_json::from_str(include_str!("../contract/request-lifecycle.json")).unwrap();
    for case in corpus["ids"].as_array().unwrap() {
        let id = case["id"].as_str().unwrap();
        assert_eq!(
            request::valid_id(id),
            case["valid"].as_bool().unwrap(),
            "{id:?}"
        );
    }
    for case in corpus["transitions"].as_array().unwrap() {
        let from = case["from"].as_str().unwrap();
        let to = case["to"].as_str().unwrap();
        assert_eq!(
            request::valid_status(from),
            case["valid_from"].as_bool().unwrap()
        );
        assert_eq!(
            request::valid_status(to),
            case["valid_to"].as_bool().unwrap()
        );
        let error = request::validate_transition(from, to)
            .err()
            .map(|error| error.to_string())
            .unwrap_or_default();
        assert_eq!(error, case["error"].as_str().unwrap(), "{from:?} -> {to:?}");
    }
    for case in corpus["generation"].as_array().unwrap() {
        let prefix = case["prefix"].as_str().unwrap();
        let mut attempts = Vec::new();
        let collisions = case["collisions"].as_u64().unwrap() as usize;
        let mut exists = |id: &str| {
            assert!(id.starts_with(&format!("{prefix}-r-")));
            attempts.push(id.len() - prefix.len() - 3);
            attempts.len() <= collisions
        };
        let id = request::new_id(
            prefix,
            Some(&mut exists),
            case["length"].as_i64().unwrap() as isize,
        );
        assert_eq!(serde_json::to_value(attempts).unwrap(), case["attempts"]);
        assert_eq!(request::valid_id(&id), case["valid"].as_bool().unwrap());
    }
}
