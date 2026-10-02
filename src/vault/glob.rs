// Copyright 2010 The Go Authors. All rights reserved.
// Adaptation is governed by the BSD-style license in GO_LICENSE.
//! Linux filepath.Match/Glob semantics. Adapted from Go's BSD-licensed
//! path/filepath/match.go; see GO_LICENSE.
use super::paths::join;
use std::{
    ffi::OsString,
    fs,
    os::unix::ffi::{OsStrExt, OsStringExt},
    path::PathBuf,
};
fn path(bytes: &[u8]) -> PathBuf {
    OsString::from_vec(bytes.into()).into()
}
fn rune(bytes: &[u8]) -> (u32, usize) {
    for n in 1..=4.min(bytes.len()) {
        if let Ok(s) = std::str::from_utf8(&bytes[..n])
            && let Some(c) = s.chars().next()
        {
            return (c as u32, n);
        }
    }
    (0xfffd, 1)
}
fn escaped(bytes: &[u8]) -> Result<(u32, &[u8]), ()> {
    if bytes.is_empty() || matches!(bytes[0], b'-' | b']') {
        return Err(());
    }
    let bytes = if bytes[0] == b'\\' {
        &bytes[1..]
    } else {
        bytes
    };
    if bytes.is_empty() {
        return Err(());
    }
    let (c, n) = rune(bytes);
    if c == 0xfffd && n == 1 || n == bytes.len() {
        return Err(());
    }
    Ok((c, &bytes[n..]))
}
fn chunk<'a>(mut pattern: &[u8], mut name: &'a [u8]) -> Result<Option<&'a [u8]>, ()> {
    let mut failed = false;
    while !pattern.is_empty() {
        if name.is_empty() {
            failed = true;
        }
        match pattern[0] {
            b'[' => {
                let c = if failed {
                    0
                } else {
                    let (c, n) = rune(name);
                    name = &name[n..];
                    c
                };
                pattern = &pattern[1..];
                let negated = pattern.first() == Some(&b'^');
                if negated {
                    pattern = &pattern[1..];
                }
                let mut matched = false;
                let mut ranges = 0;
                loop {
                    if pattern.first() == Some(&b']') && ranges > 0 {
                        pattern = &pattern[1..];
                        break;
                    }
                    let (lo, rest) = escaped(pattern)?;
                    pattern = rest;
                    let hi = if pattern[0] == b'-' {
                        let (hi, rest) = escaped(&pattern[1..])?;
                        pattern = rest;
                        hi
                    } else {
                        lo
                    };
                    if lo <= c && c <= hi {
                        matched = true;
                    }
                    ranges += 1;
                }
                if matched == negated {
                    failed = true;
                }
            }
            b'?' => {
                if !failed {
                    if name[0] == b'/' {
                        failed = true;
                    }
                    let (_, n) = rune(name);
                    name = &name[n..];
                }
                pattern = &pattern[1..];
            }
            _ => {
                if pattern[0] == b'\\' {
                    pattern = &pattern[1..];
                    if pattern.is_empty() {
                        return Err(());
                    }
                }
                if !failed {
                    if pattern[0] != name[0] {
                        failed = true;
                    }
                    name = &name[1..];
                }
                pattern = &pattern[1..];
            }
        }
    }
    Ok((!failed).then_some(name))
}
pub(crate) fn matches(mut pattern: &[u8], mut name: &[u8]) -> Result<bool, ()> {
    'pattern: while !pattern.is_empty() {
        let mut star = false;
        while pattern.first() == Some(&b'*') {
            pattern = &pattern[1..];
            star = true;
        }
        let mut in_range = false;
        let mut i = 0;
        while i < pattern.len() {
            match pattern[i] {
                b'\\' if i + 1 < pattern.len() => {
                    i += 1;
                }
                b'[' => in_range = true,
                b']' => in_range = false,
                b'*' if !in_range => break,
                _ => (),
            }
            i += 1;
        }
        let current = &pattern[..i];
        pattern = &pattern[i..];
        if star && current.is_empty() {
            return Ok(!name.contains(&b'/'));
        }
        if let Some(rest) = chunk(current, name)?
            && (rest.is_empty() || !pattern.is_empty())
        {
            name = rest;
            continue;
        }
        if star {
            for i in 0..name.len() {
                if name[i] == b'/' {
                    break;
                }
                if let Some(rest) = chunk(current, &name[i + 1..])? {
                    if pattern.is_empty() && !rest.is_empty() {
                        continue;
                    }
                    name = rest;
                    continue 'pattern;
                }
            }
        }
        return Ok(false);
    }
    Ok(name.is_empty())
}
fn meta(bytes: &[u8]) -> bool {
    bytes
        .iter()
        .any(|b| matches!(b, b'*' | b'?' | b'[' | b'\\'))
}
pub(crate) fn glob(pattern: &[u8]) -> Result<Vec<Vec<u8>>, ()> {
    glob_depth(pattern, 0)
}
fn glob_depth(pattern: &[u8], depth: usize) -> Result<Vec<Vec<u8>>, ()> {
    if depth == 10_000 {
        return Err(());
    }
    matches(pattern, b"")?;
    if !meta(pattern) {
        return Ok(if fs::symlink_metadata(path(pattern)).is_ok() {
            vec![pattern.into()]
        } else {
            vec![]
        });
    }
    let split = pattern
        .iter()
        .rposition(|&b| b == b'/')
        .map_or(0, |i| i + 1);
    let raw_dir = &pattern[..split];
    let file = &pattern[split..];
    let dir = match raw_dir {
        b"" => b".".as_slice(),
        b"/" => raw_dir,
        _ => &raw_dir[..raw_dir.len() - 1],
    };
    let dirs = if !meta(dir) {
        vec![dir.to_vec()]
    } else {
        if dir == pattern {
            return Err(());
        }
        glob_depth(dir, depth + 1)?
    };
    let mut result = Vec::new();
    for dir in dirs {
        if !fs::metadata(path(&dir)).is_ok_and(|m| m.is_dir()) {
            continue;
        }
        let Ok(entries) = fs::read_dir(path(&dir)) else {
            continue;
        };
        let mut names: Vec<_> = entries
            .map_while(Result::ok)
            .map(|e| e.file_name().as_bytes().to_vec())
            .collect();
        names.sort();
        for name in names {
            if matches(file, &name)? {
                result.push(join(&[&dir, &name]));
            }
        }
    }
    Ok(result)
}

#[cfg(test)]
mod tests {
    #[test]
    fn match_patterns_agree_with_fixed_go_byte_and_unicode_rules() {
        let corpus: serde_json::Value =
            serde_json::from_str(include_str!("../../tests/fixtures/expected/index.json")).unwrap();
        for (i, case) in corpus["patterns"].as_array().unwrap().iter().enumerate() {
            let pattern: Vec<u8> = serde_json::from_value(case["pattern"].clone()).unwrap();
            let name: Vec<u8> = serde_json::from_value(case["name"].clone()).unwrap();
            let result = super::matches(&pattern, &name);
            assert_eq!(
                result.is_err(),
                case["error"].as_bool().unwrap(),
                "pattern case{i}"
            );
            if let Ok(matched) = result {
                assert_eq!(matched, case["match"].as_bool().unwrap(), "pattern case{i}");
            }
        }
    }
}
