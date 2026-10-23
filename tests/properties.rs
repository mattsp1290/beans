use beans::kernel::splice::{Span, preserved_interval, translate_offset, valid_splices};
use proptest::prelude::*;

proptest! {
    #![proptest_config(ProptestConfig::with_cases(512))]

    #[test]
    fn validation_matches_pairwise_geometry(
        pairs in prop::collection::vec((any::<usize>(), any::<usize>()), 0..24),
        source_len in any::<usize>(),
    ) {
        let spans: Vec<_> = pairs.iter().map(|&(start, end)| Span { start, end }).collect();
        let expected = spans.iter().all(|span| span.start <= span.end && span.end <= source_len)
            && (0..spans.len()).all(|left|
                (left + 1..spans.len()).all(|right| spans[left].end <= spans[right].start));
        prop_assert_eq!(valid_splices(&spans, source_len), expected);
    }

    #[test]
    fn translation_matches_wide_integer_arithmetic(
        start in any::<usize>(), end in any::<usize>(),
        destination in any::<usize>(), offset in any::<usize>(),
    ) {
        let expected = if start <= offset && offset <= end {
            usize::try_from(destination as u128 + (offset - start) as u128).ok()
        } else {
            None
        };
        prop_assert_eq!(translate_offset(Span { start, end }, destination, offset), expected);
    }

    #[test]
    fn prompt_edits_preserve_every_unowned_byte(
        instruction in "[a-zA-Z0-9 雪☃]{0,80}",
        blanks in 0usize..5,
    ) {
        let source = format!(
            "---\nid: beans-p-ab12\naliases: [beans-p-ab12]\ntitle: Property prompt\nstatus: open\npriority: 2\nlabels: [agents]\nfrom: planner\nto: worker\nunknown: {{nested: [one, two]}}\n{}# retained comment\ncreated: 2026-09-11T15:51:49Z\nupdated: 2026-09-11T15:51:49Z\n---\n{instruction}\n\n## Log\n- opaque original\n\n## Tail\nretained tail ☃\n",
            "\n".repeat(blanks)
        );
        let path = "projects/beans/prompts/beans-p-ab12-property-prompt.md";
        let mut prompt = beans::domain::prompt::PromptDocument::parse(path, &source).unwrap();
        prop_assert_eq!(prompt.encode().unwrap().bytes, source.as_bytes());
        prompt.metadata.priority = 4;
        let encoded = prompt.encode().unwrap();
        let expected = source.replacen("priority: 2\n", "priority: 4\n", 1);
        prop_assert_eq!(&encoded.bytes, expected.as_bytes());
        for copy in encoded.copies {
            prop_assert_eq!(&source.as_bytes()[copy.source], &encoded.bytes[copy.destination]);
        }
    }

    #[test]
    fn preserved_intervals_keep_every_unchanged_byte_in_order(
        edits in prop::collection::vec((0usize..8, 0usize..8), 0..16),
        tail in 0usize..8,
    ) {
        let mut cursor = 0;
        let spans: Vec<_> = edits.iter().map(|&(gap, width)| {
            let start = cursor + gap;
            cursor = start + width;
            Span { start, end: cursor }
        }).collect();
        let source_len = cursor + tail;
        let source: Vec<_> = (0..source_len).collect();
        prop_assert!(valid_splices(&spans, source_len));
        let mut copied = Vec::new();
        for index in 0..=spans.len() {
            let interval = preserved_interval(&spans, source_len, index).unwrap();
            let destination = copied.len();
            for (offset, byte) in source.iter().enumerate().take(interval.end).skip(interval.start) {
                prop_assert_eq!(translate_offset(interval, destination, offset),
                    Some(copied.len()));
                copied.push(*byte);
            }
            prop_assert_eq!(translate_offset(interval, destination, interval.end),
                Some(copied.len()));
        }
        let expected: Vec<_> = source.into_iter().filter(|byte|
            !spans.iter().any(|span| span.start <= *byte && *byte < span.end)).collect();
        prop_assert_eq!(copied, expected);
    }
}
