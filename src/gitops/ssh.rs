//! Restrict SSH commands to simple OpenSSH invocations before adding policy.
use super::{Error, ErrorCategory, failure};
use std::{
    ffi::OsString,
    os::unix::ffi::{OsStrExt, OsStringExt},
    path::Path,
};

pub(super) fn noninteractive(command: OsString) -> Result<OsString, Error> {
    let bytes = command.as_bytes();
    let mut quote = None;
    let mut escaped = false;
    let mut word = Vec::new();
    let mut executable = None;
    let mut end = bytes.len();
    for (i, &byte) in bytes.iter().enumerate() {
        if escaped {
            word.push(byte);
            escaped = false;
            continue;
        }
        if quote == Some(b'\'') {
            if byte == b'\'' {
                quote = None;
            } else {
                word.push(byte);
            }
            continue;
        }
        match byte {
            b'\\' => escaped = true,
            b'\'' | b'"' if quote.is_none() => quote = Some(byte),
            b'"' if quote == Some(b'"') => quote = None,
            b'$' | b'`' => return Err(unsupported()),
            b';' | b'|' | b'&' | b'<' | b'>' | b'(' | b')' | b'\n' | b'\r' if quote.is_none() => {
                return Err(unsupported());
            }
            b'*' | b'?' | b'[' | b'~' if quote.is_none() => return Err(unsupported()),
            b' ' | b'\t' if quote.is_none() => {
                if executable.is_none() && !word.is_empty() {
                    executable = Some(std::mem::take(&mut word));
                    end = i;
                }
            }
            _ => word.push(byte),
        }
    }
    if quote.is_some() || escaped {
        return Err(unsupported());
    }
    let executable = executable.unwrap_or(word);
    if Path::new(std::ffi::OsStr::from_bytes(&executable)).file_name()
        != Some(std::ffi::OsStr::new("ssh"))
    {
        return Err(unsupported());
    }
    // OpenSSH uses the first value of each option. Place policy immediately
    // after the executable, preserving all original native bytes and quoting.
    let mut result = bytes[..end].to_vec();
    result.extend_from_slice(b" -o BatchMode=yes");
    result.extend_from_slice(&bytes[end..]);
    Ok(OsString::from_vec(result))
}
fn unsupported() -> Error {
    failure("SSH command must be a simple OpenSSH ssh invocation so bn can enforce BatchMode=yes; configure GIT_SSH_COMMAND or core.sshCommand without wrappers or shell expansions".into(), ErrorCategory::GitFailure)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn policy_precedes_conflicting_options_and_preserves_native_quoting() {
        let original = b"'/tmp/native\xff/ssh' -i 'key with spaces' -oBatchMode=no";
        assert_eq!(
            noninteractive(OsString::from_vec(original.to_vec()))
                .unwrap()
                .as_bytes(),
            b"'/tmp/native\xff/ssh' -o BatchMode=yes -i 'key with spaces' -oBatchMode=no"
        );
        let command = noninteractive("ssh -F /dev/null -o BatchMode=no".into()).unwrap();
        let output = std::process::Command::new("sh")
            .arg("-c")
            .arg(OsString::from_vec(
                [command.as_bytes(), b" -G example.invalid"].concat(),
            ))
            .output()
            .unwrap();
        assert!(output.status.success());
        assert!(
            output
                .stdout
                .split(|b| *b == b'\n')
                .any(|line| line == b"batchmode yes")
        );
    }
    #[test]
    fn environment_and_core_command_both_enforce_batch_mode() {
        const CHILD: &str = "BN_SSH_POLICY_CHILD";
        if std::env::var_os(CHILD).is_some() {
            let command = super::super::GitExecutor::default()
                .ssh_command(None, std::time::Duration::from_secs(2))
                .unwrap();
            let output = std::process::Command::new("sh")
                .arg("-c")
                .arg(OsString::from_vec(
                    [command.as_bytes(), b" -G example.invalid"].concat(),
                ))
                .output()
                .unwrap();
            assert!(output.status.success());
            assert!(
                output
                    .stdout
                    .split(|b| *b == b'\n')
                    .any(|line| line == b"batchmode yes")
            );
            assert!(
                output
                    .stdout
                    .split(|b| *b == b'\n')
                    .any(|line| line == b"identityfile key with spaces")
            );
            return;
        }
        let mut random = [0u8; 8];
        getrandom::fill(&mut random).unwrap();
        let config = std::env::temp_dir().join(format!(
            "bn-ssh-{}-{}",
            std::process::id(),
            u64::from_ne_bytes(random)
        ));
        std::fs::create_dir(&config).unwrap();
        std::fs::write(config.join("key with spaces"), b"fixture").unwrap();
        std::fs::write(
            config.join("config"),
            "[core]\nsshCommand = ssh -F /dev/null -i 'key with spaces' -o BatchMode=no\n",
        )
        .unwrap();
        for source in ["environment", "core"] {
            let mut child = std::process::Command::new(std::env::current_exe().unwrap());
            child.args(["--exact", "gitops::runner::ssh::tests::environment_and_core_command_both_enforce_batch_mode"])
                .env(CHILD, source).env("GIT_CONFIG_NOSYSTEM", "1")
                .env("GIT_CONFIG_GLOBAL", config.join("config")).env_remove("GIT_SSH_COMMAND")
                .current_dir(&config);
            if source == "environment" {
                child.env(
                    "GIT_SSH_COMMAND",
                    "ssh -F /dev/null -i 'key with spaces' -o BatchMode=no",
                );
            }
            assert!(child.status().unwrap().success(), "{source}");
        }
        std::fs::remove_dir_all(config).unwrap();
    }
    #[test]
    fn unsupported_commands_fail_before_execution() {
        for command in [
            "wrapper ssh",
            "ssh; echo unsafe",
            "ssh $(echo option)",
            "ssh \"$OPTIONS\"",
            "ssh 'unterminated",
        ] {
            assert!(noninteractive(command.into()).is_err(), "{command}");
        }
    }
}
