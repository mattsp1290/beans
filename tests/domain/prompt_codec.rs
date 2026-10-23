use beans::domain::{
    issue::Timestamp,
    log::LogEntry,
    prompt::{PromptDocument, PromptMetadata},
    template::default_prompt_template,
    text::Link,
};

const PATH: &str = "projects/beans/prompts/beans-p-a3f2-durable-prompt.md";
const SAMPLE: &str = "---\nid: beans-p-a3f2\naliases: [beans-p-a3f2]\ntitle: Durable prompt\nstatus: open\npriority: 2\nlabels: [agents]\nfrom: planner\nto: implementer\nissue: \"[[beans-a1b2]]\"\ncreated: 2026-09-11T15:51:49Z\nupdated: 2026-09-11T15:51:49Z\n---\n## Objective\n\nDo the thing.\n";

fn stamp(text: &str) -> Timestamp {
    let date =
        time::OffsetDateTime::parse(text, &time::format_description::well_known::Rfc3339).unwrap();
    Timestamp {
        seconds: date.unix_timestamp(),
        nanoseconds: date.nanosecond(),
        offset_seconds: date.offset().whole_seconds(),
    }
}

type PromptMutation = Box<dyn Fn(&mut PromptDocument)>;

fn metadata() -> PromptMetadata {
    PromptMetadata {
        id: "beans-p-a3f2".into(),
        title: "Durable prompt".into(),
        status: "open".into(),
        priority: 2,
        from: "planner".into(),
        to: "implementer".into(),
        created: stamp("2026-09-11T15:51:49Z"),
        updated: stamp("2026-09-11T15:51:49Z"),
        ..PromptMetadata::default()
    }
}

#[test]
fn prompt_codec_round_trips_without_rewriting() {
    let mut prompt = PromptDocument::parse(PATH, SAMPLE).unwrap();
    assert_eq!(prompt.encode().unwrap().bytes, SAMPLE.as_bytes());
    assert_eq!(prompt.original(), SAMPLE);
    assert_eq!(prompt.original_bytes(), SAMPLE.as_bytes());
    assert_eq!(prompt.body.as_bytes(), b"## Objective\n\nDo the thing.\n");
    assert!(prompt.log.is_empty());
}

#[test]
fn parses_all_fields_and_normalizes_identities() {
    let source = SAMPLE
        .replace("status: open", "status: in_progress")
        .replace("from: planner", "from: '  planner  '")
        .replace("to: implementer", "to: ' implementer '")
        .replace("issue:", "claimed_by: ' worker '\nissue:");
    let mut prompt = PromptDocument::parse(PATH, &source).unwrap();
    assert_eq!(prompt.metadata.id, "beans-p-a3f2");
    assert_eq!(prompt.metadata.aliases, ["beans-p-a3f2"]);
    assert_eq!(prompt.metadata.title, "Durable prompt");
    assert_eq!(prompt.metadata.status, "in_progress");
    assert_eq!(prompt.metadata.priority, 2);
    assert_eq!(prompt.metadata.labels, ["agents"]);
    assert_eq!(prompt.metadata.from, "planner");
    assert_eq!(prompt.metadata.to, "implementer");
    assert_eq!(prompt.metadata.claimed_by.as_deref(), Some("worker"));
    assert_eq!(prompt.metadata.issue.target, "beans-a1b2");
    assert_eq!(prompt.metadata.project, "beans");
    assert_eq!(prompt.encode().unwrap().bytes, source.as_bytes());
}

#[test]
fn rejects_malformed_missing_and_duplicate_owned_fields() {
    assert!(PromptDocument::parse("wrong.md", SAMPLE).is_err());
    assert!(PromptDocument::parse(PATH, &SAMPLE.replace('\n', "\r\n")).is_err());
    assert!(PromptDocument::parse(PATH, "not frontmatter").is_err());
    for key in [
        "id", "title", "status", "priority", "from", "to", "created", "updated",
    ] {
        let needle = format!("{key}: ");
        let source = SAMPLE
            .lines()
            .filter(|line| !line.starts_with(&needle))
            .collect::<Vec<_>>()
            .join("\n")
            + "\n";
        assert!(
            PromptDocument::parse(PATH, &source).is_err(),
            "missing {key}"
        );
    }
    for key in [
        "id", "aliases", "title", "status", "priority", "labels", "from", "to", "issue", "created",
        "updated",
    ] {
        let line = SAMPLE
            .lines()
            .find(|line| line.starts_with(&format!("{key}:")))
            .unwrap();
        let source = SAMPLE.replacen(line, &format!("{line}\n{line}"), 1);
        assert!(
            PromptDocument::parse(PATH, &source).is_err(),
            "duplicate {key}"
        );
    }
    let claimed = SAMPLE.replace("issue:", "claimed_by: worker\nissue:");
    let duplicate = claimed.replace(
        "claimed_by: worker",
        "claimed_by: worker\nclaimed_by: other",
    );
    assert!(PromptDocument::parse(PATH, &duplicate).is_err());
}

#[test]
fn rejects_invalid_metadata_and_lifecycle_combinations_at_decode() {
    let cases = [
        SAMPLE.replace("beans-p-a3f2", "beans-a3f2"),
        SAMPLE.replace("title: Durable prompt", "title: '   '"),
        SAMPLE.replace("status: open", "status: unknown"),
        SAMPLE.replace("priority: 2", "priority: 5"),
        SAMPLE.replace("labels: [agents]", "labels: [agents, agents]"),
        SAMPLE.replace("from: planner", "from: '  '"),
        SAMPLE.replace("to: implementer", "to: '  '"),
        SAMPLE.replace("status: open", "status: in_progress"),
        SAMPLE.replace("status: open", "status: completed"),
        SAMPLE.replace("issue:", "claimed_by: worker\nissue:"),
    ];
    for source in cases {
        assert!(PromptDocument::parse(PATH, &source).is_err(), "{source}");
    }
    for terminal in ["declined", "cancelled"] {
        let source = SAMPLE
            .replace("status: open", &format!("status: {terminal}"))
            .replace("issue:", "claimed_by: worker\nissue:");
        assert!(PromptDocument::parse(PATH, &source).is_ok(), "{terminal}");
    }
    let completed = SAMPLE
        .replace("status: open", "status: completed")
        .replace("issue:", "claimed_by: worker\nissue:");
    assert!(PromptDocument::parse(PATH, &completed).is_ok());
}

#[test]
fn rejects_invalid_mutated_state_at_encode_boundary() {
    let mutations: Vec<PromptMutation> = vec![
        Box::new(|p| p.metadata.id = "wrong".into()),
        Box::new(|p| p.metadata.title = " \t".into()),
        Box::new(|p| p.metadata.status = "unknown".into()),
        Box::new(|p| p.metadata.priority = -1),
        Box::new(|p| p.metadata.labels = vec!["same".into(), "same".into()]),
        Box::new(|p| p.metadata.from.clear()),
        Box::new(|p| p.metadata.to.clear()),
        Box::new(|p| p.metadata.claimed_by = Some("worker".into())),
        Box::new(|p| p.metadata.created = Timestamp::default()),
    ];
    for mutation in mutations {
        let mut prompt = PromptDocument::parse(PATH, SAMPLE).unwrap();
        mutation(&mut prompt);
        assert!(prompt.encode().is_err());
        assert_eq!(prompt.original(), SAMPLE);
    }
}

#[test]
fn every_owned_field_can_be_edited_without_rewriting_surroundings() {
    let source = SAMPLE
        .replace("aliases:", "# identity comment\naliases:")
        .replace(
            "created:",
            "unknown: {snow: [one, two]}\n# retain me\ncreated:",
        );
    let edits: Vec<(&str, PromptMutation)> = vec![
        (
            "id",
            Box::new(|p| {
                p.metadata.id = "beans-p-b4c5".into();
                p.metadata.aliases = vec!["beans-p-b4c5".into()];
            }),
        ),
        (
            "aliases",
            Box::new(|p| p.metadata.aliases.push("external".into())),
        ),
        ("title", Box::new(|p| p.metadata.title = "Snow 雪".into())),
        (
            "status",
            Box::new(|p| p.metadata.status = "declined".into()),
        ),
        ("priority", Box::new(|p| p.metadata.priority = 4)),
        (
            "labels",
            Box::new(|p| p.metadata.labels = vec!["snow 雪".into(), "true".into()]),
        ),
        ("from", Box::new(|p| p.metadata.from = "new planner".into())),
        ("to", Box::new(|p| p.metadata.to = "new worker".into())),
        (
            "claimed_by",
            Box::new(|p| {
                p.metadata.status = "in_progress".into();
                p.metadata.claimed_by = Some("worker".into());
            }),
        ),
        (
            "issue",
            Box::new(|p| p.metadata.issue = Link::new("beans-other")),
        ),
        (
            "created",
            Box::new(|p| p.metadata.created = stamp("2027-01-02T03:04:05Z")),
        ),
        (
            "updated",
            Box::new(|p| p.metadata.updated = stamp("2027-02-03T04:05:06Z")),
        ),
    ];
    for (name, edit) in edits {
        let mut prompt = PromptDocument::parse(PATH, &source).unwrap();
        edit(&mut prompt);
        let encoded = prompt.encode().unwrap();
        let output = String::from_utf8(encoded.bytes.clone()).unwrap();
        assert!(
            output.contains("unknown: {snow: [one, two]}\n# retain me\n"),
            "{name}"
        );
        assert!(output.contains("# identity comment\n"), "{name}");
        for copy in encoded.copies {
            assert_eq!(
                &source.as_bytes()[copy.source],
                &encoded.bytes[copy.destination],
                "{name}"
            );
        }
    }
}

#[test]
fn preserves_unknown_comments_body_log_and_opaque_tail() {
    let source = SAMPLE
        .replace("created:", "unknown: {nested: [a, b]}\n# trailing comment\ncreated:")
        .replace(
            "Do the thing.\n",
            "Do the thing.\n\n## Log\n- opaque, not a parsed log\n- 2026-09-11T15:51:49Z planner: created\n\n## Tail\nopaque tail bytes ☃\n",
        );
    let mut prompt = PromptDocument::parse(PATH, &source).unwrap();
    assert_eq!(
        prompt
            .unknown_fields()
            .map(|field| field.key.as_str())
            .collect::<Vec<_>>(),
        ["unknown"]
    );
    assert_eq!(prompt.body.as_bytes(), b"## Objective\n\nDo the thing.\n\n");
    assert_eq!(prompt.log.len(), 2);
    prompt.metadata.priority = 3;
    let output = String::from_utf8(prompt.encode().unwrap().bytes).unwrap();
    assert_eq!(output, source.replace("priority: 2", "priority: 3"));
}

#[test]
fn opaque_non_utf8_tail_bytes_survive_owned_edits() {
    let mut source = SAMPLE.as_bytes().to_vec();
    source.extend_from_slice(b"\n## Tail\nopaque: ");
    source.extend_from_slice(&[0xff, 0xfe, b'\n']);
    let mut prompt = PromptDocument::parse_bytes(PATH, &source).unwrap();
    assert_eq!(prompt.encode().unwrap().bytes, source);
    prompt.metadata.priority = 3;
    let encoded = prompt.encode().unwrap().bytes;
    let expected = source
        .windows(b"priority: 2".len())
        .position(|window| window == b"priority: 2")
        .unwrap();
    assert_eq!(
        &encoded[expected..expected + b"priority: 3".len()],
        b"priority: 3"
    );
    assert!(encoded.ends_with(&[0xff, 0xfe, b'\n']));
}

#[test]
fn append_log_updates_timestamp_and_preserves_original_log_bytes() {
    let source = SAMPLE.replace(
        "Do the thing.\n",
        "Do the thing.\n\n## Log\n- opaque original\n\n## Tail\nkeep\n",
    );
    let mut prompt = PromptDocument::parse(PATH, &source).unwrap();
    prompt.append_log(LogEntry {
        at: stamp("2027-01-02T03:04:05Z"),
        actor: "worker".into(),
        event: "completed\ncontinued".into(),
        ..LogEntry::default()
    });
    let output = String::from_utf8(prompt.encode().unwrap().bytes).unwrap();
    assert!(
        output
            .contains("- opaque original\n- 2027-01-02T03:04:05Z worker: completed\n  continued\n")
    );
    assert!(output.ends_with("## Tail\nkeep\n"));
    assert!(output.contains("updated: 2027-01-02T03:04:05Z"));
}

#[test]
fn new_document_uses_owned_order_alias_default_body_and_log() {
    let mut prompt = PromptDocument::new(metadata());
    prompt.set_body_bytes(default_prompt_template());
    prompt.append_log(LogEntry {
        at: stamp("2026-09-11T15:51:49Z"),
        actor: "planner".into(),
        event: "created".into(),
        ..LogEntry::default()
    });
    let actual = String::from_utf8(prompt.encode().unwrap().bytes).unwrap();
    let expected = "---\nid: beans-p-a3f2\naliases: [beans-p-a3f2]\ntitle: Durable prompt\nstatus: open\npriority: 2\nfrom: planner\nto: implementer\ncreated: 2026-09-11T15:51:49Z\nupdated: 2026-09-11T15:51:49Z\n---\n## Objective\n\n## Constraints\n\n## Expected result\n\n## Log\n- 2026-09-11T15:51:49Z planner: created\n";
    assert_eq!(actual, expected);
}

#[test]
fn explicit_empty_body_is_not_replaced_by_template() {
    let mut absent = PromptDocument::new(metadata());
    let absent_bytes = absent.encode().unwrap().bytes;
    let mut explicit_empty = PromptDocument::new(metadata());
    explicit_empty.set_body_bytes(b"");
    assert_eq!(explicit_empty.encode().unwrap().bytes, absent_bytes);
    let mut templated = PromptDocument::new(metadata());
    templated.set_body_bytes(default_prompt_template());
    assert!(
        templated
            .encode()
            .unwrap()
            .bytes
            .ends_with(default_prompt_template())
    );
    assert_ne!(templated.encode().unwrap().bytes, absent_bytes);

    let mut exact = PromptDocument::new(metadata());
    exact.set_body_bytes(b"custom template\n\n");
    assert!(
        exact
            .encode()
            .unwrap()
            .bytes
            .ends_with(b"custom template\n\n")
    );
}
