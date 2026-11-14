use clap::{Arg, ArgAction, Command};

fn channel(command: Command) -> Command {
    command.arg(Arg::new("id").index(1).required(true))
}
fn repeated(name: &'static str) -> Arg {
    Arg::new(name).long(name).action(ArgAction::Append)
}
fn unread(command: Command) -> Command {
    command.arg(Arg::new("unread").long("unread").action(ArgAction::SetTrue))
}
fn cursor(command: Command) -> Command {
    command
        .arg(
            Arg::new("cursor")
                .long("cursor")
                .conflicts_with("cursor-file")
                .help("JSON cursor returned by read or wait"),
        )
        .arg(
            Arg::new("cursor-file")
                .long("cursor-file")
                .help("read a JSON cursor from a file, or - for stdin"),
        )
}
pub(super) fn commands() -> Vec<Command> {
    vec![
        Command::new("channel")
            .about("Coordinate ongoing conversations through Git")
            .subcommand_required(true)
            .arg_required_else_help(true)
            .subcommand(
                Command::new("create")
                    .about("Create a project conversation including yourself")
                    .arg(Arg::new("name").index(1).required(true))
                    .arg(repeated("participant").help("add an actor identity"))
                    .arg(
                        Arg::new("issue")
                            .long("issue")
                            .help("associate an issue or epic"),
                    ),
            )
            .subcommand(unread(
                Command::new("list").about("Discover your channels and unread counts"),
            ))
            .subcommand(channel(
                Command::new("join")
                    .about("Join a channel or add a participant with full history")
                    .arg(Arg::new("participant").long("participant")),
            ))
            .subcommand(channel(
                Command::new("send")
                    .about("Send Markdown with a retry-safe idempotency key")
                    .arg(
                        Arg::new("key")
                            .long("key")
                            .required(true)
                            .help("reuse this key and payload when retrying"),
                    )
                    .arg(
                        Arg::new("description")
                            .long("description")
                            .short('d')
                            .help("message Markdown"),
                    )
                    .arg(
                        Arg::new("file")
                            .long("file")
                            .help("read Markdown from a file, or - for stdin"),
                    )
                    .arg(
                        repeated("to")
                            .help("address an existing participant; omitted means broadcast"),
                    )
                    .arg(
                        Arg::new("reply-to")
                            .long("reply-to")
                            .help("parent message ID"),
                    )
                    .arg(
                        repeated("reference")
                            .help("prompt, issue, PR, commit, or handoff reference"),
                    )
                    .arg(
                        Arg::new("session")
                            .long("session")
                            .help("optional session or machine label, independent of actor"),
                    ),
            ))
            .subcommand(cursor(unread(channel(Command::new("read").about(
                "Read history without acknowledging; a cursor selects newly observed IDs",
            )))))
            .subcommand(channel(
                Command::new("ack")
                    .about("Explicitly acknowledge selected messages for your actor only")
                    .arg(repeated("message").required(true)),
            ))
            .subcommand(cursor(channel(
                Command::new("wait")
                    .about("Poll Git for new messages; timeout succeeds, sync errors fail")
                    .arg(
                        Arg::new("timeout")
                            .long("timeout")
                            .value_parser(clap::value_parser!(u64).range(1..=86400))
                            .default_value("30")
                            .help("maximum wait seconds, plus bounded process cleanup"),
                    )
                    .arg(
                        Arg::new("interval")
                            .long("interval")
                            .value_parser(clap::value_parser!(u64).range(1..=3600))
                            .default_value("2")
                            .help("Git polling interval in seconds"),
                    ),
            ))),
    ]
}
