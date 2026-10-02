use super::{BLOCKED, COMPLETE, DRAFT, Plan, READY};
use crate::domain::frontmatter::Error;

pub fn valid_status(value: &str) -> bool {
    matches!(value, DRAFT | BLOCKED | READY | COMPLETE)
}

pub fn validate(plan: Option<&Plan>) -> Result<(), Error> {
    let Some(plan) = plan else {
        return Err(Error::new("nil plan".into()));
    };
    if !valid_status(&plan.status) {
        return Err(Error::new("invalid plan status".into()));
    }
    if plan.status == DRAFT {
        return Ok(());
    }
    if !meaningful(&plan.summary.outcome) {
        return Err(Error::new(
            "Summary Outcome must contain meaningful text".into(),
        ));
    }
    let risks = list_items(&plan.summary.risks);
    if plan.status == BLOCKED {
        if risks.iter().any(|risk| risk.trim().starts_with("BLOCKER:")) {
            return Ok(());
        }
        return Err(Error::new(
            "blocked plans require a BLOCKER: Risks item".into(),
        ));
    }
    if [
        &plan.summary.outcome,
        &plan.summary.affected_areas,
        &plan.summary.execution_order,
        &plan.summary.risks,
    ]
    .iter()
    .any(|value| value.contains("<!-- bn:todo -->"))
    {
        return Err(Error::new(
            "ready plans cannot contain bn:todo markers".into(),
        ));
    }
    if list_items(&plan.summary.affected_areas).is_empty() {
        return Err(Error::new("ready plans require Affected areas list".into()));
    }
    if !ordered_items(&plan.summary.execution_order) {
        return Err(Error::new(
            "ready plans require ordered Execution order".into(),
        ));
    }
    if risks.is_empty() {
        return Err(Error::new("ready plans require Risks list".into()));
    }
    if plan.graph.nodes.as_ref().is_none_or(Vec::is_empty) {
        return Err(Error::new("ready plans require a graph node".into()));
    }
    Ok(())
}

fn meaningful(value: &str) -> bool {
    let value = value.replace("<!-- bn:todo -->", "");
    let mut rest = value.as_str();
    let mut output = String::new();
    while let Some(start) = rest.find("<!--") {
        let Some(end) = rest[start + 4..].find("-->") else {
            break;
        };
        output.push_str(&rest[..start]);
        rest = &rest[start + 4 + end + 3..];
    }
    output.push_str(rest);
    !output.trim().is_empty()
}
fn list_items(value: &str) -> Vec<&str> {
    value
        .split('\n')
        .map(str::trim)
        .filter_map(|line| line.strip_prefix("- ").or_else(|| line.strip_prefix("* ")))
        .collect()
}
fn regex_space(byte: u8) -> bool {
    matches!(byte, b' ' | b'\t' | b'\n' | b'\r' | 0x0c)
}
fn ordered_items(value: &str) -> bool {
    // Go's (?m)^\s*\d+\.\s+\S uses ASCII Perl classes. The whitespace
    // may cross line boundaries; Unicode spaces and vertical tab are not \s.
    let bytes = value.as_bytes();
    let mut start = 0;
    while start < bytes.len() {
        let mut cursor = start;
        while bytes.get(cursor).is_some_and(|&b| regex_space(b)) {
            cursor += 1;
        }
        let digits = cursor;
        while bytes.get(cursor).is_some_and(u8::is_ascii_digit) {
            cursor += 1;
        }
        if digits < cursor && bytes.get(cursor) == Some(&b'.') {
            cursor += 1;
            let spaces = cursor;
            while bytes.get(cursor).is_some_and(|&b| regex_space(b)) {
                cursor += 1;
            }
            if spaces < cursor && bytes.get(cursor).is_some_and(|&b| !regex_space(b)) {
                return true;
            }
        }
        // Leading ASCII whitespace can cross several lines. All starts inside
        // that span reach the same candidate, so do not rescan it on failure.
        let Some(next_line) = bytes[cursor..].iter().position(|&b| b == b'\n') else {
            break;
        };
        start = cursor + next_line + 1;
    }
    false
}
