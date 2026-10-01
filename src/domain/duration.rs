//! time.ParseDuration-compatible signed nanoseconds, including its byte errors.
use super::frontmatter::Error;
const LIMIT: u64 = 1 << 63;
fn quoted(bytes: &[u8]) -> String {
    let mut out = String::from("\"");
    for &byte in bytes {
        match byte {
            b'"' | b'\\' => {
                out.push('\\');
                out.push(byte as char);
            }
            0..=31 | 128..=255 => out.push_str(&format!("\\x{byte:02x}")),
            _ => out.push(byte as char),
        }
    }
    out.push('"');
    out
}
pub fn parse_duration(original: &[u8]) -> Result<i64, Error> {
    let error = |message: &str| Error(format!("time: {message} {}", quoted(original)));
    let mut input = original;
    let negative = input.first() == Some(&b'-');
    if matches!(input.first(), Some(b'-' | b'+')) {
        input = &input[1..];
    }
    if input == b"0" {
        return Ok(0);
    }
    if input.is_empty() {
        return Err(error("invalid duration"));
    }
    let mut duration = 0u64;
    while !input.is_empty() {
        if !input[0].is_ascii_digit() && input[0] != b'.' {
            return Err(error("invalid duration"));
        }
        let mut integer = 0u64;
        let mut digits = 0;
        while input.first().is_some_and(u8::is_ascii_digit) {
            if integer > LIMIT / 10 {
                return Err(error("invalid duration"));
            }
            integer = integer * 10 + u64::from(input[0] - b'0');
            if integer > LIMIT {
                return Err(error("invalid duration"));
            }
            digits += 1;
            input = &input[1..];
        }
        let mut fraction = 0u64;
        let mut scale = 1f64;
        let mut fractional_digits = 0;
        if input.first() == Some(&b'.') {
            input = &input[1..];
            let mut overflow = false;
            while input.first().is_some_and(u8::is_ascii_digit) {
                let digit = u64::from(input[0] - b'0');
                input = &input[1..];
                fractional_digits += 1;
                if overflow {
                    continue;
                }
                if fraction > (LIMIT - 1) / 10 {
                    overflow = true;
                    continue;
                }
                let next = fraction * 10 + digit;
                if next > LIMIT {
                    overflow = true;
                    continue;
                }
                fraction = next;
                scale *= 10.;
            }
        }
        if digits == 0 && fractional_digits == 0 {
            return Err(error("invalid duration"));
        }
        let end = input
            .iter()
            .position(|b| b.is_ascii_digit() || *b == b'.')
            .unwrap_or(input.len());
        if end == 0 {
            return Err(error("missing unit in duration"));
        }
        let unit = match &input[..end] {
            b"ns" => 1,
            b"us" | b"\xc2\xb5s" | b"\xce\xbcs" => 1_000,
            b"ms" => 1_000_000,
            b"s" => 1_000_000_000,
            b"m" => 60_000_000_000,
            b"h" => 3_600_000_000_000,
            value => {
                return Err(error(&format!(
                    "unknown unit {} in duration",
                    quoted(value)
                )));
            }
        };
        input = &input[end..];
        if integer > LIMIT / unit {
            return Err(error("invalid duration"));
        }
        let mut value = integer * unit;
        if fraction > 0 {
            value += (fraction as f64 * (unit as f64 / scale)) as u64;
            if value > LIMIT {
                return Err(error("invalid duration"));
            }
        }
        // Preserve Go's unsigned accumulation, including two 2^63 terms
        // wrapping to zero before the range check.
        duration = duration.wrapping_add(value);
        if duration > LIMIT {
            return Err(error("invalid duration"));
        }
    }
    if negative {
        Ok((duration as i64).wrapping_neg())
    } else if duration > i64::MAX as u64 {
        Err(error("invalid duration"))
    } else {
        Ok(duration as i64)
    }
}
