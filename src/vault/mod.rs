//! Linux hub paths and project resolution.
mod classify;
mod document;
mod git;
mod glob;
mod go_lower;
mod graph;
mod index;
pub(crate) mod paths;
mod project_name;
mod remote;
mod remote_ip;
mod remote_url;
mod resolve;
pub use classify::{
    classify, is_asset_path, is_plans_directory, plan_bundle_project, plan_manifest_project,
    skip_dir_name,
};
pub use document::{DocMetadata, YamlValue, doc_metadata, split_doc_frontmatter};
pub use git::{GitCapture, GitResolver, SystemGit};
pub use graph::{GraphNote, LinkKind, LinkRef, NoteGraph, NoteKind, RawLink, Warning};
pub use index::{Index, LoadOptions, Note, NoteData, Project};
pub use paths::{Paths, check_hub, default_paths, default_paths_with};
pub use project_name::{project_name, valid_project_name};
pub use remote::{NO_REMOTE, normalize_remote_url, remote_host, validate_remote_url};
pub use resolve::{OUTSIDE_REPO, ResolveOptions, Resolved, project_dirs, resolve};
