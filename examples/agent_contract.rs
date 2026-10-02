//! Development interface for repository agent skills; never launches a product fallback.
use beans::domain::{contracts::restricted_yaml, workflow::load_workflow};
use std::{
    io::{self, Read},
    path::Path,
};
fn run() -> Result<serde_json::Value, Box<dyn std::error::Error>> {
    let args: Vec<_> = std::env::args().skip(1).collect();
    match args.first().map(String::as_str) {
        Some("yaml") if args.len() == 1 => {
            let mut text = String::new();
            io::stdin().read_to_string(&mut text)?;
            Ok(restricted_yaml(&text)?)
        }
        Some("workflow") if args.len() == 4 => {
            let read = |name: &str| -> io::Result<Vec<u8>> {
                if name.is_empty() {
                    Ok(Vec::new())
                } else {
                    std::fs::read(name)
                }
            };
            let explicit = if args[1].is_empty() {
                None
            } else {
                Some(Path::new(&args[1]))
            };
            let mut workflow = load_workflow(explicit, &read(&args[2])?, &read(&args[3])?)?;
            if workflow.transitions.is_none() {
                workflow.transitions = Some(Default::default());
            }
            Ok(serde_json::to_value(workflow)?)
        }
        _ => Err("usage: agent_contract yaml | workflow <explicit> <project> <hub>".into()),
    }
}
fn main() {
    match run() {
        Ok(value) => println!("{value}"),
        Err(error) => {
            eprintln!("{error}");
            std::process::exit(1);
        }
    }
}
