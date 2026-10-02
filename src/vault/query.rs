//! Queries over indexed semantic models and original body bytes.
mod cycles;
mod dependencies;
mod execution;
mod search;
use super::{Index, LinkRef, Note, NoteData, NoteKind};
use crate::domain::{
    issue::{IssueDocument, Timestamp},
    yaml_string::YamlString,
};
pub use dependencies::{BlockedIssue, DependencyEdge, DependencyGraph, DependencyNode};
pub use execution::{
    ExecutionBlocker, ExecutionCounts, ExecutionIssue, ExecutionNode, PlanExecution,
};
pub use search::{Hit, SearchOptions};
use std::cmp::Ordering;
pub(super) fn issue(note: &Note) -> Option<&IssueDocument> {
    if let NoteData::Issue(v) = &note.data {
        Some(v)
    } else {
        None
    }
}
pub(super) fn instant(t: &Timestamp) -> (i64, u32) {
    (t.seconds, t.nanoseconds)
}
pub(super) fn sort_issues(a: &&Note, b: &&Note) -> Ordering {
    let a = &issue(a).unwrap().metadata;
    let b = &issue(b).unwrap().metadata;
    (a.priority, instant(&a.created), &a.id).cmp(&(b.priority, instant(&b.created), &b.id))
}
pub(super) fn contains(haystack: &[u8], needle: &[u8]) -> bool {
    needle.is_empty() || haystack.windows(needle.len()).any(|s| s == needle)
}
pub(super) fn target(raw: &[u8]) -> Vec<u8> {
    let value = YamlString::from_bytes(raw.into()).trimmed();
    let bytes = value.as_bytes();
    if let Some(value) = bytes
        .strip_prefix(b"[[")
        .and_then(|v| v.strip_suffix(b"]]"))
    {
        YamlString::from_bytes(
            value
                .split(|&b| matches!(b, b'|' | b'#'))
                .next()
                .unwrap_or_default()
                .into(),
        )
        .trimmed()
        .as_bytes()
        .into()
    } else {
        bytes.into()
    }
}
#[derive(Clone, Debug, Default)]
pub struct RequestFilter {
    pub project: Vec<u8>,
    pub status: Vec<u8>,
    pub label: Vec<u8>,
    pub priority: Option<i64>,
    pub query: Vec<u8>,
    pub terminal: bool,
}
impl Index {
    pub(super) fn owned_notes(&self, kind: NoteKind) -> Vec<&Note> {
        self.ordered_notes()
            .iter()
            .filter(|n| {
                n.graph.kind == kind
                    && n.graph.id.as_ref().is_some_and(|id| {
                        self.graph
                            .by_id(kind, id)
                            .is_some_and(|owner| owner.path == n.graph.path)
                    })
            })
            .collect()
    }
    pub fn issue_by_id(&self, id: &[u8]) -> Option<&IssueDocument> {
        self.note_by_id(NoteKind::Issue, id).and_then(issue)
    }
    pub fn resolve_issue_ref(&self, raw: &[u8]) -> (Vec<u8>, Option<&Note>) {
        let target = target(raw);
        let resolved = self
            .note_by_id(NoteKind::Issue, &target)
            .or_else(|| self.lookup(&target).filter(|n| issue(n).is_some()));
        (target, resolved)
    }
    pub fn project_issues(&self, project: &[u8], include_archived: bool) -> Vec<&Note> {
        let mut out: Vec<_> = self
            .owned_notes(NoteKind::Issue)
            .into_iter()
            .filter(|n| {
                (project.is_empty() || n.project == project)
                    && (include_archived || !issue(n).unwrap().metadata.archived)
            })
            .collect();
        out.sort_by(|a, b| a.graph.id.cmp(&b.graph.id));
        out
    }
    pub fn project_requests(&self, filter: &RequestFilter) -> Vec<&Note> {
        let query = super::lowercase::lower(
            YamlString::from_bytes(filter.query.clone())
                .trimmed()
                .as_bytes(),
        );
        let mut out: Vec<_> = self
            .owned_notes(NoteKind::Request)
            .into_iter()
            .filter(|n| {
                let NoteData::Request(doc) = &n.data else {
                    return false;
                };
                let r = &doc.metadata;
                if (!filter.project.is_empty() && n.project != filter.project)
                    || (!filter.status.is_empty() && r.status.as_bytes() != filter.status)
                    || (!filter.label.is_empty()
                        && !r.labels.iter().any(|s| s.as_bytes() == filter.label))
                    || filter.priority.is_some_and(|p| p != r.priority)
                {
                    return false;
                }
                if !filter.terminal
                    && filter.status.is_empty()
                    && matches!(r.status.as_str(), "resolved" | "declined")
                {
                    return false;
                }
                if query.is_empty() {
                    return true;
                }
                let fields = [
                    n.title.as_slice(),
                    r.id.as_bytes(),
                    r.requested_by.as_bytes(),
                    r.labels.join("\n").as_bytes(),
                    n.body.as_slice(),
                ]
                .join(&b'\n');
                contains(&super::lowercase::lower(&fields), &query)
            })
            .collect();
        out.sort_by(|a, b| {
            let (NoteData::Request(a), NoteData::Request(b)) = (&a.data, &b.data) else {
                unreachable!()
            };
            (
                a.metadata.priority,
                instant(&a.metadata.created),
                &a.metadata.id,
            )
                .cmp(&(
                    b.metadata.priority,
                    instant(&b.metadata.created),
                    &b.metadata.id,
                ))
        });
        out
    }
    pub fn project_plans(&self, project: &[u8]) -> Vec<&Note> {
        let mut out: Vec<_> = self
            .owned_notes(NoteKind::Plan)
            .into_iter()
            .filter(|n| project.is_empty() || n.project == project)
            .collect();
        out.sort_by(|a, b| {
            let (NoteData::Plan(a), NoteData::Plan(b)) = (&a.data, &b.data) else {
                unreachable!()
            };
            instant(&b.updated)
                .cmp(&instant(&a.updated))
                .then_with(|| a.id.cmp(&b.id))
        });
        out
    }
    pub fn project_handoffs(&self, project: &[u8], include_archived: bool) -> Vec<&Note> {
        let mut out: Vec<_> = self
            .owned_notes(NoteKind::Handoff)
            .into_iter()
            .filter(|n| {
                let NoteData::Handoff(h) = &n.data else {
                    unreachable!()
                };
                (project.is_empty() || n.project == project)
                    && (include_archived || !h.metadata.archived)
            })
            .collect();
        out.sort_by(|a, b| {
            let (NoteData::Handoff(a), NoteData::Handoff(b)) = (&a.data, &b.data) else {
                unreachable!()
            };
            instant(&b.metadata.created)
                .cmp(&instant(&a.metadata.created))
                .then_with(|| a.metadata.id.cmp(&b.metadata.id))
        });
        out
    }
    pub fn issue_backlinks(
        &self,
        basename: &[u8],
        include_archived_handoffs: bool,
    ) -> Vec<LinkRef> {
        self.graph
            .backlinks()
            .get(basename)
            .into_iter()
            .flatten()
            .filter(|r| {
                if include_archived_handoffs {
                    return true;
                }
                let note = self
                    .graph
                    .by_basename(&r.from)
                    .and_then(|n| self.note_by_path(&n.path));
                !note.is_some_and(|n| matches!(&n.data,NoteData::Handoff(h) if h.metadata.archived))
            })
            .cloned()
            .collect()
    }
}
