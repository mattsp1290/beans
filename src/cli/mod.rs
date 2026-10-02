//! Command adapters are assembled in WP6.
mod actor;
pub use actor::Actor;
mod commands;
pub use commands::{command, execute};
mod dispatch;
mod import_commands;
mod plan_commands;
mod read_commands;
mod record_commands;

mod manual;
mod schema;
