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
        if value.value.len() == 40
            && value
                .value
                .iter()
                .all(|b| b.is_ascii_digit() || matches!(b, b'a'..=b'f'))
        {
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
