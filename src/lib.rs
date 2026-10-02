//! Native application boundaries built on the verified kernel and retained domain.
pub use beans_kernel as kernel;
pub mod cli;
pub mod domain;
pub mod gitops;
pub mod markdown;
pub mod ops;
pub mod vault;

/// Native command dispatch for the first CLI slice.
pub fn run() -> std::process::ExitCode {
    match cli::execute(cli::command().get_matches()) {
        Ok(()) => std::process::ExitCode::SUCCESS,
        Err(error) => {
            eprintln!("bn: {error}");
            std::process::ExitCode::FAILURE
        }
    }
}
