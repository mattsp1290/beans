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
    /// A parser accepts a Unicode filename view; restore only its leading
    /// filename diagnostic prefix to the caller's canonical bytes.
    pub(crate) fn with_path(self, view: &str, raw: &[u8]) -> Self {
        let prefix = [view.as_bytes(), b": "].concat();
        if let Some(rest) = self.as_bytes().strip_prefix(prefix.as_slice()) {
            Self::from_bytes([raw, b": ", rest].concat())
        } else {
            self
        }
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

impl From<String> for Error {
    fn from(message: String) -> Self {
        Self::new(message)
    }
}
impl From<&str> for Error {
    fn from(message: &str) -> Self {
        Self::new(message.into())
    }
}
impl serde::Serialize for Error {
    fn serialize<S: serde::Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        serde::Serialize::serialize(&self.message, serializer)
    }
}
