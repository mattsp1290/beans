//! Generated serial edits through the production typed codecs, with an
//! independent byte model. These are codec replay checks, not Git operation
//! replay or filesystem qualification.
use beans::domain::{
    frontmatter::EditResult,
    handoff::HandoffDocument,
    issue::{IssueDocument, Timestamp},
    memory::MemoryDocument,
    request::RequestDocument,
};
use proptest::prelude::*;

fn path(kind: &str) -> &str {
    match kind {
        "issue" => "projects/p/issues/p-one.md",
        "request" => "projects/p/requests/p-r-one.md",
        "memory" => "projects/p/memories/key.md",
        "handoff" => "projects/p/handoffs/p-one.md",
        _ => unreachable!(),
    }
}
fn edit(kind: &str, raw: &[u8], value: &str, date: u8) -> EditResult {
    let at = Timestamp {
        seconds: 1767225600 + i64::from(date) * 86400,
        nanoseconds: 0,
        offset_seconds: 0,
    };
    macro_rules! apply {
        ($codec:ty, $field:ident) => {{
            let mut d = <$codec>::parse_bytes(path(kind), raw).unwrap();
            // Every reload is independently a byte-exact no-op.
            assert_eq!(d.encode().unwrap().bytes, raw);
            d.metadata.$field = value.into();
            d.metadata.updated = at.clone();
            let output = d.encode().unwrap();
            let mut reloaded = <$codec>::parse_bytes(path(kind), &output.bytes).unwrap();
            assert_eq!(reloaded.metadata.$field, value);
            assert_eq!(reloaded.metadata.updated, at);
            assert_eq!(reloaded.encode().unwrap().bytes, output.bytes);
            // Repeating the same semantic edit after reload creates no change.
            reloaded.metadata.$field = value.into();
            reloaded.metadata.updated = at;
            assert_eq!(reloaded.encode().unwrap().bytes, output.bytes);
            output
        }};
    }
    match kind {
        "issue" => apply!(IssueDocument, title),
        "request" => apply!(RequestDocument, title),
        "memory" => apply!(MemoryDocument, kind),
        "handoff" => apply!(HandoffDocument, title),
        _ => unreachable!(),
    }
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(512))]
    #[test]
    fn serial_edits_reload_and_replay_preserve_unknown_fields_and_raw_bodies(
        steps in prop::collection::vec((0usize..4, 0u8..4), 1..25),
        body in prop::collection::vec(any::<u8>().prop_filter("LF body without CRLF", |b| *b != b'\r'), 0..256),
        unknown in 0usize..3,
        padding in 0usize..5,
    ) {
        let unknown = [
            "custom: {nested: [é, 雪, keep], enabled: true}\n",
            "custom: |\n  café 雪\n  untouched # scalar content\n",
            "custom: &unowned [é, 雪]\nother: *unowned\n",
        ][unknown];
        for kind in ["issue", "request", "memory", "handoff"] {
            let key = if kind == "memory" { "type" } else { "title" };
            let values = if kind == "memory" { ["reference", "feedback", "project", "user"] }
                else { ["Initial", "Changed", "café", "雪🌱"] };
            let emitted = if kind == "memory" { values } else { ["Initial", "Changed", "café", "\"雪\\U0001F331\""] };
            let head = match kind {
                "issue" => "id: p-one\ntype: task\nstatus: open\npriority: 2\n",
                "request" => "id: p-r-one\naliases: [p-r-one]\nstatus: open\npriority: 2\n",
                "memory" => "key: key\n",
                "handoff" => "id: p-one\n",
                _ => unreachable!(),
            };
            let mut current_value = emitted[0];
            let mut current_date = 0;
            let header = format!("---\n# 雪 head\n{head}{key}: {current_value} # owned comment\n{unknown}{}created: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z # time comment\ncustom_after: keep é\n---\n", "\n".repeat(padding));
            let mut raw = header.into_bytes();
            raw.extend_from_slice(b"opaque body\n"); raw.extend_from_slice(&body);
            let original_body = raw[raw.windows(5).position(|w| w == b"\n---\n").unwrap() + 5..].to_vec();
            for &(index, date) in &steps {
                let value = values[index];
                // Expected changes come from a separate fixed-line byte model,
                // never the codec's spans or rendering helpers.
                let end = raw.windows(5).position(|w| w == b"\n---\n").unwrap() + 5;
                let before = std::str::from_utf8(&raw[..end]).unwrap();
                let expected_header = before
                    .replacen(&format!("{key}: {current_value} # owned comment\n"), &format!("{key}: {} # owned comment\n", emitted[index]), 1)
                    .replacen(&format!("updated: 2026-01-{:02}T00:00:00Z # time comment\n", current_date + 1), &format!("updated: 2026-01-{:02}T00:00:00Z # time comment\n", date + 1), 1);
                let mut expected = expected_header.into_bytes(); expected.extend_from_slice(&original_body);
                let output = edit(kind, &raw, value, date);
                prop_assert_eq!(&output.bytes, &expected, "kind={} step={:?}", kind, (index, date));
                for copy in &output.copies {
                    prop_assert_eq!(&raw[copy.source.clone()], &output.bytes[copy.destination.clone()]);
                }
                raw = output.bytes; current_value = emitted[index]; current_date = date;
            }
        }
    }
}
