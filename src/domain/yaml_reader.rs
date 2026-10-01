//! yaml.v3 checks printability as its 512-byte reader windows are loaded.
//! Bytes after an explicit document end may never reach the reader.
use super::frontmatter::Error;
use yaml_rust2::scanner::{Scanner, TokenType};
pub(super) struct Reader<'a> {
    raw: &'a [u8],
    offsets: Vec<usize>,
    lines: Vec<usize>,
    checked: usize,
}
impl<'a> Reader<'a> {
    /// Go associates comments using two tokens beyond the current token.
    /// A following key can satisfy that demand before its value is scanned;
    /// a comment-only suffix instead requires reading through its end.
    pub fn error_lookahead(
        &mut self,
        path: &str,
        adapted: &str,
        line: usize,
        column: usize,
    ) -> Result<(), Error> {
        let mut scanner = Scanner::new(adapted.chars());
        while let Some(token) = scanner.next() {
            if token.0.line() == line && token.0.col() == column {
                for _ in 0..2 {
                    let Some(next) = scanner.next() else {
                        if let Some(error) = scanner.get_error() {
                            self.check(path, error.marker().line(), error.marker().col())?;
                        }
                        break;
                    };
                    let width = if let TokenType::Scalar(_, value) = &next.1 {
                        value.chars().count()
                    } else {
                        0
                    };
                    self.check(path, next.0.line(), next.0.col() + width)?;
                }
                break;
            }
        }
        Ok(())
    }
    pub fn document_end(
        &mut self,
        path: &str,
        adapted: &str,
        line: usize,
        column: usize,
    ) -> Result<(), Error> {
        let mut scanner = Scanner::new(adapted.chars());
        while let Some(token) = scanner.next() {
            if token.1 == TokenType::DocumentEnd
                && token.0.line() == line
                && token.0.col() == column
            {
                // yaml.v3 keeps two tokens ahead so it can attach comments.
                for _ in 0..2 {
                    let Some(next) = scanner.next() else {
                        break;
                    };
                    // Rust's scanner can scan beyond these tokens while
                    // deciding whether a flow container is a simple key.
                    // Go disables simple keys after an explicit document end.
                    let width = if let TokenType::Scalar(_, value) = &next.1 {
                        value.chars().count()
                    } else {
                        0
                    };
                    self.check(path, next.0.line(), next.0.col() + width)?;
                }
                break;
            }
        }
        Ok(())
    }
    pub fn new_raw(text: &str, raw: &'a [u8]) -> Self {
        let map = std::str::from_utf8(raw)
            .err()
            .map(|_| super::source::Source::new(raw).offsets);
        let mut offsets = Vec::new();
        let mut lines = vec![0];
        for (i, (byte, c)) in text.char_indices().enumerate() {
            offsets.push(map.as_ref().map_or(byte, |m| m[byte]));
            if c == '\n' {
                lines.push(i + 1);
            }
        }
        offsets.push(raw.len());
        Self {
            raw,
            offsets,
            lines,
            checked: 0,
        }
    }
    pub fn check(&mut self, path: &str, line: usize, column: usize) -> Result<(), Error> {
        let start = self
            .lines
            .get(line.saturating_sub(1))
            .copied()
            .unwrap_or(self.offsets.len() - 1);
        let end = self
            .lines
            .get(line)
            .copied()
            .unwrap_or(self.offsets.len() - 1);
        let character = (start + column).min(end);
        // Scanner document indicators require four characters of lookahead.
        let required = self.offsets[(character + 3).min(self.offsets.len() - 1)];
        while self.checked < self.raw.len() && self.checked <= required {
            let end = (self.checked + 512).min(self.raw.len());
            let mut at = self.checked;
            while at < end {
                let first = self.raw[at];
                let width = match first {
                    0..=127 => 1,
                    192..=223 => 2,
                    224..=239 => 3,
                    240..=247 => 4,
                    _ => return Err(reader_error(path, "invalid leading UTF-8 octet")),
                };
                if at + width > end {
                    if end == self.raw.len() {
                        return Err(reader_error(path, "incomplete UTF-8 octet sequence"));
                    }
                    break;
                }
                let mut value = u32::from(
                    first
                        & match width {
                            1 => 0x7f,
                            2 => 0x1f,
                            3 => 0xf,
                            4 => 7,
                            _ => unreachable!(),
                        },
                );
                for &b in &self.raw[at + 1..at + width] {
                    if b & 0xc0 != 0x80 {
                        return Err(reader_error(path, "invalid trailing UTF-8 octet"));
                    }
                    value = (value << 6) | u32::from(b & 0x3f);
                }
                if value < [0, 0, 0x80, 0x800, 0x10000][width] {
                    return Err(reader_error(path, "invalid length of a UTF-8 sequence"));
                }
                let Some(c) = char::from_u32(value) else {
                    return Err(reader_error(path, "invalid Unicode character"));
                };
                if !printable(c) {
                    return Err(reader_error(path, "control characters are not allowed"));
                }
                at += width;
            }
            self.checked = at;
        }
        Ok(())
    }
}
fn printable(c: char) -> bool {
    matches!(c,
        '\t' | '\n' | '\r' | ' '..='~' | '\u{85}' |
        '\u{a0}'..='\u{d7ff}' | '\u{e000}'..='\u{fffd}' | '\u{10000}'..='\u{10ffff}'
    )
}

fn reader_error(path: &str, reason: &str) -> Error {
    Error(format!("{path}: frontmatter: yaml: {reason}"))
}
