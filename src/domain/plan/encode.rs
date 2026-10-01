use super::{Plan, valid_status};
use crate::domain::{frontmatter::Error, issue::Timestamp, yaml_render::scalar_value_indented};
use time::OffsetDateTime;

fn scalar(value: &str, sequence: bool) -> String {
    scalar_value_indented(value, false, false, true, if sequence { 2 } else { 4 }, 4)
}
fn pair(output: &mut String, key: &str, value: &str) {
    output.push_str(key);
    output.push_str(": ");
    output.push_str(&scalar(value, false));
    if !output.ends_with('\n') {
        output.push('\n');
    }
}
fn sequence(output: &mut String, key: &str, values: &[String]) {
    output.push_str(key);
    if values.is_empty() {
        output.push_str(": []\n");
        return;
    }
    output.push_str(":\n");
    for value in values {
        output.push_str("    - ");
        let rendered = scalar(value, true);
        for (i, line) in rendered
            .split_inclusive(['\n', '\u{85}', '\u{2028}', '\u{2029}'])
            .enumerate()
        {
            if i > 0
                && !line
                    .chars()
                    .all(|c| matches!(c, '\n' | '\u{85}' | '\u{2028}' | '\u{2029}'))
            {
                output.push_str("    ");
            }
            output.push_str(line);
        }
        if !output.ends_with('\n') {
            output.push('\n');
        }
    }
}
fn timestamp(value: &Timestamp) -> Result<String, Error> {
    let at = OffsetDateTime::from_unix_timestamp(value.seconds)
        .map_err(|_| Error("plan timestamp is out of range".into()))?;
    let year = if at.year() < 0 {
        format!("-{:04}", -at.year())
    } else {
        format!("{:04}", at.year())
    };
    Ok(format!(
        "{year}-{:02}-{:02}T{:02}:{:02}:{:02}Z",
        at.month() as u8,
        at.day(),
        at.hour(),
        at.minute(),
        at.second()
    ))
}

/// Go's plan writer emits canonical owned frontmatter and retains the body.
/// It validates status only; manifest and lifecycle validation are separate.
pub fn encode(plan: Option<&Plan>) -> Result<Vec<u8>, Error> {
    let p = plan.ok_or_else(|| Error("nil plan".into()))?;
    if !valid_status(&p.status) {
        return Err(Error("invalid status".into()));
    }
    let mut output = String::from("---\n");
    pair(&mut output, "id", &p.id);
    sequence(&mut output, "aliases", &p.aliases);
    for (key, value) in [
        ("title", p.title.as_str()),
        ("slug", &p.slug),
        ("status", &p.status),
    ] {
        pair(&mut output, key, value);
    }
    pair(&mut output, "created", &timestamp(&p.created)?);
    pair(&mut output, "updated", &timestamp(&p.updated)?);
    if !p.sections.is_empty() {
        sequence(&mut output, "sections", &p.sections);
    }
    output.push_str("---\n");
    output.push_str(&p.body);
    if !p.body.ends_with('\n') {
        output.push('\n');
    }
    Ok(output.into_bytes())
}
