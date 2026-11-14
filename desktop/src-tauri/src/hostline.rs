//! The Reeve's line playing on the main's speakers, for the widget.
//!
//! A line that answers no spoken message (the Reeve speaking unprompted,
//! say after an agent finished) plays on the main's host, not over the
//! widget's conversation, so the conversation never tells the widget. The
//! main says what became of every line on its `/ws` as `reeve_spoke`
//! (`{status, line, content}`): `playing` when the host's player starts a
//! line, then `played`, `failed` or `dropped` under the same `line`.
//! `app.rs` (`watch_host_lines`) keeps a WebSocket to the voice main, hands
//! each message here, and emits `voice-host-line` with what changed. Only
//! the choices are here, and tested.

use std::collections::VecDeque;
use std::time::{Duration, Instant};

use serde::Serialize;
use serde_json::Value;

/// The path the app listens on.
pub const PATH: &str = "/ws";

/// How long a line is shown when its end never comes (a message lost, a
/// main that stopped), checked every CHECK, so up to LONGEST + CHECK. The
/// Reeve says 400 characters at most, well under this.
pub const LONGEST: Duration = Duration::from_secs(90);

/// How long to wait before opening the socket again after it closed or
/// could not open.
pub const RETRY: Duration = Duration::from_secs(5);

/// How often the app checks the voice main is the same, the line has not
/// outlived LONGEST, and the socket still carries (a ping).
pub const CHECK: Duration = Duration::from_secs(15);

/// What the widget is told (`voice-host-line`): the line `playing`, with
/// its text, or `ended`.
#[derive(Debug, Clone, PartialEq, Serialize)]
pub struct Change {
    pub id: String,
    pub text: String,
    pub status: &'static str,
}

/// How many ended lines are remembered, so a `playing` that arrives after
/// its own end is not shown.
const ENDS_KEPT: usize = 64;

/// The line playing on the main's speakers, if any.
///
/// The main sends each broadcast on a goroutine of its own
/// (`Hub.Broadcast`), so messages can arrive out of order: a line's end
/// before its `playing`, or an earlier line's `playing` after a later
/// one's. The host plays lines one at a time, in their numbers' order.
#[derive(Debug, Default)]
pub struct HostLine {
    playing: Option<(i64, Instant)>,
    /// The lines whose end has come, newest last.
    ended: VecDeque<i64>,
}

impl HostLine {
    /// One message from the main's `/ws`: a change for the widget, or
    /// None for anything else (other messages, other lines' fates).
    pub fn on_message(&mut self, message: &Value, now: Instant) -> Option<Change> {
        if message["type"] != "reeve_spoke" {
            return None;
        }
        // A main from before line numbers: nothing tells one line's end
        // from another's, so nothing is shown.
        let line = message["line"].as_i64()?;
        match message["status"].as_str()? {
            "playing" => {
                if self.ended.contains(&line) || self.playing.is_some_and(|(n, _)| n >= line) {
                    return None;
                }
                self.playing = Some((line, now));
                Some(Change { id: line.to_string(), text: message["content"].as_str().unwrap_or_default().into(), status: "playing" })
            }
            "played" | "failed" | "dropped" => {
                if self.ended.len() == ENDS_KEPT {
                    self.ended.pop_front();
                }
                self.ended.push_back(line);
                if self.playing.is_some_and(|(n, _)| n == line) {
                    self.end()
                } else {
                    None
                }
            }
            _ => None,
        }
    }

    /// The line playing has outlived LONGEST: it ends.
    pub fn check(&mut self, now: Instant) -> Option<Change> {
        match self.playing {
            Some((_, since)) if now.duration_since(since) >= LONGEST => self.end(),
            _ => None,
        }
    }

    /// The socket closed: nothing says the line is still playing, and the
    /// next socket may reach a main restarted with its numbers from 1.
    pub fn lost(&mut self) -> Option<Change> {
        self.ended.clear();
        self.end()
    }

    fn end(&mut self) -> Option<Change> {
        let (line, _) = self.playing.take()?;
        Some(Change { id: line.to_string(), text: String::new(), status: "ended" })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    /// reeve_spoke as the main writes it (internal/server's test reads the
    /// same file): a line playing, then played.
    const MESSAGES: &str = include_str!(concat!(env!("CARGO_MANIFEST_DIR"), "/../../internal/server/testdata/reeve-spoke/messages.json"));

    #[test]
    fn the_mains_messages_show_a_line_and_end_it() {
        let messages: Vec<Value> = serde_json::from_str(MESSAGES).unwrap();
        let now = Instant::now();
        let mut h = HostLine::default();
        assert_eq!(h.on_message(&messages[0], now), playing(7, "The build on LEG-160 passed."));
        assert_eq!(h.on_message(&messages[1], now), ended(7));
    }

    fn spoke(status: &str, line: i64, text: &str) -> Value {
        json!({ "type": "reeve_spoke", "task_id": "reeve-1", "content": text, "status": status, "line": line })
    }

    fn playing(line: i64, text: &str) -> Option<Change> {
        Some(Change { id: line.to_string(), text: text.into(), status: "playing" })
    }

    fn ended(line: i64) -> Option<Change> {
        Some(Change { id: line.to_string(), text: String::new(), status: "ended" })
    }

    #[test]
    fn a_line_shows_from_playing_until_it_has_played() {
        let now = Instant::now();
        let mut h = HostLine::default();
        assert_eq!(h.on_message(&spoke("made", 3, "The build passed."), now), None);
        assert_eq!(h.on_message(&spoke("playing", 3, "The build passed."), now), playing(3, "The build passed."));
        assert_eq!(h.on_message(&spoke("played", 3, "The build passed."), now), ended(3));
        assert_eq!(h.on_message(&spoke("played", 3, "The build passed."), now), None);
    }

    #[test]
    fn a_line_that_fails_or_is_dropped_while_playing_ends_too() {
        let now = Instant::now();
        for end in ["failed", "dropped"] {
            let mut h = HostLine::default();
            h.on_message(&spoke("playing", 1, "a"), now);
            assert_eq!(h.on_message(&spoke(end, 1, "a"), now), ended(1), "{end}");
        }
    }

    #[test]
    fn another_lines_end_does_not_end_the_one_playing() {
        let now = Instant::now();
        let mut h = HostLine::default();
        h.on_message(&spoke("playing", 4, "Done."), now);
        // A line made meanwhile that failed, or was dropped, or went to a
        // device: not the one on the speakers.
        for status in ["failed", "dropped", "sent", "made", "played"] {
            assert_eq!(h.on_message(&spoke(status, 5, "other"), now), None, "{status}");
        }
        assert_eq!(h.on_message(&spoke("played", 4, "Done."), now), ended(4));
    }

    #[test]
    fn the_next_line_playing_replaces_the_last() {
        let now = Instant::now();
        let mut h = HostLine::default();
        h.on_message(&spoke("playing", 1, "one"), now);
        assert_eq!(h.on_message(&spoke("playing", 2, "two"), now), playing(2, "two"));
        assert_eq!(h.on_message(&spoke("played", 1, "one"), now), None);
        assert_eq!(h.on_message(&spoke("played", 2, "two"), now), ended(2));
    }

    // The main sends each broadcast on its own goroutine (Hub.Broadcast),
    // so a line's end can overtake its playing.
    #[test]
    fn an_end_that_overtakes_its_playing_keeps_the_line_from_showing() {
        let now = Instant::now();
        let mut h = HostLine::default();
        for end in ["failed", "played", "dropped"] {
            assert_eq!(h.on_message(&spoke(end, 3, "a"), now), None, "{end}");
        }
        assert_eq!(h.on_message(&spoke("playing", 3, "a"), now), None);
        // A later line made and failed while an earlier one waited to play
        // does not keep the earlier one from showing.
        assert_eq!(h.on_message(&spoke("failed", 6, "b"), now), None);
        assert_eq!(h.on_message(&spoke("playing", 5, "c"), now), playing(5, "c"));
    }

    #[test]
    fn an_earlier_lines_late_playing_does_not_replace_a_later_one() {
        let now = Instant::now();
        let mut h = HostLine::default();
        h.on_message(&spoke("playing", 8, "later"), now);
        assert_eq!(h.on_message(&spoke("playing", 7, "earlier"), now), None);
        assert_eq!(h.on_message(&spoke("played", 8, "later"), now), ended(8));
    }

    #[test]
    fn a_new_socket_forgets_the_old_mains_numbers() {
        let now = Instant::now();
        let mut h = HostLine::default();
        h.on_message(&spoke("played", 1, "before a restart"), now);
        h.lost();
        assert_eq!(h.on_message(&spoke("playing", 1, "after"), now), playing(1, "after"));
    }

    #[test]
    fn other_messages_and_a_main_without_line_numbers_change_nothing() {
        let now = Instant::now();
        let mut h = HostLine::default();
        assert_eq!(h.on_message(&json!({ "type": "agent_list", "agents": [] }), now), None);
        assert_eq!(h.on_message(&json!({ "type": "reeve_spoke", "status": "playing", "content": "old main" }), now), None);
        assert_eq!(h.on_message(&json!({ "type": "reeve_spoke", "status": "played", "content": "old main" }), now), None);
    }

    #[test]
    fn a_line_whose_end_never_comes_ends_after_the_longest() {
        let now = Instant::now();
        let mut h = HostLine::default();
        h.on_message(&spoke("playing", 9, "lost"), now);
        assert_eq!(h.check(now + LONGEST - Duration::from_millis(1)), None);
        assert_eq!(h.check(now + LONGEST), ended(9));
        assert_eq!(h.check(now + LONGEST * 2), None);
    }

    #[test]
    fn a_closed_socket_ends_the_line_playing() {
        let now = Instant::now();
        let mut h = HostLine::default();
        assert_eq!(h.lost(), None);
        h.on_message(&spoke("playing", 2, "x"), now);
        assert_eq!(h.lost(), ended(2));
        assert_eq!(h.lost(), None);
    }
}
