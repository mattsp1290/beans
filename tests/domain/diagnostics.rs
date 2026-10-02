//! Compare diagnostic meaning across escaped scalar presentation conventions.
//! This does not alter class, path, position, field name, or quoted scalar bytes.
pub fn meaning(raw: &[u8]) -> Vec<u8> {
    let mut out = Vec::new();
    let mut i = 0;
    while i < raw.len() {
        if raw[i] == b'\\' && i + 1 < raw.len() {
            let mnemonic = match raw[i + 1] {
                b'0' => Some(0),
                b'a' => Some(7),
                b'b' => Some(8),
                b'f' => Some(12),
                b'n' => Some(10),
                b'r' => Some(13),
                b't' => Some(9),
                b'v' => Some(11),
                b'\\' => Some(b'\\'),
                b'"' => Some(b'"'),
                b'\'' => Some(b'\''),
                _ => None,
            };
            if let Some(byte) = mnemonic {
                out.push(byte);
                i += 2;
                continue;
            }
            let kind = raw[i + 1];
            let (start, end, consumed) = if kind == b'u' && raw.get(i + 2) == Some(&b'{') {
                let end = raw[i + 3..]
                    .iter()
                    .position(|b| *b == b'}')
                    .map(|end| i + 3 + end);
                end.map(|end| (i + 3, end, end + 1)).unwrap_or((0, 0, 0))
            } else {
                let width = match kind {
                    b'x' => 2,
                    b'u' => 4,
                    b'U' => 8,
                    _ => 0,
                };
                (i + 2, (i + 2 + width).min(raw.len()), i + 2 + width)
            };
            if end > start
                && consumed <= raw.len()
                && let Ok(hex) = std::str::from_utf8(&raw[start..end])
                && let Ok(code) = u32::from_str_radix(hex, 16)
            {
                if kind == b'x' && code <= 255 {
                    out.push(code as u8);
                    i = consumed;
                    continue;
                }
                if let Some(ch) = char::from_u32(code) {
                    out.extend_from_slice(ch.encode_utf8(&mut [0; 4]).as_bytes());
                    i = consumed;
                    continue;
                }
            }
        }
        out.push(raw[i]);
        i += 1;
    }
    out
}
pub fn text(text: &str) -> Vec<u8> {
    let mut text = text.to_owned();
    for (before, after) in [
        (
            "yaml: control characters are not allowed",
            "YAML control character is not allowed",
        ),
        (
            "yaml: incomplete UTF-16 character",
            "incomplete UTF-16 code unit",
        ),
        (
            "yaml: incomplete UTF-16 surrogate pair",
            "invalid UTF-16 surrogate pair",
        ),
        (
            "yaml: expected low surrogate area",
            "invalid UTF-16 surrogate pair",
        ),
        (
            "yaml: unexpected low surrogate area",
            "invalid UTF-16 surrogate pair",
        ),
    ] {
        text = text.replace(before, after);
    }
    meaning(text.as_bytes())
}

pub fn errors(mut value: serde_json::Value) -> serde_json::Value {
    match &mut value {
        serde_json::Value::Object(fields) => {
            for (key, field) in fields {
                if key == "Error" && field.is_array() {
                    let bytes: Vec<u8> = serde_json::from_value(field.clone()).unwrap();
                    *field = serde_json::json!(meaning(&bytes));
                } else {
                    *field = errors(field.take());
                }
            }
        }
        serde_json::Value::Array(values) => {
            for field in values {
                *field = errors(field.take());
            }
        }
        _ => (),
    }
    value
}
#[test]
fn diagnostic_meaning_keeps_classes_positions_and_scalar_bytes() {
    assert_eq!(
        text("field: \"\\u0085\\x01\\x00\""),
        text("field: \"\\u{85}\\u{1}\\0\"")
    );
    assert_ne!(text("line 1: title \"one\""), text("line 2: title \"one\""));
    assert_ne!(text("invalid \"one\""), text("invalid \"two\""));
    assert_eq!(meaning(b"invalid \\xff"), b"invalid \xff");
}

/// Independent encoding oracle for the physical frontmatter interval. Body
/// octets remain unrestricted. Standard Unicode decoders replace reader timing.
pub fn encoding_error(raw: &[u8]) -> Option<&'static str> {
    if !raw.starts_with(b"---\n") || raw.windows(2).any(|x| x == b"\r\n") {
        return None;
    }
    let end = raw[4..].windows(4).position(|x| x == b"---\n")? + 4;
    let bytes = &raw[4..end];
    let text = if matches!(bytes.get(..2), Some([255, 254] | [254, 255])) {
        if !bytes.len().is_multiple_of(2) {
            return Some("incomplete UTF-16 code unit");
        }
        let little = bytes[0] == 255;
        let decoded = char::decode_utf16(bytes[2..].as_chunks::<2>().0.iter().map(|x| {
            if little {
                u16::from_le_bytes([x[0], x[1]])
            } else {
                u16::from_be_bytes([x[0], x[1]])
            }
        }))
        .collect::<Result<String, _>>();
        match decoded {
            Ok(text) => text,
            Err(_) => return Some("invalid UTF-16 surrogate pair"),
        }
    } else {
        match std::str::from_utf8(bytes) {
            Ok(text) => text.to_owned(),
            Err(_) => return Some("invalid UTF-8 encoding"),
        }
    };
    // YAML's literal character repertoire, independently expressed as code points.
    if text.chars().any(
        |ch| matches!(ch as u32, 0..=8 | 11..=12 | 14..=31 | 127..=132 | 134..=159 | 65534..=65535),
    ) {
        return Some("YAML control character is not allowed");
    }
    None
}
