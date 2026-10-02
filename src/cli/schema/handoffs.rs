use clap::{Arg, ArgAction, Command};
pub(super) fn commands() -> Vec<Command> {
    vec![
        Command::new("handoff")
            .about("Create and manage versioned session handoffs")
            .subcommand_required(true)
            .arg_required_else_help(true)
            .subcommand(
                Command::new("archive")
                    .about("")
                    .arg(Arg::new("id").index(1).num_args(0..))
                    .arg(
                        Arg::new("all-projects")
                            .long("all-projects")
                            .help("all projects")
                            .action(ArgAction::SetTrue)
                            .num_args(0..=1)
                            .require_equals(true)
                            .default_missing_value("true"),
                    )
                    .arg(
                        Arg::new("dry-run")
                            .long("dry-run")
                            .help("report without moving")
                            .action(ArgAction::SetTrue)
                            .num_args(0..=1)
                            .require_equals(true)
                            .default_missing_value("true"),
                    )
                    .arg(
                        Arg::new("older-than")
                            .long("older-than")
                            .help("age")
                            .num_args(1),
                    ),
            )
            .subcommand(
                Command::new("attach")
                    .about("")
                    .arg(Arg::new("id").index(1).required(true))
                    .arg(Arg::new("issue-id").index(2).required(true)),
            )
            .subcommand(
                Command::new("create")
                    .about("")
                    .arg(Arg::new("title").index(1))
                    .arg(
                        Arg::new("file")
                            .long("file")
                            .help("Markdown source file or - for stdin")
                            .num_args(1),
                    )
                    .arg(
                        Arg::new("issue")
                            .long("issue")
                            .help("attach to issue ID")
                            .num_args(1),
                    )
                    .arg(
                        Arg::new("silent")
                            .long("silent")
                            .help("print only the handoff ID")
                            .action(ArgAction::SetTrue)
                            .num_args(0..=1)
                            .require_equals(true)
                            .default_missing_value("true"),
                    ),
            )
            .subcommand(
                Command::new("detach")
                    .about("")
                    .arg(Arg::new("id").index(1).required(true)),
            )
            .subcommand(
                Command::new("list")
                    .about("")
                    .arg(
                        Arg::new("all-projects")
                            .long("all-projects")
                            .help("all projects")
                            .action(ArgAction::SetTrue)
                            .num_args(0..=1)
                            .require_equals(true)
                            .default_missing_value("true"),
                    )
                    .arg(
                        Arg::new("archived")
                            .long("archived")
                            .help("include archived handoffs")
                            .action(ArgAction::SetTrue)
                            .num_args(0..=1)
                            .require_equals(true)
                            .default_missing_value("true"),
                    )
                    .arg(
                        Arg::new("issue")
                            .long("issue")
                            .help("only handoffs attached to issue")
                            .num_args(1),
                    )
                    .arg(
                        Arg::new("limit")
                            .long("limit")
                            .help("maximum entries (0 = all)")
                            .value_parser(clap::value_parser!(i64))
                            .allow_negative_numbers(true)
                            .default_value("50"),
                    )
                    .arg(
                        Arg::new("older-than")
                            .long("older-than")
                            .help("updated before age")
                            .num_args(1),
                    )
                    .arg(
                        Arg::new("sort")
                            .long("sort")
                            .help("created or updated")
                            .num_args(1)
                            .default_value("created"),
                    ),
            )
            .subcommand(
                Command::new("restore")
                    .about("")
                    .arg(Arg::new("id").index(1).required(true).num_args(1..)),
            )
            .subcommand(
                Command::new("show")
                    .about("")
                    .arg(Arg::new("id").index(1).required(true))
                    .arg(
                        Arg::new("raw")
                            .long("raw")
                            .help("write exact stored Markdown")
                            .action(ArgAction::SetTrue)
                            .num_args(0..=1)
                            .require_equals(true)
                            .default_missing_value("true"),
                    ),
            ),
    ]
}
