//! Application boundaries are added after the mandatory kernel gate passes.
//! Go remains the installed/default implementation during the migration.
pub use beans_kernel as kernel;
pub mod cli;
pub mod domain;
pub mod gitops;
pub mod markdown;
pub mod ops;
pub mod vault;

/// Process entry wiring; command adapters are implemented in WP6.
pub fn run() -> std::process::ExitCode {
    std::process::ExitCode::FAILURE
}
