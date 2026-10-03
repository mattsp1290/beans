//! Best-effort read-only system Git queries; mutations have a separate locked boundary.
use crate::domain::{frontmatter::Error, yaml_string::YamlString};
use std::path::Path;
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
pub struct PolicyGit(pub crate::gitops::GitExecutor);
fn query(executor: &crate::gitops::GitExecutor, root: &Path, args: &[&str]) -> GitCapture {
    let output = match executor
        .run(
            if root.as_os_str().is_empty() {
                None
            } else {
                Some(root)
            },
            args,
            args[0],
        )
        .and_then(|o| o.checked(args[0]))
    {
        Ok(output) => output,
        Err(e) => {
            return GitCapture {
                error: Some(e),
                ..Default::default()
            };
        }
    };
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
        PolicyGit(Default::default()).toplevel(cwd)
    }
    fn remote_url(&self, cwd: &Path) -> GitCapture {
        PolicyGit(Default::default()).remote_url(cwd)
    }
    fn head_commit(&self, cwd: &Path) -> GitCapture {
        PolicyGit(Default::default()).head_commit(cwd)
    }
    fn branch(&self, cwd: &Path) -> GitCapture {
        PolicyGit(Default::default()).branch(cwd)
    }
}
impl GitResolver for PolicyGit {
    fn toplevel(&self, cwd: &Path) -> GitCapture {
        query(&self.0, cwd, &["rev-parse", "--show-toplevel"])
    }
    fn remote_url(&self, root: &Path) -> GitCapture {
        query(&self.0, root, &["config", "--get", "remote.origin.url"])
    }
    fn head_commit(&self, root: &Path) -> GitCapture {
        let value = query(&self.0, root, &["rev-parse", "HEAD"]);
        if is_full_lowercase_hex_commit(&value.value) {
            value
        } else {
            GitCapture::default()
        }
    }
    fn branch(&self, root: &Path) -> GitCapture {
        let value = query(&self.0, root, &["rev-parse", "--abbrev-ref", "HEAD"]);
        if value.value == b"HEAD" {
            GitCapture {
                error: value.error,
                ..Default::default()
            }
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
