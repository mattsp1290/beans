//! Best-effort read-only system Git queries. Mutation/recovery belongs to WP4.
use crate::domain::{frontmatter::Error, yaml_string::YamlString};
use std::{path::Path, process::Command};
#[derive(Default)]
pub struct GitCapture {
    pub value: Vec<u8>,
    pub found: bool,
    pub error: Option<Error>,
}
pub trait GitResolver {
    fn toplevel(&self, cwd: &Path) -> GitCapture;
    fn remote_url(&self, root: &Path) -> GitCapture;
    fn head_commit(&self, root: &Path) -> GitCapture;
    fn branch(&self, _root: &Path) -> GitCapture {
        GitCapture::default()
    }
}
pub struct SystemGit;
fn query(root: &Path, args: &[&str]) -> GitCapture {
    let mut cmd = Command::new("git");
    cmd.args(args);
    if !root.as_os_str().is_empty() {
        cmd.current_dir(root);
    }
    let Ok(output) = cmd.output() else {
        return GitCapture::default();
    };
    if !output.status.success() {
        return GitCapture::default();
    }
    let value = YamlString::from_bytes(output.stdout)
        .trimmed()
        .as_bytes()
        .to_vec();
    GitCapture {
        found: !value.is_empty(),
        value,
        error: None,
    }
}
impl GitResolver for SystemGit {
    fn toplevel(&self, cwd: &Path) -> GitCapture {
        query(cwd, &["rev-parse", "--show-toplevel"])
    }
    fn remote_url(&self, root: &Path) -> GitCapture {
        query(root, &["config", "--get", "remote.origin.url"])
    }
    fn head_commit(&self, root: &Path) -> GitCapture {
        let value = query(root, &["rev-parse", "HEAD"]);
        if is_full_lowercase_hex_commit(&value.value) {
            value
        } else {
            GitCapture::default()
        }
    }
    fn branch(&self, root: &Path) -> GitCapture {
        let value = query(root, &["rev-parse", "--abbrev-ref", "HEAD"]);
        if value.value == b"HEAD" {
            GitCapture::default()
        } else {
            value
        }
    }
}

fn is_full_lowercase_hex_commit(value: &[u8]) -> bool {
    value.len() == 40
        && value
            .iter()
            .all(|b| b.is_ascii_digit() || matches!(b, b'a'..=b'f'))
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn full_lowercase_commit_rejects_malformed_and_untrimmed_values() {
        assert!(is_full_lowercase_hex_commit(
            b"0123456789abcdef0123456789abcdef01234567"
        ));
        for invalid in [
            b"0123456789abcdef0123456789abcdef0123456".as_slice(),
            b"0123456789abcdef0123456789abcdef012345678",
            b"0123456789ABCDEF0123456789abcdef01234567",
            b"0123456789abcdef0123456789abcdef0123456g",
            b"0123456789abcdef0123456789abcdef01234567\n",
            b"HEAD",
        ] {
            assert!(!is_full_lowercase_hex_commit(invalid));
        }
    }
}
