use super::*;
use serde::Serialize;
#[derive(Clone, Debug, Default)]
pub struct SearchOptions {
    pub kinds: Vec<YamlString>,
    pub include_archived_handoffs: bool,
}
#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "PascalCase")]
pub struct Hit {
    pub kind: NoteKind,
    #[serde(rename = "ID")]
    pub id: YamlString,
    pub basename: YamlString,
    pub title: YamlString,
    pub project: YamlString,
    pub path: YamlString,
    pub score: i64,
}
fn kind(kind: NoteKind) -> &'static [u8] {
    match kind {
        NoteKind::Issue => b"issue",
        NoteKind::Doc => b"doc",
        NoteKind::Memory => b"memory",
        NoteKind::Request => b"request",
        NoteKind::Plan => b"plan",
        NoteKind::Handoff => b"handoff",
    }
}
pub(super) fn body(note: &Note) -> Vec<u8> {
    match &note.data {
        NoteData::Issue(_) => [note.description.as_slice(), note.body.as_slice()].join(&b'\n'),
        NoteData::Plan(p) => {
            let mut parts = vec![
                note.body.as_slice(),
                p.summary.outcome.as_bytes(),
                p.summary.affected_areas.as_bytes(),
                p.summary.execution_order.as_bytes(),
                p.summary.risks.as_bytes(),
            ];
            for section in &p.section_bodies {
                parts.push(section.markdown.as_bytes());
            }
            parts.join(&b'\n')
        }
        _ => note.body.clone(),
    }
}
impl Index {
    pub fn search(&self, query: &[u8], options: &SearchOptions) -> Vec<Hit> {
        let query = super::super::go_lower::lower(
            YamlString::from_bytes(query.into()).trimmed().as_bytes(),
        );
        if query.is_empty() {
            return vec![];
        }
        let matched = |bytes: &[u8]| contains(&super::super::go_lower::lower(bytes), &query);
        let mut hits = Vec::new();
        for note in self.ordered_notes() {
            if !options.include_archived_handoffs
                && matches!(&note.data,NoteData::Handoff(h) if h.metadata.archived)
            {
                continue;
            }
            if !options.kinds.is_empty()
                && !options
                    .kinds
                    .iter()
                    .any(|k| k.as_bytes() == kind(note.graph.kind))
            {
                continue;
            }
            let id = note.graph.id.as_deref().unwrap_or_default();
            let score = if matched(&note.title) {
                3
            } else if !id.is_empty() && matched(id) {
                2
            } else if note.tags.iter().flatten().any(|t| matched(t)) || matched(&body(note)) {
                1
            } else {
                0
            };
            if score > 0 {
                hits.push(Hit {
                    kind: note.graph.kind,
                    id: YamlString::from_bytes(if id.is_empty() {
                        note.graph.basename.clone()
                    } else {
                        id.into()
                    }),
                    basename: YamlString::from_bytes(note.graph.basename.clone()),
                    title: YamlString::from_bytes(note.title.clone()),
                    project: YamlString::from_bytes(note.project.clone()),
                    path: YamlString::from_bytes(note.graph.path.clone()),
                    score,
                });
            }
        }
        hits.sort_by(|a, b| {
            b.score
                .cmp(&a.score)
                .then_with(|| a.basename.cmp(&b.basename))
        });
        hits
    }
}
