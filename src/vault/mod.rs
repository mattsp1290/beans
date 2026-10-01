//! Linux hub paths and project resolution.
mod go_lower;
mod paths;
mod project_name;
mod remote;
mod remote_ip;
mod remote_url;
pub use paths::{Paths, check_hub, default_paths, default_paths_with};
pub use project_name::{project_name, valid_project_name};
pub use remote::{NO_REMOTE, normalize_remote_url, remote_host, validate_remote_url};
