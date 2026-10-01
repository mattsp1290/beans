//! Read-only Unicode parser view and offsets into the original source bytes.
use super::yaml_string::YamlString;
pub(crate) struct Source {
    pub text: String,
    pub offsets: Vec<usize>,
}
impl Source {
    /// YAML chooses UTF-16 only from a leading BOM. The file itself still
    /// uses raw ASCII fences and LF spans; this is a separate parser view.
    pub fn yaml(raw: &[u8]) -> Self {
        let little = match raw.get(..2) {
            Some([255, 254]) => true,
            Some([254, 255]) => false,
            _ => return Self::new(raw),
        };
        let unit = |at: usize| {
            let b = [raw[at], raw[at + 1]];
            if little {
                u16::from_le_bytes(b)
            } else {
                u16::from_be_bytes(b)
            }
        };
        let mut text = String::new();
        let mut offsets = vec![2];
        let mut at = 2;
        while at < raw.len() {
            let start = at;
            let c = if at + 2 > raw.len() {
                at += 1;
                '�'
            } else {
                let first = unit(at);
                at += 2;
                if (0xd800..=0xdbff).contains(&first)
                    && at + 2 <= raw.len()
                    && (0xdc00..=0xdfff).contains(&unit(at))
                {
                    let low = unit(at);
                    at += 2;
                    char::from_u32(
                        0x10000 + ((u32::from(first) & 0x3ff) << 10) + (u32::from(low) & 0x3ff),
                    )
                    .unwrap()
                } else {
                    char::from_u32(u32::from(first)).unwrap_or('�')
                }
            };
            text.push(c);
            offsets.extend(std::iter::repeat_n(start, c.len_utf8() - 1));
            offsets.push(at);
        }
        Self { text, offsets }
    }

    pub fn new(raw: &[u8]) -> Self {
        let mut text = String::new();
        let mut offsets = vec![0];
        let mut at = 0;
        while at < raw.len() {
            let valid = match std::str::from_utf8(&raw[at..]) {
                Ok(_) => raw.len() - at,
                Err(e) => e.valid_up_to(),
            };
            text.push_str(std::str::from_utf8(&raw[at..at + valid]).unwrap());
            offsets.extend((at + 1)..=at + valid);
            at += valid;
            if at < raw.len() {
                text.push('�');
                offsets.extend([at, at, at + 1]);
                at += 1;
            }
        }
        Self { text, offsets }
    }
}

pub(crate) fn display(bytes: &[u8]) -> String {
    YamlString::from_bytes(bytes.into()).to_string()
}
