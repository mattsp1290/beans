use super::{Hub, io_error};
use crate::domain::{error::ErrorCategory, frontmatter::Error};
use beans_kernel::retry::{AttemptBudget, take_push_attempt};
use std::fs;
impl Hub {
    pub fn raw_git<I, S>(&self, args: I, phase: &str) -> Result<crate::gitops::GitOutput, Error>
    where
        I: IntoIterator<Item = S>,
        S: AsRef<std::ffi::OsStr>,
    {
        self.executor.run(Some(&self.dir), args, phase)
    }
    pub fn git(&self, args: &[&str]) -> Result<String, Error> {
        let phase = if args == ["rebase", "--abort"] {
            "cleanup"
        } else {
            let mut index = 0;
            while args.get(index) == Some(&"-c") {
                index += 2;
            }
            args.get(index).copied().unwrap_or("unknown")
        };
        let out = self.raw_git(args, phase)?.checked(phase)?;
        String::from_utf8(out.stdout)
            .map(|s| s.trim().to_owned())
            .map_err(|_| {
                Error::new("Git semantic output is not UTF-8".into())
                    .categorized(ErrorCategory::GitFailure)
            })
    }
    pub(super) fn push(&self) -> Result<(), (bool, Error)> {
        let args = [
            "push",
            "--porcelain",
            "origin",
            &format!("HEAD:refs/heads/{}", self.branch),
        ];
        let out = self.raw_git(args, "push").map_err(|e| (false, e))?;
        let outcome = crate::gitops::push::classify_push(&out, &self.branch);
        if outcome == crate::gitops::push::PushOutcome::Published {
            return Ok(());
        }
        let error = out.checked("push").unwrap_err();
        Err((
            outcome == crate::gitops::push::PushOutcome::Contention,
            error,
        ))
    }
    pub(super) fn worktree_clean(&self) -> Result<bool, Error> {
        Ok(self
            .raw_git(["status", "--porcelain", "-z"], "status")?
            .checked("status")?
            .stdout
            .is_empty())
    }
    pub fn preflight(&self) -> Result<(), Error> {
        for marker in ["rebase-merge", "rebase-apply", "MERGE_HEAD"] {
            if self.dir.join(".git").join(marker).exists() {
                return Err(Error::new(
                    "hub has an interrupted rebase or merge; abort it and retry".into(),
                )
                .categorized(ErrorCategory::GitConflict));
            }
        }
        let output = self.raw_git(["symbolic-ref", "--short", "-q", "HEAD"], "symbolic-ref")?;
        if output.status.code() == Some(1) {
            return Err(Error::new(
                "hub is on a detached HEAD; checkout the configured hub branch".into(),
            )
            .categorized(ErrorCategory::GitConflict));
        }
        let branch = String::from_utf8(output.checked("symbolic-ref")?.stdout).map_err(|_| {
            Error::new("invalid Git branch bytes".into()).categorized(ErrorCategory::GitFailure)
        })?;
        let branch = branch.trim();
        if branch != self.branch {
            return Err(Error::new(format!(
                "hub is on {branch}, expected {}; checkout the configured hub branch",
                self.branch
            ))
            .categorized(ErrorCategory::GitConflict));
        }
        Ok(())
    }
    pub(super) fn commit(&self, subject: &str, nonce: Option<&str>) -> Result<(), Error> {
        let name = self.actor.replace(['\n', '\r', '<', '>'], " ");
        let name = if name.trim().is_empty() {
            "bn"
        } else {
            name.trim()
        };
        let mut args = vec![
            "-c",
            "user.name=bn",
            "-c",
            "user.email=bn@localhost",
            "commit",
            "--quiet",
            "-m",
            subject,
        ];
        let trailer = nonce.map(|n| format!("Bn-Run: {n}"));
        if let Some(t) = &trailer {
            args.extend(["-m", t]);
        }
        let author = format!("{name} <bn@localhost>");
        args.extend(["--author", &author]);
        self.git(&args).map(|_| ())
    }
    pub(super) fn clear_journal(&self) -> Result<(), Error> {
        self.state.remove("op-journal.json")
    }
    pub(super) fn commit_strays(&self) -> Result<(), Error> {
        self.remove_owned_temps()?;
        if self.worktree_clean()? {
            return Ok(());
        }
        let recovered = self.state.exists("op-journal.json")?;
        self.git(&["add", "-A"])?;
        self.commit(
            if recovered {
                "bn: recovered partial operation"
            } else {
                "bn: hand edits"
            },
            None,
        )?;
        Ok(())
    }
    pub(super) fn abort_rebase(&self, original: &Error) -> Result<(), Error> {
        self.git(&["rebase", "--abort"])
            .map(|_| ())
            .map_err(|abort| {
                let prefix = [
                    original.as_bytes(),
                    b"; bounded rebase abort failed; preserve files and resolve manually",
                ]
                .concat();
                let error = abort.context(&prefix);
                if matches!(
                    original.category(),
                    Some(
                        ErrorCategory::GitTimeout
                            | ErrorCategory::GitSignaled
                            | ErrorCategory::GitCleanup
                    )
                ) {
                    error.categorized(original.category().unwrap())
                } else {
                    error
                }
            })
    }
    pub(super) fn rebase(&self) -> Result<(), Error> {
        if let Err(e) = self.git(&["rebase", "--quiet", &self.remote_ref()]) {
            self.abort_rebase(&e)?;
            let error = e.context(
                b"hub local commits conflict with origin; preserved local history; run bn sync",
            );
            return Err(if error.category() == Some(ErrorCategory::GitFailure) {
                error.categorized(ErrorCategory::GitConflict)
            } else {
                error
            });
        }
        Ok(())
    }
    pub(super) fn remote_ref(&self) -> String {
        format!("origin/{}", self.branch)
    }
    pub(super) fn ahead(&self) -> Result<usize, Error> {
        self.git(&[
            "rev-list",
            "--count",
            &format!("{}..HEAD", self.remote_ref()),
        ])?
        .parse()
        .map_err(|_| {
            Error::new("cannot determine local commit count".into())
                .categorized(ErrorCategory::GitFailure)
        })
    }
    pub(super) fn owned_head(&self, nonce: &str) -> Result<Option<String>, Error> {
        if self.ahead()? != 1 {
            return Ok(None);
        }
        let trailer = self.git(&["log", "-1", "--format=%(trailers:key=Bn-Run,valueonly)"])?;
        if trailer != nonce {
            return Ok(None);
        }
        self.git(&["rev-parse", "HEAD"]).map(Some)
    }
    pub(super) fn nonce_present(&self, nonce: &str) -> Result<bool, Error> {
        let messages = self.git(&["log", "--format=%(trailers:key=Bn-Run,valueonly)"])?;
        Ok(messages.lines().any(|line| line.trim() == nonce))
    }
    pub(super) fn remove_owned_temps(&self) -> Result<(), Error> {
        for (relative, evidence) in crate::gitops::hub_file::owned_orphans(&self.dir)? {
            let args = [
                std::ffi::OsStr::new("ls-files"),
                std::ffi::OsStr::new("-z"),
                std::ffi::OsStr::new("--"),
                relative.as_os_str(),
            ];
            let output = self.raw_git(args, "ls-files")?.checked("ls-files")?;
            if output.stdout.is_empty() {
                crate::gitops::hub_file::remove_registered_orphan(&self.dir, &relative, &evidence)?;
            }
        }
        Ok(())
    }
    pub fn initialize(&self, remote: &str) -> Result<(), Error> {
        crate::gitops::diagnostics::operation(
            self.executor.policy.diagnostics,
            "initialize",
            || self.initialize_inner(remote),
        )
    }
    pub(super) fn initialize_inner(&self, remote: &str) -> Result<(), Error> {
        let _lock = self.lock()?;
        self.legacy_gate()?;
        if self.dir.exists() {
            return Err(Error::new(format!(
                "hub path already exists: {}",
                self.dir.display()
            )));
        }
        if let Some(p) = self.dir.parent() {
            fs::create_dir_all(p).map_err(io_error)?;
        }
        let args = [
            std::ffi::OsStr::new("clone"),
            std::ffi::OsStr::new("--"),
            std::ffi::OsStr::new(remote),
            self.dir.as_os_str(),
        ];
        self.executor.run(None, args, "clone")?.checked("clone")?;
        let head = self.raw_git(["rev-parse", "--verify", "HEAD"], "rev-parse")?;
        if head.status.success() {
            self.git(&["checkout", "--quiet", &self.branch])?;
        } else {
            if head.status.code() != Some(128)
                || head.stderr.trim_ascii() != b"fatal: Needed a single revision"
            {
                return Err(head.checked("rev-parse").unwrap_err());
            }
            self.git(&[
                "symbolic-ref",
                "HEAD",
                &format!("refs/heads/{}", self.branch),
            ])?;
            crate::gitops::write_file(&self.dir.join("README.md"), b"# Beans hub\n")?;
            crate::gitops::write_file(
                &self.dir.join("beans.toml"),
                b"# Shared Beans configuration\n",
            )?;
            self.git(&["add", "-A"])?;
            self.commit("bn: initialize hub", None)?;
            let mut budget = AttemptBudget::for_mutation();
            if take_push_attempt(&mut budget) {
                self.git(&[
                    "push",
                    "--quiet",
                    "origin",
                    &format!("HEAD:{}", self.branch),
                ])?;
            }
        }
        Ok(())
    }
}
