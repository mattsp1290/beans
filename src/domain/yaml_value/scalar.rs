use super::YamlValue;
use crate::domain::{frontmatter::Node, issue::Timestamp, yaml_decode::decoded_string};
pub(super) fn decode(node: &Node) -> Result<YamlValue, ()> {
    let string = decoded_string(node).map_err(|_| ())?;
    let value = node.value.as_deref().unwrap_or_default();
    Ok(match node.tag.as_str() {
        "!!null" => YamlValue::Null,
        "!!bool" => YamlValue::Bool(value.eq_ignore_ascii_case("true")),
        "!!int" => integer(value).ok_or(())?,
        "!!float" => {
            let number = if let Some(integer) = integer(value) {
                match integer {
                    YamlValue::Int(i) => i as f64,
                    _ => return Err(()),
                }
            } else {
                match value.to_ascii_lowercase().as_str() {
                    ".nan" => f64::from_bits(0x7ff8_0000_0000_0001),
                    ".inf" | "+.inf" => f64::INFINITY,
                    "-.inf" => f64::NEG_INFINITY,
                    _ => value.replace('_', "").parse().map_err(|_| ())?,
                }
            };
            YamlValue::Float(number)
        }
        "!!timestamp" => YamlValue::Timestamp(timestamp(value).ok_or(())?),
        _ => YamlValue::String(string.as_bytes().into()),
    })
}
fn integer(value: &str) -> Option<YamlValue> {
    let number = value.replace('_', "");
    let unsigned = number.strip_prefix(['+', '-']).unwrap_or(&number);
    let (radix, digits) = if let Some(d) = unsigned
        .strip_prefix("0x")
        .or_else(|| unsigned.strip_prefix("0X"))
    {
        (16, d)
    } else if let Some(d) = unsigned
        .strip_prefix("0b")
        .or_else(|| unsigned.strip_prefix("0B"))
    {
        (2, d)
    } else if let Some(d) = unsigned
        .strip_prefix("0o")
        .or_else(|| unsigned.strip_prefix("0O"))
    {
        (8, d)
    } else if unsigned.len() > 1 && unsigned.starts_with('0') {
        (8, unsigned)
    } else {
        (10, unsigned)
    };
    let integer = u64::from_str_radix(digits, radix).ok()?;
    if number.starts_with('-') {
        if integer == 1 << 63 {
            Some(YamlValue::Int(i64::MIN))
        } else {
            Some(YamlValue::Int(-i64::try_from(integer).ok()?))
        }
    } else if let Ok(integer) = i64::try_from(integer) {
        Some(YamlValue::Int(integer))
    } else {
        Some(YamlValue::Uint(integer))
    }
}
fn timestamp(value: &str) -> Option<Timestamp> {
    let (date, clock) = value
        .split_once(['T', 't', ' '])
        .unwrap_or((value, "00:00:00Z"));
    let parts: Vec<_> = date.split('-').collect();
    if parts.len() != 3 {
        return None;
    }
    let month: u8 = parts[1].parse().ok()?;
    let day: u8 = parts[2].parse().ok()?;
    let clock = if value.contains(' ') {
        format!("{clock}Z")
    } else {
        clock.to_owned()
    };
    let (hour, rest) = clock.split_once(':')?;
    let (minute, second) = rest.split_once(':')?;
    let hour: u8 = hour.parse().ok()?;
    let minute: u8 = minute.parse().ok()?;
    let second = if second.as_bytes().get(1).is_some_and(u8::is_ascii_digit) {
        second.to_owned()
    } else {
        format!("0{second}")
    };
    crate::domain::issue::parse_timestamp(&format!(
        "{}-{month:02}-{day:02}T{hour:02}:{minute:02}:{second}",
        parts[0]
    ))
}
