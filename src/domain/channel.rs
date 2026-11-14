//! Immutable conversation records and exact observed-message cursors.
use super::frontmatter::Error;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::collections::{BTreeMap, BTreeSet};

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
pub struct Channel {
    pub id: String,
    pub name: String,
    pub project: String,
    pub created_by: String,
    pub created: i64,
    pub issue: Option<String>,
    pub members: Vec<String>,
}
#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
pub struct Message {
    pub id: String,
    pub channel: String,
    pub actor: String,
    pub session: Option<String>,
    pub key: String,
    pub created: i64,
    pub reply_to: Option<String>,
    pub to: Vec<String>,
    pub references: Vec<String>,
    #[serde(default)]
    pub body: String,
}
#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub struct Membership {
    pub channel: String,
    pub actor: String,
}
#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub struct Acknowledgement {
    pub channel: String,
    pub actor: String,
    pub message: String,
}
#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub struct Cursor {
    pub channel: String,
    pub seen: BTreeSet<String>,
}
#[derive(Clone, Debug, Serialize)]
pub struct ReadResult {
    pub messages: Vec<Message>,
    pub cursor: Cursor,
    pub unread: Vec<String>,
}
#[derive(Clone, Debug, Serialize)]
pub struct ChannelView {
    pub channel: Channel,
    pub members: BTreeSet<String>,
    pub messages: Vec<Message>,
    pub acknowledged: BTreeMap<String, BTreeSet<String>>,
}
impl ChannelView {
    pub fn unread(&self, actor: &str) -> Vec<&Message> {
        if !self.members.contains(actor) {
            return vec![];
        }
        self.messages
            .iter()
            .filter(|m| {
                m.actor != actor
                    && (m.to.is_empty() || m.to.iter().any(|to| to == actor))
                    && !self
                        .acknowledged
                        .get(actor)
                        .is_some_and(|ids| ids.contains(&m.id))
            })
            .collect()
    }
    pub fn read(
        &self,
        actor: &str,
        cursor: Option<&Cursor>,
        unread_only: bool,
    ) -> Result<ReadResult, Error> {
        if cursor.is_some_and(|c| c.channel != self.channel.id) {
            return Err(Error::new("cursor belongs to a different channel".into()));
        }
        let mut cursor = cursor.cloned().unwrap_or_else(|| Cursor {
            channel: self.channel.id.clone(),
            seen: BTreeSet::new(),
        });
        let unread: Vec<_> = self.unread(actor).iter().map(|m| m.id.clone()).collect();
        let messages: Vec<_> = self
            .messages
            .iter()
            .filter(|m| !cursor.seen.contains(&m.id) && (!unread_only || unread.contains(&m.id)))
            .cloned()
            .collect();
        cursor.seen.extend(messages.iter().map(|m| m.id.clone()));
        Ok(ReadResult {
            messages,
            cursor,
            unread,
        })
    }
}
pub fn digest(parts: &[&str]) -> String {
    let mut hash = Sha256::new();
    for part in parts {
        hash.update((part.len() as u64).to_be_bytes());
        hash.update(part.as_bytes());
    }
    hash.finalize().iter().map(|b| format!("{b:02x}")).collect()
}
pub fn message_id(channel: &str, actor: &str, key: &str) -> String {
    format!("msg-{}", digest(&[channel, actor, key]))
}
pub fn identity(value: &str) -> Result<(), Error> {
    if value.trim().is_empty() || value.len() > 1024 || value.contains(['\0', '\n', '\r']) {
        return Err(Error::new(
            "channel identity must be nonempty, single-line and at most 1024 bytes".into(),
        ));
    }
    Ok(())
}
pub fn component(value: &str) -> Result<(), Error> {
    if value.is_empty()
        || value.len() > 200
        || !value
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'-' | b'_'))
    {
        return Err(Error::new("invalid channel path identifier".into()));
    }
    Ok(())
}
pub fn encode<T: Serialize>(metadata: &T, body: &str) -> Result<Vec<u8>, Error> {
    let mut value = serde_json::to_value(metadata).map_err(|e| Error::new(e.to_string()))?;
    if let Some(map) = value.as_object_mut() {
        map.remove("body");
    }
    let header = serde_json::to_string(&value).map_err(|e| Error::new(e.to_string()))?;
    Ok(format!("---\n{header}\n---\n{body}").into_bytes())
}
pub fn decode<T: serde::de::DeserializeOwned>(bytes: &[u8]) -> Result<(T, String), Error> {
    let raw = std::str::from_utf8(bytes).map_err(|e| Error::new(e.to_string()))?;
    let (header, body) = raw
        .strip_prefix("---\n")
        .and_then(|v| v.split_once("\n---\n"))
        .ok_or_else(|| Error::new("invalid channel record frontmatter".into()))?;
    Ok((
        serde_json::from_str(header)
            .map_err(|e| Error::new(format!("invalid channel record: {e}")))?,
        body.into(),
    ))
}

pub fn parse_channel(bytes: &[u8]) -> Result<Channel, Error> {
    decode(bytes).map(|(record, _)| record)
}
pub fn parse_message(bytes: &[u8]) -> Result<Message, Error> {
    let (mut message, body): (Message, String) = decode(bytes)?;
    message.body = body;
    Ok(message)
}

#[cfg(test)]
mod tests {
    use super::*;
    fn message(id: &str, actor: &str, to: &[&str]) -> Message {
        Message {
            id: id.into(),
            channel: "c".into(),
            actor: actor.into(),
            session: None,
            key: id.into(),
            created: 0,
            reply_to: None,
            to: to.iter().map(|s| s.to_string()).collect(),
            references: vec![],
            body: id.into(),
        }
    }
    fn view() -> ChannelView {
        ChannelView {
            channel: Channel {
                id: "c".into(),
                name: "test".into(),
                project: "p".into(),
                created_by: "a".into(),
                created: 0,
                issue: None,
                members: vec!["a".into(), "b".into()],
            },
            members: ["a".into(), "b".into(), "c".into()].into(),
            messages: vec![message("new", "a", &[]), message("addressed", "a", &["b"])],
            acknowledged: BTreeMap::new(),
        }
    }
    #[test]
    fn cursor_tracks_exact_ids_so_old_offline_messages_are_new() {
        let mut v = view();
        let first = v.read("b", None, false).unwrap();
        v.messages.insert(0, message("late", "a", &[]));
        let second = v.read("b", Some(&first.cursor), false).unwrap();
        assert_eq!(
            second
                .messages
                .iter()
                .map(|m| m.id.as_str())
                .collect::<Vec<_>>(),
            ["late"]
        );
        assert_eq!(second.unread.len(), 3);
        assert!(
            v.read(
                "b",
                Some(&Cursor {
                    channel: "other".into(),
                    seen: BTreeSet::new()
                }),
                false
            )
            .is_err()
        );
    }
    #[test]
    fn acknowledgements_are_per_actor_and_addressing_does_not_hide_history() {
        let mut v = view();
        v.acknowledged.insert("b".into(), ["new".into()].into());
        assert_eq!(v.unread("b").len(), 1);
        assert_eq!(v.unread("c").len(), 1);
        assert_eq!(v.read("c", None, false).unwrap().messages.len(), 2);
        assert_eq!(v.unread("a").len(), 0);
    }
    #[test]
    fn unread_cursor_records_only_messages_returned() {
        let view = view();
        let unread = view.read("c", None, true).unwrap();
        assert_eq!(
            unread
                .messages
                .iter()
                .map(|m| m.id.as_str())
                .collect::<Vec<_>>(),
            ["new"]
        );
        assert_eq!(unread.cursor.seen, ["new".into()].into());
        let history = view.read("c", Some(&unread.cursor), false).unwrap();
        assert_eq!(
            history
                .messages
                .iter()
                .map(|m| m.id.as_str())
                .collect::<Vec<_>>(),
            ["addressed"]
        );
    }
    #[test]
    fn identities_and_markdown_roundtrip() {
        let m = message("id", "Agent A", &[]);
        let bytes = encode(&m, "# Markdown\n\n[[handoff]]\n---\n").unwrap();
        let actual = parse_message(&bytes).unwrap();
        assert_eq!(actual.body, "# Markdown\n\n[[handoff]]\n---\n");
        assert_eq!(actual.actor, m.actor);
        assert_ne!(digest(&["ab", "c"]), digest(&["a", "bc"]));
        assert_ne!(message_id("c", "a", "key"), message_id("c", "b", "key"));
        assert!(identity("Agent A").is_ok());
        assert!(component("../bad").is_err());
    }
}
