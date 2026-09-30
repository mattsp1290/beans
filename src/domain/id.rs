//! Issue namespaces, random hashes and creation-time filenames. Callers retain
//! an existing filename/slug when a title changes.
pub const DEFAULT_ID_LENGTH: usize = 4;
pub const SLUG_MAX_LEN: usize = 60;
const ALPHABET: &[u8] = b"abcdefghijklmnopqrstuvwxyz0123456789";

pub fn valid_id(id: &str) -> bool {
    let bytes = id.as_bytes();
    let alnum = |byte: u8| byte.is_ascii_lowercase() || byte.is_ascii_digit();
    let Some(dash) = bytes.iter().rposition(|&byte| byte == b'-') else {
        return false;
    };
    if dash == 0 || !alnum(bytes[0]) || !bytes[..dash].iter().all(|&b| alnum(b) || b == b'-') {
        return false;
    }
    let mut parts = bytes[dash + 1..].split(|&byte| byte == b'.');
    let hash = parts.next().unwrap();
    !hash.is_empty()
        && hash.iter().all(|&byte| alnum(byte))
        && parts.all(|part| !part.is_empty() && part.iter().all(u8::is_ascii_digit))
}

pub fn new_id(prefix: &str, exists: Option<&mut dyn FnMut(&str) -> bool>, length: isize) -> String {
    new_id_with_source(prefix, exists, length, |buffer| {
        if let Err(error) = getrandom::fill(buffer) {
            panic!("crypto/rand unavailable: {error}");
        }
    })
}

fn new_id_with_source(
    prefix: &str,
    mut exists: Option<&mut dyn FnMut(&str) -> bool>,
    length: isize,
    mut source: impl FnMut(&mut [u8]),
) -> String {
    let mut length = if length <= 0 {
        DEFAULT_ID_LENGTH
    } else {
        length as usize
    };
    let mut collisions = 0;
    loop {
        if collisions == 8 {
            length = length.checked_add(1).expect("ID hash length overflow");
            collisions = 0;
        }
        let mut buffer = vec![0; length];
        source(&mut buffer);
        let mut id = String::with_capacity(prefix.len() + 1 + length);
        id.push_str(prefix);
        id.push('-');
        for byte in buffer {
            id.push(ALPHABET[usize::from(byte) % ALPHABET.len()] as char);
        }
        if !exists.as_mut().is_some_and(|exists| exists(&id)) {
            return id;
        }
        collisions += 1;
    }
}

pub fn slug(title: &str) -> String {
    let mut output = String::new();
    let mut last_dash = true;
    for character in title.chars() {
        // Go uses simple Unicode lowercasing. Rust's full lowercase expansion
        // for U+0130 adds a combining dot; only the simple first rune belongs
        // here. The corpus qualifies every non-ASCII rune that lowers to ASCII.
        let lower = character.to_lowercase().next().unwrap();
        if lower.is_ascii_lowercase() || lower.is_ascii_digit() {
            output.push(lower);
            last_dash = false;
        } else if !last_dash {
            output.push('-');
            last_dash = true;
        }
    }
    let output = output.trim_matches('-');
    if output.len() <= SLUG_MAX_LEN {
        return output.to_owned();
    }
    let mut cut = &output[..SLUG_MAX_LEN];
    if output.as_bytes()[SLUG_MAX_LEN] != b'-'
        && let Some(index) = cut.rfind('-').filter(|&index| index > 0)
    {
        cut = &cut[..index];
    }
    cut.trim_matches('-').to_owned()
}

pub fn filename(id: &str, slug: &str) -> String {
    if slug.is_empty() {
        format!("{id}.md")
    } else {
        format!("{id}-{slug}.md")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deterministic_source_observes_every_eight_collision_growth() {
        let mut generated = Vec::new();
        let mut exists = |id: &str| {
            generated.push(id.to_owned());
            generated.len() <= 24
        };
        let mut lengths = Vec::new();
        let accepted = new_id_with_source("prefix", Some(&mut exists), 2, |buffer| {
            lengths.push(buffer.len());
            buffer.fill(255);
        });
        assert_eq!(
            lengths,
            [vec![2; 8], vec![3; 8], vec![4; 8], vec![5; 1]].concat()
        );
        assert_eq!(generated.len(), 25);
        assert_eq!(accepted, "prefix-ddddd");
        assert_eq!(generated.last(), Some(&accepted));
    }

    #[test]
    fn deterministic_source_preserves_go_modulo_alphabet_and_default_length() {
        let generated = new_id_with_source("x", None, 256, |buffer| {
            for (index, byte) in buffer.iter_mut().enumerate() {
                *byte = index as u8;
            }
        });
        let expected: String = (0..256).map(|byte| ALPHABET[byte % 36] as char).collect();
        assert_eq!(generated, format!("x-{expected}"));
        for length in [0, -1, isize::MIN] {
            assert_eq!(
                new_id_with_source("x", None, length, |buffer| buffer.fill(0)),
                "x-aaaa"
            );
        }
    }
}
