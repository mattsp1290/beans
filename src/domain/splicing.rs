//! yaml.v3 issue codecs splice physical lines, including coincident spans in
//! flow-root mappings. Replay that ordering with source provenance, then derive
//! nonoverlapping original-byte edits for the verified byte-copy engine.
use super::frontmatter::{Error, Field};
use beans_kernel::splice::Span;
use std::collections::HashSet;

#[derive(Clone)]
struct Piece<'a> {
    original: Option<Span>,
    bytes: &'a [u8],
}

pub(super) fn owned_edits(
    source: &[u8],
    fm: Span,
    fields: &[Field],
    owned_order: &[&str],
    changes: &[(&str, Option<&[u8]>)],
) -> Result<Vec<(Span, Vec<u8>)>, Error> {
    let text = source
        .get(fm.start..fm.end)
        .ok_or_else(|| Error::new("invalid frontmatter byte range".into()))?;
    let mut pieces = Vec::new();
    let mut offsets = vec![fm.start];
    let mut offset = fm.start;
    for bytes in text.split_inclusive(|byte| *byte == b'\n') {
        let span = Span {
            start: offset,
            end: offset + bytes.len(),
        };
        pieces.push(Piece {
            original: Some(span),
            bytes,
        });
        offset = span.end;
        offsets.push(offset);
    }
    let mut seen = HashSet::new();
    for &(key, _) in changes {
        if !owned_order.contains(&key) || !seen.insert(key) {
            return Err(Error::new("invalid or duplicate owned field change".into()));
        }
    }
    let mut edits = Vec::new();
    let mut added = Vec::new();
    for &key in owned_order {
        let Some((_, replacement)) = changes.iter().find(|(changed, _)| *changed == key) else {
            continue;
        };
        if let Some(field) = fields.iter().find(|field| field.key == key) {
            let start = offsets
                .binary_search(&field.start)
                .map_err(|_| Error::new("field start is not a physical line boundary".into()))?;
            let end = offsets
                .binary_search(&field.end)
                .map_err(|_| Error::new("field end is not a physical line boundary".into()))?;
            let lines = replacement
                .unwrap_or_default()
                .split_inclusive(|byte| *byte == b'\n')
                .map(|bytes| Piece {
                    original: None,
                    bytes,
                })
                .collect::<Vec<_>>();
            edits.push((start, end, lines));
        } else if let Some(replacement) = replacement {
            added.extend_from_slice(replacement);
        }
    }
    if !added.is_empty() {
        let byte = fields
            .iter()
            .filter(|field| owned_order.contains(&field.key.as_str()))
            .map(|field| field.end)
            .max()
            .unwrap_or(fm.end);
        let position = offsets
            .binary_search(&byte)
            .map_err(|_| Error::new("insertion is not a physical line boundary".into()))?;
        edits.push((
            position,
            position,
            added
                .split_inclusive(|byte| *byte == b'\n')
                .map(|bytes| Piece {
                    original: None,
                    bytes,
                })
                .collect(),
        ));
    }
    // Stable descending start; a pure insertion at a replacement's start is
    // replayed first. This also reproduces Go's same-line flow mapping quirks.
    edits.sort_by(|a, b| b.0.cmp(&a.0).then_with(|| (b.0 == b.1).cmp(&(a.0 == a.1))));
    for (start, end, lines) in edits {
        if start > end || end > pieces.len() {
            return Err(Error::new("invalid frontmatter line edit".into()));
        }
        pieces.splice(start..end, lines);
    }
    let mut byte_edits = Vec::new();
    let mut cursor = fm.start;
    let mut pending = Vec::new();
    for piece in pieces {
        if let Some(span) = piece.original {
            if span.start < cursor || span.end < span.start || span.end > fm.end {
                return Err(Error::new("invalid source provenance order".into()));
            }
            if span.start != cursor || !pending.is_empty() {
                byte_edits.push((
                    Span {
                        start: cursor,
                        end: span.start,
                    },
                    std::mem::take(&mut pending),
                ));
            }
            cursor = span.end;
        } else {
            pending.extend_from_slice(piece.bytes);
        }
    }
    if cursor != fm.end || !pending.is_empty() {
        byte_edits.push((
            Span {
                start: cursor,
                end: fm.end,
            },
            pending,
        ));
    }
    Ok(byte_edits)
}
