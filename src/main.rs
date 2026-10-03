fn main() -> std::process::ExitCode {
    let code = beans::run();
    beans::gitops::diagnostics::flush();
    code
}
