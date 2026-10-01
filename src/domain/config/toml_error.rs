//! Adapt observed parser diagnostics; source spans supply tokens and context.
use super::tree::line;
use crate::domain::frontmatter::Error;

fn find_marker(value: &toml::Value, marker: &str, path: &mut Vec<String>) -> Option<Vec<String>> {
    match value {
        toml::Value::Table(values) => {
            for (key, value) in values {
                if key == marker {
                    return Some(path.clone());
                }
                path.push(key.clone());
                if let Some(found) = find_marker(value, marker, path) {
                    return Some(found);
                }
                path.pop();
            }
        }
        toml::Value::Array(values) => {
            for value in values {
                if let Some(found) = find_marker(value, marker, path) {
                    return Some(found);
                }
            }
        }
        _ => {}
    }
    None
}
fn leaf_path(value: &toml::Value) -> Vec<String> {
    match value {
        toml::Value::Table(values) if values.len() == 1 => {
            let (key, value) = values.iter().next().unwrap();
            let mut path = vec![key.clone()];
            path.extend(leaf_path(value));
            path
        }
        _ => Vec::new(),
    }
}
fn duplicate_key(source: &str, span: std::ops::Range<usize>) -> String {
    let token = source.get(span.clone()).unwrap_or_default();
    let mut marker = "__beans_toml_error_key".to_owned();
    while source.contains(&marker) {
        marker.push('_');
    }
    // Replace only the parser's offending key token, then let the real parser
    // recover its table/inline/dotted context. This is diagnostic-only: no
    // repaired document is returned to callers or written to disk.
    let patched = format!("{}{}{}", &source[..span.start], marker, &source[span.end..]);
    let mut path = toml::from_str::<toml::Value>(&patched)
        .ok()
        .and_then(|v| find_marker(&v, &marker, &mut Vec::new()))
        .unwrap_or_default();
    let parts = toml::from_str::<toml::Value>(&format!("{token}=0"))
        .ok()
        .map(|v| leaf_path(&v))
        .unwrap_or_else(|| vec![token.to_owned()]);
    path.extend(parts);
    let mut out = Vec::new();
    for (i, key) in path.iter().enumerate() {
        if i != 0 {
            out.push(b'.');
        }
        super::super::encode::key(&mut out, key.as_bytes());
    }
    String::from_utf8(out).expect("parsed keys are UTF-8")
}
pub(super) fn adapt(source: &str, error: toml::de::Error) -> Error {
    let span = error.span().unwrap_or(source.len()..source.len());
    let mut offset = span.start;
    let message = if error
        .message()
        .starts_with("missing comma between array elements")
    {
        let found = source[offset..].chars().next();
        let found = found
            .map(|c| format!("'{c}'"))
            .unwrap_or_else(|| "end of file".into());
        format!("expected a comma (',') or array terminator (']'), but got {found}")
    } else if error.message().starts_with("unclosed array") {
        offset = source.len();
        use toml_parser::lexer::TokenKind;
        let last = toml_parser::Source::new(source)
            .lex()
            .map(|token| (token.kind(), token.span().start()))
            .filter(|(kind, _)| {
                !matches!(
                    kind,
                    TokenKind::Whitespace
                        | TokenKind::Newline
                        | TokenKind::Comment
                        | TokenKind::Eof
                )
            })
            .last();
        if let Some((TokenKind::LeftSquareBracket | TokenKind::Comma, start)) = last {
            offset = start;
            "unexpected EOF; expected value".into()
        } else {
            "expected a comma (',') or array terminator (']'), but got end of file".into()
        }
    } else if error.message() == "duplicate key"
        || error.message().starts_with("cannot extend value of type")
    {
        format!(
            "Key '{}' has already been defined.",
            duplicate_key(source, span.clone())
        )
    } else if error.message().starts_with("key with no value") {
        let missing = source[..offset].rfind('=').and_then(|eq| {
            let rest = &source[eq + 1..];
            let value = rest.trim_start_matches([' ', '\t']);
            value
                .starts_with('\n')
                .then_some(eq + 1 + rest.len() - value.len())
        });
        if let Some(newline) = missing {
            offset = newline;
            "expected value but found '\\n' instead".into()
        } else {
            error.message().into()
        }
    } else if error
        .message()
        .starts_with("invalid value, expected unicode hexadecimal value")
    {
        let prefix = &source[..span.end];
        let escape = prefix
            .rfind("\\u")
            .map(|i| (i, 6))
            .into_iter()
            .chain(prefix.rfind("\\U").map(|i| (i, 10)))
            .max_by_key(|(i, _)| *i);
        if let Some((i, len)) = escape {
            let token = source.get(i..i + len).unwrap_or_default();
            format!("Escaped character '{token}' is not valid UTF-8.")
        } else {
            error.message().into()
        }
    } else {
        error.message().into()
    };
    Error(format!("line {}: {message}", line(source, offset)))
}
