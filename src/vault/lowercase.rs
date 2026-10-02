//! Native Unicode lowercasing with one replacement per invalid byte.
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
            let ch = ch.to_lowercase().next().unwrap();
            out.extend_from_slice(ch.encode_utf8(&mut [0; 4]).as_bytes());
        }
        for _ in 0..invalid {
            out.extend_from_slice("�".as_bytes());
        }
        input = &input[valid + invalid..];
    }
    out
}

#[cfg(test)]
mod tests {
    #[test]
    fn native_case_rules_preserve_one_scalar_and_each_malformed_octet() {
        assert_eq!(super::lower("KKİẞ".as_bytes()), "kkiß".as_bytes());
        assert_eq!(super::lower(&[b'A', 255, 128, b'Z']), "a��z".as_bytes());
    }
}
