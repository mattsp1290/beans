use super::{DRAFT, Plan, encode, id::slug};
use crate::domain::{frontmatter::Error, issue::Timestamp};
const TEMPLATE: &str = include_str!("templates/plan.md");
/// Construct the same initial draft manifest as Go's embedded template.
pub fn scaffold(id: &str, title: &str, now: &Timestamp) -> Result<Vec<u8>, Error> {
    let body = TEMPLATE[4..]
        .find("\n---\n")
        .map_or(TEMPLATE, |end| &TEMPLATE[end + 9..]);
    encode(Some(&Plan {
        id: id.into(),
        aliases: vec![id.into()],
        title: title.into(),
        slug: slug(title),
        status: DRAFT.into(),
        created: now.clone(),
        updated: now.clone(),
        body: body.into(),
        ..Plan::default()
    }))
}
