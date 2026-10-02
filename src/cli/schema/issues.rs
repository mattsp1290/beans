use clap::{Arg, ArgAction, Command};
pub(super) fn commands() -> Vec<Command> {
    vec![
        Command::new("parents")
            .about("List ancestor issues")
            .arg(Arg::new("id").required(true)),
        Command::new("archive")
            .about("Move closed issues older than --older-than into archive/<year>/")
            .arg(
                Arg::new("all-projects")
                    .long("all-projects")
                    .help("every project in the hub")
                    .action(ArgAction::SetTrue)
                    .num_args(0..=1)
                    .require_equals(true)
                    .default_missing_value("true"),
            )
            .arg(
                Arg::new("dry-run")
                    .long("dry-run")
                    .help("list without moving")
                    .action(ArgAction::SetTrue)
                    .num_args(0..=1)
                    .require_equals(true)
                    .default_missing_value("true"),
            )
            .arg(
                Arg::new("older-than")
                    .long("older-than")
                    .help("age of the last log entry, e.g. 30d or 12h")
                    .num_args(1)
                    .default_value("30d"),
            ),
        Command::new("blocked")
            .about("Issues waiting on at least one open blocker")
            .arg(
                Arg::new("all-projects")
                    .long("all-projects")
                    .help("every project in the hub")
                    .action(ArgAction::SetTrue)
                    .num_args(0..=1)
                    .require_equals(true)
                    .default_missing_value("true"),
            ),
        Command::new("children")
            .about("List the issues whose parent is <id>")
            .arg(Arg::new("id").index(1).required(true)),
        Command::new("close")
            .about("Close issues with a reason (one commit per id)")
            .arg(Arg::new("id").index(1).required(true).num_args(1..))
            .arg(
                Arg::new("force")
                    .long("force")
                    .help("close without a reason")
                    .action(ArgAction::SetTrue)
                    .num_args(0..=1)
                    .require_equals(true)
                    .default_missing_value("true"),
            )
            .arg(
                Arg::new("reason")
                    .long("reason")
                    .help("why the issue is closed")
                    .short('r')
                    .num_args(1),
            )
            .arg(
                Arg::new("suggest-next")
                    .long("suggest-next")
                    .help("print ids that became ready")
                    .action(ArgAction::SetTrue)
                    .num_args(0..=1)
                    .require_equals(true)
                    .default_missing_value("true"),
            ),
        Command::new("create")
            .about("Create an issue in the current project")
            .arg(Arg::new("title").index(1).required(true))
            .arg(
                Arg::new("assignee")
                    .long("assignee")
                    .help("assignee")
                    .num_args(1),
            )
            .arg(
                Arg::new("blocked-by")
                    .long("blocked-by")
                    .help("blocking issue id (repeatable)")
                    .action(ArgAction::Append)
                    .num_args(1),
            )
            .arg(
                Arg::new("description")
                    .long("description")
                    .help("description text (the body starts from the type template)")
                    .short('d')
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
                Arg::new("parent")
                    .long("parent")
                    .help("parent issue id")
                    .num_args(1),
            )
            .arg(
                Arg::new("priority")
                    .long("priority")
                    .help("priority 0 (critical) to 4 (backlog)")
                    .short('p')
                    .value_parser(clap::value_parser!(i64))
                    .allow_negative_numbers(true)
                    .default_value("2"),
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
                Arg::new("type")
                    .long("type")
                    .help("issue type")
                    .short('t')
                    .num_args(1)
                    .default_value("task"),
            )
            .arg(Arg::new("url").long("url").help("external URL").num_args(1)),
        Command::new("delete")
            .about("Delete an issue file (refuses when other notes link to it)")
            .arg(Arg::new("id").index(1).required(true))
            .arg(
                Arg::new("force")
                    .long("force")
                    .help("also remove links from other issues")
                    .action(ArgAction::SetTrue)
                    .num_args(0..=1)
                    .require_equals(true)
                    .default_missing_value("true"),
            ),
        Command::new("dep")
            .about("Manage dependencies between issues")
            .subcommand_required(true)
            .arg_required_else_help(true)
            .subcommand(
                Command::new("add")
                    .about("Make child blocked by parent (or set its parent with -t parent-child)")
                    .arg(Arg::new("child").index(1).required(true))
                    .arg(Arg::new("parent").index(2).required(true))
                    .arg(
                        Arg::new("type")
                            .long("type")
                            .help("blocks or parent-child")
                            .short('t')
                            .num_args(1)
                            .default_value("blocks"),
                    ),
            )
            .subcommand(Command::new("cycles").about("Report dependency cycles (exit 1 when any)"))
            .subcommand(
                Command::new("remove")
                    .about("Remove a dependency")
                    .arg(Arg::new("child").index(1).required(true))
                    .arg(Arg::new("parent").index(2).required(true))
                    .arg(
                        Arg::new("type")
                            .long("type")
                            .help("blocks or parent-child")
                            .short('t')
                            .num_args(1)
                            .default_value("blocks"),
                    ),
            )
            .subcommand(
                Command::new("tree")
                    .about("Show blockers as an indented tree")
                    .arg(Arg::new("id").index(1))
                    .arg(
                        Arg::new("all-projects")
                            .long("all-projects")
                            .help("every project in the hub")
                            .action(ArgAction::SetTrue)
                            .num_args(0..=1)
                            .require_equals(true)
                            .default_missing_value("true"),
                    ),
            ),
        Command::new("list")
            .about("List issues (open by default)")
            .arg(
                Arg::new("all-projects")
                    .long("all-projects")
                    .help("every project in the hub")
                    .action(ArgAction::SetTrue)
                    .num_args(0..=1)
                    .require_equals(true)
                    .default_missing_value("true"),
            )
            .arg(
                Arg::new("archived")
                    .long("archived")
                    .help("include archived issues")
                    .action(ArgAction::SetTrue)
                    .num_args(0..=1)
                    .require_equals(true)
                    .default_missing_value("true"),
            )
            .arg(
                Arg::new("assignee")
                    .long("assignee")
                    .help("only this assignee")
                    .num_args(1),
            )
            .arg(
                Arg::new("closed")
                    .long("closed")
                    .help("only terminal issues (implies --archived)")
                    .action(ArgAction::SetTrue)
                    .num_args(0..=1)
                    .require_equals(true)
                    .default_missing_value("true"),
            )
            .arg(
                Arg::new("label")
                    .long("label")
                    .help("only issues with this label")
                    .num_args(1),
            )
            .arg(
                Arg::new("limit")
                    .long("limit")
                    .help("maximum rows (0 = all)")
                    .short('n')
                    .value_parser(clap::value_parser!(i64))
                    .allow_negative_numbers(true)
                    .default_value("50"),
            )
            .arg(
                Arg::new("sort")
                    .long("sort")
                    .help("sort by created, updated, or priority")
                    .num_args(1)
                    .default_value("created"),
            )
            .arg(
                Arg::new("status")
                    .long("status")
                    .help("only this status")
                    .num_args(1),
            )
            .arg(
                Arg::new("type")
                    .long("type")
                    .help("only this type")
                    .num_args(1),
            ),
        Command::new("note")
            .about("Append a note to an issue's log")
            .arg(Arg::new("id").index(1).required(true))
            .arg(Arg::new("text").index(2).required(true).num_args(1..)),
        Command::new("ready")
            .about("Issues with no open blockers, highest priority first")
            .arg(
                Arg::new("all-projects")
                    .long("all-projects")
                    .help("every project in the hub")
                    .action(ArgAction::SetTrue)
                    .num_args(0..=1)
                    .require_equals(true)
                    .default_missing_value("true"),
            )
            .arg(
                Arg::new("limit")
                    .long("limit")
                    .help("maximum rows (0 = all)")
                    .short('n')
                    .value_parser(clap::value_parser!(i64))
                    .allow_negative_numbers(true)
                    .default_value("0"),
            ),
        Command::new("reopen")
            .about("Return a closed issue to the default status")
            .arg(Arg::new("id").index(1).required(true)),
        Command::new("show")
            .about("Show an issue with its blockers, children, backlinks, and log")
            .arg(Arg::new("id").index(1).required(true))
            .arg(
                Arg::new("include-archived-handoffs")
                    .long("include-archived-handoffs")
                    .help("include historical handoff backlinks")
                    .action(ArgAction::SetTrue)
                    .num_args(0..=1)
                    .require_equals(true)
                    .default_missing_value("true"),
            )
            .arg(
                Arg::new("raw")
                    .long("raw")
                    .help("print the issue file as is")
                    .action(ArgAction::SetTrue)
                    .num_args(0..=1)
                    .require_equals(true)
                    .default_missing_value("true"),
            ),
        Command::new("update")
            .about("Change fields of an issue (one commit, one log line per field)")
            .arg(Arg::new("id").index(1).required(true))
            .arg(
                Arg::new("assignee")
                    .long("assignee")
                    .help("new assignee (empty clears)")
                    .num_args(1),
            )
            .arg(
                Arg::new("claim")
                    .long("claim")
                    .help("set status in_progress and assign yourself")
                    .action(ArgAction::SetTrue)
                    .num_args(0..=1)
                    .require_equals(true)
                    .default_missing_value("true"),
            )
            .arg(
                Arg::new("description")
                    .long("description")
                    .help("replace the description")
                    .num_args(1),
            )
            .arg(
                Arg::new("force")
                    .long("force")
                    .help("allow leaving a terminal status")
                    .action(ArgAction::SetTrue)
                    .num_args(0..=1)
                    .require_equals(true)
                    .default_missing_value("true"),
            )
            .arg(
                Arg::new("label")
                    .long("label")
                    .help("add a label (repeatable)")
                    .action(ArgAction::Append)
                    .num_args(1),
            )
            .arg(
                Arg::new("note")
                    .long("note")
                    .help("append a log note")
                    .num_args(1),
            )
            .arg(
                Arg::new("parent")
                    .long("parent")
                    .help("new parent id (empty clears)")
                    .num_args(1),
            )
            .arg(
                Arg::new("priority")
                    .long("priority")
                    .help("new priority 0-4")
                    .value_parser(clap::value_parser!(i64))
                    .allow_negative_numbers(true),
            )
            .arg(
                Arg::new("status")
                    .long("status")
                    .help("new status")
                    .num_args(1),
            )
            .arg(
                Arg::new("title")
                    .long("title")
                    .help("new title (the file is not renamed)")
                    .num_args(1),
            )
            .arg(Arg::new("type").long("type").help("new type").num_args(1))
            .arg(
                Arg::new("unlabel")
                    .long("unlabel")
                    .help("remove a label (repeatable)")
                    .action(ArgAction::Append)
                    .num_args(1),
            ),
    ]
}
