use beans::domain::id::{DEFAULT_ID_LENGTH, SLUG_MAX_LEN, filename, new_id, slug, valid_id};
use serde_json::Value;

// Independent ports of every issue/id_test.go regression.
#[test]
fn slug_examples() {
    for (title, expected) in [
        ("Héllo Wörld", "h-llo-w-rld"),
        ("Hello, World!!!", "hello-world"),
        ("simple-title", "simple-title"),
        ("---Trim Me---", "trim-me"),
        ("!!! --- ???", ""),
        ("Release 1.2.3", "release-1-2-3"),
    ] {
        assert_eq!(slug(title), expected);
    }
}

#[test]
fn slug_long_title_cuts_at_word_boundary() {
    let title = "this is a very long issue title that keeps going and going and going and going past the limit for sure";
    let got = slug(title);
    assert!(got.len() <= SLUG_MAX_LEN && !got.ends_with('-') && !got.is_empty());
    let full = title.replace(' ', "-");
    assert!(full.starts_with(&got));
    assert_eq!(full.as_bytes()[got.len()], b'-');
}

#[test]
fn slug_empty_filename_fallback() {
    let got = slug("!!! --- ???");
    assert!(got.is_empty());
    assert_eq!(filename("proj-a1b2", &got), "proj-a1b2.md");
}

#[test]
fn filenames() {
    assert_eq!(filename("proj-a1b2", "my-title"), "proj-a1b2-my-title.md");
    assert_eq!(filename("proj-a1b2", ""), "proj-a1b2.md");
}

#[test]
fn valid_ids() {
    for id in ["beans-a3f2", "bean-counter-x1", "beans-ceh.15"] {
        assert!(valid_id(id));
    }
    for id in ["Beans-A3", "beans", "-x", "a-"] {
        assert!(!valid_id(id));
    }
}

#[test]
fn new_id_eight_collisions() {
    let mut calls = 0;
    let mut exists = |_: &str| {
        calls += 1;
        calls <= 8
    };
    let id = new_id("beans", Some(&mut exists), 4);
    assert_eq!(calls, 9);
    assert_eq!(id.len(), "beans".len() + 1 + 5);
    assert!(valid_id(&id));
    assert!(id.starts_with("beans-"));
}

#[test]
fn new_id_all_generated_are_valid() {
    for _ in 0..50 {
        assert!(valid_id(&new_id("proj", None, 4)));
    }
}

#[test]
fn new_id_default_length() {
    assert_eq!(
        new_id("proj", None, 0).len(),
        "proj".len() + 1 + DEFAULT_ID_LENGTH
    );
}

#[test]
fn names_and_entire_unicode_ascii_lowercase_set_match_committed_contract() {
    let corpus: Value =
        serde_json::from_str(include_str!("../fixtures/expected/ids.json")).unwrap();
    for case in corpus["slugs"].as_array().unwrap() {
        assert_eq!(
            slug(case["title"].as_str().unwrap()),
            case["slug"].as_str().unwrap(),
            "{case}"
        );
    }
    for case in corpus["ids"].as_array().unwrap() {
        assert_eq!(
            valid_id(case["id"].as_str().unwrap()),
            case["valid"].as_bool().unwrap(),
            "{case}"
        );
    }
    for case in corpus["filenames"].as_array().unwrap() {
        assert_eq!(
            filename(case["id"].as_str().unwrap(), case["slug"].as_str().unwrap()),
            case["filename"].as_str().unwrap(),
            "{case}"
        );
    }
    for case in corpus["generation"].as_array().unwrap() {
        let prefix = case["prefix"].as_str().unwrap();
        let mut attempts = Vec::new();
        let mut exists = |id: &str| {
            assert!(id.starts_with(&format!("{prefix}-")));
            attempts.push(id.len() - prefix.len() - 1);
            attempts.len() <= case["collisions"].as_u64().unwrap() as usize
        };
        let generated = new_id(
            prefix,
            Some(&mut exists),
            case["length"].as_i64().unwrap() as isize,
        );
        assert_eq!(
            serde_json::to_value(attempts).unwrap(),
            case["attempts"],
            "{case}"
        );
        assert_eq!(
            valid_id(&generated),
            case["valid"].as_bool().unwrap(),
            "{case}"
        );
    }
    let mut mappings = Vec::new();
    for value in 0..=0x10ffff {
        let Some(character) = char::from_u32(value) else {
            continue;
        };
        let rendered = slug(&character.to_string());
        if !rendered.is_empty() {
            assert_eq!(rendered.len(), 1, "U+{value:04X}");
            mappings
                .push(serde_json::json!({"rune":value,"lower":u32::from(rendered.as_bytes()[0])}));
        }
    }
    assert_eq!(Value::Array(mappings), corpus["ascii_lowercase"]);
}
