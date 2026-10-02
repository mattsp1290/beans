//! Command adapters are assembled in WP6.
mod actor;
pub use actor::Actor;
mod commands;
pub use commands::{command, execute};
