//! Derived live execution state; authored plan lifecycle is never changed.
use super::*;
use serde::Serialize;
use std::collections::BTreeSet;
#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
pub struct ExecutionIssue {
    pub id: YamlString,
    pub title: YamlString,
    pub status: YamlString,
    pub priority: i64,
    pub project: YamlString,
    pub archived: bool,
}
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize)]
pub struct ExecutionBlocker {
    pub target: YamlString,
    pub id: YamlString,
    pub title: YamlString,
    pub status: YamlString,
    pub project: YamlString,
    pub missing: bool,
}
#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
pub struct ExecutionNode {
    pub node_id: String,
    pub label: String,
    pub kind: String,
    #[serde(rename = "ref")]
    pub reference: String,
    pub binding: &'static str,
    pub work_state: &'static str,
    pub hold_reason: &'static str,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub issue: Option<ExecutionIssue>,
    // Go initializes unbound nodes to [], but returns nil for bound nodes
    // without blockers. Retain this observable distinction in report JSON.
    pub blockers: Option<Vec<ExecutionBlocker>>,
}
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize)]
pub struct ExecutionCounts {
    pub unlinked: usize,
    pub reference: usize,
    pub issue: usize,
    pub missing_issue: usize,
    pub missing: usize,
    pub runnable: usize,
    pub in_progress: usize,
    pub held: usize,
    pub blocked: usize,
    pub done: usize,
    pub distinct_issues: usize,
}
#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
pub struct PlanExecution {
    pub plan_id: String,
    pub title: String,
    pub project: YamlString,
    pub lifecycle_status: String,
    pub execution_state: &'static str,
    pub lifecycle_mismatch: bool,
    pub counts: ExecutionCounts,
    pub nodes: Vec<ExecutionNode>,
}
impl Index {
    fn execution_blockers(&self, note: &Note) -> Vec<ExecutionBlocker> {
        let mut out = Vec::new();
        for link in &issue(note).unwrap().metadata.blocked_by {
            let (target, resolved) = self.resolve_issue_ref(link.raw.as_bytes());
            let target = YamlString::from_bytes(target);
            match resolved {
                None => out.push(ExecutionBlocker {
                    target,
                    missing: true,
                    ..Default::default()
                }),
                Some(n) => {
                    let m = &issue(n).unwrap().metadata;
                    if !self
                        .workflow_for(&n.project)
                        .is_terminal(m.status.as_bytes())
                    {
                        out.push(ExecutionBlocker {
                            target,
                            id: m.id.as_str().into(),
                            title: YamlString::from_bytes(n.title.clone()),
                            status: m.status.as_str().into(),
                            project: YamlString::from_bytes(n.project.clone()),
                            missing: false,
                        });
                    }
                }
            }
        }
        out.sort_by(|a, b| (&a.target, &a.id).cmp(&(&b.target, &b.id)));
        out
    }
    fn execution_state(
        &self,
        note: &Note,
    ) -> (&'static str, &'static str, Option<Vec<ExecutionBlocker>>) {
        let m = &issue(note).unwrap().metadata;
        let wf = self.workflow_for(&note.project);
        if wf.is_terminal(m.status.as_bytes()) {
            return ("done", "", None);
        }
        let blockers = self.execution_blockers(note);
        if !blockers.is_empty() {
            return ("blocked", "", Some(blockers));
        }
        if m.archived {
            return ("held", "archived", None);
        }
        if m.kind == "epic" && !self.children(m.id.as_bytes()).is_empty() {
            return ("held", "epic_has_children", None);
        }
        if m.status == "in_progress" {
            return ("in_progress", "", None);
        }
        if wf.is_active(m.status.as_bytes()) {
            return ("runnable", "", None);
        }
        ("held", "workflow_hold", None)
    }
    pub fn plan_execution(&self, id: &[u8]) -> Option<PlanExecution> {
        let note = self.note_by_id(NoteKind::Plan, id)?;
        let NoteData::Plan(p) = &note.data else {
            return None;
        };
        let mut out = PlanExecution {
            plan_id: p.id.clone(),
            title: p.title.clone(),
            project: YamlString::from_bytes(note.project.clone()),
            lifecycle_status: p.status.clone(),
            execution_state: "",
            lifecycle_mismatch: false,
            counts: Default::default(),
            nodes: vec![],
        };
        let mut distinct = BTreeSet::new();
        for g in p.graph.nodes.iter().flatten() {
            let mut n = ExecutionNode {
                node_id: g.id.clone(),
                label: g.label.clone(),
                kind: g.kind.clone(),
                reference: g.reference.clone(),
                binding: "",
                work_state: "",
                hold_reason: "",
                issue: None,
                blockers: Some(vec![]),
            };
            if g.reference.is_empty() {
                n.binding = "unlinked";
                out.counts.unlinked += 1;
            } else {
                let (target, resolved) = self.resolve_issue_ref(g.reference.as_bytes());
                if let Some(note) = resolved {
                    let m = &issue(note).unwrap().metadata;
                    n.binding = "issue";
                    n.issue = Some(ExecutionIssue {
                        id: m.id.as_str().into(),
                        title: YamlString::from_bytes(note.title.clone()),
                        status: m.status.as_str().into(),
                        priority: m.priority,
                        project: YamlString::from_bytes(note.project.clone()),
                        archived: m.archived,
                    });
                    (n.work_state, n.hold_reason, n.blockers) = self.execution_state(note);
                    out.counts.issue += 1;
                    distinct.insert(m.id.clone());
                    match n.work_state {
                        "runnable" => out.counts.runnable += 1,
                        "in_progress" => out.counts.in_progress += 1,
                        "held" => out.counts.held += 1,
                        "blocked" => out.counts.blocked += 1,
                        "done" => out.counts.done += 1,
                        _ => unreachable!(),
                    }
                } else if std::str::from_utf8(&target).is_ok_and(crate::domain::id::valid_id) {
                    n.binding = "missing_issue";
                    n.work_state = "missing";
                    out.counts.missing_issue += 1;
                    out.counts.missing += 1;
                } else {
                    n.binding = "reference";
                    out.counts.reference += 1;
                }
            }
            out.nodes.push(n);
        }
        out.counts.distinct_issues = distinct.len();
        let c = &out.counts;
        out.execution_state = if c.issue + c.missing_issue == 0 {
            "untracked"
        } else if c.missing_issue > 0 {
            "degraded"
        } else if c.in_progress > 0 {
            "in_progress"
        } else if c.runnable > 0 {
            "runnable"
        } else if c.blocked > 0 {
            "blocked"
        } else if c.issue > 0 && c.done == c.issue {
            "done"
        } else {
            "held"
        };
        out.lifecycle_mismatch = (p.status == "complete"
            && !matches!(out.execution_state, "done" | "untracked"))
            || (p.status != "complete" && out.execution_state == "done");
        Some(out)
    }
}
