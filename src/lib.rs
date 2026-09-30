//! Application boundaries are added after the mandatory kernel gate passes.
//! Go remains the installed/default implementation during the migration.
pub use beans_kernel as kernel;

/// Process entry wiring; command adapters are implemented in WP6.
pub fn run() -> std::process::ExitCode {
    std::process::ExitCode::FAILURE
}
