//! Presentation rules for newly rendered owned string nodes. Existing YAML
//! is always copied; this is not a serializer for user-owned mappings.
use time::{Date, Month};

pub(crate) fn string_pair(key: &str, value: &str, comment: &str) -> String {
    let rendered = scalar(value, false, false);
    if rendered.starts_with('|') {
        let (header, body) = rendered.split_once('\n').unwrap();
        let extra = if !comment.is_empty() && value.chars().all(|ch| ch == '\n') {
            "\n"
        } else {
            ""
        };
        format!("{key}: {header}{}\n{body}{extra}", inline_comment(comment))
    } else {
        format!("{key}: {rendered}{}\n", inline_comment(comment))
    }
}

pub(crate) fn flow_pair(key: &str, values: &[String]) -> String {
    format!(
        "{key}: [{}]\n",
        values
            .iter()
            .map(|value| scalar(value, true, false))
            .collect::<Vec<_>>()
            .join(", ")
    )
}

pub(crate) fn link_pair(key: &str, raw: &str, comment: &str) -> String {
    format!("{key}: {}{}\n", double_quoted(raw), inline_comment(comment))
}

fn inline_comment(comment: &str) -> String {
    if comment.is_empty() {
        String::new()
    } else {
        format!(" {comment}")
    }
}

fn printable(ch: char) -> bool {
    ch == '\n'
        || (' '..='~').contains(&ch)
        || ('\u{a0}'..='\u{fffd}').contains(&ch) && ch != '\u{feff}'
}

fn break_char(ch: char) -> bool {
    matches!(ch, '\n' | '\r' | '\u{85}' | '\u{2028}' | '\u{2029}')
}

fn double_quoted(value: &str) -> String {
    let mut output = String::from("\"");
    for ch in value.chars() {
        let escape = match ch {
            '\0' => Some("0"),
            '\x07' => Some("a"),
            '\x08' => Some("b"),
            '\t' => Some("t"),
            '\n' => Some("n"),
            '\x0b' => Some("v"),
            '\x0c' => Some("f"),
            '\r' => Some("r"),
            '\x1b' => Some("e"),
            '"' => Some("\""),
            '\\' => Some("\\"),
            '\u{85}' => Some("N"),
            '\u{2028}' => Some("L"),
            '\u{2029}' => Some("P"),
            _ => None,
        };
        if let Some(escape) = escape {
            output.push('\\');
            output.push_str(escape);
        } else if !printable(ch) {
            output.push_str(&if ch as u32 <= 0xff {
                format!("\\x{:02X}", ch as u32)
            } else if ch as u32 <= 0xffff {
                format!("\\u{:04X}", ch as u32)
            } else {
                format!("\\U{:08X}", ch as u32)
            });
        } else {
            output.push(ch);
        }
    }
    output.push('"');
    output
}

fn scalar(value: &str, flow: bool, force_double: bool) -> String {
    let chars: Vec<_> = value.chars().collect();
    let special = chars.iter().any(|&ch| ch != '\t' && !printable(ch));
    let space_break = chars
        .windows(2)
        .any(|pair| pair[0] == ' ' && break_char(pair[1]));
    let block_allowed = !special && !space_break && !value.ends_with(' ');
    if !flow && value.contains('\n') && block_allowed && !force_double {
        let chomp = if !value.ends_with('\n') {
            "-"
        } else if value == "\n" || value.ends_with("\n\n") {
            "+"
        } else {
            ""
        };
        let indent = if chars.first().is_some_and(|&ch| ch == ' ' || break_char(ch)) {
            "2"
        } else {
            ""
        };
        let mut output = format!("|{indent}{chomp}\n");
        let content = if value.chars().all(|ch| ch == '\n') {
            &value[1..]
        } else {
            value
        };
        for line in content.split_inclusive('\n') {
            if line != "\n" {
                output.push_str("  ");
            }
            output.push_str(line);
        }
        if !output.ends_with('\n') {
            output.push('\n');
        }
        return output;
    }
    if force_double
        || resolved_non_string(value)
        || special
        || value.contains(['\t', '\n'])
        || space_break
        || chars
            .windows(2)
            .any(|pair| break_char(pair[0]) && pair[1] == ' ')
    {
        return double_quoted(value);
    }
    let mut indicator = value.starts_with("---") || value.starts_with("...");
    for (index, &ch) in chars.iter().enumerate() {
        let followed_blank = chars
            .get(index + 1)
            .is_none_or(|ch| matches!(ch, ' ' | '\t'));
        if index == 0 {
            indicator |= matches!(
                ch,
                '#' | ','
                    | '['
                    | ']'
                    | '{'
                    | '}'
                    | '&'
                    | '*'
                    | '!'
                    | '|'
                    | '>'
                    | '\''
                    | '"'
                    | '%'
                    | '@'
                    | '`'
            ) || matches!(ch, '?' | ':') && (flow || followed_blank)
                || ch == '-' && followed_blank;
        } else {
            indicator |= flow && matches!(ch, ',' | '?' | '[' | ']' | '{' | '}' | ':')
                || ch == ':' && followed_blank
                || ch == '#' && matches!(chars[index - 1], ' ' | '\t');
        }
    }
    indicator |= chars.iter().any(|&ch| break_char(ch));
    indicator |= value.starts_with(' ') || value.ends_with(' ');
    if indicator {
        let mut output = String::from("'");
        let mut breaks = false;
        for ch in value.chars() {
            if break_char(ch) {
                output.push(ch);
                breaks = true;
            } else {
                if breaks {
                    output.push_str(if flow { "    " } else { "  " });
                }
                if ch == '\'' {
                    output.push('\'');
                }
                output.push(ch);
                breaks = false;
            }
        }
        output.push('\'');
        output
    } else {
        value.to_owned()
    }
}

fn resolved_non_string(value: &str) -> bool {
    if matches!(
        value,
        "" | "~"
            | "null"
            | "Null"
            | "NULL"
            | "true"
            | "True"
            | "TRUE"
            | "false"
            | "False"
            | "FALSE"
            | ".nan"
            | ".NaN"
            | ".NAN"
            | ".inf"
            | ".Inf"
            | ".INF"
            | "+.inf"
            | "+.Inf"
            | "+.INF"
            | "-.inf"
            | "-.Inf"
            | "-.INF"
    ) {
        return true;
    }
    let Some(first) = value.chars().next() else {
        return true;
    };
    if !(first.is_ascii_digit() || matches!(first, '.' | '+' | '-')) {
        return false;
    }
    if yaml_date(value) {
        return true;
    }
    if first == '.' {
        let bytes = value.as_bytes();
        if bytes.iter().enumerate().any(|(index, &byte)| {
            byte == b'_'
                && (index == 0
                    || !bytes[index - 1].is_ascii_digit()
                    || !bytes.get(index + 1).is_some_and(u8::is_ascii_digit))
        }) {
            return false;
        }
    }
    let number = value.replace('_', "");
    let unsigned = number.strip_prefix(['+', '-']).unwrap_or(&number);
    let (radix, digits) = if let Some(digits) = unsigned
        .strip_prefix("0x")
        .or_else(|| unsigned.strip_prefix("0X"))
    {
        (16, digits)
    } else if let Some(digits) = unsigned
        .strip_prefix("0b")
        .or_else(|| unsigned.strip_prefix("0B"))
    {
        (2, digits)
    } else if let Some(digits) = unsigned
        .strip_prefix("0o")
        .or_else(|| unsigned.strip_prefix("0O"))
    {
        (8, digits)
    } else if unsigned.len() > 1 && unsigned.starts_with('0') {
        (8, unsigned)
    } else {
        (10, unsigned)
    };
    if let Ok(integer) = u64::from_str_radix(digits, radix)
        && (!number.starts_with('-') || integer <= i64::MAX as u64 + 1)
    {
        return true;
    }
    // Go's numeric resolver only accepts decimal float syntax here.
    if !number
        .chars()
        .all(|ch| ch.is_ascii_digit() || matches!(ch, '+' | '-' | '.' | 'e' | 'E'))
    {
        return false;
    }
    number.parse::<f64>().is_ok_and(f64::is_finite)
}

fn yaml_date(value: &str) -> bool {
    let date = value.split(['T', 't', ' ']).next().unwrap_or(value);
    let parts: Vec<_> = date.split('-').collect();
    if parts.len() != 3
        || parts[0].len() != 4
        || !(1..=2).contains(&parts[1].len())
        || !(1..=2).contains(&parts[2].len())
    {
        return false;
    }
    let (Ok(year), Ok(month), Ok(day)) = (
        parts[0].parse::<i32>(),
        parts[1].parse::<u8>(),
        parts[2].parse::<u8>(),
    ) else {
        return false;
    };
    let Ok(month) = Month::try_from(month) else {
        return false;
    };
    if Date::from_calendar_date(year, month, day).is_err() {
        return false;
    }
    if date == value {
        return true;
    }
    // Normalize short date fields for the shared RFC3339 parser.
    let clock = &value[date.len() + 1..];
    let separator = value.as_bytes()[date.len()];
    let clock = if separator == b' ' {
        format!("{clock}Z")
    } else {
        clock.to_owned()
    };
    let clock_parts: Vec<_> = clock.split(':').collect();
    if clock_parts.len() < 3 {
        return false;
    }
    let (Ok(hour), Ok(minute)) = (clock_parts[0].parse::<u8>(), clock_parts[1].parse::<u8>())
    else {
        return false;
    };
    let seconds = clock_parts[2..].join(":");
    let seconds = if seconds.as_bytes().get(1).is_some_and(u8::is_ascii_digit) {
        seconds
    } else {
        format!("0{seconds}")
    };
    super::issue::parse_timestamp(&format!(
        "{year:04}-{:02}-{day:02}T{hour:02}:{minute:02}:{seconds}",
        month as u8
    ))
    .is_some()
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn owned_string_presentations_match_go() {
        let corpus: serde_json::Value = serde_json::from_str(include_str!(
            "../../tests/contract/frontmatter-primitives.json"
        ))
        .unwrap();
        for case in corpus["scalar_pairs"].as_array().unwrap() {
            let input = case["input"].as_str().unwrap();
            assert_eq!(
                string_pair("title", input, ""),
                case["pair"].as_str().unwrap(),
                "scalar {input:?}"
            );
            assert_eq!(
                string_pair("title", input, "# retained"),
                case["commented"].as_str().unwrap(),
                "comment {input:?}"
            );
            assert_eq!(
                flow_pair("labels", &[input.to_owned(), "other".to_owned()]),
                case["flow"].as_str().unwrap(),
                "flow {input:?}"
            );
            let raw = if input.is_empty() { "[[]]" } else { input };
            assert_eq!(
                link_pair("parent", raw, ""),
                case["link"].as_str().unwrap(),
                "link {input:?}"
            );
        }
    }
}
