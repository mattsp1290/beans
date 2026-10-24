use super::dispatch::{body, flag, mutation_output, number, output, value, values};
use crate::{
    domain::{frontmatter::Error, prompt},
    gitops::Hub,
    ops::{PromptEdit, RecordChange, RecordMutation},
    vault::{Index, LoadOptions, Note, NoteData, NoteKind, PromptFilter, Resolved},
};
use clap::ArgMatches;
use std::path::PathBuf;

fn optional(matches: &ArgMatches, key: &str) -> Option<String> {
    matches.try_get_one::<String>(key).ok().flatten().cloned()
}

fn identity(value: String) -> Result<String, Error> {
    prompt::normalize_identity(&value)
}

fn actor_identity(actor: &str) -> Result<String, Error> {
    prompt::normalize_identity(actor).map_err(|_| {
        Error::new("prompt requires a nonblank actor; set global --actor or BN_ACTOR".to_owned())
    })
}

fn validate_status(status: Option<&str>) -> Result<(), Error> {
    if status.is_some_and(|status| !prompt::valid_status(status)) {
        return Err(Error::new("invalid prompt status".to_owned()));
    }
    Ok(())
}

fn prompt_json(note: &Note, detail: bool) -> serde_json::Value {
    let NoteData::Prompt(document) = &note.data else {
        unreachable!("prompt projection received a non-prompt note")
    };
    let metadata = &document.metadata;
    let issue = if metadata.issue.target.is_empty() {
        serde_json::Value::Null
    } else {
        serde_json::json!({
            "raw": metadata.issue.raw,
            "target": metadata.issue.target,
        })
    };
    let mut projected = serde_json::json!({
        "id": metadata.id,
        "aliases": metadata.aliases,
        "title": metadata.title,
        "status": metadata.status,
        "priority": metadata.priority,
        "labels": metadata.labels,
        "from": metadata.from,
        "to": metadata.to,
        "claimed_by": metadata.claimed_by,
        "issue": issue,
        "created": metadata.created,
        "updated": metadata.updated,
        "path": String::from_utf8_lossy(&note.graph.path),
        "project": String::from_utf8_lossy(&note.project),
    });
    if detail {
        projected["body"] = serde_json::json!(String::from_utf8_lossy(document.body.as_bytes()));
        projected["log"] = serde_json::to_value(&document.log).expect("log is serializable");
    }
    projected
}

fn print_prompts(notes: Vec<&Note>, json: bool) -> Result<(), Error> {
    if json {
        return output(&serde_json::Value::Array(
            notes
                .into_iter()
                .map(|note| prompt_json(note, false))
                .collect(),
        ));
    }
    for note in notes {
        let NoteData::Prompt(document) = &note.data else {
            unreachable!("prompt query returned a non-prompt note")
        };
        println!("{}\t{}", document.metadata.id, document.metadata.title);
    }
    Ok(())
}

pub fn execute(
    matches: &ArgMatches,
    hub: &Hub,
    resolved: Resolved,
    resolved_actor: &str,
    explicit: Option<PathBuf>,
    json: bool,
) -> Result<(), Error> {
    let (action, leaf) = matches.subcommand().expect("prompt requires a subcommand");
    // Prompt identity defaults intentionally canonicalize the resolved actor at
    // this one boundary. Explicit coordination identities use the same rule.
    let actor = resolved_actor.trim();

    if matches!(action, "list" | "inbox" | "show") {
        let index = Index::load_snapshot(
            &hub.dir,
            LoadOptions {
                explicit_workflow: explicit,
            },
        )?;
        if action == "show" {
            let reference = value(leaf, "id");
            let note = index
                .note_by_id(NoteKind::Prompt, reference.as_bytes())
                .or_else(|| {
                    index
                        .lookup(reference.as_bytes())
                        .filter(|note| note.graph.kind == NoteKind::Prompt)
                })
                .ok_or_else(|| Error::new("prompt not found".to_owned()))?;
            if json {
                return output(&prompt_json(note, true));
            }
            print!("{}", String::from_utf8_lossy(&note.source));
            return Ok(());
        }

        let project = if flag(leaf, "all-projects") {
            Vec::new()
        } else {
            resolved.project
        };
        if action == "list" {
            let status = value(leaf, "status");
            validate_status((!status.is_empty()).then_some(status.as_str()))?;
            let filter = PromptFilter {
                project,
                status: status.into_bytes(),
                label: value(leaf, "label").into_bytes(),
                priority: number(leaf, "priority"),
                from: optional(leaf, "from")
                    .map(identity)
                    .transpose()?
                    .unwrap_or_default()
                    .into_bytes(),
                to: optional(leaf, "to")
                    .map(identity)
                    .transpose()?
                    .unwrap_or_default()
                    .into_bytes(),
                claimed_by: optional(leaf, "claimed-by")
                    .map(identity)
                    .transpose()?
                    .unwrap_or_default()
                    .into_bytes(),
                query: value(leaf, "query").into_bytes(),
                terminal: flag(leaf, "terminal"),
            };
            return print_prompts(index.project_prompts(&filter), json);
        }

        let recipient = optional(leaf, "to")
            .map(identity)
            .transpose()?
            .map_or_else(|| actor_identity(actor), Ok)?;
        let limit = number(leaf, "limit").unwrap_or(50);
        if limit < 0 {
            return Err(Error::new(
                "prompt inbox limit must not be negative".to_owned(),
            ));
        }
        let mut filter = PromptFilter {
            project,
            terminal: flag(leaf, "terminal"),
            ..Default::default()
        };
        let mut notes = if flag(leaf, "claimed") {
            index.prompt_inbox(recipient.as_bytes(), &filter)
        } else {
            filter.to = recipient.into_bytes();
            index.project_prompts(&filter)
        };
        if limit > 0 {
            notes.truncate(limit as usize);
        }
        return print_prompts(notes, json);
    }

    let reference = value(leaf, "id");
    let change = match action {
        "create" => {
            let from = optional(leaf, "from")
                .map(identity)
                .transpose()?
                .map_or_else(|| actor_identity(actor), Ok)?;
            let to = identity(value(leaf, "to"))?;
            RecordChange::PromptCreate(PromptEdit {
                title: Some(value(leaf, "title")),
                priority: number(leaf, "priority"),
                from: Some(from),
                to: Some(to),
                body: body(leaf, "body-file")?.map(String::into_bytes),
                issue: optional(leaf, "issue"),
                labels: values(leaf, "label"),
                ..Default::default()
            })
        }
        "update" => {
            let claim = flag(leaf, "claim");
            let status = optional(leaf, "status");
            validate_status(status.as_deref())?;
            if claim
                && status
                    .as_deref()
                    .is_some_and(|status| status != prompt::IN_PROGRESS)
            {
                return Err(Error::new(
                    "prompt claim can only be combined with in_progress status".to_owned(),
                ));
            }
            let to = optional(leaf, "to").map(identity).transpose()?;
            prompt::validate_update_combination(to.is_some(), claim, status.as_deref())?;
            let note = optional(leaf, "note");
            if note.as_ref().is_some_and(|note| note.trim().is_empty()) {
                return Err(Error::new("prompt note must not be blank".to_owned()));
            }
            let body = body(leaf, "body-file")?.map(String::into_bytes);
            let title = optional(leaf, "title");
            let priority = number(leaf, "priority");
            let labels = values(leaf, "label");
            let unlabels = values(leaf, "unlabel");
            if title.is_none()
                && status.is_none()
                && to.is_none()
                && priority.is_none()
                && labels.is_empty()
                && unlabels.is_empty()
                && body.is_none()
                && note.is_none()
                && !claim
            {
                return Err(Error::new(
                    "prompt update requires an edit, claim, or note".to_owned(),
                ));
            }
            if claim {
                actor_identity(actor)?;
            }
            RecordChange::PromptUpdate(PromptEdit {
                title,
                status,
                priority,
                to,
                body,
                note,
                labels,
                unlabels,
                claim,
                force: flag(leaf, "force"),
                ..Default::default()
            })
        }
        "link" => RecordChange::PromptUpdate(PromptEdit {
            issue: Some(value(leaf, "issue-id")),
            ..Default::default()
        }),
        "unlink" => RecordChange::PromptUpdate(PromptEdit {
            unlink: true,
            ..Default::default()
        }),
        _ => return Err(Error::new("unknown prompt action".to_owned())),
    };
    let mut operation =
        RecordMutation::new(resolved, reference, change, actor.to_owned(), explicit);
    let result = hub.mutate(&mut operation)?;
    mutation_output(&operation.id, result, json)
}
