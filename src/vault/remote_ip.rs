// Copyright 2020 The Go Authors. All rights reserved.
// Adapted from Go 1.25.7 net/url or net/netip; see GO_LICENSE for BSD terms.
//! Native remote IP-literal authority validation.
use super::remote::quote;
fn error(input: &[u8], message: &str, at: &[u8]) -> String {
    let mut out = format!("invalid host: ParseAddr({}): {message}", quote(input));
    if !at.is_empty() {
        out.push_str(&format!(" (at {})", quote(at)));
    }
    out
}
fn ipv4(input: &[u8], s: &[u8]) -> Result<(), String> {
    let mut value = 0u32;
    let mut digits = 0;
    let mut position = 0;
    for (i, &b) in s.iter().enumerate() {
        if b.is_ascii_digit() {
            if digits == 1 && value == 0 {
                return Err(error(input, "IPv4 field has octet with leading zero", b""));
            }
            value = value * 10 + u32::from(b - b'0');
            digits += 1;
            if value > 255 {
                return Err(error(input, "IPv4 field has value >255", b""));
            }
        } else if b == b'.' {
            if i == 0 || i == s.len() - 1 || s[i - 1] == b'.' {
                return Err(error(
                    input,
                    "IPv4 field must have at least one digit",
                    &s[i..],
                ));
            }
            if position == 3 {
                return Err(error(input, "IPv4 address too long", b""));
            }
            position += 1;
            value = 0;
            digits = 0;
        } else {
            return Err(error(input, "unexpected character", &s[i..]));
        }
    }
    if position < 3 {
        return Err(error(input, "IPv4 address too short", b""));
    }
    Ok(())
}
pub(super) fn validate(input: &[u8]) -> Result<(), String> {
    match input.iter().find(|&&b| matches!(b, b'.' | b':' | b'%')) {
        Some(b'.') => {
            ipv4(input, input)?;
            return Err("invalid IP-literal".into());
        }
        Some(b'%') => return Err(error(input, "missing IPv6 address", b"")),
        Some(b':') => {}
        _ => return Err(error(input, "unable to parse IP", b"")),
    }
    let mut s = input;
    if let Some(zone) = s.iter().position(|&b| b == b'%') {
        if zone + 1 == s.len() {
            return Err(error(input, "zone must be a non-empty string", b""));
        }
        s = &s[..zone];
    }
    let mut ellipsis = false;
    let mut used = 0;
    if s.starts_with(b"::") {
        ellipsis = true;
        s = &s[2..];
        if s.is_empty() {
            return Ok(());
        }
    }
    while used < 16 {
        let count = s.iter().take_while(|b| b.is_ascii_hexdigit()).count();
        if count > 4 {
            return Err(error(input, "each group must have 4 or less digits", s));
        }
        if count == 0 {
            return Err(error(
                input,
                "each colon-separated field must have at least one digit",
                s,
            ));
        }
        if s.get(count) == Some(&b'.') {
            if !ellipsis && used != 12 {
                return Err(error(
                    input,
                    "embedded IPv4 address must replace the final 2 fields of the address",
                    s,
                ));
            }
            if used + 4 > 16 {
                return Err(error(
                    input,
                    "too many hex fields to fit an embedded IPv4 at the end of the address",
                    s,
                ));
            }
            ipv4(input, s)?;
            s = b"";
            used += 4;
            break;
        }
        used += 2;
        s = &s[count..];
        if s.is_empty() {
            break;
        }
        if s[0] != b':' {
            return Err(error(input, "unexpected character, want colon", s));
        }
        if s.len() == 1 {
            return Err(error(input, "colon must be followed by more characters", s));
        }
        s = &s[1..];
        if s[0] == b':' {
            if ellipsis {
                return Err(error(input, "multiple :: in address", s));
            }
            ellipsis = true;
            s = &s[1..];
            if s.is_empty() {
                break;
            }
        }
    }
    if !s.is_empty() {
        return Err(error(input, "trailing garbage after address", s));
    }
    if used < 16 && !ellipsis {
        return Err(error(input, "address string too short", b""));
    }
    if used == 16 && ellipsis {
        return Err(error(
            input,
            "the :: must expand to at least one field of zeros",
            b"",
        ));
    }
    Ok(())
}
