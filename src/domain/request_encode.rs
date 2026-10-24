//! Minimal request edits use the same verified splice geometry as issues;
//! their field ownership, validation and body selection remain distinct.
use super::frontmatter::{EditResult, Error, Node};
use super::issue_encode::{comment_suffix, instant, raw_link, timestamp_pair};
use super::lossless_record::RecordMetadata;
use super::request_document::{OWNED, REQUIRED, RequestDocument, RequestMetadata};
use super::yaml_render::{flow_pair, link_pair, string_pair};

impl RequestDocument {
    /// Repair the ID alias before validation. Parsed
    /// requests retain opaque original logs and ignore edits to authored Extra.
    pub fn encode(&mut self) -> Result<EditResult, Error> {
        if !self.metadata.id.is_empty() && !self.metadata.aliases.contains(&self.metadata.id) {
            self.metadata.aliases.insert(0, self.metadata.id.clone());
        }
        self.record.encode::<RequestMetadata>(
            &self.metadata,
            &self.original_metadata,
            &self.body,
            &self.log,
            &self.new_extra,
        )
    }
}

impl RequestMetadata {
    pub(super) fn changed(&self, previous: &Self, key: &str) -> bool {
        match key {
            "id" => self.id != previous.id,
            "aliases" => self.aliases != previous.aliases,
            "title" => self.title != previous.title,
            "status" => self.status != previous.status,
            "priority" => self.priority != previous.priority,
            "labels" => self.labels != previous.labels,
            "requested_by" => self.requested_by != previous.requested_by,
            "issues" => self.issues != previous.issues,
            "created" => instant(&self.created) != instant(&previous.created),
            "updated" => instant(&self.updated) != instant(&previous.updated),
            _ => unreachable!("request owned key"),
        }
    }

    pub(super) fn render(&self, key: &str, comment: &str) -> Result<Option<String>, Error> {
        let value = match key {
            "id" => string_pair(key, &self.id, comment),
            "aliases" => flow_pair(key, &self.aliases),
            "title" => string_pair(key, &self.title, comment),
            "status" => string_pair(key, &self.status, comment),
            "priority" => format!("{key}: {}{}\n", self.priority, comment_suffix(comment)),
            "labels" => {
                if self.labels.is_empty() {
                    return Ok(None);
                }
                flow_pair(key, &self.labels)
            }
            "requested_by" => {
                if self.requested_by.is_empty() {
                    return Ok(None);
                }
                string_pair(key, &self.requested_by, comment)
            }
            "issues" => {
                if self.issues.is_empty() {
                    return Ok(None);
                }
                let mut output = format!("{key}:\n");
                for link in &self.issues {
                    output.push_str("  - ");
                    output.push_str(
                        link_pair("", &raw_link(link), "")
                            .strip_prefix(": ")
                            .unwrap(),
                    );
                }
                output
            }
            "created" => timestamp_pair(key, &self.created, comment)?,
            "updated" => timestamp_pair(key, &self.updated, comment)?,
            _ => unreachable!("request owned key"),
        };
        Ok(Some(value))
    }
}

impl RecordMetadata for RequestMetadata {
    const KIND: &'static str = "request";
    const DIRECTORY: &'static str = "requests";
    const OWNED: &'static [&'static str] = OWNED;
    const REQUIRED: &'static [&'static str] = REQUIRED;

    fn set_project(&mut self, project: String) {
        self.project = project;
    }

    fn read(&mut self, key: &str, node: &Node) -> Result<(), Error> {
        self.read(key, node)
    }

    fn validate(&self) -> Result<(), Error> {
        RequestMetadata::validate(self)
    }

    fn changed(&self, previous: &Self, key: &str) -> bool {
        self.changed(previous, key)
    }

    fn render(&self, key: &str, comment: &str) -> Result<Option<String>, Error> {
        self.render(key, comment)
    }

    fn separate_before_new_log(body: &[u8]) -> bool {
        !body.is_empty() && !body.ends_with(b"---\n")
    }
}
