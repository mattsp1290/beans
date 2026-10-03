//! Native application boundaries built on the verified kernel and retained domain.
pub use beans_kernel as kernel;
pub mod cli;
pub mod domain;
pub mod gitops;
pub mod markdown;
pub mod ops;
pub mod server;
pub mod vault;

/// Native command dispatch.
pub fn run() -> std::process::ExitCode {
    if std::env::args_os().len() == 1 {
        println!("{}", cli::command().render_long_help());
        return std::process::ExitCode::SUCCESS;
    }
    match cli::execute(cli::command().get_matches()) {
        Ok(()) => std::process::ExitCode::SUCCESS,
        Err(error) => {
            gitops::diagnostics::warning(b"bn: ", &error);
            std::process::ExitCode::FAILURE
        }
    }
}
