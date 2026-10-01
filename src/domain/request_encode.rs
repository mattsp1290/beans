//! Minimal request edits use the same verified splice geometry as issues;
//! their field ownership, validation and body selection remain distinct.
use super::frontmatter::{EditResult, Error, NodeKind};
use super::issue_encode::{comment_suffix, instant, raw_link, scalar_comment, timestamp_pair};
use super::request_document::{OWNED, RequestDocument, RequestMetadata};
use super::yaml_render::{flow_pair, link_pair, string_pair};

impl RequestDocument {
    /// Like Go EncodeRequest, repair the ID alias before validation. Parsed
    /// requests retain opaque original logs and ignore edits to authored Extra.
    pub fn encode(&mut self) -> Result<EditResult, Error> {
        if !self.metadata.id.is_empty() && !self.metadata.aliases.contains(&self.metadata.id) {
            self.metadata.aliases.insert(0, self.metadata.id.clone());
        }
        self.metadata.validate()?;
        let Some(document) = &self.document else {
            let mut output = String::from("---\n");
            for &key in OWNED {
                if let Some(field) = self.metadata.render(key, "")? {
                    output.push_str(&field);
                }
            }
            output.push_str(&super::extra_encode::mapping_fields(&self.new_extra)?);
            output.push_str("---\n");
            let mut output = output.into_bytes();
            output.extend_from_slice(&self.render_body_bytes()?);
            return Ok(EditResult {
                bytes: output,
                copies: Vec::new(),
            });
        };
        let mut rendered = Vec::new();
        for &key in OWNED {
            if self.metadata.changed(&self.original_metadata, key) {
                let comment = document
                    .fields()
                    .iter()
                    .find(|field| field.key == key)
                    .filter(|field| field.value.kind == NodeKind::Scalar)
                    .map_or(String::new(), |field| {
                        scalar_comment(
                            document.original(),
                            document.view_offset(field.start),
                            document.view_offset(field.end),
                            field.value.line,
                            field.key_line,
                        )
                    });
                rendered.push((key, self.metadata.render(key, &comment)?));
            }
        }
        let changes: Vec<_> = rendered
            .iter()
            .map(|(key, value)| (*key, value.as_ref().map(|value| value.as_bytes())))
            .collect();
        document.splice_owned(OWNED, &changes, &self.render_body_bytes()?)
    }
}

impl RequestMetadata {
    fn changed(&self, previous: &Self, key: &str) -> bool {
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

    fn render(&self, key: &str, comment: &str) -> Result<Option<String>, Error> {
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
