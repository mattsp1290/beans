//! Canonical diagnostic bytes. Rust Display is a read-only Unicode view;
//! command transports must use as_bytes to preserve Go's raw path diagnostics.
use super::yaml_string::YamlString;
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Error {
    message: YamlString,
}
impl Error {
    pub fn new(message: String) -> Self {
        Self {
            message: message.into(),
        }
    }
    pub fn from_bytes(message: Vec<u8>) -> Self {
        Self {
            message: YamlString::from_bytes(message),
        }
    }
    pub fn as_str(&self) -> Option<&str> {
        self.message.as_str()
    }
    pub fn as_bytes(&self) -> &[u8] {
        self.message.as_bytes()
    }
    /// Prefix the canonical message without formatting it through Display.
    pub fn context(self, prefix: &[u8]) -> Self {
        let mut bytes = prefix.to_vec();
        bytes.extend_from_slice(b": ");
        bytes.extend_from_slice(self.as_bytes());
        Self::from_bytes(bytes)
    }
}
impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        std::fmt::Display::fmt(&self.message, f)
    }
}
impl std::error::Error for Error {}
