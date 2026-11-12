//! The approvals the voice widget shows: every paired server's pending
//! approvals, asked for every `EVERY`, so one shows within a second or two
//! of being raised, with no conversation running. `app.rs` asks
//! (`watch_approvals`), keeps the list, emits `voice-approvals` when it
//! changes and keeps the widget shown while any waits; the widget decides
//! with `voice_approval_decide`, or signs with `prepare` and `sign` as the
//! main window's sheet does. Only the choices are here, and tested.

use std::time::Duration;

use serde::Serialize;
use serde_json::{json, Value};

/// How often each server is asked.
pub const EVERY: Duration = Duration::from_millis(1500);

/// How long one server may take to answer before it is asked again later.
pub const WITHIN: Duration = Duration::from_secs(3);

/// The path that lists a server's pending approvals.
pub const PATH: &str = "/api/approvals?status=pending";

/// One pending approval and the server it waits on, as the widget gets it.
#[derive(Debug, Clone, PartialEq, Serialize)]
pub struct Listed {
    pub server: String,
    pub server_name: String,
    pub tint: Option<String>,
    pub approval: Value,
}

/// A server's answer to `PATH`, as the widget lists it.
pub fn listed(server: &str, name: &str, tint: Option<&str>, body: &Value) -> Vec<Listed> {
    body["approvals"]
        .as_array()
        .into_iter()
        .flatten()
        .map(|a| Listed { server: server.into(), server_name: name.into(), tint: tint.map(String::from), approval: a.clone() })
        .collect()
}

/// Whether two lists say the same: the same approvals, at the same revisions.
pub fn same(a: &[Listed], b: &[Listed]) -> bool {
    let key = |l: &Listed| (l.server.clone(), l.approval["id"].clone(), l.approval["revision"].clone());
    a.len() == b.len() && a.iter().zip(b).all(|(x, y)| key(x) == key(y))
}

/// The decision the widget posts: an allow names one of the approval's
/// options, a deny none. Made by the person at the desktop, as the tray's.
pub fn decision(decision: &str, option: Option<&str>) -> Result<Value, String> {
    match (decision, option) {
        ("allow", Some(option)) => Ok(json!({ "decision": "allow", "option": option, "by": "desktop" })),
        ("allow", None) => Err("An allow names one of the approval's options.".into()),
        ("deny", _) => Ok(json!({ "decision": "deny", "by": "desktop" })),
        (other, _) => Err(format!("The widget does not {other}: open the approval in Legato.")),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_servers_approvals_are_listed_with_the_server() {
        let body = json!({ "approvals": [{ "id": "ap-1", "title": "Use github", "revision": 1 }, { "id": "ap-2", "title": "Run a command", "revision": 3 }] });
        let l = listed("s1", "tessitura", Some("teal"), &body);
        assert_eq!(l.len(), 2);
        assert_eq!(l[0], Listed { server: "s1".into(), server_name: "tessitura".into(), tint: Some("teal".into()), approval: body["approvals"][0].clone() });
        assert!(listed("s1", "tessitura", None, &json!({})).is_empty());
    }

    #[test]
    fn a_list_changes_with_an_approval_or_a_revision() {
        let one = listed("s1", "t", None, &json!({ "approvals": [{ "id": "ap-1", "revision": 1 }] }));
        let again = listed("s1", "t", None, &json!({ "approvals": [{ "id": "ap-1", "revision": 1, "title": "x" }] }));
        let revised = listed("s1", "t", None, &json!({ "approvals": [{ "id": "ap-1", "revision": 2 }] }));
        let other = listed("s2", "t", None, &json!({ "approvals": [{ "id": "ap-1", "revision": 1 }] }));
        assert!(same(&one, &again));
        assert!(!same(&one, &revised));
        assert!(!same(&one, &other));
        assert!(!same(&one, &[]));
    }

    #[test]
    fn the_widget_allows_with_an_option_and_denies_with_none() {
        assert_eq!(decision("allow", Some("once")).unwrap(), json!({ "decision": "allow", "option": "once", "by": "desktop" }));
        assert_eq!(decision("deny", None).unwrap(), json!({ "decision": "deny", "by": "desktop" }));
        assert_eq!(decision("deny", Some("once")).unwrap(), json!({ "decision": "deny", "by": "desktop" }));
        assert!(decision("allow", None).is_err());
        // An answer or a dismissal is the main window's.
        assert!(decision("answer", None).is_err());
        assert!(decision("dismiss", None).is_err());
    }
}
