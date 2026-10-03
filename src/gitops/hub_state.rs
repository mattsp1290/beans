use super::{Hub, io_error};
use crate::domain::{error::ErrorCategory, frontmatter::Error};
use std::{
    fs::{self, File},
    os::fd::AsRawFd,
    path::{Path, PathBuf},
};
pub(super) struct Lock {
    file: File,
    acquired: std::time::Instant,
    diagnostics: bool,
}
impl Drop for Lock {
    fn drop(&mut self) {
        crate::gitops::diagnostics::emit(
            self.diagnostics,
            "lock_hold",
            "lock",
            self.acquired.elapsed(),
            "released",
            None,
        );
        unsafe {
            libc::flock(self.file.as_raw_fd(), libc::LOCK_UN);
        }
    }
}
impl Hub {
    pub fn new(
        dir: PathBuf,
        legacy_cache: PathBuf,
        branch: String,
        actor: String,
        no_sync: bool,
        throttle: std::time::Duration,
    ) -> Result<Self, Error> {
        let state = crate::gitops::state::HubState::new(&dir)?;
        Ok(Self {
            dir: state.target.clone(),
            state,
            legacy_cache,
            branch,
            actor,
            no_sync,
            throttle,
            lock_timeout: std::time::Duration::from_secs(30),
            executor: crate::gitops::GitExecutor::default(),
        })
    }
    pub fn state_path(&self) -> &Path {
        &self.state.path
    }
    pub fn lock_path(&self) -> PathBuf {
        self.state.path.join("hub.lock")
    }
    pub fn journal_path(&self) -> PathBuf {
        self.state.path.join("op-journal.json")
    }
    pub(super) fn legacy_gate(&self) -> Result<(), Error> {
        let legacy = self.legacy_cache.join("op-journal.json");
        if fs::symlink_metadata(&legacy).is_ok() && !self.state.exists("op-journal.json")? {
            return Err(Error::new(format!(
                "legacy recovery evidence at {}; stop writers, back up and reconcile manually before placing the preserved journal at {} and running bn sync",
                legacy.display(),
                self.journal_path().display()
            )));
        }
        Ok(())
    }
    pub(super) fn lock_mode(&self, wait: bool) -> Result<Lock, Error> {
        let start = std::time::Instant::now();
        let file = self.state.open("hub.lock", true).inspect_err(|_| {
            crate::gitops::diagnostics::emit(
                self.executor.policy.diagnostics,
                "lock_wait",
                "lock",
                start.elapsed(),
                "io_failure",
                None,
            )
        })?;
        loop {
            if unsafe { libc::flock(file.as_raw_fd(), libc::LOCK_EX | libc::LOCK_NB) } == 0 {
                crate::gitops::diagnostics::emit(
                    self.executor.policy.diagnostics,
                    "lock_wait",
                    "lock",
                    start.elapsed(),
                    "acquired",
                    None,
                );
                return Ok(Lock {
                    file,
                    acquired: std::time::Instant::now(),
                    diagnostics: self.executor.policy.diagnostics,
                });
            }
            let e = std::io::Error::last_os_error();
            if !matches!(e.raw_os_error(), Some(libc::EAGAIN) | Some(libc::EINTR)) {
                crate::gitops::diagnostics::emit(
                    self.executor.policy.diagnostics,
                    "lock_wait",
                    "lock",
                    start.elapsed(),
                    "io_failure",
                    None,
                );
                return Err(io_error(e));
            }
            if !wait || start.elapsed() >= self.lock_timeout {
                crate::gitops::diagnostics::emit(
                    self.executor.policy.diagnostics,
                    "lock_wait",
                    "lock",
                    start.elapsed(),
                    if wait { "timeout" } else { "skipped-busy" },
                    None,
                );
                return Err(Error::new(
                    "another bn is running on this hub (lock wait expired)".into(),
                )
                .categorized(ErrorCategory::LockTimeout));
            }
            std::thread::sleep(
                std::time::Duration::from_millis(50)
                    .min(self.lock_timeout.saturating_sub(start.elapsed())),
            );
        }
    }
    pub(super) fn lock(&self) -> Result<Lock, Error> {
        self.lock_mode(true)
    }
    /// Remove only this clone's two derived timestamps under its persistent lock.
    pub fn clear_cache(&self) -> Result<(), Error> {
        crate::gitops::diagnostics::operation(
            self.executor.policy.diagnostics,
            "clear_cache",
            || self.clear_cache_inner(),
        )
    }
    pub(super) fn clear_cache_inner(&self) -> Result<(), Error> {
        let _lock = self.lock()?;
        for name in ["last-fetch", "last-fetch-attempt"] {
            self.state.remove(name)?;
        }
        Ok(())
    }
    pub(super) fn stamp(&self, name: &str) -> Result<(), Error> {
        let text = time::OffsetDateTime::now_utc()
            .format(&time::format_description::well_known::Rfc3339)
            .map_err(|e| Error::new(e.to_string()))?;
        self.state.write(name, format!("{text}\n").as_bytes())
    }
    pub(super) fn fresh(&self) -> bool {
        if let Ok(raw) = self
            .state
            .read("last-fetch-attempt")
            .and_then(|b| String::from_utf8(b).map_err(|e| Error::new(e.to_string())))
            && let Ok(at) = time::OffsetDateTime::parse(
                raw.trim(),
                &time::format_description::well_known::Rfc3339,
            )
            && (time::OffsetDateTime::now_utc() - at).whole_nanoseconds()
                < self.throttle.as_nanos() as i128
        {
            return true;
        }
        false
    }
    pub fn refresh(&self) -> Result<(), Error> {
        crate::gitops::diagnostics::operation(self.executor.policy.diagnostics, "refresh", || {
            self.refresh_inner()
        })
    }
    pub(super) fn refresh_inner(&self) -> Result<(), Error> {
        if self.fresh() {
            crate::gitops::diagnostics::emit(
                self.executor.policy.diagnostics,
                "refresh",
                "refresh",
                std::time::Duration::ZERO,
                "skipped-fresh",
                None,
            );
            return Ok(());
        }
        let _lock = self.lock_mode(false)?;
        if self.fresh() {
            crate::gitops::diagnostics::emit(
                self.executor.policy.diagnostics,
                "refresh",
                "refresh",
                std::time::Duration::ZERO,
                "skipped-fresh",
                None,
            );
            return Ok(());
        }
        self.legacy_gate()?;
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
            serde_json::json!({"hub": self.dir, "branch": self.git(&["symbolic-ref", "--short", "-q", "HEAD"])?, "ahead": counts.first().and_then(|v| v.parse::<usize>().ok()), "behind": counts.get(1).and_then(|v| v.parse::<usize>().ok()), "dirty": self.git(&["status", "--porcelain"])?, "recovery": self.state.exists("op-journal.json")?, "remote": self.git(&["remote", "get-url", "origin"])?, "last_fetch": self.state.read("last-fetch").ok().and_then(|b| String::from_utf8(b).ok())}),
        )
    }
}
