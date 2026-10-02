use super::Actor;
use crate::{
    domain::{config::load_user_config, frontmatter::Error},
    gitops::Hub,
    ops::{IssueChange, IssueMutation},
    vault::{Index, LoadOptions, NoteData, ResolveOptions, paths, resolve},
};
use clap::{Arg, ArgAction, Command};
use std::{ffi::OsStr, io::Write, path::PathBuf};
fn value(m: &clap::ArgMatches, name: &str) -> String {
    m.get_one::<String>(name).cloned().unwrap_or_default()
}
fn arg(name: &'static str) -> Arg {
    Arg::new(name).long(name).num_args(1)
}
pub fn command() -> Command {
    Command::new("bn")
        .version(env!("CARGO_PKG_VERSION"))
        .arg(arg("hub").global(true))
        .arg(arg("project").global(true))
        .arg(arg("actor").global(true))
        .arg(arg("branch").global(true))
        .arg(
            Arg::new("no-sync")
                .long("no-sync")
                .global(true)
                .action(ArgAction::SetTrue),
        )
        .arg(
            Arg::new("json")
                .long("json")
                .global(true)
                .action(ArgAction::SetTrue),
        )
        .subcommand_required(true)
        .arg_required_else_help(true)
        .subcommand(Command::new("init").arg(Arg::new("remote").required(true)))
        .subcommand(
            Command::new("create")
                .arg(Arg::new("title").required(true))
                .arg(arg("type").short('t').default_value("task"))
                .arg(
                    arg("priority")
                        .short('p')
                        .value_parser(clap::value_parser!(i64))
                        .default_value("2"),
                )
                .arg(arg("description").short('d')),
        )
        .subcommand(Command::new("ready"))
        .subcommand(Command::new("list").arg(arg("status")))
        .subcommand(Command::new("show").arg(Arg::new("id").required(true)))
        .subcommand(
            Command::new("update")
                .arg(Arg::new("id").required(true))
                .arg(Arg::new("claim").long("claim").action(ArgAction::SetTrue))
                .arg(arg("title"))
                .arg(arg("status"))
                .arg(arg("assignee"))
                .arg(arg("priority").value_parser(clap::value_parser!(i64))),
        )
        .subcommand(
            Command::new("note")
                .arg(Arg::new("id").required(true))
                .arg(Arg::new("text").required(true)),
        )
        .subcommand(
            Command::new("close")
                .arg(Arg::new("id").required(true))
                .arg(arg("reason").short('r').default_value("completed")),
        )
        .subcommand(Command::new("status"))
        .subcommand(Command::new("sync"))
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
        println!("Initialized {}", hub.dir.display());
        return Ok(());
    }
    paths::check_hub(&paths)?;
    if name == "sync" {
        hub.sync()?;
        println!("Synced");
        return Ok(());
    }
    if name == "status" {
        println!("{}", hub.status()?);
        return Ok(());
    }
    let write = matches!(name, "create" | "update" | "note" | "close");
    if !write
        && !hub.no_sync
        && let Err(e) = hub.refresh()
    {
        eprintln!("bn: using local hub: {e}");
    }
    let resolved = resolve(
        &paths.hub,
        ResolveOptions {
            flag_project: value(&m, "project").as_bytes(),
            write,
            ..ResolveOptions::default()
        },
    )?;
    let explicit = std::env::var_os("BN_CONFIG")
        .filter(|v| !v.is_empty())
        .map(PathBuf::from);
    if write {
        let change = match name {
            "create" => IssueChange::Create {
                title: value(sub, "title"),
                kind: value(sub, "type"),
                priority: *sub.get_one::<i64>("priority").unwrap(),
                description: sub.get_one::<String>("description").cloned(),
            },
            "update" => IssueChange::Update {
                claim: sub.get_flag("claim"),
                title: sub.get_one::<String>("title").cloned(),
                status: sub.get_one::<String>("status").cloned(),
                assignee: sub.get_one::<String>("assignee").cloned(),
                priority: sub.get_one::<i64>("priority").copied(),
            },
            "note" => IssueChange::Note(value(sub, "text")),
            "close" => IssueChange::Close(value(sub, "reason")),
            _ => unreachable!(),
        };
        let mut op = IssueMutation::new(
            resolved,
            if name == "create" {
                String::new()
            } else {
                value(sub, "id")
            },
            change,
            actor,
            explicit,
        );
        let result = hub.mutate(&mut op)?;
        if m.get_flag("json") {
            println!(
                "{}",
                serde_json::json!({"id": op.id, "sha": result.sha, "pushed": result.pushed, "message": result.message})
            );
        } else {
            println!("{}", op.id);
            if !result.message.is_empty() {
                eprintln!("bn: {}", result.message);
            }
        }
        return Ok(());
    }
    let index = Index::load_snapshot(
        &paths.hub,
        LoadOptions {
            explicit_workflow: explicit,
        },
    )?;
    if name == "show" {
        let (_, note) = index.resolve_issue_ref(value(sub, "id").as_bytes());
        let note = note
            .ok_or_else(|| Error::new("issue not found (check malformed hub documents)".into()))?;
        if let NoteData::Issue(issue) = &note.data {
            if m.get_flag("json") {
                println!(
                    "{}",
                    serde_json::to_string(&issue.metadata)
                        .map_err(|e| Error::new(e.to_string()))?
                );
            } else {
                std::io::stdout()
                    .write_all(&note.source)
                    .map_err(|e| Error::new(e.to_string()))?;
            }
        }
    } else {
        let notes = if name == "ready" {
            index.ready(&resolved.project, false)
        } else {
            index.project_issues(&resolved.project, false)
        };
        let status = if name == "list" {
            value(sub, "status")
        } else {
            String::new()
        };
        let issues: Vec<_> = notes
            .iter()
            .filter_map(|note| {
                if let NoteData::Issue(issue) = &note.data {
                    Some(&issue.metadata)
                } else {
                    None
                }
            })
            .filter(|issue| status.is_empty() || issue.status == status)
            .collect();
        if m.get_flag("json") {
            println!(
                "{}",
                serde_json::to_string(&issues).map_err(|e| Error::new(e.to_string()))?
            );
        } else {
            for issue in issues {
                println!("{}\t{}\t{}", issue.id, issue.status, issue.title);
            }
        }
    }
    Ok(())
}
