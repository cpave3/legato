//! What a server's page and the shell say to each other, as the Legato app
//! for Android does (mobile/.../PageContract.kt, web/src/lib/mobileShell.ts):
//! the page is told it runs in an app that signs (`window.legatoShell`), and
//! its approval tray then asks the app to open its sheet on a signed approval
//! instead of signing in the page. The app reads only the approval's ID and
//! fetches the request itself, so a page cannot change what is signed.
//!
//! A server's page has no IPC. It reaches the app by navigating to a
//! `legato-desktop:` address, which the app reads and cancels; the address
//! carries a key only the page's own script knows, so a frame or a link in
//! the page cannot pass for it. The app answers with an event in the page.
//!
//! Where the app can record (see `voice`), the page is told so too, and its
//! dictation asks the app to record in its place, as the Android app does: a
//! page served over plain HTTP has no microphone of its own.
//!
//! The page is also told the app can pair its other servers as workers of
//! the page's server (`workers: 1`, see `workers`): it asks for the list
//! (`workerCandidates`) and to open a main's pairing request on one
//! (`openWorkerPairing`), which the app does after its own confirm, with the
//! token only it holds. The answers are `legato:shell-worker` events.

/// The scheme of what the page asks of the app.
pub const SCHEME: &str = "legato-desktop";

/// What a page asks of the app.
#[derive(Debug, PartialEq)]
pub enum PageMessage {
    Theme(crate::theme::Choice),
    /// Open the sheet on this approval.
    Approval(String),
    /// Enrol this computer to sign: the app's keys.
    Enroll,
    /// Start, stop or cancel a recording for the page's dictation.
    Voice(VoiceAction),
    /// The app's other servers, which could be workers of the page's. id is
    /// the page's (a string or a number), given back as it came.
    WorkerCandidates { id: serde_json::Value },
    /// Open a main's pairing request on one of the app's servers (its ID),
    /// after the person agrees. main is what the page calls its server.
    OpenWorkerPairing { id: serde_json::Value, server: String, main: String },
    /// Connect an MCP server of the page's server with OAuth: the app
    /// receives the browser's return and hands it to that server (see
    /// `oauthreceiver`). id is the page's, given back as it came.
    McpConnect { id: serde_json::Value, connection: String },
}

/// What the page asks of a recording.
#[derive(Debug, PartialEq, Clone, Copy)]
pub enum VoiceAction {
    Start,
    Stop,
    Cancel,
}

/// The script, run first in the page's top frame, that tells the page it
/// runs in the app and turns what it posts into a `legato-desktop:` address.
/// voice says whether the app records for the page.
pub fn script(key: &str, voice: bool, origin: &str) -> String {
    let key = serde_json::to_string(key).expect("a string encodes");
    let origin = serde_json::to_string(origin).expect("origin encodes");
    let voice = if voice { " voice: 1," } else { "" };
    format!(
        r#"(() => {{
  if (window !== window.top || location.origin !== {origin}) return;
  const key = {key};
  window.legatoShell = {{ approval: 1, enroll: 1, workers: 1, mcp: 1, theme: 2,{voice} key }};
  window.ReactNativeWebView = {{
    postMessage(text) {{
      let m;
      try {{ m = JSON.parse(text); }} catch (e) {{ return; }}
      if (window !== window.top || location.origin !== {origin} || !m || m.key !== key) return;
      const ask = (kind, rest) => {{ location.href = "{SCHEME}://" + kind + "?key=" + encodeURIComponent(key) + rest; }};
      const kind = m.legatoShell || m.type;
      const id = typeof m.id === "string" ? "&id=" + encodeURIComponent(m.id) : null;
      const wid = typeof m.id === "string" || typeof m.id === "number" ? "&id=" + encodeURIComponent(JSON.stringify(m.id)) : null;
      if (kind === "approval" && id) ask("approval", id);
      else if (kind === "theme" && id && ["server", "legato", "dracula", "ocular-light", "ocular-dark", "ocular-light-vivid", "ocular-dark-vivid"].includes(m.id)) ask("theme", id);
      else if (kind === "enroll") ask("enroll", "");
      else if (kind === "voice" && typeof m.action === "string") ask("voice", "&action=" + encodeURIComponent(m.action));
      else if (kind === "workerCandidates" && wid) ask("workers", wid);
      else if (kind === "openWorkerPairing" && wid && typeof m.server === "string")
        ask("worker-pairing", wid + "&server=" + encodeURIComponent(m.server) + "&main=" + encodeURIComponent(typeof m.main === "string" ? m.main : ""));
      else if (kind === "mcpConnect" && wid && typeof m.connection === "string")
        ask("mcp-connect", wid + "&connection=" + encodeURIComponent(m.connection));
    }},
  }};
}})();"#
    )
}

/// What a navigation to url asks of the app, when it is the page's own (it
/// carries key). None for anything else.
pub fn read(url: &str, key: &str) -> Option<PageMessage> {
    let url = reqwest::Url::parse(url).ok()?;
    if url.scheme() != SCHEME {
        return None;
    }
    let param = |name: &str| url.query_pairs().find(|(k, _)| k == name).map(|(_, v)| v.into_owned());
    if param("key").as_deref() != Some(key) {
        return None;
    }
    match url.host_str()? {
        "approval" => param("id").filter(|id| !id.is_empty()).map(PageMessage::Approval),
        "theme" => serde_json::from_value(serde_json::Value::String(param("id")?)).ok().map(PageMessage::Theme),
        "enroll" => Some(PageMessage::Enroll),
        "voice" => match param("action").as_deref() {
            Some("start") => Some(PageMessage::Voice(VoiceAction::Start)),
            Some("stop") => Some(PageMessage::Voice(VoiceAction::Stop)),
            Some("cancel") => Some(PageMessage::Voice(VoiceAction::Cancel)),
            _ => None,
        },
        "workers" => worker_id(param("id")?).map(|id| PageMessage::WorkerCandidates { id }),
        "worker-pairing" => {
            let id = worker_id(param("id")?)?;
            let server = param("server").filter(|s| !s.is_empty())?;
            Some(PageMessage::OpenWorkerPairing { id, server, main: param("main").unwrap_or_default() })
        }
        "mcp-connect" => {
            let id = worker_id(param("id")?)?;
            let connection = param("connection").filter(|c| crate::oauthreceiver::valid_connection(c))?;
            Some(PageMessage::McpConnect { id, connection })
        }
        _ => None,
    }
}

/// A worker request's id, as the page's script encodes it (JSON): a string
/// that is not empty, or a number.
fn worker_id(encoded: String) -> Option<serde_json::Value> {
    match serde_json::from_str::<serde_json::Value>(&encoded).ok()? {
        serde_json::Value::String(s) if s.is_empty() => None,
        id @ (serde_json::Value::String(_) | serde_json::Value::Number(_)) => Some(id),
        _ => None,
    }
}

/// Answers an MCP connect request, as the web UI listens for it:
/// `legato:shell-mcp` with the request's id and what came of it (never a
/// code or a key).
pub fn mcp_script(detail: &serde_json::Value) -> String {
    format!(r#"(() => {{ try {{ window.dispatchEvent(new CustomEvent("legato:shell-mcp", {{ detail: {detail} }})); }} catch (e) {{}} }})();"#)
}

/// Tells the page how the sheet on an approval came out, as the web UI
/// listens for it: `legato:shell-approval` with {id, status, option?, error?}.
pub fn result_script(detail: &serde_json::Value) -> String {
    format!(r#"(() => {{ try {{ window.dispatchEvent(new CustomEvent("legato:shell-approval", {{ detail: {detail} }})); }} catch (e) {{}} }})();"#)
}

/// Answers a worker request, as the web UI listens for it:
/// `legato:shell-worker` with the request's id and what came of it.
pub fn worker_script(detail: &serde_json::Value) -> String {
    format!(r#"(() => {{ try {{ window.dispatchEvent(new CustomEvent("legato:shell-worker", {{ detail: {detail} }})); }} catch (e) {{}} }})();"#)
}

/// What the app tells the page about the recording it makes for it.
#[derive(Debug, PartialEq)]
pub enum VoiceEvent {
    Started,
    /// How loud it is now, from 0 to 1.
    Level(f64),
    /// The whole recording, as a WAV file.
    Audio(Vec<u8>),
    Cancelled,
    Error(String),
}

/// Hands the page a part of its recording, as the web UI listens for it:
/// `legato:shell-voice` with {type, …}. The page decodes the audio and has
/// its server transcribe it.
pub fn voice_script(event: &VoiceEvent) -> String {
    use base64::Engine;
    let detail = match event {
        VoiceEvent::Started => serde_json::json!({ "type": "started" }),
        VoiceEvent::Level(level) => serde_json::json!({ "type": "level", "level": level }),
        VoiceEvent::Audio(wav) => serde_json::json!({ "type": "audio", "data": base64::engine::general_purpose::STANDARD.encode(wav), "mime": "audio/wav" }),
        VoiceEvent::Cancelled => serde_json::json!({ "type": "cancelled" }),
        VoiceEvent::Error(message) => serde_json::json!({ "type": "error", "message": message }),
    };
    format!(r#"(() => {{ try {{ window.dispatchEvent(new CustomEvent("legato:shell-voice", {{ detail: {detail} }})); }} catch (e) {{}} }})();"#)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    /// Runs a script in Node with a stand-in window and gives the detail of
    /// each `legato:shell-voice` event it dispatched. None without Node.
    fn heard_in_node(script: &str) -> Option<Vec<serde_json::Value>> {
        heard_event_in_node(script, "legato:shell-voice")
    }

    /// Runs a script in Node with a stand-in window and gives the detail of
    /// each event of that name it dispatched. None without Node.
    fn heard_event_in_node(script: &str, event: &str) -> Option<Vec<serde_json::Value>> {
        let program = format!(
            r#"
class CustomEvent {{ constructor(type, init) {{ this.type = type; this.detail = init.detail; }} }}
const heard = [];
const window = {{ dispatchEvent(e) {{ if (e.type === {event:?}) heard.push(e.detail); }} }};
(function (window, CustomEvent) {{ {script} }})(window, CustomEvent);
console.log(JSON.stringify(heard));
"#
        );
        let out = std::process::Command::new("node").arg("-e").arg(&program).output().ok()?;
        assert!(out.status.success(), "node: {}", String::from_utf8_lossy(&out.stderr));
        Some(serde_json::from_slice(&out.stdout).unwrap())
    }

    // What web/src/hooks/useVoiceRecorder.ts listens for.
    #[test]
    fn the_page_hears_each_part_of_a_recording_as_the_web_ui_expects() {
        let events = [
            (VoiceEvent::Started, json!({ "type": "started" })),
            (VoiceEvent::Level(0.25), json!({ "type": "level", "level": 0.25 })),
            (VoiceEvent::Audio(b"RIFF\x00\xff".to_vec()), json!({ "type": "audio", "data": "UklGRgD/", "mime": "audio/wav" })),
            (VoiceEvent::Cancelled, json!({ "type": "cancelled" })),
            (VoiceEvent::Error("no \"parecord\" </script> \u{2028}".into()), json!({ "type": "error", "message": "no \"parecord\" </script> \u{2028}" })),
        ];
        for (event, want) in events {
            let Some(heard) = heard_in_node(&voice_script(&event)) else { return };
            assert_eq!(heard, [want]);
        }
    }

    #[test]
    fn the_page_asks_for_the_sheet_on_an_approval_with_its_key() {
        assert_eq!(read("legato-desktop://approval?key=k1&id=ap-12", "k1"), Some(PageMessage::Approval("ap-12".into())));
        assert_eq!(read("legato-desktop://approval?key=k1&id=ap%2012", "k1"), Some(PageMessage::Approval("ap 12".into())));
        assert_eq!(read("legato-desktop://enroll?key=k1", "k1"), Some(PageMessage::Enroll));
    }

    #[test]
    fn the_page_asks_the_app_to_start_stop_or_cancel_a_recording_with_its_key() {
        assert_eq!(read("legato-desktop://voice?key=k1&action=start", "k1"), Some(PageMessage::Voice(VoiceAction::Start)));
        assert_eq!(read("legato-desktop://voice?key=k1&action=stop", "k1"), Some(PageMessage::Voice(VoiceAction::Stop)));
        assert_eq!(read("legato-desktop://voice?key=k1&action=cancel", "k1"), Some(PageMessage::Voice(VoiceAction::Cancel)));
        assert_eq!(read("legato-desktop://voice?key=other&action=start", "k1"), None, "another key");
        assert_eq!(read("legato-desktop://voice?key=k1&action=erase", "k1"), None, "something else");
        assert_eq!(read("legato-desktop://voice?key=k1", "k1"), None, "no action");
    }

    #[test]
    fn anything_without_the_pages_key_or_an_approval_is_not_read() {
        assert_eq!(read("legato-desktop://approval?key=other&id=ap-1", "k1"), None, "another key");
        assert_eq!(read("legato-desktop://approval?id=ap-1", "k1"), None, "no key");
        assert_eq!(read("legato-desktop://approval?key=k1", "k1"), None, "no approval");
        assert_eq!(read("legato-desktop://approval?key=k1&id=", "k1"), None, "an empty approval");
        assert_eq!(read("legato-desktop://shutdown?key=k1", "k1"), None, "something else");
        assert_eq!(read("https://erebus.local:3080/?key=k1&id=ap-1", "k1"), None, "a page");
    }

    #[test]
    fn the_answer_carries_the_outcome_as_json_whatever_it_says() {
        let detail = json!({ "id": "ap-1", "status": "error", "error": "it said \"no\" </script> \u{2028}" });
        let script = result_script(&detail);
        assert!(script.contains(r#"new CustomEvent("legato:shell-approval""#));
        let start = script.find("detail: ").unwrap() + "detail: ".len();
        let end = script.rfind(" })); }").unwrap();
        let back: serde_json::Value = serde_json::from_str(&script[start..end]).unwrap();
        assert_eq!(back, detail);
    }

    /// Runs the page's script in Node with a stand-in window, posts what the
    /// web UI posts, and says where the page was sent. None without Node.
    fn in_node(key: &str, voice: bool, top: bool, posts: &[&str]) -> Option<Vec<String>> {
        let program = format!(
            r#"
const sent = [];
const location = {{ origin: "https://one.local", set href(v) {{ sent.push(v); }} }};
const window = {{}};
window.top = {top} ? window : {{}};
(function (window, location) {{ {script} }})(window, location);
for (const p of {posts}) window.ReactNativeWebView && window.ReactNativeWebView.postMessage(p);
console.log(JSON.stringify({{ sent, shell: window.legatoShell || null }}));
"#,
            top = top,
            script = script(key, voice, "https://one.local"),
            posts = serde_json::to_string(posts).unwrap(),
        );
        let out = std::process::Command::new("node").arg("-e").arg(&program).output().ok()?;
        assert!(out.status.success(), "node: {}", String::from_utf8_lossy(&out.stderr));
        let v: serde_json::Value = serde_json::from_slice(&out.stdout).unwrap();
        if top && voice {
            assert_eq!(v["shell"], json!({ "approval": 1, "enroll": 1, "workers": 1, "mcp": 1, "theme": 2, "voice": 1, "key": key }));
        } else if top {
            assert_eq!(v["shell"], json!({ "approval": 1, "enroll": 1, "workers": 1, "mcp": 1, "theme": 2, "key": key }), "not told it can record");
        } else {
            assert!(v["shell"].is_null(), "a frame is not told");
        }
        Some(v["sent"].as_array().unwrap().iter().map(|s| s.as_str().unwrap().to_string()).collect())
    }

    #[test]
    fn an_off_origin_document_cannot_install_or_reuse_the_theme_bridge() {
        let script = script("k1", false, "https://one.local");
        for start_allowed in [false, true] {
            let program = format!(r#"
const sent = [];
const window = {{}}; window.top = window;
const location = {{ origin: {start:?}, set href(v) {{ sent.push(v); }} }};
{script}
if (Boolean(window.legatoShell) !== {start_allowed}) throw new Error("bridge installation origin guard");
location.origin = "https://other.local";
if (window.ReactNativeWebView) window.ReactNativeWebView.postMessage(JSON.stringify({{legatoShell:"theme", key:"k1", id:"dracula"}}));
console.log(JSON.stringify(sent));
"#, start = if start_allowed { "https://one.local" } else { "https://other.local" });
            let out = std::process::Command::new("node").arg("-e").arg(program).output().expect("Node runs bridge");
            assert!(out.status.success(), "{}", String::from_utf8_lossy(&out.stderr));
            assert_eq!(String::from_utf8_lossy(&out.stdout).trim(), "[]");
        }
    }

    #[test]
    fn theme_requests_use_the_key_and_strict_theme_ids() {
        let posts = [
            r#"{"legatoShell":"theme","type":"theme","key":"k1","id":"dracula"}"#,
            r#"{"legatoShell":"theme","type":"theme","key":"k1","id":"server"}"#,
            r#"{"legatoShell":"theme","key":"k1","id":"ocular-light-vivid"}"#,
            r#"{"legatoShell":"theme","key":"k1","id":"ocular-dark-vivid"}"#,
            r#"{"legatoShell":"theme","key":"wrong","id":"legato"}"#,
            r#"{"legatoShell":"theme","key":"k1","id":"unknown"}"#,
            r#"{"legatoShell":"theme","key":"k1","id":1}"#,
        ];
        let sent = in_node("k1", false, true, &posts).unwrap();
        assert_eq!(sent.len(), 4);
        for (url, id) in sent.iter().zip(["dracula", "server", "ocular-light-vivid", "ocular-dark-vivid"]) {
            let Some(PageMessage::Theme(theme)) = read(url, "k1") else { panic!("theme request not read: {url}") };
            assert_eq!(serde_json::to_value(theme).unwrap(), id);
        }
        assert_eq!(read("legato-desktop://theme?key=wrong&id=dracula", "k1"), None);
        assert_eq!(read("legato-desktop://theme?key=k1&id=unknown", "k1"), None);
        assert!(in_node("k1", false, false, &posts).unwrap().is_empty());
    }

    // What web/src/lib/mobileShell.ts posts.
    #[test]
    fn the_pages_script_turns_what_the_web_ui_posts_into_addresses_the_app_reads() {
        let posts = [
            r#"{"legatoShell":"approval","key":"k 1","id":"ap-7"}"#,
            r#"{"legatoShell":"enroll","key":"k 1"}"#,
            r#"{"legatoShell":"approval","key":"wrong","id":"ap-8"}"#,
            r#"{"legatoShell":"restore","key":"k 1","url":"https://x/"}"#,
            "not json",
        ];
        let Some(sent) = in_node("k 1", false, true, &posts) else { return };
        assert_eq!(sent.len(), 2, "{sent:?}");
        assert_eq!(read(&sent[0], "k 1"), Some(PageMessage::Approval("ap-7".into())));
        assert_eq!(read(&sent[1], "k 1"), Some(PageMessage::Enroll));
    }

    // What web/src/hooks/useVoiceRecorder.ts posts.
    #[test]
    fn the_pages_script_turns_the_web_uis_recording_requests_into_addresses_the_app_reads() {
        let posts = [
            r#"{"legatoShell":"voice","key":"k 1","action":"start"}"#,
            r#"{"legatoShell":"voice","key":"k 1","action":"stop"}"#,
            r#"{"legatoShell":"voice","key":"k 1","action":"cancel"}"#,
            r#"{"legatoShell":"voice","key":"wrong","action":"start"}"#,
            r#"{"legatoShell":"voice","key":"k 1"}"#,
        ];
        let Some(sent) = in_node("k 1", true, true, &posts) else { return };
        let read: Vec<_> = sent.iter().map(|s| read(s, "k 1")).collect();
        assert_eq!(read, [Some(PageMessage::Voice(VoiceAction::Start)), Some(PageMessage::Voice(VoiceAction::Stop)), Some(PageMessage::Voice(VoiceAction::Cancel))], "{sent:?}");
    }

    #[test]
    fn a_frame_in_the_page_is_not_told_it_runs_in_the_app() {
        let Some(sent) = in_node("k1", true, false, &[r#"{"legatoShell":"approval","key":"k1","id":"ap-7"}"#]) else { return };
        assert!(sent.is_empty());
    }

    #[test]
    fn the_page_asks_to_connect_an_mcp_server_of_its_own_server() {
        assert_eq!(
            read("legato-desktop://mcp-connect?key=k1&id=%22m1%22&connection=notion", "k1"),
            Some(PageMessage::McpConnect { id: json!("m1"), connection: "notion".into() })
        );
        assert_eq!(read("legato-desktop://mcp-connect?key=k2&id=%22m1%22&connection=notion", "k1"), None, "another key");
        assert_eq!(read("legato-desktop://mcp-connect?key=k1&id=%22m1%22&connection=Bad%20Name", "k1"), None, "not a connection name");
        assert_eq!(read("legato-desktop://mcp-connect?key=k1&connection=notion", "k1"), None, "no id");
    }

    #[test]
    fn the_page_asks_for_the_apps_servers_and_to_open_a_workers_pairing_with_its_key() {
        assert_eq!(read("legato-desktop://workers?key=k1&id=%22w1%22", "k1"), Some(PageMessage::WorkerCandidates { id: json!("w1") }));
        assert_eq!(read("legato-desktop://workers?key=k1&id=7", "k1"), Some(PageMessage::WorkerCandidates { id: json!(7) }), "a number");
        assert_eq!(
            read("legato-desktop://worker-pairing?key=k1&id=%22w2%22&server=srv-a&main=work%20station", "k1"),
            Some(PageMessage::OpenWorkerPairing { id: json!("w2"), server: "srv-a".into(), main: "work station".into() })
        );
        assert_eq!(
            read("legato-desktop://worker-pairing?key=k1&id=%22w2%22&server=srv-a", "k1"),
            Some(PageMessage::OpenWorkerPairing { id: json!("w2"), server: "srv-a".into(), main: String::new() }),
            "no name for the main"
        );
        assert_eq!(read("legato-desktop://workers?key=other&id=%22w1%22", "k1"), None, "another key");
        assert_eq!(read("legato-desktop://workers?key=k1", "k1"), None, "no id");
        assert_eq!(read("legato-desktop://workers?key=k1&id=%22%22", "k1"), None, "an empty id");
        assert_eq!(read("legato-desktop://workers?key=k1&id=%7B%7D", "k1"), None, "an id that is neither a string nor a number");
        assert_eq!(read("legato-desktop://worker-pairing?key=k1&id=%22w2%22", "k1"), None, "no server");
        assert_eq!(read("legato-desktop://worker-pairing?key=k1&server=srv-a", "k1"), None, "no id");
    }

    // What web/src/lib/mobileShell.ts posts: the kind as legatoShell and as
    // type (the bridge contract's name), with the page's key.
    #[test]
    fn the_pages_script_turns_the_web_uis_worker_requests_into_addresses_the_app_reads() {
        let posts = [
            r#"{"legatoShell":"workerCandidates","type":"workerCandidates","key":"k 1","id":"w1"}"#,
            r#"{"type":"openWorkerPairing","key":"k 1","id":"w2","server":"srv-a","main":"work & station"}"#,
            r#"{"type":"workerCandidates","key":"wrong","id":"w3"}"#,
            r#"{"type":"openWorkerPairing","key":"k 1","id":"w4"}"#,
            r#"{"type":"workerCandidates","key":"k 1"}"#,
            r#"{"legatoShell":"workerCandidates","key":"k 1","id":5}"#,
        ];
        let Some(sent) = in_node("k 1", false, true, &posts) else { return };
        let read: Vec<_> = sent.iter().map(|s| read(s, "k 1")).collect();
        assert_eq!(
            read,
            [
                Some(PageMessage::WorkerCandidates { id: json!("w1") }),
                Some(PageMessage::OpenWorkerPairing { id: json!("w2"), server: "srv-a".into(), main: "work & station".into() }),
                Some(PageMessage::WorkerCandidates { id: json!(5) }),
            ],
            "{sent:?}"
        );
    }

    // What web/src/lib/mobileShell.ts listens for.
    #[test]
    fn the_page_hears_the_answer_to_a_worker_request_as_the_web_ui_expects() {
        let details = [
            json!({ "id": "w1", "servers": [{ "key": "srv-a", "name": "legato-1 </script>", "url": "https://legato-1.lan:3080" }] }),
            json!({ "id": "w2", "address": "https://legato-1.lan:3080", "phrase": ["bucket", "ladle", "spree", "oyster"], "expires_at": "2026-11-05T09:05:00Z" }),
            json!({ "id": "w3", "cancelled": true }),
            json!({ "id": "w4", "error": "it said \"no\" \u{2028}" }),
        ];
        for detail in details {
            let Some(heard) = heard_event_in_node(&worker_script(&detail), "legato:shell-worker") else { return };
            assert_eq!(heard, [detail]);
        }
    }
}
