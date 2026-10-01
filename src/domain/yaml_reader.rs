//! yaml.v3 checks printability as its 512-byte reader windows are loaded.
//! Bytes after an explicit document end may never reach the reader.
use super::frontmatter::Error;
use yaml_rust2::scanner::{Scanner, TokenType};
pub(super) struct Reader<'a> {
    raw: &'a [u8],
    offsets: Vec<usize>,
    lines: Vec<usize>,
    checked: usize,
    utf16: Option<bool>,
    first_window: bool,
    loaded_end: usize,
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
        let utf16 = match raw.get(..2) {
            Some([255, 254]) => Some(true),
            Some([254, 255]) => Some(false),
            _ => None,
        };
        let map = if utf16.is_some() {
            Some(super::source::Source::yaml(raw).offsets)
        } else {
            std::str::from_utf8(raw)
                .err()
                .map(|_| super::source::Source::new(raw).offsets)
        };
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
            checked: if utf16.is_some() { 2 } else { 0 },
            utf16,
            first_window: true,
            loaded_end: 0,
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
            // The slice reader reports EOF on the next refill, rather than
            // on a short nonempty read. An incomplete final unit is deferred
            // if the scanner already has enough decoded characters.
            let eof = !self.first_window && self.loaded_end == self.raw.len();
            let end = if self.first_window {
                512
            } else {
                self.checked + 512
            }
            .min(self.raw.len());
            self.first_window = false;
            self.loaded_end = end;
            let mut at = self.checked;
            while at < end {
                if let Some(little) = self.utf16 {
                    if at + 2 > end {
                        if eof {
                            return Err(reader_error(path, "incomplete UTF-16 character"));
                        }
                        break;
                    }
                    let unit = |position| {
                        let bytes = [self.raw[position], self.raw[position + 1]];
                        if little {
                            u16::from_le_bytes(bytes)
                        } else {
                            u16::from_be_bytes(bytes)
                        }
                    };
                    let first = unit(at);
                    if (0xdc00..=0xdfff).contains(&first) {
                        return Err(reader_error(path, "unexpected low surrogate area"));
                    }
                    let (value, width) = if (0xd800..=0xdbff).contains(&first) {
                        if at + 4 > end {
                            if eof {
                                return Err(reader_error(path, "incomplete UTF-16 surrogate pair"));
                            }
                            break;
                        }
                        let low = unit(at + 2);
                        if !(0xdc00..=0xdfff).contains(&low) {
                            return Err(reader_error(path, "expected low surrogate area"));
                        }
                        (
                            0x10000 + ((u32::from(first) & 0x3ff) << 10) + (u32::from(low) & 0x3ff),
                            4,
                        )
                    } else {
                        (u32::from(first), 2)
                    };
                    if !printable(char::from_u32(value).unwrap()) {
                        return Err(reader_error(path, "control characters are not allowed"));
                    }
                    at += width;
                    continue;
                }
                let first = self.raw[at];
                let width = match first {
                    0..=127 => 1,
                    192..=223 => 2,
                    224..=239 => 3,
                    240..=247 => 4,
                    _ => return Err(reader_error(path, "invalid leading UTF-8 octet")),
                };
                if at + width > end {
                    if eof {
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
    Error::new(format!("{path}: frontmatter: yaml: {reason}"))
}
