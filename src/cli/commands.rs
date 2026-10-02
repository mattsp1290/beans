use super::Actor;
use crate::{
    domain::{config::load_user_config, frontmatter::Error},
    gitops::Hub,
    vault::paths,
};
use clap::{Arg, ArgAction, Command};
use std::ffi::OsStr;
fn value(m: &clap::ArgMatches, name: &str) -> String {
    m.get_one::<String>(name).cloned().unwrap_or_default()
}
fn arg(name: &'static str) -> Arg {
    Arg::new(name).long(name).num_args(1)
}
pub fn command() -> Command {
    let root = Command::new("bn")
        .version(env!("BN_VERSION"))
        .about("Git-backed issue tracker and wiki")
        .arg_required_else_help(true);
    let root = root
        .arg(arg("branch").global(true))
        .disable_version_flag(true)
        .arg(
            Arg::new("version")
                .long("version")
                .short('v')
                .action(ArgAction::Version),
        );
    let root = root.arg(
        Arg::new("actor")
            .long("actor")
            .global(true)
            .help("audit actor (overrides $BN_ACTOR)")
            .num_args(1),
    );
    let root = root.arg(
        Arg::new("hub")
            .long("hub")
            .global(true)
            .help("hub clone directory (overrides $BEANS_HUB)")
            .num_args(1),
    );
    let root = root.arg(
        Arg::new("json")
            .long("json")
            .global(true)
            .help("machine-readable JSON output")
            .action(ArgAction::SetTrue)
            .num_args(0..=1)
            .require_equals(true)
            .default_missing_value("true"),
    );
    let root = root.arg(
        Arg::new("no-fetch")
            .long("no-fetch")
            .global(true)
            .help("reads: skip the throttled fetch")
            .action(ArgAction::SetTrue)
            .num_args(0..=1)
            .require_equals(true)
            .default_missing_value("true"),
    );
    let root = root.arg(
        Arg::new("no-sync")
            .long("no-sync")
            .global(true)
            .help("mutations: commit locally without fetching or pushing")
            .action(ArgAction::SetTrue)
            .num_args(0..=1)
            .require_equals(true)
            .default_missing_value("true"),
    );
    let root = root.arg(
        Arg::new("project")
            .long("project")
            .global(true)
            .help("project name (overrides $BEANS_PROJECT and git auto-detection)")
            .num_args(1),
    );
    root.subcommands(super::schema::commands())
}
pub fn execute(m: clap::ArgMatches) -> Result<(), Error> {
    let paths = paths::default_paths(OsStr::new(&value(&m, "hub")))?;
    let config = load_user_config(&paths.config)?;
    let mut actor = Actor::new(value(&m, "actor").as_bytes());
    let actor = String::from_utf8_lossy(actor.resolve(config.actor.as_bytes())).into_owned();
    let (name, sub) = m.subcommand().unwrap();
    let branch_flag = value(&m, "branch");
    let branch = if !branch_flag.is_empty() {
        branch_flag
    } else if !config.hub.branch.is_empty() {
        config.hub.branch.to_string()
    } else {
        let mut cmd = std::process::Command::new("git");
        if name == "init" {
            cmd.args(["ls-remote", "--symref", "--", &value(sub, "remote"), "HEAD"]);
        } else {
            cmd.arg("-C")
                .arg(&paths.hub)
                .args(["symbolic-ref", "refs/remotes/origin/HEAD"]);
        }
        cmd.env("GIT_TERMINAL_PROMPT", "0")
            .output()
            .ok()
            .filter(|o| o.status.success())
            .and_then(|o| {
                let raw = String::from_utf8_lossy(&o.stdout);
                if name == "init" {
                    raw.lines().find_map(|line| {
                        line.strip_prefix("ref: refs/heads/")
                            .and_then(|b| b.split_whitespace().next())
                            .map(str::to_owned)
                    })
                } else {
                    raw.trim()
                        .strip_prefix("refs/remotes/origin/")
                        .map(str::to_owned)
                }
            })
            .unwrap_or_else(|| "main".into())
    };
    let hub = Hub {
        dir: paths.hub.clone(),
        cache: paths.cache.clone(),
        branch,
        actor: actor.clone(),
        no_sync: m.get_flag("no-sync"),
        throttle: std::time::Duration::from_nanos(config.throttle_duration() as u64),
    };
    if name == "init" {
        let mut user: toml::Value = match std::fs::read_to_string(&paths.config) {
            Ok(raw) => toml::from_str(&raw).map_err(|e| Error::new(e.to_string()))?,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
                toml::Value::Table(Default::default())
            }
            Err(e) => return Err(Error::new(e.to_string())),
        };
        let table = user
            .as_table_mut()
            .ok_or_else(|| Error::new("user config must be a TOML table".into()))?;
        let section = table
            .entry("hub")
            .or_insert_with(|| toml::Value::Table(Default::default()));
        let section = section
            .as_table_mut()
            .ok_or_else(|| Error::new("hub config must be a TOML table".into()))?;
        section.insert("remote".into(), toml::Value::String(value(sub, "remote")));
        section.insert("branch".into(), toml::Value::String(hub.branch.clone()));
        let user = toml::to_string(&user).map_err(|e| Error::new(e.to_string()))?;
        hub.initialize(&value(sub, "remote"))?;
        crate::gitops::write_file(&paths.config, user.as_bytes())?;
        if m.get_flag("json") {
            println!(
                "{}",
                serde_json::json!({"hub":hub.dir,"remote":value(sub,"remote"),"branch":hub.branch,"config":paths.config})
            );
        } else {
            println!("Initialized {}", hub.dir.display());
        }
        return Ok(());
    }
    if name == "prime" {
        print!("{}", include_str!("../../docs/prime.md"));
        return Ok(());
    }
    if name == "man" {
        print!("{}", super::manual::render());
        return Ok(());
    }
    paths::check_hub(&paths)?;
    if name == "serve" {
        return crate::server::run(
            hub,
            value(&m, "project"),
            &value(sub, "host"),
            &value(sub, "port"),
        );
    }
    if name == "sync" {
        hub.sync()?;
        if m.get_flag("json") {
            println!(
                "{}",
                serde_json::json!({"synced":true,"status":hub.status()?})
            );
        } else {
            println!("Synced");
        }
        return Ok(());
    }
    if name == "status" {
        println!("{}", hub.status()?);
        return Ok(());
    }
    super::dispatch::execute(&m, &hub, &actor)
}
