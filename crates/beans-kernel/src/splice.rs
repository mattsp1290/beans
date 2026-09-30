//! Byte geometry for lossless top-level field edits. YAML interpretation and
//! actual byte copying remain codec responsibilities, checked separately in WP3.
use vstd::prelude::*;

verus! {

/// A half-open byte range. Empty ranges represent insertions.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Span {
    pub start: usize,
    pub end: usize,
}

pub open spec fn ordered_in_bounds(spans: Seq<Span>, source_len: usize) -> bool {
    (forall|i: int| #![trigger spans[i]] 0 <= i < spans.len() ==>
        spans[i].start <= spans[i].end <= source_len)
    && (forall|i: int, j: int| #![trigger spans[i], spans[j]] 0 <= i < j < spans.len() ==>
        spans[i].end <= spans[j].start)
}

/// Validate all ranges before copying or replacing any bytes. The specification
/// states pairwise source ordering, not just the adjacent comparison in the loop.
pub fn valid_splices(spans: &[Span], source_len: usize) -> (valid: bool)
    ensures valid == ordered_in_bounds(spans@, source_len),
{
    let mut i: usize = 0;
    let mut previous_end: usize = 0;
    while i < spans.len()
        invariant
            i <= spans.len(),
            previous_end <= source_len,
            i == 0 ==> previous_end == 0,
            i > 0 ==> previous_end == spans@[(i - 1) as int].end,
            forall|k: int| #![trigger spans@[k]] 0 <= k < i ==>
                spans@[k].start <= spans@[k].end <= source_len,
            forall|k: int| #![trigger spans@[k]] 0 <= k < i ==> spans@[k].end <= previous_end,
            forall|j: int, k: int| #![trigger spans@[j], spans@[k]] 0 <= j < k < i ==>
                spans@[j].end <= spans@[k].start,
        decreases spans.len() - i,
    {
        let span = &spans[i];
        if span.start > span.end || span.end > source_len || span.start < previous_end {
            return false;
        }
        previous_end = span.end;
        i += 1;
    }
    true
}

/// The unchanged interval before splice `index`, or the trailing interval when
/// `index == spans.len()`. Together these enumerate every preserved source byte
/// in order. An invalid splice list is rejected before returning any interval.
pub fn preserved_interval(spans: &[Span], source_len: usize, index: usize)
    -> (interval: Option<Span>)
    ensures
        interval.is_some() == (ordered_in_bounds(spans@, source_len) && index <= spans.len()),
        interval.is_some() ==> {
            let span = interval.unwrap();
            &&& span.start <= span.end <= source_len
            &&& span.start == (if index == 0 { 0 } else { spans@[(index - 1) as int].end })
            &&& span.end == (if index == spans.len() { source_len } else { spans@[index as int].start })
        },
{
    if !valid_splices(spans, source_len) || index > spans.len() {
        return None;
    }
    let start = if index == 0 { 0 } else { spans[index - 1].end };
    let end = if index == spans.len() { source_len } else { spans[index].start };
    proof {
        reveal(ordered_in_bounds);
        assert(index <= spans@.len());
        if index > 0 {
            assert(0 <= (index - 1) as int < spans@.len());
            assert(spans@[(index - 1) as int].start
                <= spans@[(index - 1) as int].end <= source_len);
        }
        if index < spans.len() {
            assert(spans@[index as int].start <= spans@[index as int].end <= source_len);
        }
        if 0 < index < spans.len() {
            assert(spans@[(index - 1) as int].end <= spans@[index as int].start);
        }
        assert(start <= end <= source_len);
    }
    Some(Span { start, end })
}

/// Translate a position within an unchanged interval to its destination. The
/// inclusive end is allowed so callers can calculate the copied interval's end.
/// Subtract first, then add: adding source offsets first can overflow even when
/// the final translated position fits. None means invalid geometry or overflow.
pub fn translate_offset(source: Span, destination_start: usize, offset: usize)
    -> (translated: Option<usize>)
    ensures
        translated.is_some() == (
            source.start <= offset <= source.end
            && destination_start as int + offset as int - source.start as int <= usize::MAX
        ),
        translated.is_some() ==> translated.unwrap() as int ==
            destination_start as int + offset as int - source.start as int,
        translated.is_some() ==> translated.unwrap() >= destination_start,
{
    if offset < source.start || offset > source.end {
        return None;
    }
    let distance = offset - source.start;
    if distance > usize::MAX - destination_start {
        None
    } else {
        Some(destination_start + distance)
    }
}

/// Two copied positions retain their source order and distance. Applying this
/// to the endpoints proves unchanged-interval length preservation. This proof
/// makes no claim about which bytes a YAML parser chooses to replace.
pub proof fn preserved_length_and_order(source: Span, destination_start: usize,
    left: usize, right: usize, translated_left: usize, translated_right: usize)
    requires
        source.start <= left <= right <= source.end,
        translated_left as int == destination_start as int + left as int - source.start as int,
        translated_right as int == destination_start as int + right as int - source.start as int,
    ensures
        translated_left <= translated_right,
        translated_right - translated_left == right - left,
{
}
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn adjacent_and_empty_ranges_are_valid_but_overlap_and_bad_bounds_are_not() {
        assert!(valid_splices(&[], 0));
        assert!(valid_splices(
            &[
                Span { start: 0, end: 0 },
                Span { start: 0, end: 2 },
                Span { start: 2, end: 2 },
            ],
            2,
        ));
        assert!(!valid_splices(&[Span { start: 2, end: 1 }], 3));
        assert!(!valid_splices(&[Span { start: 0, end: 4 }], 3));
        assert!(!valid_splices(
            &[Span { start: 0, end: 2 }, Span { start: 1, end: 3 }],
            3,
        ));
    }

    #[test]
    fn unchanged_intervals_cover_the_complement_in_source_order() {
        let edits = [Span { start: 2, end: 4 }, Span { start: 7, end: 9 }];
        assert_eq!(
            preserved_interval(&edits, 10, 0),
            Some(Span { start: 0, end: 2 })
        );
        assert_eq!(
            preserved_interval(&edits, 10, 1),
            Some(Span { start: 4, end: 7 })
        );
        assert_eq!(
            preserved_interval(&edits, 10, 2),
            Some(Span { start: 9, end: 10 })
        );
        assert_eq!(preserved_interval(&edits, 10, 3), None);
        assert_eq!(
            preserved_interval(&[Span { start: 3, end: 2 }], 10, 0),
            None
        );
    }

    #[test]
    fn translation_rejects_overflow_without_rejecting_large_source_offsets() {
        let source = Span {
            start: usize::MAX - 2,
            end: usize::MAX,
        };
        assert_eq!(translate_offset(source, usize::MAX - 1, usize::MAX), None);
        assert_eq!(
            translate_offset(source, usize::MAX - 2, usize::MAX),
            Some(usize::MAX)
        );
        assert_eq!(translate_offset(source, 0, usize::MAX), Some(2));
        assert_eq!(translate_offset(source, 0, 0), None);
        assert_eq!(translate_offset(Span { start: 2, end: 1 }, 0, 2), None);
    }
}
