use super::dispatch::{body, flag, optional, output, value, values};
use crate::{
    domain::{
        channel::{Cursor, ReadResult},
        error::ErrorCategory,
        frontmatter::Error,
    },
    gitops::Hub,
    ops::{ChannelChange, ChannelMutation, ChannelStore},
    vault::Resolved,
};
use clap::ArgMatches;
use serde_json::json;
use std::time::{Duration, Instant};

fn parsed_cursor(matches: &ArgMatches) -> Result<Option<Cursor>, Error> {
    let raw = match optional(matches, "cursor-file") {
        Some(path) if path == "-" => {
            let mut raw = String::new();
            std::io::Read::read_to_string(&mut std::io::stdin(), &mut raw)
                .map_err(|e| Error::new(format!("read channel cursor: {e}")))?;
            Some(raw)
        }
        Some(path) => Some(
            std::fs::read_to_string(path)
                .map_err(|e| Error::new(format!("read channel cursor: {e}")))?,
        ),
        None => optional(matches, "cursor"),
    };
    raw.map(|raw| {
        serde_json::from_str(&raw).map_err(|e| Error::new(format!("invalid channel cursor: {e}")))
    })
    .transpose()
}
#[derive(serde::Serialize)]
struct Freshness {
    source: &'static str,
    refresh_skipped: bool,
    refresh_reason: &'static str,
    last_fetch: Option<String>,
    last_fetch_attempt: Option<String>,
    sync_error: Option<String>,
}
fn fetch_stamp(hub: &Hub, name: &str) -> Option<String> {
    std::fs::read_to_string(hub.state_path().join(name))
        .ok()
        .map(|s| s.trim().to_owned())
}
fn freshness(hub: &Hub, error: Option<&Error>, reason: &'static str) -> Freshness {
    let last_fetch = fetch_stamp(hub, "last-fetch");
    let last_fetch_attempt = fetch_stamp(hub, "last-fetch-attempt");
    let parse = |raw: &Option<String>| {
        raw.as_deref().and_then(|raw| {
            time::OffsetDateTime::parse(raw, &time::format_description::well_known::Rfc3339).ok()
        })
    };
    let previous_failure = reason == "throttled"
        && parse(&last_fetch_attempt)
            .is_some_and(|attempt| parse(&last_fetch).is_none_or(|fetched| attempt > fetched));
    Freshness {
        source: "local",
        refresh_skipped: matches!(reason, "disabled" | "throttled" | "busy"),
        refresh_reason: reason,
        last_fetch,
        last_fetch_attempt,
        sync_error: error.map(ToString::to_string).or_else(|| {
            previous_failure
                .then(|| "last refresh attempt did not complete; using cached local state".into())
        }),
    }
}
fn read_freshness(hub: &Hub, disabled: bool) -> Freshness {
    if disabled {
        return freshness(hub, None, "disabled");
    }
    let before = fetch_stamp(hub, "last-fetch-attempt");
    let error = hub.refresh().err();
    if let Some(error) = &error {
        crate::gitops::diagnostics::warning(b"bn: reading local channels: ", error);
    }
    let reason = if error.is_some() {
        "failed"
    } else if before == fetch_stamp(hub, "last-fetch-attempt") {
        "throttled"
    } else {
        "refreshed"
    };
    freshness(hub, error.as_ref(), reason)
}
#[derive(serde::Serialize)]
struct ReadResponse<'a> {
    #[serde(flatten)]
    read: &'a ReadResult,
    status: &'static str,
    freshness: Freshness,
    #[serde(skip_serializing_if = "Option::is_none")]
    channel: Option<&'a crate::domain::channel::Channel>,
    #[serde(skip_serializing_if = "Option::is_none")]
    members: Option<&'a std::collections::BTreeSet<String>>,
    warnings: &'a [crate::ops::channels::ChannelWarning],
    #[serde(skip_serializing_if = "Option::is_none")]
    acknowledged:
        Option<&'a std::collections::BTreeMap<String, std::collections::BTreeSet<String>>>,
}
fn print_read(
    read: &ReadResult,
    status: &'static str,
    freshness: Freshness,
    context: Option<&crate::domain::channel::ChannelView>,
    warnings: &[crate::ops::channels::ChannelWarning],
    json_output: bool,
) -> Result<(), Error> {
    if json_output {
        return output(
            &serde_json::to_value(ReadResponse {
                read,
                status,
                freshness,
                channel: context.map(|v| &v.channel),
                members: context.map(|v| &v.members),
                acknowledged: context.map(|v| &v.acknowledged),
                warnings,
            })
            .expect("channel response is serializable"),
        );
    }
    for message in &read.messages {
        println!("{}\t{}\t{}", message.id, message.actor, message.body);
    }
    println!(
        "cursor {}",
        serde_json::to_string(&read.cursor).expect("cursor is serializable")
    );
    if status != "messages" {
        eprintln!("bn: channel {status}");
    }
    Ok(())
}
#[derive(serde::Serialize)]
struct ChannelListing {
    #[serde(flatten)]
    channel: crate::domain::channel::Channel,
    unread: usize,
}

pub fn execute(
    matches: &ArgMatches,
    hub: &Hub,
    resolved: Resolved,
    actor: &str,
    json_output: bool,
    no_fetch: bool,
) -> Result<(), Error> {
    let (action, leaf) = matches.subcommand().expect("channel requires a subcommand");
    let project = String::from_utf8(resolved.project.clone())
        .map_err(|_| Error::new("channels require a UTF-8 project identity".into()))?;
    if action == "wait" {
        if hub.no_sync || no_fetch {
            return Err(Error::new(
                "channel wait requires remote refresh; omit --no-sync and --no-fetch".into(),
            ));
        }
        return wait(leaf, hub, &project, actor, json_output);
    }
    if matches!(action, "read" | "list") {
        let read_freshness = read_freshness(hub, hub.no_sync || no_fetch);
        let catalog = ChannelStore::scan(&hub.dir, &project)?;
        let warnings = catalog.warnings.clone();
        for warning in &warnings {
            eprintln!("bn: {}: {}", warning.path, warning.message);
        }
        if action == "list" {
            let channels: Vec<_> = catalog
                .channels
                .into_iter()
                .filter(|view| view.members.contains(actor))
                .filter_map(|view| {
                    let unread = view.unread(actor).len();
                    if flag(leaf, "unread") && unread == 0 {
                        return None;
                    }
                    let mut channel = view.channel;
                    channel.members = view.members.into_iter().collect();
                    Some(ChannelListing { channel, unread })
                })
                .collect();
            if json_output {
                return output(
                    &json!({"channels":channels,"warnings":warnings,"freshness":read_freshness}),
                );
            }
            for channel in channels {
                println!(
                    "{}\t{}\t{} unread",
                    channel.channel.id, channel.channel.name, channel.unread
                );
            }
            return Ok(());
        }
        let view = catalog.resolve(&value(leaf, "id"))?;
        let previous = parsed_cursor(leaf)?;
        let read = view.read(actor, previous.as_ref(), flag(leaf, "unread"))?;
        return print_read(
            &read,
            "messages",
            read_freshness,
            Some(&view),
            &warnings,
            json_output,
        );
    }
    let channel = value(leaf, "id");
    let key = optional(leaf, "key");
    let change = match action {
        "create" => ChannelChange::Create {
            name: value(leaf, "name"),
            members: values(leaf, "participant"),
            issue: optional(leaf, "issue"),
        },
        "join" => ChannelChange::Join {
            channel,
            actor: optional(leaf, "participant"),
        },
        "send" => ChannelChange::Send {
            channel,
            key: value(leaf, "key"),
            body: body(leaf, "file")?.ok_or_else(|| {
                Error::new("channel send requires --description or --file".into())
            })?,
            to: values(leaf, "to"),
            reply_to: optional(leaf, "reply-to"),
            references: values(leaf, "reference"),
            session: optional(leaf, "session"),
        },
        "ack" => ChannelChange::Ack {
            channel,
            messages: values(leaf, "message"),
        },
        _ => return Err(Error::new("unknown channel action".into())),
    };
    let mut operation = ChannelMutation::new(resolved, actor.into(), change)?;
    match hub.mutate(&mut operation) {
        Ok(result) => {
            let payload = json!({"id":operation.id(),"key":key,"commit":result.sha,"sha":result.sha,"pushed":result.pushed,"delivery":if result.pushed {"published"} else {"local"},"message":result.message});
            if json_output {
                output(&payload)
            } else {
                println!("{}", operation.id());
                if !result.message.is_empty() {
                    eprintln!("bn: {}", result.message);
                }
                Ok(())
            }
        }
        Err(error) => {
            if json_output {
                let delivery = operation.failure_delivery();
                let mut response = json!({"id":operation.id(),"key":key,"pushed":false,"delivery":delivery,"error":error.to_string()});
                if delivery == "uncertain" {
                    response["retry"] = json!(
                        "reuse the same channel, actor, key, and payload; run bn sync to recover"
                    );
                }
                output(&response)?;
            }
            Err(error)
        }
    }
}

fn wait(
    matches: &ArgMatches,
    hub: &Hub,
    project: &str,
    actor: &str,
    json_output: bool,
) -> Result<(), Error> {
    let timeout = Duration::from_secs(*matches.get_one::<u64>("timeout").expect("timeout default"));
    let interval = Duration::from_secs(
        *matches
            .get_one::<u64>("interval")
            .expect("interval default"),
    );
    let deadline = Instant::now()
        .checked_add(timeout)
        .ok_or_else(|| Error::new("channel wait timeout is too large".into()))?;
    let reference = value(matches, "id");
    let cursor = parsed_cursor(matches)?;
    if let Ok(view) = ChannelStore::resolve(&hub.dir, project, &reference) {
        if !view.members.contains(actor) {
            return Err(Error::new(
                "actor is not a channel member; join first".into(),
            ));
        }
        view.read(actor, cursor.as_ref(), true)?;
    }
    let mut last_read = None;
    let mut reason = "refreshed";
    loop {
        let remaining = deadline.saturating_duration_since(Instant::now());
        if remaining.is_zero() || last_read.is_some() && remaining < interval {
            std::thread::sleep(remaining);
            return wait_timeout(
                hub,
                last_read.as_ref(),
                cursor.as_ref(),
                reason,
                None,
                json_output,
            );
        }
        if let Err(error) = hub.refresh_until(deadline) {
            if error.category() == Some(ErrorCategory::LockTimeout) {
                reason = "busy";
                std::thread::sleep(
                    interval.min(deadline.saturating_duration_since(Instant::now())),
                );
                continue;
            }
            if error.category() == Some(ErrorCategory::GitTimeout)
                && last_read.is_some()
                && Instant::now() >= deadline
            {
                return wait_timeout(
                    hub,
                    last_read.as_ref(),
                    cursor.as_ref(),
                    "deadline",
                    Some(&error),
                    json_output,
                );
            }
            if json_output {
                output(
                    &json!({"status":"sync_error","messages":[],"cursor":cursor,"unread":[],"freshness":freshness(hub,Some(&error),"failed")}),
                )?;
            } else {
                eprintln!("bn: channel sync_error");
            }
            return Err(error);
        }
        let view = ChannelStore::resolve(&hub.dir, project, &reference)?;
        if !view.members.contains(actor) {
            return Err(Error::new(
                "actor is not a channel member; join first".into(),
            ));
        }
        let read = view.read(actor, cursor.as_ref(), true)?;
        if !read.messages.is_empty() {
            return print_read(
                &read,
                "messages",
                freshness(hub, None, "refreshed"),
                None,
                &[],
                json_output,
            );
        }
        last_read = Some(read);
        reason = "refreshed";
        std::thread::sleep(interval.min(deadline.saturating_duration_since(Instant::now())));
    }
}
fn wait_timeout(
    hub: &Hub,
    last_read: Option<&ReadResult>,
    cursor: Option<&Cursor>,
    reason: &'static str,
    error: Option<&Error>,
    json_output: bool,
) -> Result<(), Error> {
    if let Some(read) = last_read {
        return print_read(
            read,
            "timeout",
            freshness(hub, error, reason),
            None,
            &[],
            json_output,
        );
    }
    if json_output {
        output(
            &json!({"status":"timeout","messages":[],"cursor":cursor,"unread":[],"freshness":freshness(hub,error,reason)}),
        )
    } else {
        eprintln!("bn: channel timeout");
        Ok(())
    }
}
