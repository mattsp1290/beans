//! Read-only Unicode parser view and offsets into the original source bytes.
use super::{frontmatter::Error, yaml_string::YamlString};
pub(crate) struct Source<'a> {
    pub raw: &'a [u8],
    pub text: String,
    pub offsets: Vec<usize>,
}
impl<'a> Source<'a> {
    pub fn new(raw: &'a [u8]) -> Self {
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
        Self { raw, text, offsets }
    }
    pub fn validate_frontmatter(&self, path: &str) -> Result<(), Error> {
        if self.raw.windows(2).any(|b| b == b"\r\n") || !self.raw.starts_with(b"---\n") {
            return Ok(());
        }
        let mut end = 4;
        for line in self.raw[4..].split_inclusive(|&b| b == b'\n') {
            if line.strip_suffix(b"\n").unwrap_or(line) == b"---" {
                let fm = &self.raw[4..end];
                if let Err(e) = std::str::from_utf8(fm) {
                    let byte = fm[e.valid_up_to()];
                    let reason = if !(0xc0..=0xf7).contains(&byte) {
                        "invalid leading UTF-8 octet"
                    } else if e.error_len().is_none() {
                        "incomplete UTF-8 octet sequence"
                    } else {
                        "invalid trailing UTF-8 octet"
                    };
                    return Err(Error(format!("{path}: frontmatter: yaml: {reason}")));
                }
                break;
            }
            end += line.len();
        }
        Ok(())
    }
}
pub(crate) fn display(bytes: &[u8]) -> String {
    YamlString::from_bytes(bytes.into()).to_string()
}
