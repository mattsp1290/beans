use super::{Plan, YamlString, valid_status};
use crate::domain::{frontmatter::Error, issue::Timestamp, yaml_render::scalar_value_indented};

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
fn sequence(output: &mut String, key: &str, values: &[YamlString]) -> Result<(), Error> {
    output.push_str(key);
    if values.is_empty() {
        output.push_str(": []\n");
        return Ok(());
    }
    output.push_str(":\n");
    for value in values {
        output.push_str("    - ");
        let value = value
            .as_str()
            .ok_or_else(|| Error::new("yaml: cannot marshal invalid UTF-8 data as !!str".into()))?;
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
    Ok(())
}
fn timestamp(value: &Timestamp) -> String {
    // Use a March-based absolute epoch and wrapping uint64 seconds,
    // including time.Unix values outside ordinary date-library ranges.
    const ABSOLUTE_YEARS: u64 = 292_277_022_400;
    const UNIX_TO_ABSOLUTE: u64 = (ABSOLUTE_YEARS * 146_097 / 400 + 306 + 719_162) * 86_400;
    let absolute = (value.seconds as u64).wrapping_add(UNIX_TO_ABSOLUTE);
    let cycle = 4 * (absolute / 86_400) + 3;
    let century = cycle / 146_097;
    let century_day = (cycle % 146_097) | 3;
    let year_in_century = century_day / 1_461;
    let year_day = century_day % 1_461 / 4;
    let jan_feb = u64::from(year_day >= 306);
    let month_day = 5 * year_day + 461;
    let month = month_day / 153 - 12 * jan_feb;
    let day = month_day % 153 / 5 + 1;
    let year =
        (century * 100) as i64 - ABSOLUTE_YEARS as i64 + year_in_century as i64 + jan_feb as i64;
    let year = if year < 0 {
        format!("-{:04}", -year)
    } else {
        format!("{year:04}")
    };
    let clock = absolute % 86_400;
    format!(
        "{year}-{month:02}-{day:02}T{:02}:{:02}:{:02}Z",
        clock / 3_600,
        clock % 3_600 / 60,
        clock % 60
    )
}

/// The plan writer emits canonical owned frontmatter and retains the body.
/// It validates status only; manifest and lifecycle validation are separate.
pub fn encode(plan: Option<&Plan>) -> Result<Vec<u8>, Error> {
    let p = plan.ok_or_else(|| Error::new("nil plan".into()))?;
    if !valid_status(&p.status) {
        return Err(Error::new("invalid status".into()));
    }
    let mut output = String::from("---\n");
    pair(&mut output, "id", &p.id);
    sequence(&mut output, "aliases", &p.aliases)?;
    for (key, value) in [
        ("title", p.title.as_str()),
        ("slug", &p.slug),
        ("status", &p.status),
    ] {
        pair(&mut output, key, value);
    }
    pair(&mut output, "created", &timestamp(&p.created));
    pair(&mut output, "updated", &timestamp(&p.updated));
    if !p.sections.is_empty() {
        sequence(&mut output, "sections", &p.sections)?;
    }
    output.push_str("---\n");
    output.push_str(&p.body);
    if !p.body.ends_with('\n') {
        output.push('\n');
    }
    Ok(output.into_bytes())
}

/// Edit revision/body while retaining unrelated manifest spelling and comments.
pub fn revise_manifest(source: &[u8], plan: &Plan) -> Result<Vec<u8>, Error> {
    let document = crate::domain::frontmatter::Frontmatter::parse_bytes("plan.md", source)?;
    let comment = document
        .fields()
        .iter()
        .find(|f| f.key == "updated")
        .map(|f| document.scalar_comment(f))
        .unwrap_or_default();
    let field = format!("updated: {}{}\n", timestamp(&plan.updated), comment);
    Ok(document
        .splice_owned(
            &["updated"],
            &[("updated", Some(field.as_bytes()))],
            plan.body.as_bytes(),
        )?
        .bytes)
}
