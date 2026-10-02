use super::{
    go_lower::lower,
    paths::clean,
    remote_url::{self, Url},
};
use crate::domain::{frontmatter::Error, yaml_string::YamlString};
pub const NO_REMOTE: &str = "repository: no remote URL (local-only repo)";
pub(super) fn quote(s: &[u8]) -> String {
    YamlString::from_bytes(s.into()).quoted()
}
fn trim(s: &[u8]) -> Vec<u8> {
    YamlString::from_bytes(s.into()).trimmed().as_bytes().into()
}
fn control(s: &[u8]) -> bool {
    s.iter().any(|b| matches!(b, 0 | b'\r' | b'\n'))
}
fn scp(s: &[u8]) -> Option<usize> {
    if s.windows(3).any(|p| p == b"://") {
        return None;
    }
    let colon = s.iter().position(|&b| b == b':')?;
    if colon <= 1 {
        return None;
    }
    if s.iter()
        .position(|b| matches!(b, b'/' | b'\\'))
        .is_some_and(|slash| slash < colon)
    {
        return None;
    }
    Some(colon)
}
fn strip_user(s: &[u8]) -> &[u8] {
    s.iter()
        .rposition(|&b| b == b'@')
        .map_or(s, |at| &s[at + 1..])
}
fn trim_slash(mut s: &[u8]) -> &[u8] {
    while s.ends_with(b"/") {
        s = &s[..s.len() - 1];
    }
    s
}
fn suffix(s: &[u8]) -> &[u8] {
    let s = trim_slash(s);
    s.strip_suffix(b".git").unwrap_or(s)
}
fn validate(s: &[u8]) -> Result<(), Error> {
    if s.is_empty() {
        return Err(Error::new("repository: remote_url is required".into()));
    }
    if control(s) {
        return Err(Error::new(
            "repository: remote_url contains control characters".into(),
        ));
    }
    if scp(s).is_some() {
        return Ok(());
    }
    let url = remote_url::parse(s)
        .map_err(|e| Error::new(format!("repository: remote_url parse: {e}")))?;
    match url.scheme.as_slice() {
        b"" => Ok(()),
        b"file" if url.user => Err(Error::new(
            "repository: file remote_url must not include userinfo".into(),
        )),
        b"file" => Ok(()),
        b"ssh" | b"git" | b"http" | b"https" => {
            if url.host.is_empty() {
                return Err(Error::new(format!(
                    "repository: {} remote_url requires a host",
                    String::from_utf8_lossy(&url.scheme)
                )));
            }
            if matches!(url.scheme.as_slice(), b"http" | b"https") && url.user {
                return Err(Error::new(
                    "repository: http(s) remote_url must not include userinfo".into(),
                ));
            }
            Ok(())
        }
        _ => Err(Error::new(format!(
            "repository: unsupported remote_url scheme {}",
            quote(&url.scheme)
        ))),
    }
}
pub fn validate_remote_url(remote: &[u8]) -> Result<(), Error> {
    validate(&trim(remote))
}
pub fn remote_host(remote: &[u8]) -> Result<Option<Vec<u8>>, Error> {
    let s = trim(remote);
    validate(&s)?;
    if let Some(colon) = scp(&s) {
        return Ok(Some(lower(strip_user(&s[..colon]))));
    }
    let url = remote_url::parse(&s)
        .map_err(|e| Error::new(format!("repository: remote_url parse: {e}")))?;
    if matches!(url.scheme.as_slice(), b"" | b"file") {
        return Ok(None);
    }
    Ok(Some(lower(remote_url::host_port(&url.host).0)))
}
pub fn normalize_remote_url(remote: &[u8]) -> Result<Vec<u8>, Error> {
    let s = trim(remote);
    if s.is_empty() {
        return Err(Error::new(NO_REMOTE.into()));
    }
    if control(&s) {
        return Err(Error::new(
            "repository: remote_url contains control characters".into(),
        ));
    }
    if let Some(colon) = scp(&s) {
        let host = lower(&trim(strip_user(&s[..colon])));
        if host.is_empty() {
            return Err(Error::new(
                "repository: NormalizeRemoteURL: empty host in SCP remote".into(),
            ));
        }
        let mut path = &s[colon + 1..];
        while path.starts_with(b"/") {
            path = &path[1..];
        }
        path = suffix(path);
        while path.starts_with(b"/") {
            path = &path[1..];
        }
        path = trim_slash(path);
        return Ok([b"https://".as_slice(), &host, b"/", path].concat());
    }
    let Url {
        scheme, host, path, ..
    } = remote_url::parse(&s)
        .map_err(|e| Error::new(format!("repository: NormalizeRemoteURL parse: {e}")))?;
    match scheme.as_slice() {
        b"http" | b"https" | b"ssh" | b"git" => {
            let (name, port) = remote_url::host_port(&host);
            let name = lower(name);
            if name.is_empty() {
                return Err(Error::new(
                    "repository: NormalizeRemoteURL: missing host in URL".into(),
                ));
            }
            let default = match scheme.as_slice() {
                b"http" => b"80".as_slice(),
                b"https" => b"443",
                b"ssh" => b"22",
                _ => b"9418",
            };
            let mut out = [b"https://".as_slice(), &name].concat();
            if !port.is_empty() && port != default {
                out.push(b':');
                out.extend(port);
            }
            let path = suffix(&path);
            out.extend(if path.is_empty() { b"/" } else { path });
            Ok(out)
        }
        b"file" => {
            if !host.is_empty() && !host.eq_ignore_ascii_case(b"localhost") {
                return Err(Error::new(format!(
                    "repository: NormalizeRemoteURL: file:// URL must not specify a host, got {}; use ssh:// for network file remotes",
                    quote(&host)
                )));
            }
            let path = suffix(&path);
            if path.is_empty() {
                return Err(Error::new(
                    "repository: NormalizeRemoteURL: empty path in file URL".into(),
                ));
            }
            Ok([b"file://".as_slice(), path].concat())
        }
        b"" => {
            if !s.starts_with(b"/") {
                return Err(Error::new(format!(
                    "repository: NormalizeRemoteURL: relative path {} is ambiguous as a canonical key; use an absolute path or file:// URL",
                    quote(&s)
                )));
            }
            let path = clean(&s);
            let path = path.strip_suffix(b".git").unwrap_or(&path);
            Ok([b"file://".as_slice(), path].concat())
        }
        _ => Err(Error::new(format!(
            "repository: NormalizeRemoteURL: unsupported scheme {}",
            quote(&scheme)
        ))),
    }
}
