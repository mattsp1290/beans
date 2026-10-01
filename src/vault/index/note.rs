use super::source::display;
use super::*;
use crate::vault::{DocMetadata, LinkKind, RawLink, doc_metadata};
use crate::{
    domain::{
        handoff::HandoffDocument,
        issue::IssueDocument,
        memory::MemoryDocument,
        plan::{self, Plan},
        request::RequestDocument,
        text::Link,
    },
    markdown,
};
#[derive(Clone, Debug)]
pub enum NoteData {
    Issue(Box<IssueDocument>),
    Request(Box<RequestDocument>),
    Memory(Box<MemoryDocument>),
    Handoff(Box<HandoffDocument>),
    Plan(Box<Plan>),
    Doc(Box<DocMetadata>),
}
#[derive(Clone, Debug)]
pub struct Note {
    pub graph: GraphNote,
    pub project: Vec<u8>,
    pub title: Vec<u8>,
    pub tags: Option<Vec<Vec<u8>>>,
    pub description: Vec<u8>,
    pub body: Vec<u8>,
    pub data: NoteData,
    /// Original file bytes, never the UTF-8 parser view.
    pub source: Vec<u8>,
}
fn strings(values: &[String]) -> Vec<Vec<u8>> {
    values.iter().map(|v| v.as_bytes().into()).collect()
}
fn tags(values: &[String]) -> Option<Vec<Vec<u8>>> {
    (!values.is_empty()).then(|| strings(values))
}
fn links(source: &[u8]) -> Vec<RawLink> {
    markdown::links(source)
        .into_iter()
        .map(|l| RawLink {
            target: l.target,
            kind: if l.embed {
                LinkKind::Embed
            } else {
                LinkKind::Body
            },
        })
        .collect()
}
fn structural(out: &mut Vec<RawLink>, link: &Link, kind: LinkKind) {
    if !link.is_zero() {
        out.push(RawLink {
            target: link.target.as_bytes().into(),
            kind,
        });
    }
}
impl Note {
    fn empty(kind: NoteKind, project: Vec<u8>, rel: &[u8], data: NoteData) -> Self {
        let basename = rel
            .rsplit(|&b| b == b'/')
            .next()
            .unwrap_or_default()
            .strip_suffix(b".md")
            .unwrap_or_default()
            .to_vec();
        Self {
            graph: GraphNote {
                kind,
                path: rel.into(),
                basename,
                id: None,
                aliases: vec![],
                raw_out: vec![],
                outlinks: vec![],
            },
            project,
            title: vec![],
            tags: None,
            description: vec![],
            body: vec![],
            source: vec![],
            data,
        }
    }
    fn parse(kind: NoteKind, project: Vec<u8>, rel: &[u8], bytes: &[u8]) -> Result<Self, Error> {
        if kind == NoteKind::Doc {
            let basename = rel
                .rsplit(|&b| b == b'/')
                .next()
                .unwrap()
                .strip_suffix(b".md")
                .unwrap();
            let doc = doc_metadata(basename, bytes);
            let mut note = Self::empty(kind, project, rel, NoteData::Doc(Box::new(doc.clone())));
            note.title = doc.title;
            note.tags = doc.tags;
            note.body = doc.body;
            if let Some(fm) = doc.frontmatter
                && let Some(value) = fm.get(b"aliases".as_slice())
            {
                match value {
                    crate::vault::YamlValue::String(v) if !v.is_empty() => {
                        note.graph.aliases.push(v.clone())
                    }
                    crate::vault::YamlValue::Sequence(v) => {
                        for item in v {
                            if let crate::vault::YamlValue::String(v) = item {
                                note.graph.aliases.push(v.clone());
                            }
                        }
                    }
                    _ => (),
                }
            }
            note.graph.raw_out = links(&note.body);
            note.source = bytes.into();
            return Ok(note);
        }
        let name = display(rel);
        let (data, id, aliases, title, note_tags, description, body, raw) = match kind {
            NoteKind::Issue => {
                let doc = IssueDocument::parse_bytes(&name, bytes)
                    .map_err(|e| e.with_path(&name, rel))?;
                let description = doc.description.as_bytes().to_vec();
                let body = doc.body.as_bytes().to_vec();
                let mut raw = links(&[description.as_slice(), body.as_slice()].concat());
                structural(&mut raw, &doc.metadata.parent, LinkKind::Parent);
                for link in &doc.metadata.blocked_by {
                    structural(&mut raw, link, LinkKind::BlockedBy);
                }
                let meta = &doc.metadata;
                let fields = (
                    Some(meta.id.as_bytes().to_vec()),
                    strings(&meta.aliases),
                    meta.title.as_bytes().to_vec(),
                    tags(&meta.labels),
                );
                (
                    NoteData::Issue(Box::new(doc)),
                    fields.0,
                    fields.1,
                    fields.2,
                    fields.3,
                    description,
                    body,
                    raw,
                )
            }
            NoteKind::Request => {
                let doc = RequestDocument::parse_bytes(&name, bytes)
                    .map_err(|e| e.with_path(&name, rel))?;
                let body = doc.body.as_bytes().to_vec();
                let mut raw = links(&body);
                for link in &doc.metadata.issues {
                    structural(&mut raw, link, LinkKind::RequestIssue);
                }
                let meta = &doc.metadata;
                let fields = (
                    Some(meta.id.as_bytes().to_vec()),
                    strings(&meta.aliases),
                    meta.title.as_bytes().to_vec(),
                    tags(&meta.labels),
                );
                (
                    NoteData::Request(Box::new(doc)),
                    fields.0,
                    fields.1,
                    fields.2,
                    fields.3,
                    vec![],
                    body,
                    raw,
                )
            }
            NoteKind::Memory => {
                let doc = MemoryDocument::parse_bytes(&name, bytes)
                    .map_err(|e| e.with_path(&name, rel))?;
                let body = doc.body.as_bytes().to_vec();
                let raw = links(&body);
                let fields = (
                    doc.metadata.key.as_bytes().to_vec(),
                    tags(&doc.metadata.tags),
                );
                (
                    NoteData::Memory(Box::new(doc)),
                    None,
                    vec![],
                    fields.0,
                    fields.1,
                    vec![],
                    body,
                    raw,
                )
            }
            NoteKind::Handoff => {
                let doc = HandoffDocument::parse_bytes(&name, bytes)
                    .map_err(|e| e.with_path(&name, rel))?;
                let body = doc.body.as_bytes().to_vec();
                let mut raw = links(&body);
                structural(&mut raw, &doc.metadata.issue, LinkKind::HandoffIssue);
                let meta = &doc.metadata;
                let fields = (
                    Some(meta.id.as_bytes().to_vec()),
                    strings(&meta.aliases),
                    meta.title.as_bytes().to_vec(),
                );
                (
                    NoteData::Handoff(Box::new(doc)),
                    fields.0,
                    fields.1,
                    fields.2,
                    None,
                    vec![],
                    body,
                    raw,
                )
            }
            _ => unreachable!(),
        };
        // Go derives issue/memory projects from the last `projects` path
        // component. Use the original Linux path bytes, not the parser view.
        let project = if matches!(kind, NoteKind::Issue | NoteKind::Memory) {
            let parts: Vec<_> = rel.split(|&b| b == b'/').collect();
            parts
                .windows(2)
                .rfind(|p| p[0] == b"projects")
                .map_or_else(Vec::new, |p| p[1].to_vec())
        } else {
            project
        };
        let mut note = Self::empty(kind, project, rel, data);
        note.title = title;
        note.tags = note_tags;
        note.description = description;
        note.body = body;
        note.source = bytes.into();
        note.graph.id = id;
        note.graph.aliases = aliases;
        note.graph.raw_out = raw;
        Ok(note)
    }
}
impl Index {
    pub(super) fn load_path(&mut self, rel: &[u8]) {
        if crate::vault::is_asset_path(rel) {
            self.assets.insert(rel.into());
            return;
        }
        let Some((kind, project)) = crate::vault::classify(rel) else {
            return;
        };
        let full = self.hub_dir.join(path(rel));
        match read(&full).and_then(|bytes| Note::parse(kind, project, rel, &bytes)) {
            Ok(note) => self.order.push(note),
            Err(e) => self.warning(rel, e),
        }
    }
    pub(super) fn load_plan(&mut self, project: &[u8], rel: &[u8]) {
        let full = self.hub_dir.join(path(rel));
        let result = crate::gitops::recover_plan_temp(&full.join("plan.md"))
            .and_then(|_| plan::load_path(&full));
        let mut bundle = match result {
            Ok(b) => b,
            Err(e) => {
                self.warning(rel, e);
                return;
            }
        };
        let mut plan = bundle.plan.take().unwrap();
        let Some(record) = self.projects.get(project) else {
            self.warning(
                rel,
                format!(
                    "plan project {} has no beans.toml",
                    path_name(&path(project)).quoted()
                ),
            );
            return;
        };
        if !record
            .config
            .prefix
            .as_str()
            .is_some_and(|prefix| plan::id::valid_id(prefix, &plan.id))
            && !plan::id::valid_id(&display(project), &plan.id)
        {
            self.warning(
                rel,
                format!(
                    "plan id {} does not match project",
                    crate::domain::issue::quoted(&plan.id)
                ),
            );
            return;
        }
        let manifest = [rel, b"/plan.md"].concat();
        plan.path = crate::domain::yaml_string::YamlString::from_bytes(manifest.clone());
        let mut raw = links(plan.body.as_bytes());
        for section in &bundle.sections {
            raw.extend(links(section.markdown.as_bytes()));
        }
        let mut note = Note::empty(
            NoteKind::Plan,
            project.into(),
            &manifest,
            NoteData::Plan(Box::new(plan.clone())),
        );
        note.graph.basename = plan.id.as_bytes().into();
        note.graph.id = Some(plan.id.as_bytes().into());
        note.graph.aliases = plan.aliases.iter().map(|s| s.as_bytes().into()).collect();
        note.title = plan.title.as_bytes().into();
        note.body = plan.body.as_bytes().into();
        note.graph.raw_out = raw;
        self.order.push(note);
    }
}
