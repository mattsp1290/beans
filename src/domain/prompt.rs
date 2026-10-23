//! Prompt-specific identity, namespace, and fixed lifecycle rules.
pub use super::prompt_document::{PromptDocument, PromptMetadata};
use super::{frontmatter::Error, id, issue::quoted};

pub const OPEN: &str = "open";
pub const IN_PROGRESS: &str = "in_progress";
pub const COMPLETED: &str = "completed";
pub const DECLINED: &str = "declined";
pub const CANCELLED: &str = "cancelled";

pub fn valid_id(value: &str) -> bool {
    id::valid_id(value)
        && value.rsplit_once("-p-").is_some_and(|(prefix, suffix)| {
            !prefix.is_empty()
                && prefix.as_bytes()[0].is_ascii_alphanumeric()
                && !suffix.contains('-')
                && !suffix.contains('.')
        })
}

pub fn new_id(prefix: &str, exists: Option<&mut dyn FnMut(&str) -> bool>, length: isize) -> String {
    id::new_id(&format!("{prefix}-p"), exists, length)
}

pub fn valid_status(status: &str) -> bool {
    matches!(
        status,
        OPEN | IN_PROGRESS | COMPLETED | DECLINED | CANCELLED
    )
}

/// Validates the fixed prompt transition matrix. Force bypasses only that
/// matrix: both endpoints must still be prompt statuses.
pub fn validate_transition(from: &str, to: &str, force: bool) -> Result<(), Error> {
    if valid_status(from)
        && valid_status(to)
        && (force
            || from == to
            || matches!(
                (from, to),
                (OPEN, IN_PROGRESS | DECLINED | CANCELLED)
                    | (IN_PROGRESS, COMPLETED | DECLINED | CANCELLED)
            ))
    {
        return Ok(());
    }
    Err(Error::new(format!(
        "invalid prompt status transition {} -> {}",
        quoted(from),
        quoted(to)
    )))
}

/// Trims coordination metadata without case folding or interpreting it as an
/// authenticated principal.
pub fn normalize_identity(value: &str) -> Result<String, Error> {
    let value = value.trim();
    if value.is_empty() {
        return Err(Error::new("prompt identity must not be blank".to_owned()));
    }
    Ok(value.to_owned())
}

/// Validates claimant/status invariants independently of transition policy.
pub fn validate_state(status: &str, claimed_by: Option<&str>) -> Result<(), Error> {
    if !valid_status(status) {
        return Err(Error::new(format!(
            "invalid prompt status {}",
            quoted(status)
        )));
    }
    if let Some(claimed_by) = claimed_by {
        normalize_identity(claimed_by)?;
    }
    if status == OPEN && claimed_by.is_some() {
        return Err(Error::new("open prompt must not be claimed".to_owned()));
    }
    if matches!(status, IN_PROGRESS | COMPLETED) && claimed_by.is_none() {
        return Err(Error::new(format!("{status} prompt requires a claimant")));
    }
    Ok(())
}

/// The lifecycle subset required to make claim and reassignment decisions.
/// Persistence layers remain responsible for applying the returned state.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct State {
    pub status: String,
    pub claimed_by: Option<String>,
    pub to: String,
}

impl State {
    pub fn new(status: &str, claimed_by: Option<&str>, to: &str) -> Result<Self, Error> {
        validate_state(status, claimed_by)?;
        Ok(Self {
            status: status.to_owned(),
            claimed_by: claimed_by.map(normalize_identity).transpose()?,
            to: normalize_identity(to)?,
        })
    }

    fn validate(&self) -> Result<(), Error> {
        validate_state(&self.status, self.claimed_by.as_deref())?;
        normalize_identity(&self.to)?;
        Ok(())
    }

    /// Returns an atomic claim effect. The source value is never mutated on a
    /// conflict, and claiming an existing claim is idempotent only by the same
    /// case-sensitive actor.
    pub fn claim(&self, actor: &str) -> Result<Self, Error> {
        self.validate()?;
        let actor = normalize_identity(actor)?;
        if self.status == OPEN && self.claimed_by.is_none() {
            return Ok(Self {
                status: IN_PROGRESS.to_owned(),
                claimed_by: Some(actor),
                to: self.to.clone(),
            });
        }
        if self.status == IN_PROGRESS && self.claimed_by.as_deref() == Some(actor.as_str()) {
            return Ok(self.clone());
        }
        Err(Error::new(
            "prompt cannot be claimed by this actor".to_owned(),
        ))
    }

    /// Applies an explicit status effect while retaining claimant provenance,
    /// except that reopening clears the claimant atomically. Entering
    /// `in_progress` this way requires the already-recorded claimant to equal
    /// the invocation actor; new claims must use [`Self::claim`].
    pub fn transition(
        &self,
        status: &str,
        actor: Option<&str>,
        force: bool,
    ) -> Result<Self, Error> {
        self.validate()?;
        validate_transition(&self.status, status, force)?;
        let actor = actor.map(normalize_identity).transpose()?;
        if status == IN_PROGRESS && self.claimed_by.as_deref() != actor.as_deref() {
            return Err(Error::new(
                "in_progress status requires the invocation actor to own the claim".to_owned(),
            ));
        }
        let next = Self {
            status: status.to_owned(),
            claimed_by: if status == OPEN {
                None
            } else {
                self.claimed_by.clone()
            },
            to: self.to.clone(),
        };
        next.validate()?;
        Ok(next)
    }

    /// Changes the recipient. An ordinary idempotent recipient update changes
    /// nothing; a forced reassignment effect on claimed or terminal state
    /// reopens it and clears the claim.
    pub fn reassign(&self, to: &str, force: bool) -> Result<Self, Error> {
        self.validate()?;
        let to = normalize_identity(to)?;
        let reset = force && (self.claimed_by.is_some() || is_terminal(&self.status));
        if to == self.to && !reset {
            return Ok(self.clone());
        }
        if !force && (self.status != OPEN || self.claimed_by.is_some()) {
            return Err(Error::new(
                "prompt recipient can only be changed while open and unclaimed".to_owned(),
            ));
        }
        Ok(Self {
            status: if reset {
                OPEN.to_owned()
            } else {
                self.status.clone()
            },
            claimed_by: if reset { None } else { self.claimed_by.clone() },
            to,
        })
    }
}

pub fn is_terminal(status: &str) -> bool {
    matches!(status, COMPLETED | DECLINED | CANCELLED)
}

/// Recipient changes are standalone effects so later persistence layers cannot
/// accidentally combine them with claim or explicit-status effects.
pub fn validate_update_combination(
    changes_recipient: bool,
    claims: bool,
    explicit_status: Option<&str>,
) -> Result<(), Error> {
    if changes_recipient && (claims || explicit_status.is_some()) {
        return Err(Error::new(
            "prompt recipient cannot be combined with claim or status".to_owned(),
        ));
    }
    Ok(())
}

pub fn slug(title: &str) -> String {
    id::slug(title)
}

pub fn filename(prompt_id: &str, title: &str) -> String {
    id::filename(prompt_id, &slug(title))
}
