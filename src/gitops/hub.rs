//! Shared native Git transaction boundary. Failed effects are retained for sync.
use crate::domain::frontmatter::Error;
use beans_kernel::retry::{
    AttemptBudget, RetryAction, RetryFacts, decide_retry, take_push_attempt,
};
use std::{
    fs::{self, File, OpenOptions},
    os::fd::AsRawFd,
    path::{Path, PathBuf},
    process::Command,
};

pub trait Operation {
    fn subject(&self) -> String;
    /// After a retry changes ancestry, the Git boundary verifies whether this
    /// invocation's nonce-bearing commit survived. Missing is distinct from a
    /// different writer's identical patch; adapters must reapply their own event.
    fn after_rebase(&mut self, _operation_present: bool) {}
    /// Re-read affected documents on every call; return only relative staged paths.
    fn apply(&mut self, hub: &Path) -> Result<Vec<PathBuf>, Error>;
}
#[derive(Debug, Default)]
pub struct MutationResult {
    pub sha: String,
    pub pushed: bool,
    pub message: String,
}
pub struct Hub {
    pub dir: PathBuf,
    pub cache: PathBuf,
    pub branch: String,
    pub actor: String,
    pub no_sync: bool,
    pub throttle: std::time::Duration,
}
struct Lock(File);
impl Drop for Lock {
    fn drop(&mut self) {
        unsafe {
            libc::flock(self.0.as_raw_fd(), libc::LOCK_UN);
        }
    }
}
impl Hub {
    fn lock(&self) -> Result<Lock, Error> {
        fs::create_dir_all(&self.cache).map_err(io_error)?;
        let file = OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .truncate(false)
            .open(self.cache.join("hub.lock"))
            .map_err(io_error)?;
        // Take an exclusive nonblocking flock so callers can retry contention.
        if unsafe { libc::flock(file.as_raw_fd(), libc::LOCK_EX | libc::LOCK_NB) } != 0 {
            return Err(Error::new(
                "another bn is running on this hub (lock held)".into(),
            ));
        }
        Ok(Lock(file))
    }
    /// Clear derived cache entries under the lock, retaining its inode and recovery evidence.
    pub fn clear_cache(&self) -> Result<(), Error> {
        let _lock = self.lock()?;
        for entry in fs::read_dir(&self.cache).map_err(io_error)? {
            let entry = entry.map_err(io_error)?;
            if matches!(
                entry.file_name().to_str(),
                Some("hub.lock" | "op-journal.json")
            ) {
                continue;
            }
            let kind = entry.file_type().map_err(io_error)?;
            if kind.is_dir() {
                fs::remove_dir_all(entry.path()).map_err(io_error)?;
            } else {
                fs::remove_file(entry.path()).map_err(io_error)?;
            }
        }
        Ok(())
    }
    pub fn git(&self, args: &[&str]) -> Result<String, Error> {
        let out = Command::new("git")
            .arg("-C")
            .arg(&self.dir)
            .args(args)
            .env("GIT_TERMINAL_PROMPT", "0")
            .env("GIT_COMMITTER_NAME", "bn")
            .env("GIT_COMMITTER_EMAIL", "bn@localhost")
            .output()
            .map_err(io_error)?;
        if !out.status.success() {
            return Err(Error::new(format!(
                "git {}: {}",
                args.join(" "),
                String::from_utf8_lossy(&out.stderr).trim()
            )));
        }
        Ok(String::from_utf8_lossy(&out.stdout).trim().to_owned())
    }
    pub fn preflight(&self) -> Result<(), Error> {
        for marker in ["rebase-merge", "rebase-apply", "MERGE_HEAD"] {
            if self.dir.join(".git").join(marker).exists() {
                return Err(Error::new(
                    "hub has an interrupted rebase or merge; abort it and retry".into(),
                ));
            }
        }
        let branch = self
            .git(&["symbolic-ref", "--short", "-q", "HEAD"])
            .map_err(|_| {
                Error::new("hub is on a detached HEAD; checkout the configured hub branch".into())
            })?;
        if branch != self.branch {
            return Err(Error::new(format!(
                "hub is on {branch}, expected {}; checkout the configured hub branch",
                self.branch
            )));
        }
        Ok(())
    }
    fn commit(&self, subject: &str, nonce: Option<&str>) -> Result<(), Error> {
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
    fn journal(&self) -> PathBuf {
        self.cache.join("op-journal.json")
    }
    fn clear_journal(&self) -> Result<(), Error> {
        match fs::remove_file(self.journal()) {
            Ok(()) => Ok(()),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(()),
            Err(e) => Err(io_error(e)),
        }
    }
    fn commit_strays(&self) -> Result<(), Error> {
        self.remove_owned_temps()?;
        if self.git(&["status", "--porcelain"])?.is_empty() {
            return self.clear_journal();
        }
        let recovered = self.journal().exists();
        self.git(&["add", "-A"])?;
        self.commit(
            if recovered {
                "bn: recovered partial operation"
            } else {
                "bn: hand edits"
            },
            None,
        )?;
        self.clear_journal()
    }
    fn rebase(&self) -> Result<(), Error> {
        if let Err(e) = self.git(&["rebase", "--quiet", &self.remote_ref()]) {
            self.git(&["rebase", "--abort"]).map_err(|abort| {
                Error::new(format!(
                    "{e}; abort failed: {abort}; preserve files and resolve manually"
                ))
            })?;
            return Err(Error::new(format!(
                "hub local commits conflict with origin; preserved local history; run bn sync: {e}"
            )));
        }
        Ok(())
    }
    fn remote_ref(&self) -> String {
        format!("origin/{}", self.branch)
    }
    fn ahead(&self) -> Result<usize, Error> {
        self.git(&[
            "rev-list",
            "--count",
            &format!("{}..HEAD", self.remote_ref()),
        ])?
        .parse()
        .map_err(|_| Error::new("cannot determine local commit count".into()))
    }
    fn owned_head(&self, nonce: &str) -> Result<Option<String>, Error> {
        if self.ahead()? != 1 {
            return Ok(None);
        }
        let trailer = self.git(&["log", "-1", "--format=%(trailers:key=Bn-Run,valueonly)"])?;
        if trailer != nonce {
            return Ok(None);
        }
        self.git(&["rev-parse", "HEAD"]).map(Some)
    }
    fn nonce_present(&self, nonce: &str) -> Result<bool, Error> {
        let messages = self.git(&["log", "--format=%(trailers:key=Bn-Run,valueonly)"])?;
        Ok(messages.lines().any(|line| line.trim() == nonce))
    }
    fn remove_owned_temps(&self) -> Result<(), Error> {
        for (relative, evidence) in super::hub_file::owned_orphans(&self.dir)? {
            let output = Command::new("git")
                .arg("-C")
                .arg(&self.dir)
                .args(["ls-files", "-z", "--"])
                .arg(&relative)
                .output()
                .map_err(io_error)?;
            if !output.status.success() {
                return Err(Error::new(
                    "cannot verify temporary tracked state; preserving recovery files".into(),
                ));
            }
            if output.stdout.is_empty() {
                super::hub_file::remove_registered_orphan(&self.dir, &relative, &evidence)?;
            }
        }
        Ok(())
    }
    fn apply_commit(&self, op: &mut dyn Operation, nonce: &str) -> Result<Option<String>, Error> {
        let journal = serde_json::json!({"subject": op.subject(), "nonce": nonce});
        super::write_file(
            &self.journal(),
            &serde_json::to_vec(&journal).map_err(|e| Error::new(e.to_string()))?,
        )?;
        // Failure retains journal and partial files. Never reset/clean unrelated files.
        let paths = op.apply(&self.dir)?;
        if paths.is_empty() {
            return Ok(None);
        }
        if paths.iter().any(|p| {
            p.is_absolute()
                || p.components()
                    .any(|c| !matches!(c, std::path::Component::Normal(_)))
        }) {
            return Err(Error::new("operation returned unsafe staging path".into()));
        }
        let mut cmd = Command::new("git");
        cmd.arg("-C")
            .arg(&self.dir)
            .args(["add", "-A", "--"])
            .args(paths);
        let out = cmd.output().map_err(io_error)?;
        if !out.status.success() {
            return Err(Error::new(format!(
                "stage failed: {}",
                String::from_utf8_lossy(&out.stderr)
            )));
        }
        // diff --exit-code's 1 means differences; all other errors are fatal.
        let out = Command::new("git")
            .arg("-C")
            .arg(&self.dir)
            .args(["diff", "--cached", "--quiet"])
            .output()
            .map_err(io_error)?;
        match out.status.code() {
            Some(0) => return Ok(None),
            Some(1) => (),
            _ => {
                return Err(Error::new(format!(
                    "staged diff failed: {}",
                    String::from_utf8_lossy(&out.stderr)
                )));
            }
        }
        self.commit(&op.subject(), Some(nonce))?;
        self.git(&["rev-parse", "HEAD"]).map(Some)
    }
    pub fn mutate(&self, op: &mut dyn Operation) -> Result<MutationResult, Error> {
        let _lock = self.lock()?;
        self.preflight()?;
        self.commit_strays()?;
        if !self.no_sync {
            match self.git(&["fetch", "--quiet", "origin"]) {
                Ok(_) => self.rebase()?,
                Err(e) => eprintln!("bn: fetch failed, working offline: {e}"),
            }
        }
        let mut random = [0u8; 16];
        getrandom::fill(&mut random).map_err(|e| Error::new(e.to_string()))?;
        let nonce: String = random.iter().map(|b| format!("{b:02x}")).collect();
        let mut budget = AttemptBudget::for_mutation();
        let mut result = MutationResult::default();
        loop {
            if let Some(sha) = self.apply_commit(op, &nonce)? {
                result.sha = sha;
            } else if !self.no_sync && self.ahead()? == 0 {
                self.clear_journal()?;
                result.pushed = true;
                return Ok(result);
            }
            if self.no_sync {
                self.clear_journal()?;
                result.message = "committed locally (--no-sync)".into();
                return Ok(result);
            }
            if !take_push_attempt(&mut budget) {
                return Err(Error::new(format!(
                    "push rejected three times; change retained locally {}; run bn sync",
                    result.sha
                )));
            }
            match self.git(&[
                "push",
                "--quiet",
                "origin",
                &format!("HEAD:{}", self.branch),
            ]) {
                Ok(_) => {
                    self.clear_journal()?;
                    result.pushed = true;
                    return Ok(result);
                }
                Err(e) => {
                    let rejected = is_retryable_push_rejection(&e.to_string());
                    if !rejected {
                        return Err(Error::new(format!(
                            "change committed locally as {}; push failed: {e}; run bn sync",
                            result.sha
                        )));
                    }
                    if decide_retry(RetryFacts {
                        operation_present: false,
                        head_is_operation: false,
                        local_commits: usize::MAX,
                        owned_by_run: false,
                        push_attempts: usize::from(3 - budget.remaining()),
                    }) == RetryAction::Exhausted
                    {
                        return Err(Error::new(format!(
                            "push rejected three times; change retained locally {}; run bn sync",
                            result.sha
                        )));
                    }
                    self.git(&["fetch", "--quiet", "origin"])?;
                    if self.git(&["rebase", "--quiet", &self.remote_ref()]).is_ok() {
                        op.after_rebase(self.nonce_present(&nonce)?);
                        continue;
                    }
                    self.git(&["rebase", "--abort"])?;
                    // Abort is verified before facts permit any discard.
                    self.preflight()?;
                    if !self.git(&["status", "--porcelain"])?.is_empty() {
                        return Err(Error::new(
                            "hub changed during retry; preserve files and run bn sync".into(),
                        ));
                    }
                    let head = self.git(&["rev-parse", "HEAD"])?;
                    let count = self.ahead()?;
                    let owned = self.owned_head(&nonce)?;
                    let facts = RetryFacts {
                        operation_present: owned.is_some(),
                        head_is_operation: owned.as_ref() == Some(&head),
                        local_commits: count,
                        owned_by_run: owned.is_some(),
                        push_attempts: usize::from(3 - budget.remaining()),
                    };
                    match decide_retry(facts) {
                        RetryAction::DiscardOwnedAndReplay => {
                            self.git(&["reset", "--hard", "--quiet", &self.remote_ref()])?;
                            op.after_rebase(self.nonce_present(&nonce)?);
                        }
                        _ => {
                            return Err(Error::new(
                                "hub conflict: preserved unowned local commits; run bn sync".into(),
                            ));
                        }
                    }
                }
            }
        }
    }
    pub fn sync(&self) -> Result<(), Error> {
        let _lock = self.lock()?;
        self.preflight()?;
        self.commit_strays()?;
        let mut budget = AttemptBudget::for_mutation();
        while take_push_attempt(&mut budget) {
            self.git(&["fetch", "--quiet", "origin"])?;
            self.rebase()?;
            if self
                .git(&[
                    "push",
                    "--quiet",
                    "origin",
                    &format!("HEAD:{}", self.branch),
                ])
                .is_ok()
            {
                self.clear_journal()?;
                return Ok(());
            }
        }
        Err(Error::new(
            "sync push failed after three attempts; local history preserved".into(),
        ))
    }
    fn stamp(&self, name: &str) -> Result<(), Error> {
        let text = time::OffsetDateTime::now_utc()
            .format(&time::format_description::well_known::Rfc3339)
            .map_err(|e| Error::new(e.to_string()))?;
        super::write_file(&self.cache.join(name), format!("{text}\n").as_bytes())
    }
    pub fn refresh(&self) -> Result<(), Error> {
        if let Ok(raw) = fs::read_to_string(self.cache.join("last-fetch-attempt"))
            && let Ok(at) = time::OffsetDateTime::parse(
                raw.trim(),
                &time::format_description::well_known::Rfc3339,
            )
            && (time::OffsetDateTime::now_utc() - at).whole_nanoseconds()
                < self.throttle.as_nanos() as i128
        {
            return Ok(());
        }
        let _lock = self.lock()?;
        self.preflight()?;
        self.stamp("last-fetch-attempt")?;
        self.git(&["fetch", "--quiet", "origin"])?;
        self.stamp("last-fetch")?;
        if self.git(&["status", "--porcelain"])?.is_empty() {
            self.git(&["merge", "--ff-only", "--quiet", &self.remote_ref()])?;
        }
        Ok(())
    }
    pub fn status(&self) -> Result<serde_json::Value, Error> {
        let counts = self.git(&[
            "rev-list",
            "--left-right",
            "--count",
            &format!("HEAD...{}", self.remote_ref()),
        ])?;
        let counts: Vec<_> = counts.split_whitespace().collect();
        Ok(
            serde_json::json!({"hub": self.dir, "branch": self.git(&["symbolic-ref", "--short", "-q", "HEAD"])?, "ahead": counts.first().and_then(|v| v.parse::<usize>().ok()), "behind": counts.get(1).and_then(|v| v.parse::<usize>().ok()), "dirty": self.git(&["status", "--porcelain"])?, "recovery": self.journal().exists(), "remote": self.git(&["remote", "get-url", "origin"])?, "last_fetch": fs::read_to_string(self.cache.join("last-fetch")).ok()}),
        )
    }
    pub fn initialize(&self, remote: &str) -> Result<(), Error> {
        let _lock = self.lock()?;
        if self.dir.exists() {
            return Err(Error::new(format!(
                "hub path already exists: {}",
                self.dir.display()
            )));
        }
        if let Some(p) = self.dir.parent() {
            fs::create_dir_all(p).map_err(io_error)?;
        }
        let out = Command::new("git")
            .args(["clone", "--", remote])
            .arg(&self.dir)
            .env("GIT_TERMINAL_PROMPT", "0")
            .env("GIT_COMMITTER_NAME", "bn")
            .env("GIT_COMMITTER_EMAIL", "bn@localhost")
            .output()
            .map_err(io_error)?;
        if !out.status.success() {
            return Err(Error::new(format!(
                "clone failed: {}",
                String::from_utf8_lossy(&out.stderr)
            )));
        }
        if self.git(&["rev-parse", "--verify", "HEAD"]).is_ok() {
            self.git(&["checkout", "--quiet", &self.branch])?;
        } else {
            self.git(&[
                "symbolic-ref",
                "HEAD",
                &format!("refs/heads/{}", self.branch),
            ])?;
            super::write_file(&self.dir.join("README.md"), b"# Beans hub\n")?;
            super::write_file(
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
fn io_error(e: std::io::Error) -> Error {
    Error::new(e.to_string())
}

/// Recognize Git's receiver compare-and-swap rejection without treating hooks,
/// permissions, or transport failures as permission to replay a transaction.
fn is_retryable_push_rejection(message: &str) -> bool {
    message.contains("[rejected]")
        || message.contains("failed to update ref")
        || message.contains("non-fast-forward")
        || message.contains("fetch first")
        || message.lines().any(|line| {
            let line = line.trim();
            line.starts_with("! [remote rejected] ")
                && line.ends_with("(incorrect old value provided)")
        })
}

#[cfg(test)]
mod tests {
    use super::is_retryable_push_rejection;

    #[test]
    fn push_race_status_is_distinct_from_remote_hook_and_transport_failures() {
        for status in [
            " ! [rejected] HEAD -> main (fetch first)",
            " ! [remote rejected] HEAD -> main (failed to update ref)",
            "remote: error: cannot lock ref 'refs/heads/main': is at new but expected old\n ! [remote rejected] HEAD -> main (incorrect old value provided)\nerror: failed to push some refs",
        ] {
            assert!(is_retryable_push_rejection(status), "{status}");
        }
        for status in [
            " ! [remote rejected] HEAD -> main (pre-receive hook declined)",
            "remote: hook says incorrect old value provided\n ! [remote rejected] HEAD -> main (hook declined)",
            "remote: ! [remote rejected] HEAD -> main (incorrect old value provided)",
            "fatal: Authentication failed",
            "fatal: unable to access remote: connection reset",
            "error: cannot lock ref: Permission denied",
        ] {
            assert!(!is_retryable_push_rejection(status), "{status}");
        }
    }
}
