//! Go's pinned Unicode printability for strconv-style quoted diagnostics.
#[path = "go_print_ranges.rs"]
mod table;
pub(crate) fn printable(ch: char) -> bool {
    let scalar = ch as u32;
    let index = table::RANGES.partition_point(|&(_, end)| end < scalar);
    table::RANGES
        .get(index)
        .is_some_and(|&(start, _)| start <= scalar)
}
#[cfg(test)]
mod tests {
    #[test]
    fn production_printability_matches_go_for_every_unicode_scalar() {
        let fixture: serde_json::Value =
            serde_json::from_str(include_str!("../../tests/contract/config-foundation.json"))
                .unwrap();
        let ranges: Vec<(u32, u32)> =
            serde_json::from_value(fixture["print_ranges"].clone()).unwrap();
        assert_eq!(ranges, super::table::RANGES);
        let mut range = ranges.iter().peekable();
        for cp in 0..=0x10ffff {
            while range.peek().is_some_and(|&&(_, end)| end < cp) {
                range.next();
            }
            let expected = range
                .peek()
                .is_some_and(|&&(start, end)| start <= cp && cp <= end);
            if let Some(ch) = char::from_u32(cp) {
                assert_eq!(super::printable(ch), expected, "U+{cp:04X}");
            }
        }
    }
}
