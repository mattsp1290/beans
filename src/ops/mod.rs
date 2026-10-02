//! Replay-safe shared operations for native command and server adapters.
mod context;
pub use context::{OperationConfig, prefix_for};
pub(crate) mod issues;
pub use issues::{IssueChange, IssueFields, IssueMutation};
pub(crate) mod records;
pub use records::{RecordChange, RecordMutation, RequestEdit};
pub(crate) fn scaffold_paths(
    hub: &std::path::Path,
    resolved: &crate::vault::Resolved,
) -> Result<Vec<std::path::PathBuf>, crate::domain::frontmatter::Error> {
    use std::os::unix::ffi::OsStringExt;
    Ok(
        crate::vault::create_project_files(hub, &resolved.project, &resolved.repo_remote)?
            .unwrap_or_default()
            .into_iter()
            .map(|p| std::ffi::OsString::from_vec(p).into())
            .collect(),
    )
}
mod archive;
mod content;
mod project_config;
pub use archive::{ArchiveMutation, ArchiveSelection};
pub use content::{ContentChange, ContentMutation};
mod plans;
pub use plans::{PlanChange, PlanMutation};
mod import;
pub use import::ImportMutation;
