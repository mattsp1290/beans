//! Runtime workflow vocabulary and decoded overrides, independent of file I/O.
use super::{frontmatter::Error, yaml_string::YamlString};
use serde::{Deserialize, Serialize};
use std::collections::{BTreeMap, HashSet};
mod file;
mod yaml;
pub use file::{decode_workflow_file, decode_workflow_file_bytes, load_workflow};

pub type States = Option<Vec<YamlString>>;
pub type Transitions = Option<BTreeMap<YamlString, States>>;

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct WorkflowConfig {
    pub statuses: States,
    pub default: YamlString,
    pub active: States,
    pub terminal: States,
    pub transitions: Transitions,
}
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct WorkflowFile {
    pub statuses: States,
    pub default: YamlString,
    pub active: States,
    pub terminal: States,
    pub transitions: Transitions,
}
fn states(values: &[&str]) -> States {
    Some(values.iter().map(|v| (*v).into()).collect())
}
fn list(values: &States) -> &[YamlString] {
    values.as_deref().unwrap_or_default()
}
fn contains(values: &States, state: &[u8]) -> bool {
    list(values).iter().any(|s| s.as_bytes() == state)
}
fn clean(values: &States) -> States {
    Some(
        list(values)
            .iter()
            .map(YamlString::trimmed)
            .filter(|s| !s.as_bytes().is_empty())
            .collect(),
    )
}

impl WorkflowConfig {
    pub fn built_in() -> Self {
        Self {
            statuses: states(&[
                "open",
                "in_progress",
                "ready_for_review",
                "ready_for_validation",
                "ready_for_merge",
                "blocked",
                "closed",
                "done",
            ]),
            default: "open".into(),
            active: states(&["open"]),
            terminal: states(&["closed", "done"]),
            transitions: None,
        }
    }
    pub fn is_valid(&self, status: &[u8]) -> bool {
        contains(&self.statuses, status)
    }
    pub fn is_active(&self, status: &[u8]) -> bool {
        contains(&self.active, status)
    }
    pub fn is_terminal(&self, status: &[u8]) -> bool {
        contains(&self.terminal, status)
    }
    pub fn is_hold(&self, status: &[u8]) -> bool {
        self.is_valid(status) && !self.is_active(status) && !self.is_terminal(status)
    }
    pub fn default_state(&self) -> YamlString {
        if self.default.as_bytes().is_empty() {
            "open".into()
        } else {
            self.default.clone()
        }
    }
    pub fn status_names(&self) -> States {
        self.statuses.clone()
    }
    pub fn validate(&self) -> Result<(), Error> {
        if list(&self.statuses).is_empty() {
            return Err(Error::new("workflow: statuses must not be empty".into()));
        }
        let mut known = HashSet::new();
        for state in list(&self.statuses) {
            if state.trimmed().as_bytes().is_empty() {
                return Err(Error::new(
                    "workflow: statuses must not contain empty values".into(),
                ));
            }
            if !known.insert(state) {
                return Err(Error::new(format!(
                    "workflow: duplicate status {}",
                    state.quoted()
                )));
            }
        }
        if !known.contains(&self.default) {
            return Err(Error::new(format!(
                "workflow: default status {} is not in statuses",
                self.default.quoted()
            )));
        }
        for state in list(&self.active) {
            if !known.contains(state) {
                return Err(Error::new(format!(
                    "workflow: active status {} is not in statuses",
                    state.quoted()
                )));
            }
        }
        for state in list(&self.terminal) {
            if !known.contains(state) {
                return Err(Error::new(format!(
                    "workflow: terminal status {} is not in statuses",
                    state.quoted()
                )));
            }
        }
        let terminal: HashSet<_> = list(&self.terminal).iter().collect();
        let mut overlap: Vec<_> = list(&self.active)
            .iter()
            .filter(|state| terminal.contains(state))
            .cloned()
            .collect();
        if !overlap.is_empty() {
            overlap.sort();
            return Err(Error::new(format!(
                "workflow: status(es) cannot be both active and terminal: {}",
                overlap
                    .iter()
                    .map(ToString::to_string)
                    .collect::<Vec<_>>()
                    .join(", ")
            )));
        }
        for (from, to) in self.transitions.iter().flatten() {
            if !known.contains(from) {
                return Err(Error::new(format!(
                    "workflow: transition source {} is not in statuses",
                    from.quoted()
                )));
            }
            for state in list(to) {
                if !known.contains(state) {
                    return Err(Error::new(format!(
                        "workflow: transition target {} (from {}) is not in statuses",
                        state.quoted(),
                        from.quoted()
                    )));
                }
            }
        }
        Ok(())
    }
    pub fn merge(&self, file: &WorkflowFile) -> Self {
        let mut merged = self.clone();
        if !list(&file.statuses).is_empty() {
            merged.statuses = clean(&file.statuses);
        }
        let default = file.default.trimmed();
        if !default.as_bytes().is_empty() {
            merged.default = default;
        }
        if !list(&file.active).is_empty() {
            merged.active = clean(&file.active);
        }
        if !list(&file.terminal).is_empty() {
            merged.terminal = clean(&file.terminal);
        }
        if let Some(transitions) = &file.transitions
            && !transitions.is_empty()
        {
            merged.transitions = Some(
                transitions
                    .iter()
                    .map(|(from, to)| (from.clone(), clean(to)))
                    .collect(),
            );
        }
        merged
    }
}
impl WorkflowFile {
    pub fn is_empty(&self) -> bool {
        list(&self.statuses).is_empty()
            && self.default.trimmed().as_bytes().is_empty()
            && list(&self.active).is_empty()
            && list(&self.terminal).is_empty()
            && self.transitions.as_ref().is_none_or(BTreeMap::is_empty)
    }
}
