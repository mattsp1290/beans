//! Native application boundaries built on the verified kernel and retained domain.
pub use beans_kernel as kernel;
pub mod cli;
pub mod domain;
pub mod gitops;
pub mod markdown;
pub mod ops;
pub mod server;
mod upgrade;
pub mod vault;

/// Native command dispatch.
pub fn run() -> std::process::ExitCode {
    upgrade::check_for_updates();
    if std::env::args_os().len() == 1 {
        println!("{}", cli::command().render_long_help());
        return std::process::ExitCode::SUCCESS;
    }
    let matches = match cli::command().try_get_matches() {
        Ok(matches) => matches,
        Err(error) => {
            let code = error.exit_code() as u8;
            let _ = error.print();
            return std::process::ExitCode::from(code);
        }
    };
    match cli::execute(matches) {
        Ok(()) => std::process::ExitCode::SUCCESS,
        Err(error) => {
            gitops::diagnostics::warning(b"bn: ", &error);
            std::process::ExitCode::FAILURE
        }
    }
}
