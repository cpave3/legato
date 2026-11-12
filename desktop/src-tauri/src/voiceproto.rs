//! The voice session's messages (`GET /api/reeve/voice/session`), as the
//! main's `service.VoiceIn` and `service.VoiceOut` have them. Text frames
//! are these, as JSON; binary frames are audio, 16 kHz mono s16le PCM.
//! `internal/server/testdata/voice-session/` pins them for the Go server,
//! this app and the web UI alike.

use serde::{Deserialize, Serialize};

/// What the device says.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum DeviceMessage {
    /// The first frame: who listens, and whether it plays lines.
    Hello { device: String, client: String, play: bool },
    /// Start a turn.
    Listen,
    /// Send the turn now.
    Send,
    /// Drop this turn.
    Cancel,
    /// Stop speaking and drop the lines; while thinking, interrupt the Reeve.
    Stop,
    Playing { line: String },
    Played { line: String },
    /// End the conversation.
    Bye,
}

/// What the main says. A message of a type this app does not know is
/// `Unknown`, so a newer main does not break the session.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum MainMessage {
    /// `idle`, `listening`, `thinking` or `speaking`; an idle state may say
    /// why (`no_speech`, `nothing_heard`, `cancelled`, `no_answer`, `failed`).
    State {
        state: String,
        #[serde(default, skip_serializing_if = "Option::is_none")]
        reason: Option<String>,
    },
    /// The pause bar: ms of quiet so far, of the pause that ends the turn.
    Pause {
        #[serde(default, skip_serializing_if = "is_zero")]
        ms: u64,
        of: u64,
    },
    /// Words so far.
    Partial { text: String },
    /// The turn as sent to the Reeve.
    Heard { text: String },
    /// What the Reeve is doing, one line.
    Activity { text: String },
    /// A line ready to play: fetch url (relative, with the web token).
    Line { id: String, text: String, url: String },
    /// The turn ended with no line spoken.
    Written,
    /// The session is over: taken over, voice turned off, bye.
    Ended { reason: String },
    #[serde(other)]
    Unknown,
}

fn is_zero(n: &u64) -> bool {
    *n == 0
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::Value;

    const MESSAGES: &str = include_str!(concat!(env!("CARGO_MANIFEST_DIR"), "/../../internal/server/testdata/voice-session/messages.json"));

    fn fixture(side: &str) -> Vec<Value> {
        let all: Value = serde_json::from_str(MESSAGES).unwrap();
        all[side].as_array().unwrap().clone()
    }

    #[test]
    fn every_message_the_device_sends_is_written_as_the_fixture_has_it() {
        for want in fixture("device") {
            let message: DeviceMessage = serde_json::from_value(want.clone()).unwrap_or_else(|e| panic!("{want}: {e}"));
            assert_eq!(serde_json::to_value(&message).unwrap(), want);
        }
    }

    #[test]
    fn every_message_the_main_sends_is_read() {
        for want in fixture("main") {
            let message: MainMessage = serde_json::from_value(want.clone()).unwrap_or_else(|e| panic!("{want}: {e}"));
            assert_ne!(message, MainMessage::Unknown, "{want}");
            assert_eq!(serde_json::to_value(&message).unwrap(), want);
        }
    }

    #[test]
    fn a_message_from_a_newer_main_is_read_as_unknown() {
        let message: MainMessage = serde_json::from_str(r#"{"type":"word_timings","at":[1,2]}"#).unwrap();
        assert_eq!(message, MainMessage::Unknown);
    }

    #[test]
    fn a_pause_without_ms_has_none_yet() {
        let message: MainMessage = serde_json::from_str(r#"{"type":"pause","of":1000}"#).unwrap();
        assert_eq!(message, MainMessage::Pause { ms: 0, of: 1000 });
    }
}
