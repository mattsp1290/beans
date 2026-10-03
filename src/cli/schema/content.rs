use clap::{Arg, ArgAction, Command};
pub(super) fn commands() -> Vec<Command> {
    vec![
        Command::new("prime").about("Print agent workflow instructions"),
        Command::new("sync").about("Commit local edits and synchronize the hub"),
        Command::new("cache")
            .about("Manage ~/.beans/cache")
            .subcommand_required(true)
            .arg_required_else_help(true)
            .subcommand(
                Command::new("clear")
                    .about("Remove derived data under ~/.beans/cache (keeps an active lock)"),
            ),
        Command::new("doc")
            .about("Wiki pages under docs/")
            .subcommand_required(true)
            .arg_required_else_help(true)
            .subcommand(
                Command::new("backlinks")
                    .about("Notes that link to a doc")
                    .arg(Arg::new("path").index(1).required(true)),
            )
            .subcommand(
                Command::new("list")
                    .about("List docs with their titles")
                    .arg(Arg::new("dir").index(1))
                    .arg(
                        Arg::new("global")
                            .long("global")
                            .help("hub-level docs only")
                            .action(ArgAction::SetTrue)
                            .num_args(0..=1)
                            .require_equals(true)
                            .default_missing_value("true"),
                    ),
            )
            .subcommand(
                Command::new("new")
                    .about("Create docs/<path>.md from templates/doc.md or a title line")
                    .arg(Arg::new("path").index(1).required(true))
                    .arg(
                        Arg::new("global")
                            .long("global")
                            .help("hub-level docs/ instead of the project's")
                            .action(ArgAction::SetTrue)
                            .num_args(0..=1)
                            .require_equals(true)
                            .default_missing_value("true"),
                    ),
            ),
        Command::new("doctor")
            .about("Check the hub for problems (exit 1 when any)")
            .arg(
                Arg::new("all-projects")
                    .long("all-projects")
                    .help("every project in the hub")
                    .action(ArgAction::SetTrue)
                    .num_args(0..=1)
                    .require_equals(true)
                    .default_missing_value("true"),
            ),
        Command::new("forget")
            .about("Delete a memory")
            .arg(Arg::new("key").index(1).required(true))
            .arg(
                Arg::new("global")
                    .long("global")
                    .help("hub-wide memory instead of the project's")
                    .action(ArgAction::SetTrue)
                    .num_args(0..=1)
                    .require_equals(true)
                    .default_missing_value("true"),
            ),
        Command::new("import")
            .about("One-time imports into the hub")
            .subcommand_required(true)
            .arg_required_else_help(true)
            .subcommand(
                Command::new("bd")
                    .about("Import a bd export (bd export -o file) as one commit")
                    .arg(Arg::new("export.jsonl").index(1).required(true))
                    .arg(
                        Arg::new("dry-run")
                            .long("dry-run")
                            .help("report the mapping and write nothing")
                            .action(ArgAction::SetTrue)
                            .num_args(0..=1)
                            .require_equals(true)
                            .default_missing_value("true"),
                    )
                    .arg(
                        Arg::new("force")
                            .long("force")
                            .help("overwrite files that already exist")
                            .action(ArgAction::SetTrue)
                            .num_args(0..=1)
                            .require_equals(true)
                            .default_missing_value("true"),
                    ),
            ),
        Command::new("init")
            .about("Clone the hub repository into ~/.beans/hub")
            .arg(Arg::new("remote").index(1).required(true)),
        Command::new("man").about("Generates manpages"),
        Command::new("memories")
            .about("List memories, optionally filtered by keyword")
            .arg(Arg::new("keyword").index(1).num_args(0..))
            .arg(
                Arg::new("all")
                    .long("all")
                    .help("hub-wide, not just the current project")
                    .action(ArgAction::SetTrue)
                    .num_args(0..=1)
                    .require_equals(true)
                    .default_missing_value("true"),
            )
            .arg(
                Arg::new("limit")
                    .long("limit")
                    .help("maximum entries (0 = all)")
                    .short('n')
                    .value_parser(clap::value_parser!(i64))
                    .allow_negative_numbers(true)
                    .default_value("0"),
            )
            .arg(
                Arg::new("tag")
                    .long("tag")
                    .help("only memories with this tag")
                    .num_args(1),
            )
            .arg(
                Arg::new("type")
                    .long("type")
                    .help("only this memory type")
                    .num_args(1),
            ),
        Command::new("project")
            .about("List, inspect, create, and link projects in the hub")
            .subcommand_required(true)
            .arg_required_else_help(true)
            .subcommand(
                Command::new("create")
                    .about("Create a project in the hub (one commit)")
                    .arg(Arg::new("name").index(1).required(true))
                    .arg(
                        Arg::new("link")
                            .long("link")
                            .help("record the current repository's remote in the new project")
                            .action(ArgAction::SetTrue)
                            .num_args(0..=1)
                            .require_equals(true)
                            .default_missing_value("true"),
                    ),
            )
            .subcommand(
                Command::new("link")
                    .about("Add the current repository's remote to a project's remotes")
                    .arg(Arg::new("name").index(1).required(true)),
            )
            .subcommand(Command::new("list").about("List the projects in the hub"))
            .subcommand(
                Command::new("show")
                    .about("Show a project's configuration")
                    .arg(Arg::new("name").index(1).required(true)),
            ),
        Command::new("remember")
            .about("Save a memory as memories/<key>.md")
            .arg(Arg::new("text").index(1).required(true).num_args(1..))
            .arg(
                Arg::new("global")
                    .long("global")
                    .help("hub-wide memory instead of the project's")
                    .action(ArgAction::SetTrue)
                    .num_args(0..=1)
                    .require_equals(true)
                    .default_missing_value("true"),
            )
            .arg(
                Arg::new("key")
                    .long("key")
                    .help("memory key (default: derived from the first words)")
                    .num_args(1),
            )
            .arg(
                Arg::new("tag")
                    .long("tag")
                    .help("tag (repeatable)")
                    .action(ArgAction::Append)
                    .num_args(1),
            )
            .arg(
                Arg::new("type")
                    .long("type")
                    .help("user, feedback, project, or reference")
                    .num_args(1),
            ),
        Command::new("search")
            .about("Search issues, requests, docs, and memories")
            .arg(Arg::new("query").index(1).required(true).num_args(1..))
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
                Arg::new("include-archived-handoffs")
                    .long("include-archived-handoffs")
                    .help("include historical handoffs")
                    .action(ArgAction::SetTrue)
                    .num_args(0..=1)
                    .require_equals(true)
                    .default_missing_value("true"),
            )
            .arg(
                Arg::new("kind")
                    .long("kind")
                    .help("issue, request, doc, memory, plan, or handoff")
                    .num_args(1),
            ),
        Command::new("status").about("Show the hub clone state and the resolved project"),
        Command::new("upgrade")
            .about("Replace this bn binary with the latest release")
            .arg(
                Arg::new("check")
                    .long("check")
                    .help("report whether a newer release exists; install nothing")
                    .action(ArgAction::SetTrue)
                    .num_args(0..=1)
                    .require_equals(true)
                    .default_missing_value("true"),
            )
            .arg(
                Arg::new("force")
                    .long("force")
                    .help("install even when not newer or when Cargo manages this binary")
                    .action(ArgAction::SetTrue)
                    .num_args(0..=1)
                    .require_equals(true)
                    .default_missing_value("true"),
            )
            // The root command owns the id `version` for its banner flag.
            .arg(
                Arg::new("tag")
                    .long("version")
                    .help("install exactly this release tag (vX.Y.Z)")
                    .value_name("tag")
                    .num_args(1),
            ),
    ]
}
