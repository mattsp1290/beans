use super::{HubConfig, ProjectConfig, TypesConfig, UserConfig};
use crate::domain::{
    file_io::{path_error, path_name},
    frontmatter::Error,
    workflow::{States, WorkflowFile},
    yaml_string::YamlString,
};
use serde::Deserialize;
use std::{collections::BTreeMap, fs::File, io::Read, path::Path};
use toml::Spanned;
#[path = "toml_error.rs"]
mod syntax;
#[path = "toml_tree.rs"]
mod tree;
use tree::{Node, line};

pub(crate) fn equal_field(actual: &str, expected: &str) -> bool {
    actual
        .chars()
        .map(|c| match c {
            'ſ' => 's',
            'K' => 'k',
            _ => c.to_ascii_lowercase(),
        })
        .eq(expected.chars())
}
struct Context<'a> {
    source: &'a str,
    key: String,
    line: usize,
}
impl<'a> Context<'a> {
    fn child(&self, key: &str, value: &Spanned<Node>) -> Self {
        let mut quoted_key = Vec::new();
        super::encode::key(&mut quoted_key, key.as_bytes());
        let key = String::from_utf8(quoted_key).expect("TOML keys are UTF-8");
        let line = if key == "\"\"" {
            0
        } else {
            line(self.source, value.span().start)
        };
        Self {
            source: self.source,
            key: if self.key.is_empty() {
                key
            } else {
                format!("{}.{}", self.key, key)
            },
            line,
        }
    }
    fn error(&self, message: String) -> Error {
        let context = YamlString::from(self.key.clone());
        let line = if self.line == 0 {
            String::new()
        } else {
            format!("line {} ", self.line)
        };
        Error::new(format!(
            "toml: {line}(last key {}): {message}",
            context.quoted()
        ))
    }
    fn bad_type(&self, value: &Spanned<Node>, destination: &str) -> Error {
        self.error(format!(
            "incompatible types: TOML value has type {}; destination has type {destination}",
            value.get_ref().go_type(self.source, value.span())
        ))
    }
    fn string(&self, value: &Spanned<Node>) -> Result<YamlString, Error> {
        if let Node::String(s) = value.get_ref() {
            Ok(s.clone().into())
        } else {
            Err(self.bad_type(value, "string"))
        }
    }
    fn list(&self, value: &Spanned<Node>) -> Result<States, Error> {
        if let Node::Array(values) = value.get_ref() {
            // Go retains the array field's line/key even for bad later items.
            values
                .iter()
                .map(|v| self.string(v))
                .collect::<Result<Vec<_>, _>>()
                .map(Some)
        } else {
            Err(self.bad_type(value, "slice"))
        }
    }
    fn table<'v>(
        &self,
        value: &'v Spanned<Node>,
        name: &str,
    ) -> Result<&'v [(String, Spanned<Node>)], Error> {
        if let Node::Table(values) = value.get_ref()
            && value.get_ref().go_type(self.source, value.span()) == "map[string]any"
        {
            return Ok(values);
        }
        Err(self.error(format!(
            "type mismatch for issue.{name}: expected table but found {}",
            value.get_ref().go_type(self.source, value.span())
        )))
    }
    fn workflow(&self, value: &Spanned<Node>, cfg: &mut WorkflowFile) -> Result<(), Error> {
        for (key, value) in self.table(value, "WorkflowFile")? {
            let context = self.child(key, value);
            if equal_field(key, "statuses") {
                cfg.statuses = context.list(value)?;
            } else if equal_field(key, "default") {
                cfg.default = context.string(value)?;
            } else if equal_field(key, "active") {
                cfg.active = context.list(value)?;
            } else if equal_field(key, "terminal") {
                cfg.terminal = context.list(value)?;
            } else if equal_field(key, "transitions") {
                // Go's map unifier silently ignores non-table values.
                if let Node::Table(values) = value.get_ref()
                    && value.get_ref().go_type(self.source, value.span()) == "map[string]any"
                {
                    let mut transitions = BTreeMap::new();
                    for (from, to) in values {
                        transitions.insert(from.clone().into(), context.child(from, to).list(to)?);
                    }
                    cfg.transitions = Some(transitions);
                }
            }
        }
        Ok(())
    }
}

fn parse_mode(data: &[u8], full: bool) -> Result<(String, Spanned<Node>), Error> {
    let source = std::str::from_utf8(data).map_err(|e| {
        let offset = e.valid_up_to();
        let line = 1 + data[..offset].iter().filter(|&&b| b == b'\n').count();
        let error = Error::new(format!(
            "line {line}: invalid UTF-8 byte: 0x{:02x}",
            data[offset]
        ));
        if full {
            super::toml_metadata::full_error(
                std::str::from_utf8(&data[..offset]).unwrap(),
                offset,
                error,
            )
        } else {
            error
        }
    })?;
    let adapt = |e: toml::de::Error| {
        let offset = e.span().map(|s| s.start).unwrap_or(source.len());
        let error = syntax_error(source, e);
        if full {
            super::toml_metadata::full_error(source, offset, error)
        } else {
            error
        }
    };
    let parser = toml::de::Deserializer::parse(source).map_err(&adapt)?;
    let root = Spanned::<Node>::deserialize(parser).map_err(adapt)?;
    Ok((source.into(), root))
}
fn parse(data: &[u8]) -> Result<(String, Spanned<Node>), Error> {
    parse_mode(data, false)
}
pub(crate) fn decode_workflow_toml(data: &[u8]) -> Result<WorkflowFile, Error> {
    let (source, node) = parse_mode(data, true)?;
    let base = root(&source);
    let mut cfg = WorkflowFile::default();
    for (key, value) in base.table(&node, "workflowFile")? {
        if equal_field(key, "workflow") {
            base.child(key, value).workflow(value, &mut cfg)?;
        }
    }
    let unknown: Vec<_> = super::toml_metadata::keys(&source)
        .into_iter()
        .filter(|entry| {
            let path = &entry.path;
            path.first().is_some_and(|k| k == "workflow")
                && path.len() > 1
                && !["statuses", "default", "active", "terminal", "transitions"]
                    .iter()
                    .any(|key| equal_field(&path[1], key))
        })
        .map(|entry| super::toml_metadata::format_key(&entry.path))
        .collect();
    if !unknown.is_empty() {
        return Err(Error::new(format!(
            "unknown key(s): {}",
            unknown.join(", ")
        )));
    }
    Ok(cfg)
}
fn syntax_error(source: &str, error: toml::de::Error) -> Error {
    syntax::adapt(source, error)
}
fn root(source: &str) -> Context<'_> {
    Context {
        source,
        key: String::new(),
        line: 1,
    }
}

pub fn decode_user_config(data: &[u8]) -> Result<UserConfig, Error> {
    let (source, node) = parse(data)?;
    let base = root(&source);
    let mut cfg = UserConfig::default();
    for (key, value) in base.table(&node, "UserConfig")? {
        let context = base.child(key, value);
        if equal_field(key, "actor") {
            cfg.actor = context.string(value)?;
        } else if equal_field(key, "hub") {
            for (key, value) in context.table(value, "UserHubConfig")? {
                let context = context.child(key, value);
                if equal_field(key, "remote") {
                    cfg.hub.remote = context.string(value)?;
                } else if equal_field(key, "branch") {
                    cfg.hub.branch = context.string(value)?;
                }
            }
        } else if equal_field(key, "fetch") {
            for (key, value) in context.table(value, "UserFetchConfig")? {
                if equal_field(key, "throttle") {
                    cfg.fetch.throttle = context.child(key, value).string(value)?;
                }
            }
        }
    }
    Ok(cfg)
}
pub fn decode_project_config(data: &[u8]) -> Result<ProjectConfig, Error> {
    let (source, node) = parse(data)?;
    let base = root(&source);
    let mut cfg = ProjectConfig::default();
    for (key, value) in base.table(&node, "ProjectConfig")? {
        let context = base.child(key, value);
        if equal_field(key, "name") {
            cfg.name = context.string(value)?;
        } else if equal_field(key, "prefix") {
            cfg.prefix = context.string(value)?;
        } else if equal_field(key, "remotes") {
            cfg.remotes = context.list(value)?;
        } else if equal_field(key, "workflow") {
            context.workflow(value, &mut cfg.workflow)?;
        }
    }
    Ok(cfg)
}
pub fn decode_hub_config(data: &[u8]) -> Result<HubConfig, Error> {
    let (source, node) = parse(data)?;
    let base = root(&source);
    let mut cfg = HubConfig::built_in();
    for (key, value) in base.table(&node, "HubConfig")? {
        let context = base.child(key, value);
        if equal_field(key, "workflow") {
            context.workflow(value, &mut cfg.workflow)?;
        } else if equal_field(key, "types") {
            for (key, value) in context.table(value, "TypesConfig")? {
                if equal_field(key, "names") {
                    cfg.types.names = context.child(key, value).list(value)?;
                }
            }
        } else if equal_field(key, "ids") {
            for (key, value) in context.table(value, "IDsConfig")? {
                if equal_field(key, "length") {
                    if let Node::Integer(n) = value.get_ref() {
                        cfg.ids.length = *n;
                    } else {
                        return Err(context.child(key, value).bad_type(value, "integer"));
                    }
                }
            }
        }
    }
    if cfg.ids.length <= 0 {
        cfg.ids.length = 4;
    }
    if cfg.types.names.as_ref().is_none_or(Vec::is_empty) {
        cfg.types = TypesConfig::built_in();
    }
    Ok(cfg)
}

fn file_error(path: &Path, operation: &str, error: std::io::Error) -> Error {
    path_error(operation, path, error).context(path_name(path).as_bytes())
}
fn load<T>(
    path: &Path,
    missing: impl FnOnce() -> T,
    decode: impl FnOnce(&[u8]) -> Result<T, Error>,
) -> Result<T, Error> {
    let mut file = match File::open(path) {
        Ok(file) => file,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(missing()),
        Err(e) => return Err(file_error(path, "open", e)),
    };
    let mut data = Vec::new();
    file.read_to_end(&mut data)
        .map_err(|e| file_error(path, "read", e))?;
    decode(&data).map_err(|e| e.context(path_name(path).as_bytes()))
}
pub fn load_user_config(path: &Path) -> Result<UserConfig, Error> {
    load(path, UserConfig::default, decode_user_config)
}
pub fn load_project_config(path: &Path) -> Result<ProjectConfig, Error> {
    load(path, ProjectConfig::default, decode_project_config)
}
pub fn load_hub_config(path: &Path) -> Result<HubConfig, Error> {
    load(path, HubConfig::built_in, decode_hub_config)
}
