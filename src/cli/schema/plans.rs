use clap::{Arg, ArgAction, Command};
pub(super) fn commands() -> Vec<Command> {
    vec![
        Command::new("plan")
            .about("Create, validate, publish, and inspect project plans")
            .subcommand_required(true)
            .arg_required_else_help(true)
            .subcommand(
                Command::new("get")
                    .about("Copy a published plan into a new local directory")
                    .arg(Arg::new("id").index(1).required(true))
                    .arg(
                        Arg::new("output")
                            .long("output")
                            .help("new local directory")
                            .num_args(1),
                    ),
            )
            .subcommand(
                Command::new("init")
                    .about("Create a local plan draft")
                    .arg(Arg::new("title").index(1).required(true))
                    .arg(
                        Arg::new("output")
                            .long("output")
                            .help("local directory for the draft")
                            .num_args(1),
                    ),
            )
            .subcommand(
                Command::new("link")
                    .about("Bind a plan graph node to an issue")
                    .arg(Arg::new("plan-id").index(1).required(true))
                    .arg(Arg::new("node-id").index(2).required(true))
                    .arg(Arg::new("issue-id").index(3).required(true))
                    .arg(
                        Arg::new("force")
                            .long("force")
                            .help("replace an existing ref")
                            .action(ArgAction::SetTrue),
                    ),
            )
            .subcommand(
                Command::new("list").about("List plans in the project").arg(
                    Arg::new("status")
                        .long("status")
                        .help("filter exact lifecycle status")
                        .num_args(1),
                ),
            )
            .subcommand(
                Command::new("put")
                    .about("Publish a validated plan bundle")
                    .arg(Arg::new("directory").index(1).required(true)),
            )
            .subcommand(
                Command::new("show")
                    .about("Show a published plan")
                    .arg(Arg::new("id").index(1).required(true)),
            )
            .subcommand(
                Command::new("status")
                    .about("Show derived execution state for a plan")
                    .arg(Arg::new("plan-id").index(1).required(true)),
            )
            .subcommand(
                Command::new("unlink")
                    .about("Remove an expected plan issue binding")
                    .arg(Arg::new("plan-id").index(1).required(true))
                    .arg(Arg::new("node-id").index(2).required(true))
                    .arg(Arg::new("issue-id").index(3).required(true)),
            )
            .subcommand(
                Command::new("validate")
                    .about("Validate a local plan bundle")
                    .arg(Arg::new("directory").index(1).required(true)),
            ),
    ]
}
