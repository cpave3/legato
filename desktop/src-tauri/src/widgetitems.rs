//! What agents posted for the person (operator items, `for_operator`) in the
//! Legato widget's feed (src/voice/Feed.tsx), its second kind of item: each
//! server's items from its needs-you rail (`PATH`), so the rail's settings
//! apply. `app.rs` asks with the approvals (`watch_approvals`), keeps the
//! list, emits `voice-items` when it changes and keeps the widget shown
//! while any waits; the widget marks one done with `voice_item_done` and
//! opens the main window on one with `voice_item_open`. Only the choices
//! are here, and tested.

use serde::Serialize;
use serde_json::{json, Value};

/// The path that lists a server's needs-you rail: Attention as its rail
/// settings show it.
pub const PATH: &str = "/api/attention?view=rail";

/// The Attention producer of operator items.
pub const PRODUCER: &str = "operator_items";

/// One operator item and the server it waits on, as the widget gets it.
#[derive(Debug, Clone, PartialEq, Serialize)]
pub struct Listed {
    pub server: String,
    pub server_name: String,
    pub tint: Option<String>,
    pub item: Value,
}

/// A server's answer to `PATH`: its operator items, as the widget lists them.
pub fn listed(server: &str, name: &str, tint: Option<&str>, body: &Value) -> Vec<Listed> {
    body["items"]
        .as_array()
        .into_iter()
        .flatten()
        .filter(|i| i["producer"] == PRODUCER)
        .map(|i| Listed { server: server.into(), server_name: name.into(), tint: tint.map(String::from), item: i.clone() })
        .collect()
}

/// Whether two lists say the same: the same items, at the same revisions.
pub fn same(a: &[Listed], b: &[Listed]) -> bool {
    let key = |l: &Listed| (l.server.clone(), l.item["id"].clone(), l.item["source_revision"].clone());
    a.len() == b.len() && a.iter().zip(b).all(|(x, y)| key(x) == key(y))
}

/// The path of an item's Done.
pub fn done_path(item: &str) -> String {
    format!("/api/attention/{item}/actions/done")
}

/// The body of Done: the revision the widget showed, so a changed item is
/// refused (409) rather than marked done unseen.
pub fn done_body(revision: &str) -> Value {
    json!({ "expected_revision": revision })
}

/// The page of the main window an item opens: its agent, its task on the
/// board, else the Attention page.
pub fn page_for(kind: &str, target: &str) -> (&'static str, (&'static str, String)) {
    match kind {
        "agent" => ("/agents", ("task", target.to_string())),
        "task" => ("/board", ("highlight_task", target.to_string())),
        _ => ("/attention", ("from", "widget".to_string())),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn only_operator_items_of_the_rail_are_listed() {
        let body = json!({ "items": [
            { "id": "att-1", "producer": "operator_items", "title": "Run /to-tickets", "source_revision": "1" },
            { "id": "att-2", "producer": "approvals", "title": "Run a command", "source_revision": "1" },
            { "id": "att-3", "producer": "workflows", "title": "Step failed", "source_revision": "1" },
        ] });
        let l = listed("s1", "tessitura", Some("teal"), &body);
        assert_eq!(l.len(), 1);
        assert_eq!(l[0], Listed { server: "s1".into(), server_name: "tessitura".into(), tint: Some("teal".into()), item: body["items"][0].clone() });
        assert!(listed("s1", "tessitura", None, &json!({})).is_empty());
    }

    #[test]
    fn a_list_changes_with_an_item_or_a_revision() {
        let one = listed("s1", "t", None, &json!({ "items": [{ "id": "att-1", "producer": "operator_items", "source_revision": "1" }] }));
        let again = listed("s1", "t", None, &json!({ "items": [{ "id": "att-1", "producer": "operator_items", "source_revision": "1", "title": "x" }] }));
        let revised = listed("s1", "t", None, &json!({ "items": [{ "id": "att-1", "producer": "operator_items", "source_revision": "2" }] }));
        assert!(same(&one, &again));
        assert!(!same(&one, &revised));
        assert!(!same(&one, &[]));
    }

    #[test]
    fn done_names_the_revision_the_widget_showed() {
        assert_eq!(done_path("att-1"), "/api/attention/att-1/actions/done");
        assert_eq!(done_body("3"), json!({ "expected_revision": "3" }));
    }

    #[test]
    fn an_item_opens_its_agent_or_its_task() {
        assert_eq!(page_for("agent", "LEG-363"), ("/agents", ("task", "LEG-363".to_string())));
        assert_eq!(page_for("task", "LEG-1"), ("/board", ("highlight_task", "LEG-1".to_string())));
        assert_eq!(page_for("settings", "updates").0, "/attention");
    }
}
