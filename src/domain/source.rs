//! Read-only Unicode parser view and offsets into the original source bytes.
use super::yaml_string::YamlString;
pub(crate) struct Source {
    pub text: String,
    pub offsets: Vec<usize>,
}
impl Source {
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
