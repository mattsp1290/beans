use super::dispatch::*;
use crate::{
    domain::frontmatter::Error,
    gitops::{Hub, Operation},
    ops::ImportMutation,
    vault::Resolved,
};
use clap::ArgMatches;
pub fn execute(m: &ArgMatches, hub: &Hub, resolved: &Resolved, _json: bool) -> Result<(), Error> {
    let (_, s) = m.subcommand().unwrap();
    let source =
        std::fs::read_to_string(value(s, "export.jsonl")).map_err(|e| Error::new(e.to_string()))?;
    let mut records = Vec::new();
    let mut warnings = Vec::new();
    for (i, line) in source.lines().enumerate() {
        let line = line.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        if line.len() > 4 * 1024 * 1024 {
            return Err(Error::new("import line exceeds 4MiB".into()));
        }
        match serde_json::from_str::<serde_json::Value>(line) {
            Ok(v) if v.is_object() => records.push(v),
            Ok(_) => warnings.push(format!("line {}: expected JSON object", i + 1)),
            Err(e) => warnings.push(format!("line {}: {e}", i + 1)),
        }
    }
    let dry = flag(s, "dry-run");
    let mut op = ImportMutation::new(resolved.clone(), records, warnings, flag(s, "force"), dry);
    let mut result = crate::gitops::MutationResult::default();
    if dry {
        op.apply(&hub.dir)?;
    } else {
        result = hub.mutate(&mut op)?;
    }
    output(
        &serde_json::json!({"report":op.report,"dry_run":dry,"commit":result.sha,"pushed":result.pushed}),
    )
}
