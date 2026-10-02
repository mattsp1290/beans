//! Markdown adapters shared by indexing and later rendering.
mod links;
pub use links::{Link, links};
mod render;
pub use render::{Rendered, TocEntry, encode, render};
