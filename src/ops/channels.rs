//! Immutable, independently replayable channel records under the shared Git boundary.
use crate::{
    domain::{
        channel::{self, Acknowledgement, Channel, ChannelView, Membership, Message},
        frontmatter::Error,
    },
    gitops::{Operation, check_hub_write_path, write_hub_file},
    vault::{Index, Resolved, read_public_file, valid_project_name},
};
use std::{
    collections::{BTreeMap, BTreeSet},
    path::{Path, PathBuf},
};

pub struct ChannelStore;
#[derive(Clone, Debug, serde::Serialize)]
pub struct ChannelWarning {
    pub path: String,
    pub message: String,
}
#[derive(Default)]
pub struct ChannelCatalog {
    pub channels: Vec<ChannelView>,
    pub warnings: Vec<ChannelWarning>,
}
impl ChannelCatalog {
    pub fn resolve(self, reference: &str) -> Result<ChannelView, Error> {
        if let Some(view) = self.channels.iter().find(|v| v.channel.id == reference) {
            return Ok(view.clone());
        }
        let mut matches = self
            .channels
            .into_iter()
            .filter(|v| v.channel.name == reference);
        let view = matches.next().ok_or_else(|| {
            if let Some(warning) = self
                .warnings
                .iter()
                .find(|w| w.path.rsplit('/').next() == Some(reference))
            {
                error(format!(
                    "invalid channel {}: {}",
                    reference, warning.message
                ))
            } else {
                error("channel not found")
            }
        })?;
        if matches.next().is_some() {
            return Err(error("ambiguous channel name; use its stable ID"));
        }
        Ok(view)
    }
}
fn error(value: impl std::fmt::Display) -> Error {
    Error::new(value.to_string())
}
fn base(project: &str) -> Result<PathBuf, Error> {
    if !valid_project_name(project.as_bytes()) {
        return Err(error("invalid channel project"));
    }
    Ok(PathBuf::from("projects").join(project).join("channels"))
}
fn directories(hub: &Path, relative: &Path) -> Result<Vec<PathBuf>, Error> {
    if !crate::gitops::check_hub_directory(hub, relative)? {
        return Ok(vec![]);
    }
    let entries = match std::fs::read_dir(hub.join(relative)) {
        Ok(entries) => entries,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(vec![]),
        Err(e) => return Err(error(e)),
    };
    let mut result = vec![];
    for entry in entries {
        let entry = entry.map_err(error)?;
        if entry.file_name().to_string_lossy().starts_with('.') {
            continue;
        }
        let kind = entry.file_type().map_err(error)?;
        if kind.is_symlink() {
            return Err(error("symlink in channel records"));
        }
        result.push(relative.join(entry.file_name()));
    }
    result.sort();
    Ok(result)
}
fn read<T: serde::de::DeserializeOwned>(hub: &Path, path: &Path) -> Result<(T, String), Error> {
    channel::decode(&read_public_file(hub, &path.to_string_lossy())?)
}
fn records<T: serde::de::DeserializeOwned>(
    hub: &Path,
    path: &Path,
) -> Result<Vec<(PathBuf, T, String)>, Error> {
    directories(hub, path)?
        .into_iter()
        .map(|p| {
            if p.extension().and_then(|v| v.to_str()) != Some("md") {
                return Err(error("unexpected file in channel record directory"));
            }
            let (record, body) = read(hub, &p)?;
            Ok((p, record, body))
        })
        .collect()
}
impl ChannelStore {
    pub fn load(hub: &Path, project: &str) -> Result<Vec<ChannelView>, Error> {
        Ok(Self::scan(hub, project)?.channels)
    }
    pub fn scan(hub: &Path, project: &str) -> Result<ChannelCatalog, Error> {
        let root = base(project)?;
        if !crate::gitops::check_hub_directory(hub, &root)? {
            return Ok(ChannelCatalog::default());
        }
        let mut catalog = ChannelCatalog::default();
        let entries = std::fs::read_dir(hub.join(&root)).map_err(error)?;
        for entry in entries {
            let entry = entry.map_err(error)?;
            let kind = entry.file_type().map_err(error)?;
            if !kind.is_dir() && !kind.is_symlink() {
                continue;
            }
            let path = root.join(entry.file_name());
            match Self::load_one(hub, project, &path) {
                Ok(view) => catalog.channels.push(view),
                Err(error) => catalog.warnings.push(ChannelWarning {
                    path: path.to_string_lossy().into_owned(),
                    message: error.to_string(),
                }),
            }
        }
        catalog.channels.sort_by(|a, b| {
            a.channel
                .name
                .cmp(&b.channel.name)
                .then(a.channel.id.cmp(&b.channel.id))
        });
        catalog.warnings.sort_by(|a, b| a.path.cmp(&b.path));
        Ok(catalog)
    }
    fn load_one(hub: &Path, project: &str, path: &Path) -> Result<ChannelView, Error> {
        let id = path
            .file_name()
            .and_then(|v| v.to_str())
            .ok_or_else(|| error("invalid channel path"))?;
        channel::component(id)?;
        let (channel, _): (Channel, String) = read(hub, &path.join("channel.md"))?;
        if channel.id != id || channel.project != project {
            return Err(error("channel path does not match metadata"));
        }
        channel::component(&channel.name)?;
        channel::identity(&channel.created_by)?;
        let mut members: BTreeSet<_> = channel.members.iter().cloned().collect();
        for actor in &members {
            channel::identity(actor)?;
        }
        members.insert(channel.created_by.clone());
        for (p, member, _) in records::<Membership>(hub, &path.join("members"))? {
            channel::identity(&member.actor)?;
            if member.channel != id
                || p.file_stem().and_then(|v| v.to_str())
                    != Some(&channel::digest(&[&member.actor]))
            {
                return Err(error("membership path does not match metadata"));
            }
            members.insert(member.actor);
        }
        let mut messages = BTreeMap::new();
        for (p, mut message, body) in records::<Message>(hub, &path.join("messages"))? {
            if message.channel != id
                || message.id != channel::message_id(id, &message.actor, &message.key)
                || p.file_stem().and_then(|v| v.to_str()) != Some(&message.id)
            {
                return Err(error("message path does not match metadata"));
            }
            validate_message(&message, &members)?;
            message.body = body;
            if messages.insert(message.id.clone(), message).is_some() {
                return Err(error("duplicate channel message ID"));
            }
        }
        let messages = ordered(messages)?;
        let mut acknowledged: BTreeMap<String, BTreeSet<String>> = BTreeMap::new();
        for directory in directories(hub, &path.join("acks"))? {
            for (p, ack, _) in records::<Acknowledgement>(hub, &directory)? {
                if ack.channel != id
                    || directory.file_name().and_then(|v| v.to_str())
                        != Some(&channel::digest(&[&ack.actor]))
                    || p.file_stem().and_then(|v| v.to_str()) != Some(&ack.message)
                    || !members.contains(&ack.actor)
                    || !messages.iter().any(|m| m.id == ack.message)
                {
                    return Err(error("invalid channel acknowledgement"));
                }
                acknowledged
                    .entry(ack.actor)
                    .or_default()
                    .insert(ack.message);
            }
        }
        Ok(ChannelView {
            channel,
            members,
            messages,
            acknowledged,
        })
    }
    pub fn resolve(hub: &Path, project: &str, reference: &str) -> Result<ChannelView, Error> {
        Self::scan(hub, project)?.resolve(reference)
    }
}

fn validate_message(message: &Message, members: &BTreeSet<String>) -> Result<(), Error> {
    channel::identity(&message.actor)?;
    channel::identity(&message.key)?;
    if !members.contains(&message.actor) || message.to.iter().any(|to| !members.contains(to)) {
        return Err(error(
            "message actor and addressees must be channel members",
        ));
    }
    if let Some(session) = &message.session {
        channel::identity(session)?;
    }
    if let Some(reply) = &message.reply_to {
        channel::component(reply)?;
    }
    Ok(())
}
fn ordered(mut messages: BTreeMap<String, Message>) -> Result<Vec<Message>, Error> {
    let mut children: BTreeMap<String, BTreeSet<String>> = BTreeMap::new();
    let mut ready = BTreeSet::new();
    for (id, message) in &messages {
        if let Some(parent) = &message.reply_to {
            if !messages.contains_key(parent) {
                return Err(error("channel reply references a missing message"));
            }
            children
                .entry(parent.clone())
                .or_default()
                .insert(id.clone());
        } else {
            ready.insert(id.clone());
        }
    }
    let mut result = vec![];
    while let Some(id) = ready.pop_first() {
        result.push(messages.remove(&id).unwrap());
        if let Some(next) = children.remove(&id) {
            ready.extend(next);
        }
    }
    if !messages.is_empty() {
        return Err(error("cycle in channel replies"));
    }
    Ok(result)
}
#[derive(Clone, Debug)]
pub enum ChannelChange {
    Create {
        name: String,
        members: Vec<String>,
        issue: Option<String>,
    },
    Join {
        channel: String,
        actor: Option<String>,
    },
    Send {
        channel: String,
        key: String,
        body: String,
        to: Vec<String>,
        reply_to: Option<String>,
        references: Vec<String>,
        session: Option<String>,
    },
    Ack {
        channel: String,
        messages: Vec<String>,
    },
}
#[derive(Clone, Copy)]
enum MutationPhase {
    Prepared,
    Validating,
    Writing,
}
pub struct ChannelMutation {
    resolved: Resolved,
    actor: String,
    change: ChannelChange,
    id: String,
    created: i64,
    phase: MutationPhase,
}
impl ChannelMutation {
    pub fn failure_delivery(&self) -> &'static str {
        match self.phase {
            MutationPhase::Prepared => "not_attempted",
            MutationPhase::Validating => "rejected",
            MutationPhase::Writing => "uncertain",
        }
    }
    pub fn id(&self) -> &str {
        &self.id
    }
    pub fn new(resolved: Resolved, actor: String, change: ChannelChange) -> Result<Self, Error> {
        channel::identity(&actor)?;
        let id = match &change {
            ChannelChange::Create { name, .. } => {
                channel::component(name)?;
                let project = std::str::from_utf8(&resolved.project).map_err(error)?;
                format!("ch-{}", channel::digest(&[project, name]))
            }
            ChannelChange::Send { channel, key, .. } => {
                channel::component(channel)?;
                channel::identity(key)?;
                channel::message_id(channel, &actor, key)
            }
            ChannelChange::Join { channel, .. } | ChannelChange::Ack { channel, .. } => {
                channel.clone()
            }
        };
        Ok(Self {
            resolved,
            actor,
            change,
            id,
            created: time::OffsetDateTime::now_utc().unix_timestamp(),
            phase: MutationPhase::Prepared,
        })
    }
}
fn immutable<T: serde::Serialize>(
    hub: &Path,
    path: &Path,
    record: &T,
    body: &str,
) -> Result<Vec<PathBuf>, Error> {
    check_hub_write_path(hub, path)?;
    let bytes = channel::encode(record, body)?;
    match std::fs::symlink_metadata(hub.join(path)) {
        Ok(_) => {
            if read_public_file(hub, &path.to_string_lossy())? != bytes {
                return Err(error("immutable channel record collision"));
            }
            Ok(vec![])
        }
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
            write_hub_file(hub, path, &bytes)?;
            Ok(vec![path.into()])
        }
        Err(e) => Err(error(e)),
    }
}
impl ChannelMutation {
    fn create(
        &mut self,
        hub: &Path,
        name: &str,
        members: &[String],
        issue: Option<&str>,
    ) -> Result<Vec<PathBuf>, Error> {
        let project = std::str::from_utf8(&self.resolved.project).map_err(error)?;
        let root = base(project)?;
        let existing = ChannelStore::load(hub, project)?;
        if existing
            .iter()
            .any(|v| v.channel.name == name && v.channel.id != self.id)
        {
            return Err(error("channel name already exists"));
        }
        let mut members: BTreeSet<_> = members.iter().cloned().collect();
        members.insert(self.actor.clone());
        for member in &members {
            channel::identity(member)?;
        }
        let issue = issue
            .map(|reference| {
                let ix = Index::load_snapshot(hub, Default::default())?;
                super::issues::canonical_link(&ix, reference).map(|link| link.raw.to_string())
            })
            .transpose()?;
        let mut channel = Channel {
            id: self.id.clone(),
            name: name.into(),
            project: project.into(),
            created_by: self.actor.clone(),
            created: self.created,
            issue,
            members: members.into_iter().collect(),
        };
        if let Some(previous) = existing.iter().find(|v| v.channel.id == self.id) {
            channel.created = previous.channel.created;
            if channel != previous.channel {
                return Err(error(
                    "channel name already used with different creation metadata",
                ));
            }
            self.phase = MutationPhase::Writing;
            return Ok(vec![]);
        }
        self.phase = MutationPhase::Writing;
        let mut paths = super::scaffold_paths(hub, &self.resolved)?;
        paths.extend(immutable(
            hub,
            &root.join(&self.id).join("channel.md"),
            &channel,
            &format!("# {name}\n"),
        )?);
        Ok(paths)
    }
    fn resolve(
        &self,
        hub: &Path,
        reference: &str,
        self_join: bool,
    ) -> Result<(PathBuf, ChannelView), Error> {
        let project = std::str::from_utf8(&self.resolved.project).map_err(error)?;
        let view = ChannelStore::resolve(hub, project, reference)?;
        if !self_join && !view.members.contains(&self.actor) {
            return Err(error("actor is not a channel member; join first"));
        }
        Ok((base(project)?.join(&view.channel.id), view))
    }
}
impl Operation for ChannelMutation {
    fn subject(&self) -> String {
        format!("bn: channel {}", self.id)
    }
    fn apply(&mut self, hub: &Path) -> Result<Vec<PathBuf>, Error> {
        if !matches!(self.phase, MutationPhase::Writing) {
            self.phase = MutationPhase::Validating;
        }
        match self.change.clone() {
            ChannelChange::Create {
                name,
                members,
                issue,
            } => self.create(hub, &name, &members, issue.as_deref()),
            ChannelChange::Join { channel, actor } => {
                let (root, view) = self.resolve(hub, &channel, actor.is_none())?;
                let actor = actor.as_ref().unwrap_or(&self.actor);
                channel::identity(actor)?;
                if view.members.contains(actor) {
                    self.phase = MutationPhase::Writing;
                    return Ok(vec![]);
                }
                self.phase = MutationPhase::Writing;
                immutable(
                    hub,
                    &root
                        .join("members")
                        .join(format!("{}.md", channel::digest(&[actor]))),
                    &Membership {
                        channel: view.channel.id,
                        actor: actor.clone(),
                    },
                    "",
                )
            }
            ChannelChange::Send {
                channel,
                key,
                body,
                mut to,
                reply_to,
                references,
                session,
            } => {
                let (root, view) = self.resolve(hub, &channel, false)?;
                self.id = channel::message_id(&view.channel.id, &self.actor, &key);
                to.sort();
                to.dedup();
                let mut message = Message {
                    id: self.id.clone(),
                    channel: view.channel.id.clone(),
                    actor: self.actor.clone(),
                    session,
                    key,
                    created: self.created,
                    reply_to,
                    to,
                    references,
                    body,
                };
                validate_message(&message, &view.members)?;
                if message.body.trim().is_empty() {
                    return Err(error("channel message body must not be blank"));
                }
                if let Some(reply) = &message.reply_to
                    && !view.messages.iter().any(|m| m.id == *reply)
                {
                    return Err(error("reply message not found in this channel"));
                }
                if let Some(previous) = view.messages.iter().find(|m| m.id == self.id) {
                    message.created = previous.created;
                    if *previous != message {
                        return Err(error(
                            "idempotency key already used with a different message",
                        ));
                    }
                    self.phase = MutationPhase::Writing;
                    return Ok(vec![]);
                }
                self.phase = MutationPhase::Writing;
                immutable(
                    hub,
                    &root.join("messages").join(format!("{}.md", self.id)),
                    &message,
                    &message.body,
                )
            }
            ChannelChange::Ack { channel, messages } => {
                let (root, view) = self.resolve(hub, &channel, false)?;
                let mut paths = vec![];
                let unique: BTreeSet<_> = messages.iter().collect();
                for id in &unique {
                    if !view.messages.iter().any(|m| &m.id == *id) {
                        return Err(error("acknowledgement message not found"));
                    }
                }
                self.phase = MutationPhase::Writing;
                for id in unique {
                    let ack = Acknowledgement {
                        channel: view.channel.id.clone(),
                        actor: self.actor.clone(),
                        message: id.clone(),
                    };
                    paths.extend(immutable(
                        hub,
                        &root
                            .join("acks")
                            .join(channel::digest(&[&self.actor]))
                            .join(format!("{id}.md")),
                        &ack,
                        "",
                    )?);
                }
                Ok(paths)
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::fs::symlink;
    struct Temp(PathBuf);
    impl Temp {
        fn new() -> Self {
            let mut random = [0; 8];
            getrandom::fill(&mut random).unwrap();
            let path = std::env::temp_dir()
                .join(format!("bn-channel-core-{:x}", u64::from_ne_bytes(random)));
            std::fs::create_dir(&path).unwrap();
            Self(path)
        }
    }
    impl Drop for Temp {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }
    fn resolved() -> Resolved {
        Resolved {
            project: b"test".to_vec(),
            ..Default::default()
        }
    }
    fn create(hub: &Path) -> String {
        let mut op = ChannelMutation::new(
            resolved(),
            "a".into(),
            ChannelChange::Create {
                name: "team".into(),
                members: vec!["b".into()],
                issue: None,
            },
        )
        .unwrap();
        op.apply(hub).unwrap();
        op.id
    }
    fn send(channel: &str, key: &str, body: &str) -> ChannelMutation {
        ChannelMutation::new(
            resolved(),
            "a".into(),
            ChannelChange::Send {
                channel: channel.into(),
                key: key.into(),
                body: body.into(),
                to: vec![],
                reply_to: None,
                references: vec![],
                session: None,
            },
        )
        .unwrap()
    }
    #[test]
    fn optional_record_fields_and_sibling_corruption_do_not_block_healthy_channels() {
        let temp = Temp::new();
        let id = create(&temp.0);
        let mut op = send(&id, "future-fields", "Markdown body");
        op.apply(&temp.0).unwrap();
        let view = ChannelStore::resolve(&temp.0, "test", &id).unwrap();
        let root = temp.0.join("projects/test/channels").join(&id);
        let mut manifest = serde_json::to_value(&view.channel).unwrap();
        manifest["future_optional"] = serde_json::json!("extra coordination metadata");
        std::fs::write(
            root.join("channel.md"),
            channel::encode(&manifest, "").unwrap(),
        )
        .unwrap();
        let mut message = serde_json::to_value(&view.messages[0]).unwrap();
        message["future_optional"] = serde_json::json!({"flag":true});
        std::fs::write(
            root.join("messages").join(format!("{}.md", op.id())),
            channel::encode(&message, "Markdown body").unwrap(),
        )
        .unwrap();
        let invalid = temp.0.join("projects/test/channels/broken");
        std::fs::create_dir(&invalid).unwrap();
        std::fs::write(invalid.join("channel.md"), "invalid").unwrap();
        std::fs::write(temp.0.join("projects/test/channels/notes.md"), "notes").unwrap();
        let catalog = ChannelStore::scan(&temp.0, "test").unwrap();
        assert_eq!(catalog.channels.len(), 1);
        assert_eq!(catalog.warnings.len(), 1);
        assert_eq!(
            catalog.resolve("team").unwrap().messages[0].body,
            "Markdown body"
        );
        assert!(
            send(&id, "healthy-continues", "Still works")
                .apply(&temp.0)
                .is_ok()
        );
    }
    #[test]
    fn delivery_failure_state_distinguishes_validation_from_possible_effects() {
        let temp = Temp::new();
        let id = create(&temp.0);
        let mut original = send(&id, "key", "original");
        assert_eq!(original.failure_delivery(), "not_attempted");
        original.apply(&temp.0).unwrap();
        assert_eq!(original.failure_delivery(), "uncertain");
        let mut conflicting = send(&id, "key", "different");
        assert!(conflicting.apply(&temp.0).is_err());
        assert_eq!(conflicting.failure_delivery(), "rejected");
    }
    #[test]
    fn sending_by_name_uses_the_resolved_channel_identity() {
        let temp = Temp::new();
        let channel = create(&temp.0);
        let mut named_send = send("team", "by-name", "hello");
        named_send.apply(&temp.0).unwrap();
        assert_eq!(named_send.id, channel::message_id(&channel, "a", "by-name"));
        let view = ChannelStore::resolve(&temp.0, "test", &channel).unwrap();
        assert_eq!(view.messages.len(), 1);
        assert_eq!(view.messages[0].id, named_send.id);
        assert!(
            send(&channel, "by-name", "hello")
                .apply(&temp.0)
                .unwrap()
                .is_empty()
        );
    }
    #[test]
    fn immutable_send_retry_ack_and_join_survive_reload() {
        let temp = Temp::new();
        let id = create(&temp.0);
        let mut send = send(&id, "key", "# result\n\n[[evidence]]\n");
        assert_eq!(send.apply(&temp.0).unwrap().len(), 1);
        let message_id = send.id.clone();
        let mut retry = self::send(&id, "key", "# result\n\n[[evidence]]\n");
        retry.created += 99;
        assert!(retry.apply(&temp.0).unwrap().is_empty());
        assert!(
            self::send(&id, "key", "changed")
                .apply(&temp.0)
                .unwrap_err()
                .to_string()
                .contains("idempotency")
        );
        let view = ChannelStore::resolve(&temp.0, "test", "team").unwrap();
        assert_eq!(view.messages.len(), 1);
        assert_eq!(view.unread("b").len(), 1);
        let mut ack = ChannelMutation::new(
            resolved(),
            "b".into(),
            ChannelChange::Ack {
                channel: id.clone(),
                messages: vec![message_id.clone()],
            },
        )
        .unwrap();
        ack.apply(&temp.0).unwrap();
        assert!(ack.apply(&temp.0).unwrap().is_empty());
        let mut join = ChannelMutation::new(
            resolved(),
            "c".into(),
            ChannelChange::Join {
                channel: id.clone(),
                actor: None,
            },
        )
        .unwrap();
        join.apply(&temp.0).unwrap();
        let view = ChannelStore::resolve(&temp.0, "test", &id).unwrap();
        assert_eq!(view.unread("b").len(), 0);
        assert_eq!(view.unread("c").len(), 1);
        assert_eq!(view.messages[0].body, "# result\n\n[[evidence]]\n");
        let mut bad_ack = ChannelMutation::new(
            resolved(),
            "c".into(),
            ChannelChange::Ack {
                channel: id,
                messages: vec![message_id, "missing".into()],
            },
        )
        .unwrap();
        assert!(bad_ack.apply(&temp.0).is_err());
        assert_eq!(
            ChannelStore::load(&temp.0, "test").unwrap()[0]
                .unread("c")
                .len(),
            1
        );
    }
    #[test]
    fn reply_order_is_topological_and_cycles_are_rejected() {
        let temp = Temp::new();
        let id = create(&temp.0);
        let mut root = send(&id, "parent", "question");
        root.apply(&temp.0).unwrap();
        let mut reply = ChannelMutation::new(
            resolved(),
            "b".into(),
            ChannelChange::Send {
                channel: id.clone(),
                key: "reply".into(),
                body: "answer".into(),
                to: vec!["a".into()],
                reply_to: Some(root.id.clone()),
                references: vec!["commit:deadbeef".into()],
                session: Some("mac".into()),
            },
        )
        .unwrap();
        reply.apply(&temp.0).unwrap();
        let view = ChannelStore::resolve(&temp.0, "test", &id).unwrap();
        assert_eq!(view.messages[0].id, root.id);
        assert_eq!(view.messages[1].id, reply.id);
        let mut messages: BTreeMap<_, _> = view
            .messages
            .into_iter()
            .map(|m| (m.id.clone(), m))
            .collect();
        messages.get_mut(&root.id).unwrap().reply_to = Some(reply.id);
        assert!(ordered(messages).is_err());
    }
    #[test]
    fn symlinked_channel_records_are_never_read_or_written() {
        let temp = Temp::new();
        let outside = Temp::new();
        std::fs::create_dir_all(temp.0.join("projects/test")).unwrap();
        symlink(&outside.0, temp.0.join("projects/test/channels")).unwrap();
        assert!(ChannelStore::load(&temp.0, "test").is_err());
        let mut op = ChannelMutation::new(
            resolved(),
            "a".into(),
            ChannelChange::Create {
                name: "team".into(),
                members: vec![],
                issue: None,
            },
        )
        .unwrap();
        assert!(op.apply(&temp.0).is_err());
        assert!(std::fs::read_dir(&outside.0).unwrap().next().is_none());
    }
    #[test]
    fn same_name_has_one_path_and_nonmembers_cannot_send() {
        let temp = Temp::new();
        let id = create(&temp.0);
        let mut retry = ChannelMutation::new(
            resolved(),
            "a".into(),
            ChannelChange::Create {
                name: "team".into(),
                members: vec!["b".into()],
                issue: None,
            },
        )
        .unwrap();
        retry.created += 99;
        assert_eq!(retry.id, id);
        assert!(retry.apply(&temp.0).unwrap().is_empty());
        let mut same = ChannelMutation::new(
            resolved(),
            "b".into(),
            ChannelChange::Create {
                name: "team".into(),
                members: vec![],
                issue: None,
            },
        )
        .unwrap();
        assert_eq!(same.id, id);
        assert!(same.apply(&temp.0).is_err());
        let mut op = ChannelMutation::new(
            resolved(),
            "outsider".into(),
            ChannelChange::Send {
                channel: id.clone(),
                key: "key".into(),
                body: "hello".into(),
                to: vec![],
                reply_to: None,
                references: vec![],
                session: None,
            },
        )
        .unwrap();
        assert!(op.apply(&temp.0).is_err());
        assert_eq!(ChannelStore::load(&temp.0, "test").unwrap().len(), 1);
    }
}
