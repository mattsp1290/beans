//! yaml.v3 checks printability as its 512-byte reader windows are loaded.
//! Bytes after an explicit document end may never reach the reader.
use super::frontmatter::Error;
use yaml_rust2::scanner::{Scanner, TokenType};
pub(super) struct Reader<'a> {
    text: &'a str,
    offsets: Vec<usize>,
    lines: Vec<usize>,
    checked: usize,
}
impl<'a> Reader<'a> {
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
    pub fn new(text: &'a str) -> Self {
        let mut offsets = Vec::new();
        let mut lines = vec![0];
        for (i, (byte, c)) in text.char_indices().enumerate() {
            offsets.push(byte);
            if c == '\n' {
                lines.push(i + 1);
            }
        }
        offsets.push(text.len());
        Self {
            text,
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
        while self.checked < self.text.len() && self.checked <= required {
            let end = self
                .text
                .floor_char_boundary((self.checked + 512).min(self.text.len()));
            if self.text[self.checked..end].chars().any(|c| !printable(c)) {
                return Err(Error(format!(
                    "{path}: frontmatter: yaml: control characters are not allowed"
                )));
            }
            self.checked = end;
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
