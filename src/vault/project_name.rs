//! Project names are distinct from issue slugs: internal hyphens are retained.
pub fn valid_project_name(name: &[u8]) -> bool {
    name.first().is_some_and(u8::is_ascii_alphanumeric)
        && name
            .iter()
            .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || *b == b'-')
}

pub fn project_name(mut input: &[u8]) -> String {
    let mut name = String::new();
    while !input.is_empty() {
        let (valid, invalid) = match std::str::from_utf8(input) {
            Ok(_) => (input.len(), 0),
            Err(error) => (
                error.valid_up_to(),
                error
                    .error_len()
                    .unwrap_or(input.len() - error.valid_up_to()),
            ),
        };
        for ch in std::str::from_utf8(&input[..valid]).unwrap().chars() {
            // Stored project names use one lowercased scalar rather than case folding.
            let ch = ch.to_lowercase().next().unwrap();
            name.push(
                if ch.is_ascii_lowercase() || ch.is_ascii_digit() || ch == '-' {
                    ch
                } else {
                    '-'
                },
            );
        }
        // Each malformed byte contributes one separator.
        name.extend(std::iter::repeat_n('-', invalid));
        input = &input[valid + invalid..];
    }
    name.trim_matches('-').into()
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn every_unicode_scalar_preserves_project_name_rules() {
        let fixture: serde_json::Value =
            serde_json::from_str(include_str!("../../tests/fixtures/expected/paths.json")).unwrap();
        let mapping: Vec<(u32, u32)> =
            serde_json::from_value(fixture["ascii_lower"].clone()).unwrap();
        for cp in 0..=0x10ffff {
            let Some(ch) = char::from_u32(cp) else {
                continue;
            };
            let expected =
                if let Some((_, lower)) = mapping.iter().find(|(source, _)| *source == cp) {
                    char::from_u32(*lower).unwrap()
                } else if ch.is_ascii_digit() || ch == '-' {
                    ch
                } else {
                    '-'
                };
            assert_eq!(
                project_name(format!("x{ch}x").as_bytes()),
                format!("x{expected}x"),
                "U+{cp:04X}"
            );
        }
    }
}
