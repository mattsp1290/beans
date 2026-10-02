use clap::{Arg, ArgAction, Command};
pub(super) fn commands() -> Vec<Command> {
    vec![
        Command::new("request")
            .about("Create and manage project requests")
            .subcommand_required(true)
            .arg_required_else_help(true)
            .subcommand(
                Command::new("create")
                    .about("Create a request")
                    .arg(Arg::new("title").index(1).required(true))
                    .arg(
                        Arg::new("body-file")
                            .long("body-file")
                            .help("read request Markdown from file")
                            .num_args(1),
                    )
                    .arg(
                        Arg::new("description")
                            .long("description")
                            .help("request Markdown")
                            .short('d')
                            .num_args(1),
                    )
                    .arg(
                        Arg::new("issue")
                            .long("issue")
                            .help("linked issue id (repeatable)")
                            .action(ArgAction::Append)
                            .num_args(1),
                    )
                    .arg(
                        Arg::new("label")
                            .long("label")
                            .help("label (repeatable)")
                            .short('l')
                            .action(ArgAction::Append)
                            .num_args(1),
                    )
                    .arg(
                        Arg::new("priority")
                            .long("priority")
                            .help("priority 0 to 4")
                            .short('p')
                            .value_parser(clap::value_parser!(i64))
                            .allow_negative_numbers(true)
                            .default_value("2"),
                    )
                    .arg(
                        Arg::new("requested-by")
                            .long("requested-by")
                            .help("requester")
                            .num_args(1),
                    )
                    .arg(
                        Arg::new("silent")
                            .long("silent")
                            .help("print only the new id")
                            .action(ArgAction::SetTrue)
                            .num_args(0..=1)
                            .require_equals(true)
                            .default_missing_value("true"),
                    )
                    .arg(
                        Arg::new("stdin")
                            .long("stdin")
                            .help("read request Markdown from stdin")
                            .action(ArgAction::SetTrue)
                            .num_args(0..=1)
                            .require_equals(true)
                            .default_missing_value("true"),
                    ),
            )
            .subcommand(
                Command::new("link")
                    .about("link issue links")
                    .arg(Arg::new("id").index(1).required(true))
                    .arg(Arg::new("issue-id").index(2).required(true).num_args(1..)),
            )
            .subcommand(
                Command::new("list")
                    .about("List requests")
                    .arg(
                        Arg::new("all-projects")
                            .long("all-projects")
                            .help("every project")
                            .action(ArgAction::SetTrue)
                            .num_args(0..=1)
                            .require_equals(true)
                            .default_missing_value("true"),
                    )
                    .arg(
                        Arg::new("label")
                            .long("label")
                            .help("only this label")
                            .num_args(1),
                    )
                    .arg(
                        Arg::new("priority")
                            .long("priority")
                            .help("only this priority")
                            .short('p')
                            .value_parser(clap::value_parser!(i64))
                            .allow_negative_numbers(true),
                    )
                    .arg(
                        Arg::new("query")
                            .long("query")
                            .help("text query")
                            .num_args(1),
                    )
                    .arg(
                        Arg::new("status")
                            .long("status")
                            .help("only this status")
                            .num_args(1),
                    )
                    .arg(
                        Arg::new("terminal")
                            .long("terminal")
                            .help("include terminal requests")
                            .action(ArgAction::SetTrue)
                            .num_args(0..=1)
                            .require_equals(true)
                            .default_missing_value("true"),
                    ),
            )
            .subcommand(
                Command::new("show")
                    .about("Show a request")
                    .arg(Arg::new("id").index(1).required(true)),
            )
            .subcommand(
                Command::new("unlink")
                    .about("unlink issue links")
                    .arg(Arg::new("id").index(1).required(true))
                    .arg(Arg::new("issue-id").index(2).required(true).num_args(1..)),
            )
            .subcommand(
                Command::new("update")
                    .about("Update a request")
                    .arg(Arg::new("id").index(1).required(true))
                    .arg(
                        Arg::new("body-file")
                            .long("body-file")
                            .help("read body from file")
                            .num_args(1),
                    )
                    .arg(
                        Arg::new("description")
                            .long("description")
                            .help("new request body")
                            .short('d')
                            .num_args(1),
                    )
                    .arg(
                        Arg::new("force")
                            .long("force")
                            .help("allow any valid status correction")
                            .action(ArgAction::SetTrue)
                            .num_args(0..=1)
                            .require_equals(true)
                            .default_missing_value("true"),
                    )
                    .arg(
                        Arg::new("label")
                            .long("label")
                            .help("add label")
                            .action(ArgAction::Append)
                            .num_args(1),
                    )
                    .arg(
                        Arg::new("priority")
                            .long("priority")
                            .help("new priority")
                            .value_parser(clap::value_parser!(i64))
                            .allow_negative_numbers(true),
                    )
                    .arg(
                        Arg::new("requested-by")
                            .long("requested-by")
                            .help("new requester")
                            .num_args(1),
                    )
                    .arg(
                        Arg::new("status")
                            .long("status")
                            .help("new status")
                            .num_args(1),
                    )
                    .arg(
                        Arg::new("stdin")
                            .long("stdin")
                            .help("read body from stdin")
                            .action(ArgAction::SetTrue)
                            .num_args(0..=1)
                            .require_equals(true)
                            .default_missing_value("true"),
                    )
                    .arg(
                        Arg::new("title")
                            .long("title")
                            .help("new title")
                            .num_args(1),
                    )
                    .arg(
                        Arg::new("unlabel")
                            .long("unlabel")
                            .help("remove label")
                            .action(ArgAction::Append)
                            .num_args(1),
                    ),
            ),
    ]
}
