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
    let (name, _) = m.subcommand().unwrap();
    if name == "prime" {
        print!("{}", include_str!("../../docs/prime.md"));
        return Ok(());
    }
    if name == "man" {
        print!("{}", super::manual::render());
        return Ok(());
    }
    let paths = paths::default_paths(OsStr::new(&value(&m, "hub")))?;
    let config = load_user_config(&paths.config)?;
    let executor = crate::gitops::GitExecutor {
        policy: config.git.policy()?,
    };
    let _diagnostics = crate::gitops::diagnostics::scope(executor.policy.diagnostics, "cli");
    crate::gitops::diagnostics::operation(executor.policy.diagnostics, "cli", || {
        execute_configured(m, paths, config, executor)
    })
}
fn execute_configured(
    m: clap::ArgMatches,
    paths: paths::Paths,
    config: crate::domain::config::UserConfig,
    executor: crate::gitops::GitExecutor,
) -> Result<(), Error> {
    let (name, sub) = m.subcommand().unwrap();
    let mut actor = Actor::new(value(&m, "actor").as_bytes());
    let actor = String::from_utf8_lossy(actor.resolve_policy(config.actor.as_bytes(), &executor))
        .into_owned();
    let branch_flag = value(&m, "branch");
    let branch = if !branch_flag.is_empty() {
        branch_flag
    } else if !config.hub.branch.is_empty() {
        config.hub.branch.to_string()
    } else {
        let result = if name == "init" {
            executor.run(
                None,
                ["ls-remote", "--symref", "--", &value(sub, "remote"), "HEAD"],
                "ls-remote",
            )
        } else {
            executor.run(
                Some(&paths.hub),
                ["symbolic-ref", "refs/remotes/origin/HEAD"],
                "symbolic-ref",
            )
        };
        if name == "init"
            && let Err(e) = &result
        {
            return Err(e.clone());
        }
        let result = if name == "init" {
            result.and_then(|o| o.checked("ls-remote"))
        } else {
            result
        };
        if name == "init"
            && let Err(e) = &result
        {
            return Err(e.clone());
        }
        result
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
    let mut hub = Hub::new(
        paths.hub.clone(),
        paths.cache.clone(),
        branch,
        actor.clone(),
        m.get_flag("no-sync"),
        std::time::Duration::from_nanos(config.throttle_duration() as u64),
    )?;
    hub.executor = executor;
    let resolver = crate::vault::PolicyGit(hub.executor.clone());
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
    if name == "cache" {
        hub.clear_cache()?;
        println!("{}", serde_json::json!({"cleared":true}));
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
        let mut status = hub.status()?;
        match crate::vault::resolve(
            &hub.dir,
            crate::vault::ResolveOptions {
                flag_project: value(&m, "project").as_bytes(),
                git: Some(&resolver),
                ..Default::default()
            },
        ) {
            Ok(resolved) => {
                status["project"] = String::from_utf8_lossy(&resolved.project)
                    .into_owned()
                    .into();
                status["project_dir"] = resolved.project_dir.to_string_lossy().into_owned().into();
                status["resolution"] = if resolved.project.is_empty() {
                    "none"
                } else {
                    "resolved"
                }
                .into();
            }
            Err(error) => status["resolution"] = error.to_string().into(),
        }
        println!("{status}");
        return Ok(());
    }
    super::dispatch::execute(&m, &hub, &actor)
}
