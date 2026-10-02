// Copyright 2009 The Go Authors. All rights reserved.
// Adapted from Go 1.25.7 net/url or net/netip; see GO_LICENSE for BSD terms.
//! Native remote URL authority/path parsing with decoded byte retention.
//! Parsing retains decoded filename bytes and does not normalize URL paths.
use super::remote::quote;
#[derive(Default)]
pub(super) struct Url {
    pub scheme: Vec<u8>,
    pub host: Vec<u8>,
    pub path: Vec<u8>,
    pub user: bool,
}
#[derive(Clone, Copy)]
enum Mode {
    Path,
    Host,
    Zone,
}
fn host_byte(b: u8) -> bool {
    b.is_ascii_alphanumeric() || b"!$&'()*+,;=:[]<>\"-_.~".contains(&b)
}
fn hex(b: u8) -> Option<u8> {
    match b {
        b'0'..=b'9' => Some(b - b'0'),
        b'a'..=b'f' => Some(b - b'a' + 10),
        b'A'..=b'F' => Some(b - b'A' + 10),
        _ => None,
    }
}
fn unescape(s: &[u8], mode: Mode) -> Result<Vec<u8>, String> {
    let mut out = Vec::new();
    let mut i = 0;
    while i < s.len() {
        let b = s[i];
        if b == b'%' {
            let parsed = s
                .get(i + 1)
                .copied()
                .and_then(hex)
                .zip(s.get(i + 2).copied().and_then(hex));
            let Some((hi, lo)) = parsed else {
                return Err(format!(
                    "invalid URL escape {}",
                    quote(&s[i..s.len().min(i + 3)])
                ));
            };
            let v = hi * 16 + lo;
            let pct = &s[i..i + 3] == b"%25";
            if matches!(mode, Mode::Host) && v < 128 && !pct
                || matches!(mode, Mode::Zone) && !pct && v != b' ' && !host_byte(v)
            {
                return Err(format!("invalid URL escape {}", quote(&s[i..i + 3])));
            }
            out.push(v);
            i += 3;
        } else {
            if matches!(mode, Mode::Host | Mode::Zone) && b < 128 && !host_byte(b) {
                return Err(format!(
                    "invalid character {} in host name",
                    quote(&s[i..i + 1])
                ));
            }
            out.push(b);
            i += 1;
        }
    }
    Ok(out)
}
pub(super) fn optional_port(s: &[u8]) -> bool {
    s.is_empty() || s[0] == b':' && s[1..].iter().all(u8::is_ascii_digit)
}
fn host(s: &[u8]) -> Result<Vec<u8>, String> {
    if let Some(open) = s.iter().rposition(|&b| b == b'[') {
        let close = s
            .iter()
            .rposition(|&b| b == b']')
            .ok_or("missing ']' in host")?;
        if close < open {
            return Err("missing ']' in host".into());
        }
        let port = &s[close + 1..];
        if !optional_port(port) {
            return Err(format!("invalid port {} after host", quote(port)));
        }
        let port = unescape(port, Mode::Host)?;
        let name = &s[open + 1..close];
        let decoded = if let Some(zone) = name.windows(3).position(|p| p == b"%25") {
            let mut host = unescape(&name[..zone], Mode::Host)?;
            host.extend(unescape(&name[zone..], Mode::Zone)?);
            host
        } else {
            unescape(name, Mode::Host)?
        };
        super::remote_ip::validate(&decoded)?;
        let mut host = vec![b'['];
        host.extend(decoded);
        host.push(b']');
        host.extend(port);
        return Ok(host);
    }
    if let Some(colon) = s.iter().rposition(|&b| b == b':')
        && !optional_port(&s[colon..])
    {
        return Err(format!("invalid port {} after host", quote(&s[colon..])));
    }
    unescape(s, Mode::Host)
}
fn scheme(s: &[u8]) -> Result<(&[u8], &[u8]), String> {
    for (i, &b) in s.iter().enumerate() {
        match b {
            b'a'..=b'z' | b'A'..=b'Z' => {}
            b'0'..=b'9' | b'+' | b'-' | b'.' if i > 0 => {}
            b':' if i == 0 => return Err("missing protocol scheme".into()),
            b':' => return Ok((&s[..i], &s[i + 1..])),
            _ => break,
        }
    }
    Ok((&[], s))
}
fn parse_inner(raw: &[u8]) -> Result<Url, String> {
    if raw.iter().any(|&b| b < 32 || b == 127) {
        return Err("net/url: invalid control character in URL".into());
    }
    if raw == b"*" {
        return Ok(Url {
            path: raw.into(),
            ..Url::default()
        });
    }
    let (scheme, rest) = scheme(raw)?;
    let mut url = Url {
        scheme: scheme.to_ascii_lowercase(),
        ..Url::default()
    };
    let mut rest = rest.split(|&b| b == b'?').next().unwrap();
    if !rest.starts_with(b"/") {
        if !url.scheme.is_empty() {
            return Ok(url);
        }
        if rest.split(|&b| b == b'/').next().unwrap().contains(&b':') {
            return Err("first path segment in URL cannot contain colon".into());
        }
    }
    if (!url.scheme.is_empty() || !rest.starts_with(b"///")) && rest.starts_with(b"//") {
        let authority = &rest[2..];
        let end = authority
            .iter()
            .position(|&b| b == b'/')
            .unwrap_or(authority.len());
        rest = &authority[end..];
        let authority = &authority[..end];
        if let Some(at) = authority.iter().rposition(|&b| b == b'@') {
            url.host = host(&authority[at + 1..])?;
            let user = &authority[..at];
            if !user
                .iter()
                .all(|b| b.is_ascii_alphanumeric() || b"-._:~!$&'()*+,;=%@".contains(b))
            {
                return Err("net/url: invalid userinfo".into());
            }
            if let Some(colon) = user.iter().position(|&b| b == b':') {
                unescape(&user[..colon], Mode::Path)?;
                unescape(&user[colon + 1..], Mode::Path)?;
            } else {
                unescape(user, Mode::Path)?;
            }
            url.user = true;
        } else {
            url.host = host(authority)?;
        }
    }
    url.path = unescape(rest, Mode::Path)?;
    Ok(url)
}
pub(super) fn parse(raw: &[u8]) -> Result<Url, String> {
    let split = raw.iter().position(|&b| b == b'#').unwrap_or(raw.len());
    let url =
        parse_inner(&raw[..split]).map_err(|e| format!("parse {}: {e}", quote(&raw[..split])))?;
    if split < raw.len() {
        unescape(&raw[split + 1..], Mode::Path)
            .map_err(|e| format!("parse {}: {e}", quote(raw)))?;
    }
    Ok(url)
}
pub(super) fn host_port(s: &[u8]) -> (&[u8], &[u8]) {
    let (mut host, port) = if let Some(colon) = s
        .iter()
        .rposition(|&b| b == b':')
        .filter(|&i| optional_port(&s[i..]))
    {
        (&s[..colon], &s[colon + 1..])
    } else {
        (s, &b""[..])
    };
    if host.starts_with(b"[") && host.ends_with(b"]") {
        host = &host[1..host.len() - 1];
    }
    (host, port)
}
