//! Legato's desktop app: every Legato server in one window, each paired by a
//! four-word phrase with its certificate pinned, and approvals signed with a
//! security key. The window itself is in `shell`; the rest is testable alone.

pub mod pairing;
pub mod pin;
pub mod store;
pub mod webauthn;
pub mod app;
pub mod fido;
pub mod legato;
pub mod discovery;
pub mod updates;
pub mod touchid;
pub mod systrust;
pub mod bridge;
pub mod voice;
