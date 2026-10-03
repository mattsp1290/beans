//! Finite Unix process-group boundary for every system-Git invocation.
#[path = "ssh.rs"]
mod ssh;
use crate::domain::{config::ExecutionPolicy, error::ErrorCategory, frontmatter::Error};
use std::{
    ffi::{OsStr, OsString},
    io::Read,
    os::{
        fd::AsRawFd,
        unix::process::{CommandExt, ExitStatusExt},
    },
    path::Path,
    process::{Child, Command, ExitStatus, Stdio},
    time::{Duration, Instant},
};

#[derive(Debug)]
pub struct GitOutput {
    pub stdout: Vec<u8>,
    pub stderr: Vec<u8>,
    pub status: ExitStatus,
}
impl GitOutput {
    pub fn checked(self, phase: &str) -> Result<Self, Error> {
        if self.status.success() {
            return Ok(self);
        }
        let mut message = format!("git {phase}: ").into_bytes();
        message.extend_from_slice(&self.stderr);
        if self.status.signal().is_some() {
            message.extend_from_slice(b" (terminated by signal)");
        }
        Err(
            Error::from_bytes(message).categorized(if self.status.signal().is_some() {
                ErrorCategory::GitSignaled
            } else {
                ErrorCategory::GitFailure
            }),
        )
    }
}
#[derive(Clone, Debug, Default)]
pub struct GitExecutor {
    pub policy: ExecutionPolicy,
}
struct ChildGuard {
    child: Child,
    reaped: bool,
    cleaned: bool,
}
impl ChildGuard {
    fn signal(&self, signal: i32) -> Result<(), Error> {
        if unsafe { libc::kill(-(self.child.id() as i32), signal) } < 0 {
            let e = std::io::Error::last_os_error();
            if e.raw_os_error() != Some(libc::ESRCH) {
                return Err(failure(
                    format!("Git process-group signaling failed: {e}"),
                    ErrorCategory::GitCleanup,
                ));
            }
        }
        Ok(())
    }
    fn cleanup(&mut self) -> Result<(), Error> {
        self.signal(libc::SIGTERM)?;
        std::thread::sleep(Duration::from_millis(250));
        self.signal(libc::SIGKILL)?;
        if !self.reaped {
            self.child.wait().map_err(|e| {
                failure(
                    format!("Git child reap failed: {e}"),
                    ErrorCategory::GitCleanup,
                )
            })?;
            self.reaped = true;
        }
        self.cleaned = true;
        Ok(())
    }
}
impl Drop for ChildGuard {
    fn drop(&mut self) {
        if !self.cleaned {
            let _ = self.cleanup();
        }
    }
}
fn failure(message: String, category: ErrorCategory) -> Error {
    Error::new(message).categorized(category)
}
fn nonblocking(fd: i32) -> Result<(), Error> {
    let flags = unsafe { libc::fcntl(fd, libc::F_GETFL) };
    if flags < 0 || unsafe { libc::fcntl(fd, libc::F_SETFL, flags | libc::O_NONBLOCK) } < 0 {
        return Err(failure(
            format!("Git pipe setup: {}", std::io::Error::last_os_error()),
            ErrorCategory::GitFailure,
        ));
    }
    Ok(())
}
fn drain(pipe: &mut impl Read, output: &mut Vec<u8>) -> Result<bool, Error> {
    let mut buf = [0u8; 16384];
    // Bound work per loop so an endlessly writing child cannot evade its deadline.
    for _ in 0..16 {
        match pipe.read(&mut buf) {
            Ok(0) => return Ok(true),
            Ok(n) => {
                if output.len().saturating_add(n) > 64 * 1024 * 1024 {
                    return Err(failure(
                        "Git output exceeds 64 MiB; semantic output was not truncated".into(),
                        ErrorCategory::GitFailure,
                    ));
                }
                output.extend_from_slice(&buf[..n]);
            }
            Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => return Ok(false),
            Err(e) if e.kind() == std::io::ErrorKind::Interrupted => continue,
            Err(e) => {
                return Err(failure(
                    format!("Git output read: {e}"),
                    ErrorCategory::GitFailure,
                ));
            }
        }
    }
    Ok(false)
}
impl GitExecutor {
    pub fn run<I, S>(&self, dir: Option<&Path>, args: I, phase: &str) -> Result<GitOutput, Error>
    where
        I: IntoIterator<Item = S>,
        S: AsRef<OsStr>,
    {
        let at = Instant::now();
        let result = self.run_inner(dir, args, phase);
        let outcome = match &result {
            Ok(out) if out.status.success() => "success",
            Ok(out) if out.status.signal().is_some() => "signaled",
            Ok(_) => "nonzero",
            Err(_) => super::diagnostics::result(&result),
        };
        super::diagnostics::emit(
            self.policy.diagnostics,
            "git_step",
            phase,
            at.elapsed(),
            outcome,
            None,
        );
        result
    }
    fn ssh_command(&self, dir: Option<&Path>, budget: Duration) -> Result<OsString, Error> {
        use std::os::unix::ffi::{OsStrExt, OsStringExt};
        let command = if let Some(command) = std::env::var_os("GIT_SSH_COMMAND") {
            command
        } else {
            let mut query = self.clone();
            query.policy.command_timeout = query.policy.command_timeout.min(budget);
            let output = query.run(dir, ["config", "--get", "core.sshCommand"], "config")?;
            if output.status.success() {
                OsString::from_vec(output.stdout.trim_ascii_end().to_vec())
            } else if output.status.code() == Some(1) {
                if let Some(executable) = std::env::var_os("GIT_SSH") {
                    let mut quoted = vec![b'\''];
                    for &byte in executable.as_bytes() {
                        if byte == b'\'' {
                            quoted.extend_from_slice(b"'\\''");
                        } else {
                            quoted.push(byte);
                        }
                    }
                    quoted.push(b'\'');
                    OsString::from_vec(quoted)
                } else {
                    OsString::from("ssh")
                }
            } else {
                return Err(output.checked("config").unwrap_err());
            }
        };
        ssh::noninteractive(command)
    }
    fn run_inner<I, S>(&self, dir: Option<&Path>, args: I, phase: &str) -> Result<GitOutput, Error>
    where
        I: IntoIterator<Item = S>,
        S: AsRef<OsStr>,
    {
        let timeout = if phase == "cleanup" {
            self.policy.cleanup_timeout
        } else if matches!(phase, "fetch" | "push" | "clone" | "ls-remote") {
            self.policy.network_timeout
        } else {
            self.policy.command_timeout
        };
        let start = Instant::now();
        let ssh = if matches!(phase, "fetch" | "push" | "clone" | "ls-remote") {
            Some(self.ssh_command(dir, timeout)?)
        } else {
            None
        };
        let mut cmd = Command::new("git");
        if let Some(dir) = dir {
            cmd.arg("-C").arg(dir);
        }
        cmd.args(args)
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .env("GIT_TERMINAL_PROMPT", "0")
            .env("GIT_COMMITTER_NAME", "bn")
            .env("GIT_COMMITTER_EMAIL", "bn@localhost")
            .env("LC_ALL", "C")
            .process_group(0);
        if let Some(command) = ssh {
            cmd.env("GIT_SSH_COMMAND", command);
        }
        if start.elapsed() >= timeout {
            return Err(failure(
                format!("git {phase} timed out before spawn"),
                ErrorCategory::GitTimeout,
            ));
        }
        let child = cmd
            .spawn()
            .map_err(|e| failure(format!("Git spawn: {e}"), ErrorCategory::GitFailure))?;
        let mut guard = ChildGuard {
            child,
            reaped: false,
            cleaned: false,
        };
        let mut stdout = guard.child.stdout.take().expect("piped stdout");
        let mut stderr = guard.child.stderr.take().expect("piped stderr");
        nonblocking(stdout.as_raw_fd())?;
        nonblocking(stderr.as_raw_fd())?;
        let mut out = Vec::new();
        let mut err = Vec::new();
        let mut status = None;
        let mut out_done = false;
        let mut err_done = false;
        loop {
            if !out_done {
                out_done = drain(&mut stdout, &mut out)?;
            }
            if !err_done {
                err_done = drain(&mut stderr, &mut err)?;
            }
            if status.is_none() {
                status = guard
                    .child
                    .try_wait()
                    .map_err(|e| failure(format!("Git wait: {e}"), ErrorCategory::GitFailure))?;
                guard.reaped = status.is_some();
            }
            if out_done
                && err_done
                && let Some(status) = status
            {
                // A hook may close both pipes and keep running after its leader exits.
                if unsafe { libc::kill(-(guard.child.id() as i32), 0) } == 0 {
                    guard.cleanup()?;
                } else {
                    guard.cleaned = true;
                }
                return Ok(GitOutput {
                    stdout: out,
                    stderr: err,
                    status,
                });
            }
            if start.elapsed() >= timeout {
                guard.cleanup()?;
                return Err(failure(
                    format!("git {phase} timed out; process group terminated"),
                    ErrorCategory::GitTimeout,
                ));
            }
            std::thread::sleep(
                Duration::from_millis(5).min(timeout.saturating_sub(start.elapsed())),
            );
        }
    }
}
