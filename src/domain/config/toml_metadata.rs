//! Source-ordered keys from the same pinned TOML parser's physical events.
use crate::domain::{frontmatter::Error, yaml_string::YamlString};
use toml_parser::{
    Source,
    parser::{Event, EventKind, parse_document},
};
pub(super) struct Key {
    pub path: Vec<String>,
    pub offset: usize,
}
pub(super) fn format_key(path: &[String]) -> String {
    let mut out = Vec::new();
    for (i, key) in path.iter().enumerate() {
        if i > 0 {
            out.push(b'.');
        }
        super::encode::key(&mut out, key.as_bytes());
    }
    String::from_utf8(out).expect("decoded TOML keys are UTF-8")
}
pub(super) fn keys(text: &str) -> Vec<Key> {
    let source = Source::new(text);
    let tokens: Vec<_> = source.lex().collect();
    let mut errors = Vec::new();
    let mut events: Vec<Event> = Vec::new();
    parse_document(&tokens, &mut events, &mut errors);
    let mut table = Vec::new();
    let mut parts = Vec::new();
    let mut result = Vec::new();
    let mut header = false;
    let mut header_offset = 0;
    let mut pending = Vec::new();
    let mut containers: Vec<Vec<String>> = Vec::new();
    for event in events {
        match event.kind() {
            EventKind::StdTableOpen | EventKind::ArrayTableOpen => {
                header = true;
                parts.clear();
                header_offset = event.span().start();
            }
            EventKind::StdTableClose | EventKind::ArrayTableClose => {
                table = std::mem::take(&mut parts);
                result.push(Key {
                    path: table.clone(),
                    offset: header_offset,
                });
                header = false;
            }
            EventKind::SimpleKey => {
                let mut key = String::new();
                if let Some(raw) = source.get(event) {
                    raw.decode_key(&mut key, &mut errors);
                }
                parts.push(key);
            }
            EventKind::KeyValSep if !header => {
                pending = containers.last().unwrap_or(&table).clone();
                pending.append(&mut parts);
                result.push(Key {
                    path: pending.clone(),
                    offset: event.span().start(),
                });
            }
            EventKind::InlineTableOpen | EventKind::ArrayOpen => {
                containers.push(pending.clone());
            }
            EventKind::InlineTableClose | EventKind::ArrayClose => {
                pending = containers.pop().unwrap_or_default();
                parts.clear();
            }
            _ => {}
        }
    }
    result
}
pub(super) fn full_error(source: &str, offset: usize, error: Error) -> Error {
    let Some((line, message)) = error.as_str().and_then(|message| message.split_once(": ")) else {
        return error;
    };
    let key = keys(source).into_iter().rfind(|k| k.offset < offset);
    let context = key
        .map(|k| {
            format!(
                " (last key {})",
                YamlString::from(format_key(&k.path)).quoted()
            )
        })
        .unwrap_or_default();
    Error::new(format!("toml: {line}{context}: {message}"))
}
