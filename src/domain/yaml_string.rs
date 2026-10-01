//! yaml.v3 string lists can contain non-UTF-8 bytes decoded from !!binary.
use serde::{Deserialize, Deserializer, Serialize, Serializer};
#[derive(Clone, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct YamlString(Vec<u8>);
impl YamlString {
    pub fn from_bytes(bytes: Vec<u8>) -> Self {
        Self(bytes)
    }
    pub fn as_bytes(&self) -> &[u8] {
        &self.0
    }
    pub fn as_str(&self) -> Option<&str> {
        std::str::from_utf8(&self.0).ok()
    }
    pub(crate) fn trimmed(&self) -> Self {
        fn width(bytes: &[u8]) -> Option<usize> {
            let n = match *bytes.first()? {
                0..=127 => 1,
                192..=223 => 2,
                224..=239 => 3,
                240..=247 => 4,
                _ => return None,
            };
            let ch = std::str::from_utf8(bytes.get(..n)?).ok()?.chars().next()?;
            matches!(ch, '\u{9}'..='\u{d}' | ' ' | '\u{85}' | '\u{a0}' | '\u{1680}' | '\u{2000}'..='\u{200a}' | '\u{2028}' | '\u{2029}' | '\u{202f}' | '\u{205f}' | '\u{3000}').then_some(n)
        }
        let mut bytes = self.as_bytes();
        while let Some(n) = width(bytes) {
            bytes = &bytes[n..];
        }
        loop {
            let n = (1..=4.min(bytes.len())).find(|&n| width(&bytes[bytes.len() - n..]) == Some(n));
            match n {
                Some(n) => bytes = &bytes[..bytes.len() - n],
                None => break,
            }
        }
        Self::from_bytes(bytes.into())
    }
    fn display_text(&self) -> String {
        let mut bytes = self.0.as_slice();
        let mut text = String::new();
        loop {
            match std::str::from_utf8(bytes) {
                Ok(s) => {
                    text.push_str(s);
                    break;
                }
                Err(e) => {
                    text.push_str(std::str::from_utf8(&bytes[..e.valid_up_to()]).unwrap());
                    let count = e.error_len().unwrap_or(bytes.len() - e.valid_up_to());
                    text.push_str(&"�".repeat(count));
                    bytes = &bytes[e.valid_up_to() + count..];
                }
            }
        }
        text
    }
    pub(crate) fn quoted(&self) -> String {
        let mut bytes = self.0.as_slice();
        let mut out = String::from("\"");
        while !bytes.is_empty() {
            let (valid, invalid) = match std::str::from_utf8(bytes) {
                Ok(_) => (bytes.len(), 0),
                Err(e) => (
                    e.valid_up_to(),
                    e.error_len().unwrap_or(bytes.len() - e.valid_up_to()),
                ),
            };
            let text = super::issue::quoted(std::str::from_utf8(&bytes[..valid]).unwrap());
            out.push_str(&text[1..text.len() - 1]);
            for byte in &bytes[valid..valid + invalid] {
                out.push_str(&format!("\\x{byte:02x}"));
            }
            bytes = &bytes[valid + invalid..];
        }
        out.push('"');
        out
    }
}
impl std::borrow::Borrow<[u8]> for YamlString {
    fn borrow(&self) -> &[u8] {
        self.as_bytes()
    }
}
impl std::fmt::Display for YamlString {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.display_text())
    }
}
impl From<String> for YamlString {
    fn from(value: String) -> Self {
        Self(value.into_bytes())
    }
}
impl From<&str> for YamlString {
    fn from(value: &str) -> Self {
        Self(value.as_bytes().into())
    }
}
impl Serialize for YamlString {
    fn serialize<S: Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        serializer.serialize_str(&self.display_text())
    }
}
impl<'de> Deserialize<'de> for YamlString {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        String::deserialize(deserializer).map(Self::from)
    }
}

pub(super) fn binary(value: &str) -> Option<Vec<u8>> {
    let bytes: Vec<_> = value
        .bytes()
        .filter(|b| !matches!(b, b'\r' | b'\n'))
        .collect();
    if bytes.len() % 4 != 0 {
        return None;
    }
    let digit = |b: u8| -> Option<u32> {
        Some(match b {
            b'A'..=b'Z' => (b - b'A') as u32,
            b'a'..=b'z' => (b - b'a' + 26) as u32,
            b'0'..=b'9' => (b - b'0' + 52) as u32,
            b'+' => 62,
            b'/' => 63,
            _ => return None,
        })
    };
    let mut out = Vec::new();
    for (i, c) in bytes.as_chunks::<4>().0.iter().enumerate() {
        let a = digit(c[0])?;
        let b = digit(c[1])?;
        let last = (i + 1) * 4 == bytes.len();
        out.push((a * 4 + b / 16) as u8);
        if c[2] == b'=' {
            if !last || c[3] != b'=' {
                return None;
            }
            continue;
        }
        let d = digit(c[2])?;
        out.push((b * 16 + d / 4) as u8);
        if c[3] == b'=' {
            if !last {
                return None;
            }
            continue;
        }
        let e = digit(c[3])?;
        out.push((d * 64 + e) as u8);
    }
    Some(out)
}

#[cfg(test)]
mod tests {
    #[test]
    fn production_trimming_matches_go_for_every_unicode_scalar() {
        let fixture: serde_json::Value =
            serde_json::from_str(include_str!("../../tests/contract/config-foundation.json"))
                .unwrap();
        let ranges: Vec<(u32, u32)> =
            serde_json::from_value(fixture["space_ranges"].clone()).unwrap();
        for cp in 0..=0x10ffff {
            let Some(ch) = char::from_u32(cp) else {
                continue;
            };
            let space = ranges.iter().any(|&(start, end)| start <= cp && cp <= end);
            let raw = super::YamlString::from(format!(" {ch} \t"));
            let expected = if space { String::new() } else { ch.to_string() };
            assert_eq!(raw.trimmed().as_bytes(), expected.as_bytes(), "U+{cp:04X}");
        }
        for bytes in [
            vec![32, 255, 32],
            vec![32, 226, 130, 32],
            vec![32, 192, 175, 32],
            vec![32, 128, 32],
        ] {
            let raw = super::YamlString::from_bytes(bytes.clone());
            assert_eq!(raw.trimmed().as_bytes(), &bytes[1..bytes.len() - 1]);
        }
    }
}
