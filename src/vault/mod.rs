//! Linux hub paths and project resolution.
mod paths;
mod project_name;
pub use paths::{Paths, check_hub, default_paths, default_paths_with};
pub use project_name::{project_name, valid_project_name};
