//! Lossless domain documents and their semantic views.
pub mod authored_yaml;
pub mod config;
pub mod duration;
mod extra_encode;
pub(crate) mod file_io;
pub mod frontmatter;
mod go_print;
pub mod handoff;
pub mod id;
pub mod issue;
mod issue_encode;
pub mod log;
pub mod memory;
pub mod plan;
pub mod request;
mod request_document;
mod request_encode;
mod splicing;
pub mod template;
pub mod text;
pub mod workflow;
mod yaml;
mod yaml_decode;
mod yaml_render;
pub(crate) mod yaml_string;
pub(crate) mod yaml_value;

mod yaml_reader;

mod byte_edit;
