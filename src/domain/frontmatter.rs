//! Original-byte frontmatter primitives. Typed note schemas supply ownership
//! and field rendering; parsing alone never rewrites YAML or Markdown.
use beans_kernel::splice::Span;
use serde::Serialize;
use std::fmt;

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum NodeKind {
    Scalar,
    Sequence,
    Mapping,
    Alias,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
pub struct Node {
    /// Resolved YAML tag used by schemas that restrict scalar/container types.
    #[serde(skip)]
    pub tag: String,
    /// Parser identity of an anchor or the target of an alias.
    #[serde(skip)]
    pub anchor_id: usize,
    /// Scanner spelling for aliases and anchored nodes; semantic JSON is unchanged.
    #[serde(skip)]
    pub anchor_name: String,
    pub kind: NodeKind,
    pub line: usize,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub value: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub null: Option<bool>,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub children: Vec<Node>,
}

impl Node {
    /// yaml.v3's owned string fields accept any scalar's text, including
    /// numeric and boolean spellings. Null becomes empty; aliases are rejected.
    pub fn scalar(&self, key: &str) -> Result<&str, Error> {
        if self.kind != NodeKind::Scalar {
            return Err(Error(format!("{key} must be a string")));
        }
        Ok(if self.null == Some(true) {
            ""
        } else {
            self.value.as_deref().unwrap_or_default()
        })
    }

    pub fn string_list(&self, key: &str) -> Result<Vec<&str>, Error> {
        match self.kind {
            NodeKind::Scalar => {
                let value = self.value.as_deref().unwrap_or_default();
                if self.null == Some(true) || value.trim().is_empty() {
                    Ok(Vec::new())
                } else {
                    Ok(vec![value])
                }
            }
            NodeKind::Sequence
                if self
                    .children
                    .iter()
                    .all(|node| node.kind == NodeKind::Scalar) =>
            {
                Ok(self
                    .children
                    .iter()
                    .map(|node| node.value.as_deref().unwrap_or_default())
                    .collect())
            }
            _ => Err(Error(format!("{key} must be a list of strings"))),
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
pub struct Field {
    pub key: String,
    pub key_line: usize,
    pub value: Node,
    /// Absolute byte positions in the original document, excluding preceding
    /// or trailing blank/comment lines, matching Go's closeSpans policy.
    pub start: usize,
    pub end: usize,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Error(pub String);

impl fmt::Display for Error {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(&self.0)
    }
}

impl std::error::Error for Error {}

#[derive(Clone, Debug)]
pub struct Frontmatter {
    source: String,
    raw: Vec<u8>,
    offsets: Vec<usize>,
    fields: Vec<Field>,
    fm_start: usize,
    fm_end: usize,
    body_start: usize,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CopyRange {
    pub source: std::ops::Range<usize>,
    pub destination: std::ops::Range<usize>,
}

/// Retained copy geometry lets callers check or translate original ranges.
/// The bytes are created by the real verified-helper path, not a test model.
#[derive(Debug)]
pub struct EditResult {
    pub bytes: Vec<u8>,
    pub copies: Vec<CopyRange>,
}

impl Frontmatter {
    pub fn parse(path: &str, source: &str) -> Result<Self, Error> {
        Self::parse_bytes(path, source.as_bytes())
    }

    pub fn parse_bytes(path: &str, raw: &[u8]) -> Result<Self, Error> {
        if let Ok(text) = std::str::from_utf8(raw) {
            return Self::parse_text(path, text, raw, Vec::new());
        }
        let view = super::source::Source::new(raw);
        Self::parse_text(path, &view.text, raw, view.offsets)
    }

    fn parse_text(
        path: &str,
        source: &str,
        raw: &[u8],
        mapping: Vec<usize>,
    ) -> Result<Self, Error> {
        if source.contains("\r\n") {
            return Err(Error(format!(
                "{path}: has Windows line endings (\\r\\n); bn requires \\n"
            )));
        }
        if !source.starts_with("---\n") {
            return Err(Error(format!(
                "{path}: line 1: file must start with a --- frontmatter fence"
            )));
        }
        let fm_start = 4;
        let mut fm_end = None;
        let mut body_start = source.len();
        let mut offset = fm_start;
        for line in source[fm_start..].split_inclusive('\n') {
            if line.strip_suffix('\n').unwrap_or(line) == "---" {
                fm_end = Some(offset);
                body_start = offset + line.len();
                break;
            }
            offset += line.len();
        }
        let fm_end =
            fm_end.ok_or_else(|| Error(format!("{path}: frontmatter has no closing --- fence")))?;
        let text = &source[fm_start..fm_end];
        let map = |at: usize| mapping.get(at).copied().unwrap_or(at);
        let root = super::yaml::parse_raw(path, text, &raw[map(fm_start)..map(fm_end)])?;
        if root.kind != NodeKind::Mapping {
            return Err(Error(format!(
                "{path}: line {}: frontmatter must be a mapping",
                root.line + 1
            )));
        }
        let lines: Vec<&str> = text.split_inclusive('\n').collect();
        let offsets: Vec<usize> = std::iter::once(fm_start)
            .chain(lines.iter().scan(fm_start, |position, line| {
                *position += line.len();
                Some(*position)
            }))
            .collect();
        let mut fields = Vec::new();
        let (pairs, remainder) = root.children.as_chunks::<2>();
        if !remainder.is_empty() {
            return Err(Error(format!("{path}: invalid YAML mapping pairs")));
        }
        for pair in pairs {
            let key = &pair[0];
            if key.kind != NodeKind::Scalar {
                return Err(Error(format!(
                    "{path}: line {}: frontmatter keys must be strings",
                    key.line + 1
                )));
            }
            let line = key
                .line
                .checked_sub(1)
                .ok_or_else(|| Error(format!("{path}: invalid frontmatter line position")))?;
            let start = *offsets
                .get(line)
                .ok_or_else(|| Error(format!("{path}: invalid frontmatter line position")))?;
            fields.push(Field {
                key: key.value.clone().unwrap_or_default(),
                key_line: key.line,
                value: pair[1].clone(),
                start,
                end: fm_end,
            });
        }
        for index in 0..fields.len() {
            let start_line = fields[index].key_line - 1;
            let mut end_line = fields
                .get(index + 1)
                .map_or(lines.len(), |next| next.key_line - 1);
            while end_line > start_line + 1 {
                let line = lines[end_line - 1].trim();
                if !line.is_empty() && !line.starts_with('#') {
                    break;
                }
                end_line -= 1;
            }
            fields[index].end = offsets[end_line];
        }
        for field in &mut fields {
            field.start = map(field.start);
            field.end = map(field.end);
        }
        Ok(Self {
            source: source.to_owned(),
            raw: raw.into(),
            offsets: mapping,
            fields,
            fm_start,
            fm_end,
            body_start,
        })
    }

    pub(crate) fn raw_offset(&self, view: usize) -> usize {
        self.offsets.get(view).copied().unwrap_or(view)
    }
    pub(crate) fn view_offset(&self, raw: usize) -> usize {
        if self.offsets.is_empty() {
            raw
        } else {
            self.offsets.partition_point(|&v| v < raw)
        }
    }
    pub fn original_bytes(&self) -> &[u8] {
        &self.raw
    }

    /// A read-only Unicode view. Use original_bytes for preservation or writes.
    pub fn original(&self) -> &str {
        &self.source
    }

    pub fn fields(&self) -> &[Field] {
        &self.fields
    }

    pub fn yaml_text(&self) -> &str {
        &self.source[self.fm_start..self.fm_end]
    }

    /// Diagnostic lines follow yaml.v3's raw Unicode line-break counting.
    /// Physical LF byte ranges remain separate for preservation/splicing.
    pub(crate) fn diagnostic_line(&self, yaml_line: usize) -> usize {
        let extra = self
            .yaml_text()
            .split_inclusive('\n')
            .take(yaml_line.saturating_sub(1))
            .flat_map(str::chars)
            .filter(|ch| matches!(ch, '\u{85}' | '\u{2028}' | '\u{2029}'))
            .count();
        yaml_line + 1 + extra
    }

    pub fn body(&self) -> &str {
        &self.source[self.body_start..]
    }

    pub fn body_bytes(&self) -> &[u8] {
        self.slice_bytes(self.body())
    }

    /// Slice only fragments borrowed from this document's parser view.
    pub(crate) fn slice_bytes(&self, fragment: &str) -> &[u8] {
        if fragment.is_empty() {
            return &[];
        }
        let start = fragment.as_ptr() as usize - self.source.as_ptr() as usize;
        if self.offsets.is_empty() {
            &self.raw[start..start + fragment.len()]
        } else {
            &self.raw[self.offsets[start]..self.offsets[start + fragment.len()]]
        }
    }

    /// Replace whole parsed top-level spans with already rendered field bytes.
    /// Ownership and YAML value rendering belong to typed note adapters. This
    /// primitive never derives offsets from user input or character indices.
    pub fn replace_fields(&self, replacements: &[(usize, &[u8])]) -> Result<EditResult, Error> {
        let mut edits = Vec::new();
        let mut seen = std::collections::HashSet::new();
        for &(index, bytes) in replacements {
            if !seen.insert(index) {
                return Err(Error("duplicate field replacement".into()));
            }
            let field = self
                .fields
                .get(index)
                .ok_or_else(|| Error("field replacement is out of bounds".into()))?;
            edits.push((
                Span {
                    start: field.start,
                    end: field.end,
                },
                bytes,
            ));
        }
        self.apply_edits(edits)
    }

    pub(crate) fn splice_owned(
        &self,
        owned_order: &[&str],
        changes: &[(&str, Option<&[u8]>)],
        body: &[u8],
    ) -> Result<EditResult, Error> {
        let mut edits = super::splicing::owned_edits(
            &self.raw,
            Span {
                start: self.raw_offset(self.fm_start),
                end: self.raw_offset(self.fm_end),
            },
            &self.fields,
            owned_order,
            changes,
        )?;
        if body != self.body_bytes() {
            edits.push((
                Span {
                    start: self.raw_offset(self.body_start),
                    end: self.raw.len(),
                },
                body.to_vec(),
            ));
        }
        self.apply_edits(
            edits
                .iter()
                .map(|(span, bytes)| (*span, bytes.as_slice()))
                .collect(),
        )
    }

    fn apply_edits(&self, edits: Vec<(Span, &[u8])>) -> Result<EditResult, Error> {
        super::byte_edit::apply(&self.raw, edits)
    }
}
