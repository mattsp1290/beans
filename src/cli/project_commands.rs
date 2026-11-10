use super::dispatch::{output, value};
use crate::{
    domain::frontmatter::Error,
    gitops::Hub,
    ops::{ContentChange, ContentMutation, projects},
    vault::Resolved,
};
use clap::ArgMatches;
use std::io::Read;

pub(super) fn execute(matches: &ArgMatches, hub: &Hub) -> Result<(), Error> {
    let (action, leaf) = matches.subcommand().unwrap();
    if action == "catalog" {
        return output(
            &serde_json::json!({"schema_version":1,"projects":projects::catalog(&hub.dir)?}),
        );
    }
    if action == "inspect" {
        return output(
            &serde_json::json!({"schema_version":1,"project":projects::inspect(&hub.dir, &value(leaf,"project-id"))?}),
        );
    }
    let file = value(leaf, "request-file");
    let mut bytes = Vec::new();
    let mut reader: Box<dyn Read> = if file == "-" {
        Box::new(std::io::stdin())
    } else {
        Box::new(std::fs::File::open(&file).map_err(|e| Error::new(e.to_string()))?)
    };
    reader
        .by_ref()
        .take(1_048_577)
        .read_to_end(&mut bytes)
        .map_err(|e| Error::new(e.to_string()))?;
    if bytes.len() > 1_048_576 {
        return Err(Error::new("project request exceeds 1 MiB".into()));
    }
    let request: projects::ProjectRequest =
        serde_json::from_slice(&bytes).map_err(|e| Error::new(e.to_string()))?;
    request.validate()?;
    let operation_id = request.operation_id.clone();
    let mut operation =
        ContentMutation::new(Resolved::default(), ContentChange::ProjectApply(request));
    let result = hub.mutate(&mut operation)?;
    output(
        &serde_json::json!({"schema_version":1,"operation_id":operation_id,"replayed":operation.project_replayed,
        "project":operation.project_snapshot,"commit":result.sha,"sha":result.sha,"pushed":result.pushed,"message":result.message}),
    )
}
