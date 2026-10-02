//! Byte-oriented output matching the existing BurntSushi configuration writer.
//! Authored fields can contain invalid UTF-8, so output remains bytes.
use super::{ProjectConfig, UserConfig};
use crate::domain::{workflow::States, yaml_string::YamlString};

fn quoted(out: &mut Vec<u8>, value: &[u8]) {
    const HEX: &[u8] = b"0123456789abcdef";
    out.push(b'"');
    for &byte in value {
        match byte {
            b'"' => out.extend_from_slice(b"\\\""),
            b'\\' => out.extend_from_slice(b"\\\\"),
            8 => out.extend_from_slice(b"\\b"),
            9 => out.extend_from_slice(b"\\t"),
            10 => out.extend_from_slice(b"\\n"),
            12 => out.extend_from_slice(b"\\f"),
            13 => out.extend_from_slice(b"\\r"),
            0..=31 | 127 => {
                out.extend_from_slice(b"\\u00");
                out.push(HEX[usize::from(byte >> 4)]);
                out.push(HEX[usize::from(byte & 15)]);
            }
            _ => out.push(byte),
        }
    }
    out.push(b'"');
}

pub(super) fn key(out: &mut Vec<u8>, name: &[u8]) {
    if !name.is_empty()
        && name
            .iter()
            .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'_' | b'-'))
    {
        out.extend_from_slice(name);
    } else {
        quoted(out, name);
    }
}

fn scalar(out: &mut Vec<u8>, prefix: &[u8], value: &YamlString) {
    out.extend_from_slice(prefix);
    quoted(out, value.as_bytes());
    out.push(b'\n');
}

fn array(out: &mut Vec<u8>, values: &[YamlString]) {
    out.push(b'[');
    for (i, value) in values.iter().enumerate() {
        if i != 0 {
            out.extend_from_slice(b", ");
        }
        quoted(out, value.as_bytes());
    }
    out.extend_from_slice(b"]\n");
}

fn optional_array(out: &mut Vec<u8>, prefix: &[u8], values: &States) {
    if let Some(values) = values {
        out.extend_from_slice(prefix);
        array(out, values);
    }
}

pub fn encode_user_config(config: &UserConfig) -> Vec<u8> {
    let mut out = Vec::new();
    scalar(&mut out, b"actor = ", &config.actor);
    out.extend_from_slice(b"\n[hub]\n");
    scalar(&mut out, b"  remote = ", &config.hub.remote);
    scalar(&mut out, b"  branch = ", &config.hub.branch);
    out.extend_from_slice(b"\n[fetch]\n");
    scalar(&mut out, b"  throttle = ", &config.fetch.throttle);
    out
}

pub fn encode_project_config(config: &ProjectConfig) -> Vec<u8> {
    let mut out = Vec::new();
    scalar(&mut out, b"name = ", &config.name);
    scalar(&mut out, b"prefix = ", &config.prefix);
    out.extend_from_slice(b"remotes = ");
    array(&mut out, config.remotes.as_deref().unwrap_or_default());
    let workflow = &config.workflow;
    if !workflow.is_empty() {
        out.extend_from_slice(b"\n[workflow]\n");
        optional_array(&mut out, b"  statuses = ", &workflow.statuses);
        scalar(&mut out, b"  default = ", &workflow.default);
        optional_array(&mut out, b"  active = ", &workflow.active);
        optional_array(&mut out, b"  terminal = ", &workflow.terminal);
        if let Some(transitions) = &workflow.transitions {
            out.extend_from_slice(b"  [workflow.transitions]\n");
            for (from, to) in transitions {
                if let Some(to) = to {
                    out.extend_from_slice(b"    ");
                    key(&mut out, from.as_bytes());
                    out.extend_from_slice(b" = ");
                    array(&mut out, to);
                }
            }
        }
    }
    out
}
