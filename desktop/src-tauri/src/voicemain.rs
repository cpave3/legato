//! Which server's Reeve the app talks to, and whether it can listen now.
//!
//! Each paired server answers `GET /api/reeve/voice` with its instance (ID,
//! name, role) and whether its voice is ready (`service.VoiceReadiness`).
//! The main of the cluster is the one to talk to: exactly one server that
//! says `main`; else the only server; else the one the person chose in
//! Servers & keys ("The Reeve to talk to"), kept by instance ID so a new
//! address does not lose it.

use serde::{Deserialize, Serialize};

/// A Legato instance, as `/api/reeve/voice` names it.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct Instance {
    pub id: String,
    pub name: String,
    /// `main`, `worker`, `main and worker` or `standalone`.
    pub role: String,
}

/// What `/api/reeve/voice` answers. Only what the app reads.
#[derive(Debug, Clone, Default, PartialEq, Deserialize)]
pub struct Readiness {
    pub instance: Instance,
    #[serde(default)]
    pub ready: bool,
    /// One line saying why not, for the person.
    #[serde(default)]
    pub reason: String,
    /// The setting to change, for Open settings.
    #[serde(default)]
    pub setting: String,
}

/// One of the app's servers and the instance it last said it is, if it
/// has answered.
#[derive(Debug, Clone, PartialEq, Serialize)]
pub struct Seen {
    /// The app's ID for the server.
    pub server: String,
    pub instance: Option<Instance>,
}

/// Which server to talk to.
#[derive(Debug, Clone, PartialEq, Serialize)]
#[serde(tag = "kind", content = "server", rename_all = "lowercase")]
pub enum Choice {
    /// This server (the app's ID).
    Server(String),
    /// Several could be; the person picks one in Servers & keys.
    Ask,
    /// The app has no server.
    None,
}

/// The server to talk to. `saved` is the instance ID the person chose,
/// used only when the roles do not decide.
pub fn choose(seen: &[Seen], saved: Option<&str>) -> Choice {
    let mains: Vec<&Seen> = seen.iter().filter(|s| s.instance.as_ref().is_some_and(|i| i.role == "main")).collect();
    if let [main] = mains.as_slice() {
        return Choice::Server(main.server.clone());
    }
    match seen {
        [] => Choice::None,
        [only] => Choice::Server(only.server.clone()),
        _ => match saved.and_then(|id| seen.iter().find(|s| s.instance.as_ref().is_some_and(|i| i.id == id))) {
            Some(s) => Choice::Server(s.server.clone()),
            None => Choice::Ask,
        },
    }
}

/// Why the app does not listen, in one line; None when it can. `name` is
/// the app's name for the server, used when it could not be asked.
pub fn not_ready(answer: &Result<Readiness, String>, name: &str, microphone: bool) -> Option<String> {
    match answer {
        Err(_) => Some(format!("Can't reach {name}")),
        Ok(r) if !r.ready => Some(if r.reason.is_empty() { format!("Voice isn't ready on {name}") } else { r.reason.clone() }),
        Ok(_) if !microphone => Some("No microphone on this computer".into()),
        Ok(_) => None,
    }
}

/// How often the app asks every server again, besides when its servers
/// change.
pub const REFRESH: std::time::Duration = std::time::Duration::from_secs(600);

/// How long the status call on the key may take.
pub const STATUS_TIMEOUT: std::time::Duration = std::time::Duration::from_secs(2);

#[cfg(test)]
mod tests {
    use super::*;

    fn seen(server: &str, id: &str, role: &str) -> Seen {
        Seen { server: server.into(), instance: Some(Instance { id: id.into(), name: server.into(), role: role.into() }) }
    }

    fn silent(server: &str) -> Seen {
        Seen { server: server.into(), instance: None }
    }

    #[test]
    fn the_one_main_is_talked_to() {
        let servers = [seen("s-1", "i-1", "worker"), seen("s-2", "i-2", "main"), silent("s-3")];
        assert_eq!(choose(&servers, Some("i-1")), Choice::Server("s-2".into()));
    }

    #[test]
    fn with_no_main_the_only_server_is_talked_to_even_unanswered() {
        assert_eq!(choose(&[seen("s-1", "i-1", "standalone")], None), Choice::Server("s-1".into()));
        assert_eq!(choose(&[silent("s-1")], None), Choice::Server("s-1".into()));
    }

    #[test]
    fn otherwise_the_person_chooses_and_the_choice_is_kept_by_instance() {
        let two_mains = [seen("s-1", "i-1", "main"), seen("s-2", "i-2", "main")];
        assert_eq!(choose(&two_mains, None), Choice::Ask);
        assert_eq!(choose(&two_mains, Some("i-2")), Choice::Server("s-2".into()));

        let both = [seen("s-1", "i-1", "main and worker"), seen("s-2", "i-2", "worker")];
        assert_eq!(choose(&both, None), Choice::Ask);
        assert_eq!(choose(&both, Some("i-1")), Choice::Server("s-1".into()));
        // A choice of an instance the app no longer has asks again.
        assert_eq!(choose(&both, Some("i-gone")), Choice::Ask);
    }

    #[test]
    fn with_no_server_there_is_none() {
        assert_eq!(choose(&[], Some("i-1")), Choice::None);
    }

    #[test]
    fn the_status_route_is_read() {
        let r: Readiness = serde_json::from_str(
            r#"{"instance":{"id":"i-1","name":"tessitura","role":"main"},"reeve":{"name":"The Reeve","state":"suspended","busy":false},
               "speech":{"enabled":true,"command":true,"last_error":""},"transcription":{"enabled":true,"reachable":false},
               "session":{"open":false,"device":""},"ready":false,"reason":"Whisper isn't answering on tessitura","setting":"voice.whisper_url"}"#,
        )
        .unwrap();
        assert_eq!(r.instance, Instance { id: "i-1".into(), name: "tessitura".into(), role: "main".into() });
        assert!(!r.ready);
        assert_eq!(r.setting, "voice.whisper_url");
    }

    #[test]
    fn the_line_says_why_it_will_not_listen() {
        let ready = Readiness { ready: true, ..Readiness::default() };
        assert_eq!(not_ready(&Ok(ready.clone()), "tessitura", true), None);
        assert_eq!(not_ready(&Ok(ready), "tessitura", false).as_deref(), Some("No microphone on this computer"));
        assert_eq!(not_ready(&Err("timed out".into()), "tessitura", true).as_deref(), Some("Can't reach tessitura"));
        let off = Readiness { reason: "The Reeve's voice is off on tessitura".into(), ..Readiness::default() };
        assert_eq!(not_ready(&Ok(off), "tessitura", true).as_deref(), Some("The Reeve's voice is off on tessitura"));
    }
}
