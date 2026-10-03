//! Shared native Git transaction boundary. Failed effects are retained for sync.
use crate::domain::{error::ErrorCategory, frontmatter::Error};
use beans_kernel::retry::{
    AttemptBudget, RetryAction, RetryFacts, decide_retry, take_push_attempt,
};
use std::path::{Path, PathBuf};

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
    state: super::state::HubState,
    legacy_cache: PathBuf,
    pub executor: super::GitExecutor,
    pub branch: String,
    pub actor: String,
    pub no_sync: bool,
    pub throttle: std::time::Duration,
}
#[path = "hub_git.rs"]
mod hub_git;
#[path = "hub_state.rs"]
mod hub_state;
impl Hub {
    fn apply_commit(&self, op: &mut dyn Operation, nonce: &str) -> Result<Option<String>, Error> {
        let journal = serde_json::json!({"subject": op.subject(), "nonce": nonce});
        self.state.write(
            "op-journal.json",
            &serde_json::to_vec(&journal).map_err(|e| Error::new(e.to_string()))?,
        )?;
        // Failure retains journal and partial files. Never reset/clean unrelated files.
        let at = std::time::Instant::now();
        let apply = op.apply(&self.dir);
        super::diagnostics::emit(
            self.executor.policy.diagnostics,
            "apply",
            "apply",
            at.elapsed(),
            super::diagnostics::result(&apply),
            None,
        );
        let paths = apply?;
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
        let mut args = vec![std::ffi::OsString::from("add"), "-A".into(), "--".into()];
        args.extend(paths.into_iter().map(PathBuf::into_os_string));
        self.raw_git(args, "add")?.checked("add")?;
        // diff --exit-code's 1 means differences; all other errors are fatal.
        let out = self.raw_git(["diff", "--cached", "--quiet"], "diff")?;
        match out.status.code() {
            Some(0) => return Ok(None),
            Some(1) => (),
            _ => return Err(out.checked("diff").unwrap_err()),
        }
        self.commit(&op.subject(), Some(nonce))?;
        self.git(&["rev-parse", "HEAD"]).map(Some)
    }
    pub fn mutate(&self, op: &mut dyn Operation) -> Result<MutationResult, Error> {
        super::diagnostics::operation(self.executor.policy.diagnostics, "mutate", || {
            self.mutate_inner(op)
        })
    }
    fn mutate_inner(&self, op: &mut dyn Operation) -> Result<MutationResult, Error> {
        let _lock = self.lock()?;
        self.legacy_gate()?;
        self.preflight()?;
        self.commit_strays()?;
        if !self.no_sync {
            match self.git(&["fetch", "--quiet", "origin"]) {
                Ok(_) => self.rebase()?,
                Err(e)
                    if matches!(
                        e.category(),
                        Some(
                            ErrorCategory::GitTimeout
                                | ErrorCategory::GitCleanup
                                | ErrorCategory::GitSignaled
                        )
                    ) =>
                {
                    return Err(e.context(b"initial fetch stopped before Apply"));
                }
                Err(e) => super::diagnostics::warning(b"bn: fetch failed, working offline: ", &e),
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
                ))
                .categorized(ErrorCategory::GitFailure));
            }
            super::diagnostics::emit(
                self.executor.policy.diagnostics,
                "push_attempt",
                "push",
                std::time::Duration::ZERO,
                "started",
                Some(usize::from(3 - budget.remaining())),
            );
            match self.push() {
                Ok(_) => {
                    self.clear_journal()?;
                    result.pushed = true;
                    return Ok(result);
                }
                Err((rejected, e)) => {
                    if !rejected {
                        return Err(e.context(format!("change committed locally as {}; publication failed; remote outcome may be unknown; run bn sync", result.sha).as_bytes()));
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
                        ))
                        .categorized(ErrorCategory::GitFailure));
                    }
                    self.git(&["fetch", "--quiet", "origin"])?;
                    let rebase = self.git(&["rebase", "--quiet", &self.remote_ref()]);
                    if rebase.is_ok() {
                        op.after_rebase(self.nonce_present(&nonce)?);
                        continue;
                    }
                    let rebase_error = rebase.unwrap_err();
                    self.abort_rebase(&rebase_error)?;
                    if matches!(
                        rebase_error.category(),
                        Some(
                            ErrorCategory::GitTimeout
                                | ErrorCategory::GitCleanup
                                | ErrorCategory::GitSignaled
                        )
                    ) {
                        return Err(rebase_error
                            .context(b"retry rebase interrupted; local history preserved"));
                    }
                    // Abort is verified before facts permit any discard.
                    self.preflight()?;
                    if !self.worktree_clean()? {
                        return Err(Error::new(
                            "hub changed during retry; preserve files and run bn sync".into(),
                        )
                        .categorized(ErrorCategory::GitConflict));
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
                            )
                            .categorized(ErrorCategory::GitConflict));
                        }
                    }
                }
            }
        }
    }
    pub fn sync(&self) -> Result<(), Error> {
        super::diagnostics::operation(self.executor.policy.diagnostics, "sync", || {
            self.sync_inner()
        })
    }
    fn sync_inner(&self) -> Result<(), Error> {
        let _lock = self.lock()?;
        self.legacy_gate()?;
        self.preflight()?;
        self.commit_strays()?;
        let mut budget = AttemptBudget::for_mutation();
        while take_push_attempt(&mut budget) {
            self.git(&["fetch", "--quiet", "origin"])?;
            self.rebase()?;
            super::diagnostics::emit(
                self.executor.policy.diagnostics,
                "push_attempt",
                "push",
                std::time::Duration::ZERO,
                "started",
                Some(usize::from(3 - budget.remaining())),
            );
            match self.push() {
                Ok(()) => {
                    self.clear_journal()?;
                    return Ok(());
                }
                Err((false, e)) => {
                    return Err(e.context(b"sync publication failed; local history preserved"));
                }
                Err((true, _)) => (),
            }
        }
        Err(
            Error::new("sync push failed after three attempts; local history preserved".into())
                .categorized(ErrorCategory::GitFailure),
        )
    }
}
fn io_error(e: std::io::Error) -> Error {
    Error::new(e.to_string())
}
