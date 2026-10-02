//! Edit the owned remote array without re-encoding an authored TOML document.
use crate::domain::{config, frontmatter::Error, yaml_string::YamlString};
use std::collections::BTreeMap;
use toml_parser::{Source, lexer::TokenKind};

pub(super) fn add_remote(source: &[u8], remote: &YamlString) -> Result<Option<Vec<u8>>, Error> {
    let config = config::decode_project_config(source)?;
    if config
        .remotes
        .as_ref()
        .is_some_and(|values| values.contains(remote))
    {
        return Ok(None);
    }
    let text = std::str::from_utf8(source).map_err(|e| Error::new(e.to_string()))?;
    let fields: BTreeMap<String, toml::Spanned<toml::Value>> =
        toml::from_str(text).map_err(|e| Error::new(e.to_string()))?;
    let value = fields
        .iter()
        .rev()
        .find_map(|(key, value)| config::equal_field(key, "remotes").then_some(value));
    let remote = std::str::from_utf8(remote.as_bytes()).map_err(|e| Error::new(e.to_string()))?;
    let quoted = toml::Value::String(remote.into()).to_string();
    let Some(value) = value else {
        let mut bytes = format!("remotes = [{quoted}]\n").into_bytes();
        bytes.extend_from_slice(source);
        config::decode_project_config(&bytes)?;
        return Ok(Some(bytes));
    };
    let span = value.span();
    let significant: Vec<_> = Source::new(text)
        .lex()
        .filter(|token| token.span().start() >= span.start && token.span().end() <= span.end)
        .filter(|token| {
            !matches!(
                token.kind(),
                TokenKind::Whitespace | TokenKind::Comment | TokenKind::Newline | TokenKind::Eof
            )
        })
        .collect();
    if significant.first().map(|t| t.kind()) != Some(TokenKind::LeftSquareBracket)
        || significant.last().map(|t| t.kind()) != Some(TokenKind::RightSquareBracket)
    {
        return Err(Error::new("project remotes must be an array".into()));
    }
    let close = significant.last().unwrap().span().start();
    let previous = significant[significant.len() - 2];
    let needs_comma = !matches!(
        previous.kind(),
        TokenKind::LeftSquareBracket | TokenKind::Comma
    );
    let comma = previous.span().end();
    let mut bytes = Vec::new();
    bytes.extend_from_slice(&source[..comma]);
    if needs_comma {
        bytes.push(b',');
    }
    bytes.extend_from_slice(&source[comma..close]);
    bytes.extend_from_slice(format!("\n  {quoted}\n").as_bytes());
    bytes.extend_from_slice(&source[close..]);
    let updated = config::decode_project_config(&bytes)?;
    if !updated
        .remotes
        .as_ref()
        .is_some_and(|values| values.iter().any(|v| v.as_bytes() == remote.as_bytes()))
    {
        return Err(Error::new(
            "project remote edit did not update the owned field".into(),
        ));
    }
    Ok(Some(bytes))
}
