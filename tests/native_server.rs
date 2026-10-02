//! Real HTTP, native Git/CLI and lifecycle regressions.
include!("native_server/support.rs");

#[path = "native_server/api.rs"]
mod api;
#[path = "native_server/lifecycle.rs"]
mod lifecycle;
#[path = "native_server/markdown.rs"]
mod markdown;
