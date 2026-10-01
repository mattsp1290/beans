use crate::domain::{frontmatter::Error, yaml_string::YamlString};
use std::{
    ffi::{OsStr, OsString},
    os::unix::ffi::{OsStrExt, OsStringExt},
    path::PathBuf,
};

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Paths {
    pub home: PathBuf,
    pub hub: PathBuf,
    pub cache: PathBuf,
    pub config: PathBuf,
}

// filepath.Clean on Linux is lexical and preserves arbitrary filename bytes.
pub(crate) fn clean(path: &[u8]) -> Vec<u8> {
    let rooted = path.starts_with(b"/");
    let mut parts: Vec<&[u8]> = Vec::new();
    for part in path.split(|&b| b == b'/') {
        match part {
            b"" | b"." => {}
            b".." => {
                if parts.last().is_some_and(|last| *last != b"..") {
                    parts.pop();
                } else if !rooted {
                    parts.push(part);
                }
            }
            _ => parts.push(part),
        }
    }
    let mut result = if rooted { vec![b'/'] } else { Vec::new() };
    for part in parts {
        if !result.is_empty() && !result.ends_with(b"/") {
            result.push(b'/');
        }
        result.extend_from_slice(part);
    }
    if result.is_empty() {
        result.push(b'.');
    }
    result
}

// Go Join concatenates before cleaning; PathBuf::push resets on absolute parts.
pub(crate) fn join(parts: &[&[u8]]) -> Vec<u8> {
    let parts: Vec<_> = parts.iter().copied().filter(|p| !p.is_empty()).collect();
    if parts.is_empty() {
        return Vec::new();
    }
    clean(&parts.join(&b'/'))
}

fn trimmed(value: &OsStr) -> Vec<u8> {
    YamlString::from_bytes(value.as_bytes().into())
        .trimmed()
        .as_bytes()
        .into()
}
fn path(bytes: Vec<u8>) -> PathBuf {
    OsString::from_vec(bytes).into()
}

pub fn default_paths(flag_hub: &OsStr) -> Result<Paths, Error> {
    default_paths_with(
        flag_hub,
        |key| std::env::var_os(key).unwrap_or_default(),
        || {
            std::env::current_dir().map_err(|e| {
                let message = e.to_string();
                Error::new(format!(
                    "getwd: {}",
                    message
                        .split(" (os error ")
                        .next()
                        .unwrap_or(&message)
                        .to_lowercase()
                ))
            })
        },
    )
}

/// Injectable environment and lazy working directory lookup, without changing
/// process-wide environment in tests or concurrent command adapters.
pub fn default_paths_with(
    flag_hub: &OsStr,
    env: impl Fn(&str) -> OsString,
    cwd: impl FnOnce() -> Result<PathBuf, Error>,
) -> Result<Paths, Error> {
    let mut home = trimmed(&env("BEANS_HOME"));
    if home.is_empty() {
        let user_home = env("HOME");
        if user_home.is_empty() {
            return Err(Error::new(
                "resolve home directory: $HOME is not defined".into(),
            ));
        }
        home = join(&[user_home.as_bytes(), b".beans"]);
    }
    let mut hub = trimmed(flag_hub);
    if hub.is_empty() {
        hub = trimmed(&env("BEANS_HUB"));
    }
    if hub.is_empty() {
        hub = join(&[&home, b"hub"]);
    }
    if !hub.starts_with(b"/") {
        hub = join(&[cwd()?.as_os_str().as_bytes(), &hub]);
    }
    Ok(Paths {
        cache: path(join(&[&home, b"cache"])),
        config: path(join(&[&home, b"config.toml"])),
        home: path(home),
        hub: path(hub),
    })
}

pub fn check_hub(paths: &Paths) -> Result<(), Error> {
    let git = path(join(&[paths.hub.as_os_str().as_bytes(), b".git"]));
    if std::fs::metadata(git).is_ok_and(|meta| meta.is_dir()) {
        return Ok(());
    }
    Err(Error::new(format!(
        "no hub found; run bn init <remote> (expected a clone at {})",
        YamlString::from_bytes(paths.hub.as_os_str().as_bytes().into())
    )))
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn lexical_paths_match_fixed_go() {
        let fixture: serde_json::Value =
            serde_json::from_str(include_str!("../../tests/contract/paths.json")).unwrap();
        for case in fixture["lexical"].as_array().unwrap() {
            let parts: Vec<Vec<u8>> = serde_json::from_value(case["Parts"].clone()).unwrap();
            let expected_clean: Vec<u8> = serde_json::from_value(case["Clean"].clone()).unwrap();
            let expected_join: Vec<u8> = serde_json::from_value(case["Joined"].clone()).unwrap();
            assert_eq!(clean(&parts[0]), expected_clean, "{parts:?}");
            assert_eq!(join(&[&parts[0], &parts[1]]), expected_join, "{parts:?}");
        }
    }
}
