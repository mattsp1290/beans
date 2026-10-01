//! Request-specific namespaces and the fixed authored lifecycle. Reverse issue
//! links are derived from request documents, never owned by issue files.
pub use super::request_document::{RequestDocument, RequestMetadata};
use super::{frontmatter::Error, id, issue::quoted};

pub const OPEN: &str = "open";
pub const ACCEPTED: &str = "accepted";
pub const IN_PROGRESS: &str = "in_progress";
pub const RESOLVED: &str = "resolved";
pub const DECLINED: &str = "declined";

pub fn valid_id(value: &str) -> bool {
    // The ordinary ID grammar alone also accepts a prefix ending in a dash.
    // The request namespace requires a nonempty project prefix before `-r-`.
    id::valid_id(value)
        && value.rsplit_once("-r-").is_some_and(|(prefix, suffix)| {
            !prefix.is_empty()
                && prefix.as_bytes()[0].is_ascii_alphanumeric()
                && !suffix.contains('-')
        })
}

pub fn new_id(prefix: &str, exists: Option<&mut dyn FnMut(&str) -> bool>, length: isize) -> String {
    id::new_id(&format!("{prefix}-r"), exists, length)
}

pub fn valid_status(status: &str) -> bool {
    matches!(status, OPEN | ACCEPTED | IN_PROGRESS | RESOLVED | DECLINED)
}

/// Ordinary lifecycle transitions are idempotent only for valid statuses.
pub fn validate_transition(from: &str, to: &str) -> Result<(), Error> {
    if valid_status(from)
        && valid_status(to)
        && (from == to
            || matches!(
                (from, to),
                (OPEN, ACCEPTED | DECLINED)
                    | (ACCEPTED, IN_PROGRESS | DECLINED)
                    | (IN_PROGRESS, RESOLVED | DECLINED)
            ))
    {
        return Ok(());
    }
    Err(Error(format!(
        "invalid request status transition {} -> {}",
        quoted(from),
        quoted(to)
    )))
}
