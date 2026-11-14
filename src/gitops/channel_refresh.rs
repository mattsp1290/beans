use super::Hub;
use crate::domain::{error::ErrorCategory, frontmatter::Error};
use std::time::Instant;

impl Hub {
    pub(crate) fn channel_remote_applied(&self) -> Result<bool, Error> {
        let output = self.raw_git(
            ["merge-base", "--is-ancestor", &self.remote_ref(), "HEAD"],
            "merge-base",
        )?;
        match output.status.code() {
            Some(0) => Ok(true),
            Some(1) => Ok(false),
            _ => Err(output.checked("merge-base").unwrap_err()),
        }
    }
    /// Fetch and fast-forward under the hub lock without publishing local work.
    /// Each Git step uses the remaining deadline; process cleanup adds at most
    /// the executor's bounded termination grace period.
    pub(crate) fn refresh_until<T>(
        &self,
        deadline: Instant,
        snapshot: impl FnOnce() -> Result<T, Error>,
    ) -> Result<T, Error> {
        let _lock = self.lock_mode(false)?;
        self.legacy_gate()?;
        let run = |args: &[&str], phase: &str| {
            let remaining = deadline.saturating_duration_since(Instant::now());
            if remaining.is_zero() {
                return Err(Error::new("channel refresh deadline expired".into())
                    .categorized(ErrorCategory::GitTimeout));
            }
            let mut executor = self.executor.clone();
            executor.policy.command_timeout = executor.policy.command_timeout.min(remaining);
            executor.policy.network_timeout = executor.policy.network_timeout.min(remaining);
            executor.run(Some(&self.dir), args, phase)
        };
        self.preflight_using(run)?;
        if !run(&["status", "--porcelain", "-z"], "status")?
            .checked("status")?
            .stdout
            .is_empty()
        {
            return Err(
                Error::new("channel refresh requires a clean hub; run bn sync".into())
                    .categorized(ErrorCategory::GitConflict),
            );
        }
        self.stamp("last-fetch-attempt")?;
        run(&["fetch", "--quiet", "origin"], "fetch")?.checked("fetch")?;
        run(
            &["merge", "--ff-only", "--quiet", &self.remote_ref()],
            "merge",
        )?
        .checked("merge")?;
        self.stamp("last-fetch")?;
        snapshot()
    }
}
