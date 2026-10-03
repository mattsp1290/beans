//! `bn upgrade` adapter: the environment and flags become one upgrade request.
use crate::{
    domain::frontmatter::Error,
    upgrade::{BASE_URL_ENV, DEFAULT_BASE_URL, Request, SMOKE_TIMEOUT, cargo_bins, run},
};

pub(super) fn execute(matches: &clap::ArgMatches, json: bool) -> Result<(), Error> {
    let exe = std::env::current_exe()
        .and_then(std::fs::canonicalize)
        .map_err(|e| Error::from(format!("cannot locate the running bn executable: {e}")))?;
    let base_url = match std::env::var(BASE_URL_ENV) {
        Ok(value) if !value.is_empty() => value,
        Ok(_) | Err(std::env::VarError::NotPresent) => DEFAULT_BASE_URL.to_owned(),
        Err(e) => return Err(Error::from(format!("{BASE_URL_ENV}: {e}"))),
    };
    let current = env!("BN_VERSION");
    let outcome = run(
        &Request {
            current,
            exe: &exe,
            base_url: &base_url,
            cargo_bins: &cargo_bins(),
            check: matches.get_flag("check"),
            force: matches.get_flag("force"),
            tag: matches.get_one::<String>("tag").map(String::as_str),
            smoke_timeout: SMOKE_TIMEOUT,
        },
        &|line| {
            if !json {
                println!("{line}");
            }
        },
    )?;
    if json {
        println!(
            "{}",
            serde_json::json!({
                "current": current,
                "latest": outcome.latest,
                "target": outcome.target,
                "path": exe.to_string_lossy(),
                "update_available": outcome.update_available,
                "upgraded": outcome.upgraded,
            })
        );
    }
    Ok(())
}
