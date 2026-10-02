//! Go Unicode simple lowercasing, preserving one replacement per invalid byte.
#[path = "go_lower_table.rs"]
mod table;
pub(super) fn lower(mut input: &[u8]) -> Vec<u8> {
    let mut out = Vec::new();
    while !input.is_empty() {
        let (valid, invalid) = match std::str::from_utf8(input) {
            Ok(_) => (input.len(), 0),
            Err(e) => (
                e.valid_up_to(),
                e.error_len().unwrap_or(input.len() - e.valid_up_to()),
            ),
        };
        for ch in std::str::from_utf8(&input[..valid]).unwrap().chars() {
            let cp = ch as u32;
            let lowered = table::LOWER
                .binary_search_by_key(&cp, |&(a, _)| a)
                .map(|i| table::LOWER[i].1)
                .unwrap_or(cp);
            let ch = char::from_u32(lowered).unwrap();
            out.extend_from_slice(ch.encode_utf8(&mut [0; 4]).as_bytes());
        }
        for _ in 0..invalid {
            out.extend_from_slice("�".as_bytes());
        }
        input = &input[valid + invalid..];
    }
    out
}
