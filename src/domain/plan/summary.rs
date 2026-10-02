use super::{ChangeGraph, Summary, parse_graph_bytes};
use crate::domain::{frontmatter::Error, issue::quoted};

pub fn parse_summary(path: &str, body: &str) -> Result<(Summary, ChangeGraph), Error> {
    parse_summary_bytes(path.as_bytes(), body)
}
pub fn parse_summary_bytes(path: &[u8], body: &str) -> Result<(Summary, ChangeGraph), Error> {
    let fail = |message: &str| Error::new(message.into()).context(path);
    let lines: Vec<_> = body.split('\n').collect();
    let mut summary = None;
    let mut in_fence = false;
    for (i, line) in lines.iter().enumerate() {
        if line.trim().starts_with("```") {
            in_fence = !in_fence;
            continue;
        }
        if !in_fence && line.trim() == "## Summary" && summary.replace(i).is_some() {
            return Err(fail("duplicate Summary"));
        }
    }
    let start = summary.ok_or_else(|| fail("missing Summary"))?;
    let mut end = lines.len();
    in_fence = false;
    for (i, line) in lines.iter().enumerate().skip(start + 1) {
        if line.trim().starts_with("```") {
            in_fence = !in_fence;
            continue;
        }
        if !in_fence && line.starts_with("## ") {
            end = i;
            break;
        }
    }
    let names = [
        "Outcome",
        "Affected areas",
        "Execution order",
        "Risks",
        "Change graph",
    ];
    let mut starts = [None; 5];
    in_fence = false;
    for (i, line) in lines.iter().enumerate().take(end).skip(start + 1) {
        if line.trim().starts_with("```") {
            in_fence = !in_fence;
            continue;
        }
        if !in_fence && let Some(name) = line.strip_prefix("### ") {
            let name = name.trim();
            if let Some(index) = names.iter().position(|&n| n == name)
                && starts[index].replace(i).is_some()
            {
                return Err(fail(&format!(
                    "duplicate Summary subsection {}",
                    quoted(name)
                )));
            }
        }
    }
    for i in 0..5 {
        let Some(current) = starts[i] else {
            // A missing section index is -1; the diagnostic quotes that index.
            return Err(fail("missing Summary subsection '�'"));
        };
        if i > 0 && current < starts[i - 1].unwrap() {
            return Err(fail("Summary subsections out of order"));
        }
    }
    let values: Vec<_> = (0..5)
        .map(|i| {
            lines[starts[i].unwrap() + 1..if i < 4 { starts[i + 1].unwrap() } else { end }]
                .join("\n")
                .trim()
                .to_owned()
        })
        .collect();
    let graph = parse_graph_bytes(path, &values[4]).map_err(|error| error.diagnostic())?;
    Ok((
        Summary {
            outcome: values[0].clone(),
            affected_areas: values[1].clone(),
            execution_order: values[2].clone(),
            risks: values[3].clone(),
            change_graph: values[4].clone(),
        },
        graph,
    ))
}
