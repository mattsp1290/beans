use clap::{Arg, ArgAction, Command};

fn flag(name: &'static str, help: &'static str) -> Arg {
    Arg::new(name)
        .long(name)
        .help(help)
        .action(ArgAction::SetTrue)
        .num_args(0..=1)
        .require_equals(true)
        .default_missing_value("true")
}

fn body_args(command: Command) -> Command {
    command
        .arg(
            Arg::new("description")
                .long("description")
                .short('d')
                .help("prompt Markdown")
                .num_args(1),
        )
        .arg(
            Arg::new("body-file")
                .long("body-file")
                .help("read prompt Markdown from file")
                .num_args(1),
        )
        .arg(flag("stdin", "read prompt Markdown from stdin"))
}

pub(super) fn commands() -> Vec<Command> {
    let create = body_args(
        Command::new("create")
            .about("Create a prompt")
            .arg(Arg::new("title").index(1).required(true))
            .arg(Arg::new("to").long("to").help("recipient").required(true))
            .arg(Arg::new("from").long("from").help("sender"))
            .arg(Arg::new("issue").long("issue").help("linked issue id"))
            .arg(
                Arg::new("label")
                    .long("label")
                    .short('l')
                    .help("label (repeatable)")
                    .action(ArgAction::Append),
            )
            .arg(
                Arg::new("priority")
                    .long("priority")
                    .short('p')
                    .help("priority 0 to 4")
                    .value_parser(clap::value_parser!(i64).range(0..=4))
                    .default_value("2"),
            ),
    );
    let list = Command::new("list")
        .about("List prompts")
        .arg(flag("all-projects", "every project"))
        .arg(Arg::new("from").long("from").help("only this sender"))
        .arg(Arg::new("to").long("to").help("only this recipient"))
        .arg(
            Arg::new("claimed-by")
                .long("claimed-by")
                .help("only this claimant"),
        )
        .arg(Arg::new("status").long("status").help("only this status"))
        .arg(Arg::new("label").long("label").help("only this label"))
        .arg(
            Arg::new("priority")
                .long("priority")
                .short('p')
                .help("only this priority")
                .value_parser(clap::value_parser!(i64).range(0..=4)),
        )
        .arg(Arg::new("query").long("query").help("text query"))
        .arg(flag("terminal", "include terminal prompts"));
    let inbox = Command::new("inbox")
        .about("List prompts addressed to you")
        .arg(
            Arg::new("to")
                .long("to")
                .help("recipient (default: resolved actor)"),
        )
        .arg(flag("all-projects", "every project"))
        .arg(flag(
            "claimed",
            "include prompts claimed by the selected actor even if addressed elsewhere",
        ))
        .arg(flag("terminal", "include terminal prompts"))
        .arg(
            Arg::new("limit")
                .long("limit")
                .help("maximum prompts (0 = all)")
                .value_parser(clap::value_parser!(i64).range(0..))
                .default_value("50"),
        );
    let update = body_args(
        Command::new("update")
            .about("Update a prompt")
            .arg(Arg::new("id").index(1).required(true))
            .arg(flag("claim", "claim this prompt"))
            .arg(Arg::new("status").long("status").help("new status"))
            .arg(flag("force", "allow reassignment or status correction"))
            .arg(Arg::new("title").long("title").help("new title"))
            .arg(Arg::new("to").long("to").help("new recipient"))
            .arg(
                Arg::new("priority")
                    .long("priority")
                    .help("new priority")
                    .value_parser(clap::value_parser!(i64).range(0..=4)),
            )
            .arg(
                Arg::new("label")
                    .long("label")
                    .help("add label (repeatable)")
                    .action(ArgAction::Append),
            )
            .arg(
                Arg::new("unlabel")
                    .long("unlabel")
                    .help("remove label (repeatable)")
                    .action(ArgAction::Append),
            )
            .arg(Arg::new("note").long("note").help("audit note")),
    );
    vec![
        Command::new("prompt")
            .about("Create and manage agent prompts")
            .subcommand_required(true)
            .arg_required_else_help(true)
            .subcommand(create)
            .subcommand(list)
            .subcommand(inbox)
            .subcommand(
                Command::new("show")
                    .about("Show a prompt")
                    .arg(Arg::new("id").index(1).required(true)),
            )
            .subcommand(update)
            .subcommand(
                Command::new("link")
                    .about("Link a prompt to an issue")
                    .arg(Arg::new("id").index(1).required(true))
                    .arg(Arg::new("issue-id").index(2).required(true)),
            )
            .subcommand(
                Command::new("unlink")
                    .about("Unlink a prompt from its issue")
                    .arg(Arg::new("id").index(1).required(true)),
            ),
    ]
}
