mod content;
mod handoffs;
mod issues;
mod plans;
mod requests;

pub(super) fn commands() -> Vec<clap::Command> {
    let mut commands = Vec::new();
    commands.extend(issues::commands());
    commands.extend(requests::commands());
    commands.extend(handoffs::commands());
    commands.extend(plans::commands());
    commands.extend(content::commands());
    commands
}
