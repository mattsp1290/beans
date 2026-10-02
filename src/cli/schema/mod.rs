mod content;
mod handoffs;
mod issues;
mod plans;
mod requests;

pub(super) fn commands() -> Vec<clap::Command> {
    let mut commands = vec![
        clap::Command::new("serve")
            .about("Serve the issue board and wiki")
            .arg(
                clap::Arg::new("host")
                    .long("host")
                    .default_value("127.0.0.1"),
            )
            .arg(clap::Arg::new("port").long("port").default_value("3000")),
    ];
    commands.extend(issues::commands());
    commands.extend(requests::commands());
    commands.extend(handoffs::commands());
    commands.extend(plans::commands());
    commands.extend(content::commands());
    commands
}
