use super::*;
use serde::Serialize;
use std::collections::{BTreeMap, BTreeSet};
#[derive(Debug)]
pub struct BlockedIssue<'a> {
    pub issue: &'a Note,
    pub blockers: Vec<YamlString>,
}
#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "PascalCase")]
pub struct DependencyNode {
    #[serde(rename = "ID")]
    pub id: YamlString,
    pub title: YamlString,
    pub status: YamlString,
    #[serde(rename = "Type")]
    pub kind: YamlString,
    pub project: YamlString,
    pub priority: i64,
    pub archived: bool,
}
#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "PascalCase")]
pub struct DependencyEdge {
    pub from: YamlString,
    pub to: YamlString,
    pub kind: YamlString,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct DependencyGraph {
    pub nodes: Vec<DependencyNode>,
    pub edges: Vec<DependencyEdge>,
}
impl Index {
    pub fn ready(&self, project: &[u8], all: bool) -> Vec<&Note> {
        let mut out: Vec<_> = self
            .project_issues(if all { b"" } else { project }, false)
            .into_iter()
            .filter(|n| {
                let m = &issue(n).unwrap().metadata;
                self.workflow_for(&n.project).is_active(m.status.as_bytes())
                    && !(m.kind == "epic" && !self.children(m.id.as_bytes()).is_empty())
                    && m.blocked_by.iter().all(|b| {
                        self.resolve_issue_ref(b.raw.as_bytes()).1.is_some_and(|n| {
                            self.workflow_for(&n.project)
                                .is_terminal(issue(n).unwrap().metadata.status.as_bytes())
                        })
                    })
            })
            .collect();
        out.sort_by(sort_issues);
        out
    }
    pub fn blocked(&self, project: &[u8], all: bool) -> Vec<BlockedIssue<'_>> {
        let mut out = Vec::new();
        for note in self.project_issues(if all { b"" } else { project }, false) {
            let m = &issue(note).unwrap().metadata;
            if self
                .workflow_for(&note.project)
                .is_terminal(m.status.as_bytes())
            {
                continue;
            }
            let mut blockers = Vec::new();
            for b in &m.blocked_by {
                match self
                    .lookup(b.target.as_bytes())
                    .filter(|n| issue(n).is_some())
                {
                    None => blockers.push(b.target.as_str().into()),
                    Some(n)
                        if !self
                            .workflow_for(&n.project)
                            .is_terminal(issue(n).unwrap().metadata.status.as_bytes()) =>
                    {
                        blockers.push(issue(n).unwrap().metadata.id.as_str().into())
                    }
                    _ => (),
                }
            }
            if !blockers.is_empty() {
                out.push(BlockedIssue {
                    issue: note,
                    blockers,
                });
            }
        }
        out
    }
    pub fn children(&self, id: &[u8]) -> Vec<&Note> {
        self.project_issues(b"", true)
            .into_iter()
            .filter(|n| {
                let m = &issue(n).unwrap().metadata;
                !m.parent.is_zero()
                    && self
                        .lookup(m.parent.target.as_bytes())
                        .and_then(issue)
                        .is_some_and(|p| p.metadata.id.as_bytes() == id)
            })
            .collect()
    }
    pub fn parents(&self, id: &[u8]) -> Vec<&Note> {
        let Some(mut note) = self.note_by_id(NoteKind::Issue, id) else {
            return vec![];
        };
        let mut visited = BTreeSet::from([id.to_vec()]);
        let mut out = Vec::new();
        loop {
            let parent = &issue(note).unwrap().metadata.parent;
            if parent.is_zero() {
                break;
            }
            let Some(next) = self
                .lookup(parent.target.as_bytes())
                .filter(|n| issue(n).is_some())
            else {
                break;
            };
            if !visited.insert(issue(next).unwrap().metadata.id.as_bytes().to_vec()) {
                break;
            }
            out.push(next);
            note = next;
        }
        out
    }
    pub fn blockers<'a>(&'a self, note: &Note) -> (Vec<&'a Note>, Vec<YamlString>) {
        let mut resolved = Vec::new();
        let mut unresolved = Vec::new();
        for b in &issue(note).expect("issue note").metadata.blocked_by {
            if let Some(n) = self.resolve_issue_ref(b.raw.as_bytes()).1 {
                resolved.push(n);
            } else {
                unresolved.push(b.target.as_str().into());
            }
        }
        (resolved, unresolved)
    }
    pub fn dependency_graph(&self, project: &[u8], all: bool) -> DependencyGraph {
        let base = self.project_issues(if all { b"" } else { project }, true);
        let mut nodes: BTreeMap<_, _> = base
            .iter()
            .map(|n| (issue(n).unwrap().metadata.id.clone(), *n))
            .collect();
        let mut edges = Vec::new();
        for note in base {
            let m = &issue(note).unwrap().metadata;
            let refs = m
                .blocked_by
                .iter()
                .map(|b| (b, "blocks"))
                .chain((!m.parent.is_zero()).then_some((&m.parent, "parent")));
            for (link, kind) in refs {
                if let Some(n) = self
                    .lookup(link.target.as_bytes())
                    .filter(|n| issue(n).is_some())
                {
                    let to = &issue(n).unwrap().metadata.id;
                    nodes.entry(to.clone()).or_insert(n);
                    edges.push(DependencyEdge {
                        from: m.id.as_str().into(),
                        to: to.as_str().into(),
                        kind: kind.into(),
                    });
                }
            }
        }
        edges.sort_by(|a, b| (&a.from, &a.kind, &a.to).cmp(&(&b.from, &b.kind, &b.to)));
        let nodes = nodes
            .into_values()
            .map(|n| {
                let m = &issue(n).unwrap().metadata;
                DependencyNode {
                    id: m.id.as_str().into(),
                    title: YamlString::from_bytes(n.title.clone()),
                    status: m.status.as_str().into(),
                    kind: m.kind.as_str().into(),
                    project: YamlString::from_bytes(n.project.clone()),
                    priority: m.priority,
                    archived: m.archived,
                }
            })
            .collect();
        DependencyGraph { nodes, edges }
    }
}
