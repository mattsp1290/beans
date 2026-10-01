//! Plan IDs have a literal project prefix and no imported dotted suffix.
pub const DEFAULT_ID_LENGTH: usize = 4;

pub fn valid_id(prefix: &str, id: &str) -> bool {
    id.strip_prefix(&format!("{prefix}-plan-"))
        .is_some_and(|hash| {
            !hash.is_empty()
                && hash
                    .bytes()
                    .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit())
        })
}
pub fn valid_slug(value: &str) -> bool {
    !value.is_empty()
        && value.split('-').all(|part| {
            !part.is_empty()
                && part
                    .bytes()
                    .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit())
        })
}
pub fn directory_name(id: &str, slug: &str) -> String {
    format!("{id}-{slug}")
}
pub fn new_id(prefix: &str, exists: Option<&mut dyn FnMut(&str) -> bool>, length: isize) -> String {
    crate::domain::id::new_id(&format!("{prefix}-plan"), exists, length)
}
pub fn slug(title: &str) -> String {
    let normalized = crate::domain::id::normalized_slug(title);
    let value = normalized[..normalized.len().min(60)].trim_matches('-');
    if value.is_empty() {
        "plan".to_owned()
    } else {
        value.to_owned()
    }
}
