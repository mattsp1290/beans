//! These cases call typed encoders only; the coverage gate observes the real
//! kernel helpers, separately for each schema and input encoding.
use beans::domain::{
    frontmatter::Frontmatter,
    handoff::HandoffDocument,
    issue::{IssueDocument, Timestamp},
    memory::MemoryDocument,
    request::RequestDocument,
};

fn source(kind: &str, encoding: &str) -> (String, Vec<u8>, usize, usize) {
    let (path, head, first_line) = match kind {
        "issue" => (
            "projects/p/issues/p-one.md",
            "id: p-one\ntitle: Initial é # keep\ntype: task\nstatus: open\npriority: 2\n",
            2,
        ),
        "request" => (
            "projects/p/requests/p-r-one.md",
            "id: p-r-one\naliases: [p-r-one]\ntitle: Initial é # keep\nstatus: open\npriority: 2\n",
            3,
        ),
        "memory" => (
            "projects/p/memories/key.md",
            "key: key\ntype: reference # keep\n",
            2,
        ),
        "handoff" => (
            "projects/p/handoffs/p-one.md",
            "id: p-one\ntitle: Initial é # keep\n",
            2,
        ),
        _ => unreachable!(),
    };
    let head = format!("custom: [é, keep]\n{head}");
    let updated_line = head.lines().count() + 1;
    let yaml = format!("{head}created: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n...\n");
    let mut raw = b"---\n".to_vec();
    if encoding == "utf8" {
        raw.extend_from_slice(yaml.as_bytes());
    } else {
        let little = encoding == "utf16le";
        raw.extend(if little { [255, 254] } else { [254, 255] });
        for unit in yaml.encode_utf16() {
            raw.extend(if little {
                unit.to_le_bytes()
            } else {
                unit.to_be_bytes()
            });
        }
    }
    raw.extend_from_slice(b"\n\n---\nbody\xff\n");
    (path.into(), raw, first_line, updated_line)
}
fn run_case(kind: &str, encoding: &str) {
    let (path, raw, first_line, updated_line) = source(kind, encoding);
    let parsed = Frontmatter::parse_bytes(&path, &raw).unwrap();
    let first_key = if kind == "memory" { "type" } else { "title" };
    let first = parsed.fields().iter().find(|f| f.key == first_key).unwrap();
    let updated = parsed.fields().iter().find(|f| f.key == "updated").unwrap();
    // Independently locate raw physical LF lines, without consulting field
    // offsets or the kernel. The fixture UTF-16 edits also use these physical lines.
    let mut lines = Vec::new();
    let mut at = 4;
    for line in raw[4..].split_inclusive(|b| *b == b'\n') {
        if line == b"---\n" {
            break;
        }
        lines.push((at, at + line.len(), line));
        at += line.len();
    }
    let first_range = lines[first_line].0..lines[first_line].1;
    let updated_start = lines[updated_line].0;
    let updated_end = lines
        .iter()
        .rev()
        .find(|(_, _, line)| {
            !line
                .iter()
                .all(|b| matches!(b, b' ' | b'\t' | b'\r' | b'\n'))
        })
        .unwrap()
        .1;
    assert_eq!(
        (first.start, first.end),
        (first_range.start, first_range.end)
    );
    assert_eq!((updated.start, updated.end), (updated_start, updated_end));
    let now = Timestamp {
        seconds: 1798761600,
        nanoseconds: 0,
        offset_seconds: 0,
    };
    let output = match kind {
        "issue" => {
            let mut d = IssueDocument::parse_bytes(&path, &raw).unwrap();
            d.metadata.title = "Changed".into();
            d.metadata.updated = now;
            d.encode().unwrap()
        }
        "request" => {
            let mut d = RequestDocument::parse_bytes(&path, &raw).unwrap();
            d.metadata.title = "Changed".into();
            d.metadata.updated = now;
            d.encode().unwrap()
        }
        "memory" => {
            let mut d = MemoryDocument::parse_bytes(&path, &raw).unwrap();
            d.metadata.kind = "feedback".into();
            d.metadata.updated = now;
            d.encode().unwrap()
        }
        "handoff" => {
            let mut d = HandoffDocument::parse_bytes(&path, &raw).unwrap();
            d.metadata.title = "Changed".into();
            d.metadata.updated = now;
            d.encode().unwrap()
        }
        _ => unreachable!(),
    };
    let mut expected = raw[..first_range.start].to_vec();
    expected.extend_from_slice(
        format!(
            "{first_key}: {} # keep\n",
            if kind == "memory" {
                "feedback"
            } else {
                "Changed"
            }
        )
        .as_bytes(),
    );
    expected.extend_from_slice(&raw[first_range.end..updated_start]);
    expected.extend_from_slice(b"updated: 2027-01-01T00:00:00Z\n");
    expected.extend_from_slice(&raw[updated_end..]);
    assert_eq!(output.bytes, expected);
    let preserved = [
        0..first_range.start,
        first_range.end..updated_start,
        updated_end..raw.len(),
    ];
    assert_eq!(output.copies.len(), preserved.len());
    for (copy, range) in output.copies.iter().zip(preserved) {
        assert_eq!(copy.source, range);
        assert_eq!(&output.bytes[copy.destination.clone()], &raw[range]);
    }
    assert_eq!(parsed.original_bytes(), raw);
    assert!(
        parsed
            .replace_fields(&[(parsed.fields().len(), b"bad")])
            .is_err()
    );
    assert!(
        parsed
            .replace_fields(&[(0, b"bad"), (0, b"duplicate")])
            .is_err()
    );
    assert_eq!(parsed.original_bytes(), raw);
}
macro_rules! case {
    ($name:ident,$kind:literal,$encoding:literal) => {
        #[test]
        fn $name() {
            run_case($kind, $encoding)
        }
    };
}
case!(issue_utf8, "issue", "utf8");
case!(issue_utf16le, "issue", "utf16le");
case!(issue_utf16be, "issue", "utf16be");
case!(request_utf8, "request", "utf8");
case!(request_utf16le, "request", "utf16le");
case!(request_utf16be, "request", "utf16be");
case!(memory_utf8, "memory", "utf8");
case!(memory_utf16le, "memory", "utf16le");
case!(memory_utf16be, "memory", "utf16be");
case!(handoff_utf8, "handoff", "utf8");
case!(handoff_utf16le, "handoff", "utf16le");
case!(handoff_utf16be, "handoff", "utf16be");
#[test]
fn parse_only_negative_control() {
    let (path, raw, _, _) = source("issue", "utf8");
    let d = IssueDocument::parse_bytes(&path, &raw).unwrap();
    assert_eq!(d.metadata.title, "Initial é");
    assert_eq!(d.original_bytes(), raw);
}

#[test]
fn plan_graph_utf8() {
    let now = Timestamp {
        seconds: 1789128000,
        nanoseconds: 0,
        offset_seconds: 0,
    };
    let bytes = beans::domain::plan::scaffold("beans-plan-a3f2", "x", &now).unwrap();
    let text = String::from_utf8(bytes).unwrap().replacen(
        "nodes: []",
        "nodes:\n  - id: model\n    label: Model\n    kind: component",
        1,
    );
    let mut plan = beans::domain::plan::parse("plan.md", text.as_bytes()).unwrap();
    plan.body = format!("雪🌱 café\n{}\n## Notes\nαβ雪\n", plan.body);
    let before = plan.body.clone();
    let start = before.find("```bn-change-graph\n").unwrap() + "```bn-change-graph\n".len();
    let end = start + before[start..].find("\n```").unwrap();
    let output =
        beans::domain::plan::set_node_ref(Some(&mut plan), "model", "[[beans-a1b2|雪🌱]]").unwrap();
    assert_eq!(
        plan.graph.nodes.as_ref().unwrap()[0].reference,
        "[[beans-a1b2|雪🌱]]"
    );
    assert_eq!(&plan.body[..start], &before[..start]);
    assert!(plan.body.ends_with(&before[end..]));
    assert_eq!(output.bytes, plan.body.as_bytes());
    assert_eq!(output.copies.len(), 2);
    for (copy, source) in output.copies.iter().zip([0..start, end..before.len()]) {
        assert_eq!(copy.source, source);
        assert_eq!(
            &output.bytes[copy.destination.clone()],
            &before.as_bytes()[source]
        );
    }
    let encoded = plan.body.clone();
    assert!(beans::domain::plan::set_node_ref(Some(&mut plan), "missing", "x").is_err());
    assert_eq!(plan.body, encoded);
}
