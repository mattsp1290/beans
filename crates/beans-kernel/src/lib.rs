//! Same-source verified runtime decisions used by later application packages.
//! Proof annotations are erased during normal Cargo compilation. The Git and
//! codec adapters must derive these inputs from real repository/file state.

pub mod retry;
