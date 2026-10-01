//! Checked byte copying shared by frontmatter edits and plan graph fences.
use super::frontmatter::{CopyRange, EditResult, Error};
use beans_kernel::splice::{Span, preserved_interval, translate_offset, valid_splices};
pub(crate) fn apply(source: &[u8], mut edits: Vec<(Span, &[u8])>) -> Result<EditResult, Error> {
    edits.sort_by_key(|(span, _)| (span.start, span.end));
    let spans: Vec<_> = edits.iter().map(|(span, _)| *span).collect();
    if !valid_splices(&spans, source.len()) {
        return Err(Error("invalid frontmatter splice geometry".into()));
    }
    let mut output_len = source.len();
    for (span, replacement) in &edits {
        output_len = output_len
            .checked_sub(span.end - span.start)
            .and_then(|len| len.checked_add(replacement.len()))
            .ok_or_else(|| Error("frontmatter output length overflow".into()))?;
    }
    let mut bytes = Vec::new();
    bytes
        .try_reserve_exact(output_len)
        .map_err(|_| Error("frontmatter output allocation failed".into()))?;
    let mut copies = Vec::new();
    for index in 0..=edits.len() {
        let preserved = preserved_interval(&spans, source.len(), index)
            .ok_or_else(|| Error("invalid preserved interval".into()))?;
        let destination_start = bytes.len();
        let destination_end = translate_offset(preserved, destination_start, preserved.end)
            .ok_or_else(|| Error("frontmatter offset overflow".into()))?;
        bytes.extend_from_slice(&source[preserved.start..preserved.end]);
        copies.push(CopyRange {
            source: preserved.start..preserved.end,
            destination: destination_start..destination_end,
        });
        if let Some((_, replacement)) = edits.get(index) {
            bytes.extend_from_slice(replacement);
        }
    }
    Ok(EditResult { bytes, copies })
}
