//! Plan timestamps expose Go RFC3339 parse diagnostics and require UTC location.
use crate::domain::{
    frontmatter::Error,
    issue::{Timestamp, parse_timestamp},
};
use time::{Date, Month};
const LAYOUT: &str = "2006-01-02T15:04:05Z07:00";
fn quoted(bytes: &[u8]) -> String {
    let mut out = String::from("\"");
    for &b in bytes {
        if !(32..128).contains(&b) {
            out.push_str(&format!("\\x{b:02x}"));
        } else {
            if matches!(b, b'"' | b'\\') {
                out.push('\\');
            }
            out.push(b as char);
        }
    }
    out.push('"');
    out
}
struct Cursor<'a> {
    raw: &'a str,
    position: usize,
}
impl Cursor<'_> {
    fn remaining(&self) -> &[u8] {
        &self.raw.as_bytes()[self.position..]
    }
    fn invalid(&self, layout: &str) -> Error {
        Error(format!(
            "parsing time {} as {}: cannot parse {} as {}",
            quoted(self.raw.as_bytes()),
            quoted(LAYOUT.as_bytes()),
            quoted(self.remaining()),
            quoted(layout.as_bytes())
        ))
    }
    fn range(&self, field: &str) -> Error {
        Error(format!(
            "parsing time {}: {field} out of range",
            quoted(self.raw.as_bytes())
        ))
    }
    fn prefix(&mut self, expected: &str) -> Result<(), Error> {
        if !self.remaining().starts_with(expected.as_bytes()) {
            return Err(self.invalid(expected));
        }
        self.position += expected.len();
        Ok(())
    }
    fn number(&mut self, layout: &str, width: usize, fixed: bool) -> Result<u32, Error> {
        let bytes = self.remaining();
        let count = bytes
            .iter()
            .take(width)
            .take_while(|b| b.is_ascii_digit())
            .count();
        if count == 0 || (fixed && count != width) {
            return Err(self.invalid(layout));
        }
        let value = bytes[..count]
            .iter()
            .fold(0, |n, b| n * 10 + u32::from(b - b'0'));
        self.position += count;
        Ok(value)
    }
}
pub(super) fn parse(value: &str) -> Result<Timestamp, Error> {
    let mut c = Cursor {
        raw: value,
        position: 0,
    };
    let year = c.number("2006", 4, true)?;
    c.prefix("-")?;
    let month = c.number("01", 2, true)?;
    if !(1..=12).contains(&month) {
        return Err(c.range("month"));
    }
    c.prefix("-")?;
    let day = c.number("02", 2, true)?;
    c.prefix("T")?;
    let hour = c.number("15", 2, false)?;
    if hour >= 24 {
        return Err(c.range("hour"));
    }
    c.prefix(":")?;
    let minute = c.number("04", 2, true)?;
    if minute >= 60 {
        return Err(c.range("minute"));
    }
    c.prefix(":")?;
    let second = c.number("05", 2, true)?;
    if second >= 60 {
        return Err(c.range("second"));
    }
    if c.remaining().len() >= 2
        && matches!(c.remaining()[0], b'.' | b',')
        && c.remaining()[1].is_ascii_digit()
    {
        c.position += 1;
        while c.remaining().first().is_some_and(u8::is_ascii_digit) {
            c.position += 1;
        }
    }
    let utc = c.remaining().starts_with(b"Z");
    if utc {
        c.position += 1;
    } else {
        let zone = c.remaining();
        if zone.len() < 6 || zone[3] != b':' {
            return Err(c.invalid("Z07:00"));
        }
        let number = |bytes: &[u8]| -> Option<u32> {
            bytes
                .iter()
                .all(u8::is_ascii_digit)
                .then(|| bytes.iter().fold(0, |n, b| n * 10 + u32::from(b - b'0')))
        };
        let hr = number(&zone[1..3]);
        let min = hr.and_then(|_| number(&zone[4..6]));
        if min.is_some_and(|x| x > 60) {
            return Err(c.range("time zone offset minute"));
        }
        if hr.is_some_and(|x| x > 24) {
            return Err(c.range("time zone offset hour"));
        }
        if hr.is_none() || min.is_none() || !matches!(zone[0], b'+' | b'-') {
            return Err(c.invalid("Z07:00"));
        }
        c.position += 6;
    }
    if !c.remaining().is_empty() {
        return Err(Error(format!(
            "parsing time {}: extra text: {}",
            quoted(value.as_bytes()),
            quoted(c.remaining())
        )));
    }
    if Date::from_calendar_date(
        year as i32,
        Month::try_from(month as u8).unwrap(),
        day as u8,
    )
    .is_err()
    {
        return Err(c.range("day"));
    }
    if !utc {
        return Err(Error("must be UTC".into()));
    }
    parse_timestamp(value).ok_or_else(|| c.invalid(LAYOUT))
}
