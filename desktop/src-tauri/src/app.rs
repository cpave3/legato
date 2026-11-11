//! The window and what the shell asks of it. One frameless window holds the
//! shell (title bar, dock, panels, the approval sheet) across its whole
//! area, and a webview per server over the content area, made when first
//! shown and kept, so switching is instant and one server going down leaves
//! the others alone. A server's webview trusts its pinned certificate only.

use std::collections::{HashMap, HashSet};
use std::sync::Mutex;

use base64::engine::general_purpose::{STANDARD, URL_SAFE_NO_PAD};
use base64::Engine;
use rand::RngCore;
use serde::Serialize;
use serde_json::{json, Value};
use tauri::{AppHandle, Emitter, LogicalPosition, LogicalSize, Manager, State, WebviewBuilder, WebviewUrl, WindowBuilder, WindowEvent};

use crate::legato::{self, Client};
use crate::applink::{self, ImportLink};
use crate::store::{ImportTarget, Keyring, Server, Store};
use crate::updates;
use crate::webauthn::{self, Attachment, ORIGIN, RP_ID};
use crate::discovery::{Browser, Found};
use crate::links;
use crate::bridge::{self, PageMessage, VoiceAction, VoiceEvent};
use crate::{fido, systrust, touchid, voice, voiceclient, voicekey, voicemain, workers};
use crate::voiceclient::{Command, Ui};
use crate::voicekey::{Backend, Input, VoiceKey};

// The shell's chrome, in logical pixels: the title bar across the top and
// the dock down the left. Server pages fill the rest.
const TITLE_BAR: f64 = 40.0;
const DOCK: f64 = 68.0;
// A strip along the right and bottom edges left to the shell, where it
// catches a drag to resize the frameless window.
const EDGE: f64 = 4.0;

/// A decision challenge fetched for one approval, ready to sign. The sheet
/// draws the request from it; the signature covers exactly this.
struct Prepared {
    device: String,
    challenge: String,
    canonical: String,
    rp_id: String,
    origin: String,
    credential: Vec<u8>,
    /// A Touch ID key's Secure Enclave blob; None for a security key.
    enclave: Option<Vec<u8>>,
}

pub struct Desktop {
    store: Mutex<Store>,
    prepared: Mutex<HashMap<String, Prepared>>,
    active: Mutex<Option<String>>,
    content_visible: Mutex<bool>,
    browser: Browser,
    /// Servers whose page this computer's web view is known to trust.
    page_trusted: Mutex<HashSet<String>>,
    /// The recording made for a page's dictation, if one is.
    recording: Mutex<Option<Recording>>,
    recordings: std::sync::atomic::AtomicU64,
    /// Tickets the browser extension handed the app, for the shell to take.
    imports: Mutex<Vec<ImportLink>>,
    /// The key that starts talking to the Reeve.
    keys: Mutex<voicekey::Keys>,
    key: Mutex<KeyInfo>,
    #[cfg(target_os = "linux")]
    portal: Mutex<Option<std::sync::Arc<crate::voicekey_portal::Portal>>>,
    /// The instance each server last said it is (`/api/reeve/voice`), by
    /// the app's server ID: which one is the main.
    voice_targets: Mutex<HashMap<String, voicemain::Instance>>,
    /// The conversation with the Reeve, while there is one.
    voice_session: Mutex<Option<tokio::sync::mpsc::UnboundedSender<Command>>>,
}

fn webview_label(id: &str) -> String {
    format!("server-{id}")
}

fn client(store: &Store, id: &str) -> Result<Client, String> {
    let s = store.server(id).ok_or("no such server")?;
    Client::new(&s.url, s.ca_pem.as_deref(), store.token(id))
}

fn device_name() -> String {
    let host = std::fs::read_to_string("/etc/hostname").ok().map(|h| h.trim().to_string()).filter(|h| !h.is_empty());
    format!("{} (desktop app)", host.unwrap_or_else(|| "This computer".into()))
}

// ---- Layout ----

fn content_rect(app: &AppHandle) -> Option<(LogicalPosition<f64>, LogicalSize<f64>)> {
    let window = app.get_window("main")?;
    let scale = window.scale_factor().ok()?;
    let size = window.inner_size().ok()?.to_logical::<f64>(scale);
    Some((LogicalPosition::new(DOCK, TITLE_BAR), LogicalSize::new((size.width - DOCK - EDGE).max(1.0), (size.height - TITLE_BAR - EDGE).max(1.0))))
}

/// On Linux, Tauri packs a window's webviews into a vertical box, one under
/// the other. The shell instead fills a GTK overlay, and each server's page
/// floats over it, held to the content area by its margins, so GTK keeps
/// it there as the window resizes.
#[cfg(target_os = "linux")]
mod overlay {
    use gtk::prelude::*;

    /// Puts an overlay in the window's box, for the webviews to move into.
    pub fn install(window: &tauri::Window) -> tauri::Result<()> {
        let vbox = window.default_vbox()?;
        let overlay = gtk::Overlay::new();
        vbox.pack_start(&overlay, true, true, 0);
        overlay.show();
        Ok(())
    }

    /// Moves a webview out of the window's box into the overlay: the shell
    /// as its base, a server's page over the content area.
    pub fn adopt(view: &webkit2gtk::WebView, content: Option<(i32, i32, i32)>) {
        let Some(vbox) = view.parent().and_then(|p| p.downcast::<gtk::Box>().ok()) else { return };
        let Some(overlay) = vbox.children().into_iter().find_map(|c| c.downcast::<gtk::Overlay>().ok()) else { return };
        vbox.remove(view);
        match content {
            None => overlay.add(view),
            Some((left, top, edge)) => {
                view.set_halign(gtk::Align::Fill);
                view.set_valign(gtk::Align::Fill);
                view.set_margin_start(left);
                view.set_margin_top(top);
                view.set_margin_end(edge);
                view.set_margin_bottom(edge);
                overlay.add_overlay(view);
            }
        }
        view.show();
    }
}

fn relayout(app: &AppHandle) {
    if cfg!(target_os = "linux") {
        return; // GTK lays the overlay out.
    }
    let Some(window) = app.get_window("main") else { return };
    let Ok(scale) = window.scale_factor() else { return };
    let Ok(size) = window.inner_size() else { return };
    if let Some(shell) = app.get_webview("shell") {
        let _ = shell.set_position(LogicalPosition::new(0.0, 0.0));
        let _ = shell.set_size(size.to_logical::<f64>(scale));
    }
    if let Some((pos, size)) = content_rect(app) {
        for (label, view) in app.webviews() {
            if label.starts_with("server-") {
                let _ = view.set_position(pos);
                let _ = view.set_size(size);
            }
        }
    }
}

/// Shows the active server's page when the content area is the server's,
/// and hides every other one.
fn show_only_active(app: &AppHandle, desktop: &Desktop) {
    let active = desktop.active.lock().unwrap().clone();
    let visible = *desktop.content_visible.lock().unwrap();
    for (label, view) in app.webviews() {
        if !label.starts_with("server-") {
            continue;
        }
        let show = visible && active.as_deref().map(webview_label).as_deref() == Some(label.as_str());
        let _ = if show { view.show() } else { view.hide() };
    }
}

/// Makes a server's webview at path: the web UI's token seeded before its
/// scripts run, the bridge to the shell (see `bridge`), and its certificate
/// checked against the pin on each TLS error.
fn make_server_view(app: &AppHandle, server: &Server, token: Option<String>, path: &str) -> Result<(), String> {
    let window = app.get_window("main").ok_or("no window")?;
    let (pos, size) = content_rect(app).ok_or("no window size")?;
    let origin = reqwest::Url::parse(&server.url).map_err(|e| e.to_string())?.origin().ascii_serialization();
    // Before the page's scripts: the web UI's token where it keeps it, and no
    // service worker. The web UI's offline cache would take over the page's
    // requests from its second load, and its requests do not get the pinned
    // certificate's allowance, so they hang; the dock shows a server that is
    // down instead. One already there is removed, and the page reloaded once.
    let seed = format!(
        r#"(() => {{
  if (location.origin !== {origin:?}) return;
  try {{ localStorage.setItem("legato:token:local", {token:?}); }} catch (e) {{}}
  const sw = navigator.serviceWorker;
  if (!sw) return;
  try {{ sw.register = () => Promise.reject(new DOMException("The desktop app keeps no offline copy.", "NotSupportedError")); }} catch (e) {{}}
  sw.getRegistrations().then((all) => {{
    if (!all.length) return;
    Promise.all(all.map((r) => r.unregister())).then(() => {{ if (sw.controller) location.reload(); }});
  }}).catch(() => {{}});
}})();"#,
        token = token.unwrap_or_default()
    );
    // What the page asks of the shell: read, handed to the shell, and the
    // navigation cancelled. Every other navigation goes ahead.
    let key = {
        let mut b = [0u8; 16];
        rand::rngs::OsRng.fill_bytes(&mut b);
        hex::encode(b)
    };
    let shell_bridge = format!("if (location.origin === {origin:?}) {}", bridge::script(&key, voice::system_recorder().is_some()));
    let (handle, server_id) = (app.clone(), server.id.clone());
    let (server_url, new_window_server_url) = (server.url.clone(), server.url.clone());
    let builder = WebviewBuilder::new(webview_label(&server.id), WebviewUrl::External("about:blank".parse().unwrap()))
        .initialization_script(&seed)
        .initialization_script(&shell_bridge)
        .on_new_window(move |url, _| {
            // A link that opens a window (target=_blank, window.open) never
            // makes one: another site goes to the system's browser.
            if links::classify(&new_window_server_url, &url) == links::Link::External {
                let _ = links::open_in_browser(&url);
            }
            tauri::webview::NewWindowResponse::Deny
        })
        .on_navigation(move |url| {
            if url.scheme() != bridge::SCHEME {
                // The page never leaves its server: another site opens in the
                // system's browser and the navigation is cancelled.
                if links::classify(&server_url, url) == links::Link::External {
                    let _ = links::open_in_browser(url);
                    return false;
                }
                return true;
            }
            let payload = match bridge::read(url.as_str(), &key) {
                Some(PageMessage::Approval(id)) => Some(json!({ "server": server_id, "kind": "approval", "id": id })),
                Some(PageMessage::Enroll) => Some(json!({ "server": server_id, "kind": "enroll" })),
                Some(PageMessage::Voice(action)) => {
                    record_for_page(&handle, &server_id, action);
                    None
                }
                Some(PageMessage::WorkerCandidates { id }) => {
                    answer_worker_candidates(&handle, &server_id, &id);
                    None
                }
                Some(PageMessage::OpenWorkerPairing { id, server, main }) => {
                    pair_worker_for_page(&handle, &server_id, id, &server, &main);
                    None
                }
                None => None,
            };
            if let Some(p) = payload {
                let _ = handle.emit_to("shell", "page-message", p);
            }
            false
        });
    let view = window.add_child(builder, pos, size).map_err(|e| e.to_string())?;
    let url = format!("{}{path}", server.url).parse::<tauri::Url>().map_err(|e| e.to_string())?;

    #[cfg(target_os = "linux")]
    {
        let pin = server.ca_pem.as_deref().map(crate::pin::Pin::from_pem).transpose()?;
        let target = url.to_string();
        let (failed_handle, failed_server) = (app.clone(), server.id.clone());
        view.with_webview(move |platform| {
            use webkit2gtk::gio::prelude::TlsCertificateExt;
            use webkit2gtk::{WebContextExt, WebViewExt};
            let page = platform.inner();
            overlay::adopt(&page, Some((DOCK as i32, TITLE_BAR as i32, EDGE as i32)));
            page.connect_load_failed_with_tls_errors(move |page, uri, cert, _flags| {
                let host = reqwest::Url::parse(uri).ok().and_then(|u| u.host_str().map(String::from)).unwrap_or_default();
                let leaf = cert.certificate().map(|b| b.to_vec()).unwrap_or_default();
                match &pin {
                    Some(pin) if pin.trusts(&leaf, &host) => {
                        if let Some(ctx) = page.context() {
                            ctx.allow_tls_certificate_for_host(cert, &host);
                        }
                        page.load_uri(uri);
                        true
                    }
                    _ => false,
                }
            });
            // A page that did not load (the server is down) is the shell's
            // to explain: WebKit's own error page is not drawn, and the shell
            // loads the address again when the server answers. A navigation
            // the app cancelled itself is not a failure.
            page.connect_load_failed(move |_, _, uri, error| {
                if error.matches(webkit2gtk::NetworkError::Cancelled) || error.is::<webkit2gtk::PolicyError>() {
                    return false;
                }
                println!("[page] {failed_server}: {uri} did not load: {error}");
                let _ = failed_handle.emit_to("shell", "page-load-failed", json!({ "server": failed_server, "uri": uri }));
                true
            });
            page.load_uri(&target);
        })
        .map_err(|e| e.to_string())?;
    }
    #[cfg(not(target_os = "linux"))]
    view.navigate(url.clone()).map_err(|e| e.to_string())?;
    let _ = url;
    Ok(())
}

// ---- Adding a worker from a page ----

/// Answers a page's worker request with a `legato:shell-worker` event.
fn tell_page_worker(app: &AppHandle, page: &str, detail: Value) {
    let Some(view) = app.get_webview(&webview_label(page)) else { return };
    if let Err(e) = view.eval(bridge::worker_script(&detail)) {
        println!("[worker] {page}: the page could not be told: {e}");
    }
}

/// Tells a page the app's other servers, which could be its workers.
fn answer_worker_candidates(app: &AppHandle, page: &str, id: &Value) {
    let list = workers::candidates(&app.state::<Desktop>().store.lock().unwrap().state.servers, page);
    tell_page_worker(app, page, workers::answer(id, Ok(Some(list))));
}

/// Makes one of the app's servers (key) a worker of the page's server: asks
/// the person in a dialog of the system's, outside the page, and on yes
/// opens a main's pairing request on it with its token and pinned
/// certificate, and gives the page the address and phrase (see `workers`).
fn pair_worker_for_page(app: &AppHandle, page: &str, id: Value, key: &str, main: &str) {
    use tauri_plugin_dialog::{DialogExt, MessageDialogButtons, MessageDialogKind};
    let desktop = app.state::<Desktop>();
    let prepared = {
        let store = desktop.store.lock().unwrap();
        let page_name = store.server(page).map(|s| s.name.clone()).unwrap_or_default();
        workers::target(&store.state.servers, page, key).and_then(|s| {
            let token = store.token(&s.id).ok_or_else(|| workers::no_token(&s.name))?;
            let client = Client::new(&s.url, s.ca_pem.as_deref(), Some(token))?;
            Ok((s.name.clone(), s.url.clone(), client, workers::main_name(main, &page_name)))
        })
    };
    let (name, url, client, main) = match prepared {
        Ok(prepared) => prepared,
        Err(e) => return tell_page_worker(app, page, workers::answer(&id, Err(e))),
    };
    println!("[worker] {page}: asks to make {name} a worker of {main}");
    let (handle, page) = (app.clone(), page.to_string());
    let mut dialog = app
        .dialog()
        .message(workers::confirm_text(&name, &main))
        .title("Add a worker")
        .kind(MessageDialogKind::Info)
        .buttons(MessageDialogButtons::OkCancelCustom("Make it a worker".into(), "Cancel".into()));
    if let Some(window) = app.get_window("main") {
        dialog = dialog.parent(&window);
    }
    dialog.show(move |yes| {
        if !yes {
            println!("[worker] {page}: the person said no");
            return tell_page_worker(&handle, &page, workers::answer(&id, Ok(None)));
        }
        tauri::async_runtime::spawn(async move {
            let outcome = workers::open_main_request(&client, &name, &url).await;
            match &outcome {
                Ok(_) => println!("[worker] {page}: opened a main's pairing request on {name}"),
                Err(e) => println!("[worker] {page}: {name} did not open a request: {e}"),
            }
            tell_page_worker(&handle, &page, workers::answer(&id, outcome.map(Some)));
        });
    });
}

// ---- Recording for a page ----

/// How often the page is told how loud it is.
const LEVEL_EVERY: std::time::Duration = std::time::Duration::from_millis(150);
/// A recording no page has stopped (it was reloaded or closed) ends by itself.
const LONGEST_RECORDING: std::time::Duration = std::time::Duration::from_secs(10 * 60);

/// The one recording the app makes at a time, for a server's page.
struct Recording {
    server: String,
    /// Tells this recording from a later one for the same server.
    number: u64,
    recorder: voice::Recorder,
}

fn tell_page(app: &AppHandle, server: &str, event: VoiceEvent) {
    match &event {
        VoiceEvent::Level(_) => {}
        VoiceEvent::Audio(wav) => println!("[voice] {server}: sending {} bytes of WAV to the page", wav.len()),
        other => println!("[voice] {server}: telling the page {other:?}"),
    }
    let Some(view) = app.get_webview(&webview_label(server)) else {
        println!("[voice] {server}: no page to tell");
        return;
    };
    if let Err(e) = view.eval(bridge::voice_script(&event)) {
        println!("[voice] {server}: the page could not be told: {e}");
    }
}

/// Starts, stops or cancels a recording for a server's page, which hears how
/// it goes as `legato:shell-voice` events (see `voice`). Starting one stops
/// another page's.
fn record_for_page(app: &AppHandle, server: &str, action: VoiceAction) {
    println!("[voice] {server}: the page asks to {action:?}");
    let desktop = app.state::<Desktop>();
    let mut slot = desktop.recording.lock().unwrap();
    match action {
        VoiceAction::Start => {
            if let Some(other) = slot.take() {
                other.recorder.cancel();
                tell_page(app, &other.server, VoiceEvent::Cancelled);
            }
            let Some((program, args)) = voice::system_recorder() else {
                tell_page(app, server, VoiceEvent::Error("Recording needs parecord or arecord, and neither is installed.".into()));
                return;
            };
            println!("[voice] {server}: recording with {program}");
            let recorder = match voice::Recorder::start(program, args) {
                Ok(recorder) => recorder,
                Err(e) => return tell_page(app, server, VoiceEvent::Error(e)),
            };
            let number = desktop.recordings.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
            *slot = Some(Recording { server: server.into(), number, recorder });
            tell_page(app, server, VoiceEvent::Started);
            let (app, server) = (app.clone(), server.to_string());
            std::thread::spawn(move || {
                let started = std::time::Instant::now();
                loop {
                    std::thread::sleep(LEVEL_EVERY);
                    // The page is told with the recording let go of.
                    let event = {
                        let desktop = app.state::<Desktop>();
                        let mut slot = desktop.recording.lock().unwrap();
                        let Some(recording) = slot.as_mut().filter(|r| r.number == number) else { return };
                        match recording.recorder.level() {
                            Ok(_) if started.elapsed() > LONGEST_RECORDING => {
                                slot.take();
                                Err("The recording was stopped after ten minutes.".to_string())
                            }
                            Ok(level) => Ok(level),
                            Err(e) => {
                                slot.take();
                                Err(e)
                            }
                        }
                    };
                    match event {
                        Ok(level) => tell_page(&app, &server, VoiceEvent::Level(level)),
                        Err(e) => return tell_page(&app, &server, VoiceEvent::Error(e)),
                    }
                }
            });
        }
        VoiceAction::Stop => {
            let Some(recording) = slot.take_if(|r| r.server == server) else {
                println!("[voice] {server}: nothing is recording for this page");
                return;
            };
            let (app, server) = (app.clone(), server.to_string());
            // Stopping waits for the recorder to finish writing.
            std::thread::spawn(move || {
                let event = match recording.recorder.stop() {
                    Ok(wav) => VoiceEvent::Audio(wav),
                    Err(e) => VoiceEvent::Error(e),
                };
                tell_page(&app, &server, event);
            });
        }
        VoiceAction::Cancel => {
            if let Some(recording) = slot.take_if(|r| r.server == server) {
                recording.recorder.cancel();
                tell_page(app, server, VoiceEvent::Cancelled);
            }
        }
    }
}

// ---- Servers ----

#[derive(Serialize)]
pub struct ServerView {
    id: String,
    name: String,
    url: String,
    tint: Option<String>,
    pinned: bool,
    keys: Vec<String>,
}

fn view(s: &Server) -> ServerView {
    ServerView { id: s.id.clone(), name: s.name.clone(), url: s.url.clone(), tint: s.tint.clone(), pinned: s.ca_pem.is_some(), keys: s.devices.keys().cloned().collect() }
}

#[tauri::command]
fn servers(desktop: State<'_, Desktop>) -> Vec<ServerView> {
    desktop.store.lock().unwrap().state.servers.iter().map(view).collect()
}

#[tauri::command]
fn show_server(app: AppHandle, desktop: State<'_, Desktop>, id: String) -> Result<(), String> {
    let label = webview_label(&id);
    if app.get_webview(&label).is_none() {
        let (server, token) = {
            let store = desktop.store.lock().unwrap();
            (store.server(&id).cloned().ok_or("no such server")?, store.token(&id))
        };
        make_server_view(&app, &server, token, "")?;
    }
    *desktop.active.lock().unwrap() = Some(id);
    show_only_active(&app, &desktop);
    Ok(())
}

/// Whether the content area shows the server's page (false while the shell
/// draws a panel or a sheet over it).
// ---- The page's approvals ----

/// Tells a server's page how the sheet on one of its approvals came out
/// (`bridge::result_script`), so its tray stops waiting and shows it.
#[tauri::command]
fn page_approval_done(app: AppHandle, server: String, detail: Value) -> Result<(), String> {
    if let Some(view) = app.get_webview(&webview_label(&server)) {
        view.eval(bridge::result_script(&detail)).map_err(|e| e.to_string())?;
    }
    Ok(())
}

/// Opens a server's page on an approval, where its tray shows it: an
/// approval the shell does not decide (it needs no signature: an answer, a
/// plan, a note for the agent).
#[tauri::command]
fn open_approval_in_page(app: AppHandle, desktop: State<'_, Desktop>, server: String, approval: String) -> Result<(), String> {
    open_in_page(&app, &desktop, server, "/", ("approval", &approval))
}

/// Opens a server's page at path?name=value, making its page when it has none,
/// and puts the server in front.
fn open_in_page(app: &AppHandle, desktop: &Desktop, server: String, path: &str, (name, value): (&str, &str)) -> Result<(), String> {
    let app = app.clone();
    let base = desktop.store.lock().unwrap().server(&server).map(|s| s.url.clone()).ok_or("no such server")?;
    let mut url = tauri::Url::parse(&base).map_err(|e| e.to_string())?;
    url.set_path(path);
    url.query_pairs_mut().clear().append_pair(name, value);
    match app.get_webview(&webview_label(&server)) {
        Some(view) => view.navigate(url).map_err(|e| e.to_string())?,
        None => {
            let path = format!("{}?{}", url.path(), url.query().unwrap_or_default());
            let (s, token) = {
                let store = desktop.store.lock().unwrap();
                (store.server(&server).cloned().ok_or("no such server")?, store.token(&server))
            };
            make_server_view(&app, &s, token, &path)?;
        }
    }
    *desktop.active.lock().unwrap() = Some(server);
    show_only_active(&app, desktop);
    Ok(())
}

// ---- The key that starts talking to the Reeve ----

/// The key as the settings show it, and the widget will.
#[derive(Clone, Debug, Serialize)]
pub struct KeyInfo {
    backend: Backend,
    /// The bound key as the person reads it; None when none is bound.
    key: Option<String>,
    hold_to_talk: bool,
    /// The desktop can show its own dialog to change the key.
    can_change: bool,
    /// Why the key is not working, when it is not: the link still is.
    problem: Option<String>,
    /// The link that does what the key does, for the desktop's own shortcuts.
    link: &'static str,
}

const VOICE_LINK: &str = "legato-desktop://voice";

fn key_changed(app: &AppHandle, change: impl FnOnce(&mut KeyInfo)) {
    let info = {
        let desktop = app.state::<Desktop>();
        let mut key = desktop.key.lock().unwrap();
        change(&mut key);
        key.clone()
    };
    let _ = app.emit("voice-key-changed", info);
}

/// A key went down or up: what it asks, if anything, is sent on.
fn voice_key_input(app: &AppHandle, input: Input, from: &'static str) {
    let asked = app.state::<Desktop>().keys.lock().unwrap().on(input);
    if let Some(key) = asked {
        voice_key(app, key, from);
    }
}

/// Tells the app the key asked something: the `voice-key` event,
/// `{key: "talk" | "hold" | "release" | "cancel", from: "portal" | "shortcut" | "link"}`.
/// Until the widget exists, a press also brings the window to the front.
fn voice_key(app: &AppHandle, key: VoiceKey, from: &'static str) {
    println!("[voice key] {key:?} from the {from}");
    let _ = app.emit_filter("voice-key", json!({ "key": key, "from": from }), for_the_app);
    voice_command(app, Command::Key(key));
}

/// The app's own pages: the shell, and the voice widget. A server's page
/// is not told what is said to the Reeve.
fn for_the_app(target: &tauri::EventTarget) -> bool {
    use tauri::EventTarget::*;
    match target {
        AnyLabel { label } | Window { label } | Webview { label } | WebviewWindow { label } => label == "shell" || label == "voice",
        _ => false,
    }
}

// ---- Talking to the Reeve ----

fn tell_widget(app: &AppHandle, ui: Ui) {
    match &ui {
        Ui::Level(_) | Ui::Pause { .. } => {}
        other => println!("[voice] {}: {}", other.name(), other.payload()),
    }
    let _ = app.emit_filter(ui.name(), ui.payload(), for_the_app);
}

/// Hands a command to the conversation; the key starts one when there is
/// none.
fn voice_command(app: &AppHandle, command: Command) {
    let desktop = app.state::<Desktop>();
    let mut session = desktop.voice_session.lock().unwrap();
    if let Some(tx) = session.as_ref() {
        if tx.send(command).is_ok() {
            return;
        }
        session.take();
    }
    if !matches!(command, Command::Key(VoiceKey::Talk | VoiceKey::Hold)) {
        return;
    }
    let (tx, rx) = tokio::sync::mpsc::unbounded_channel();
    *session = Some(tx.clone());
    drop(session);
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        converse(&app, rx).await;
        let desktop = app.state::<Desktop>();
        let mut session = desktop.voice_session.lock().unwrap();
        if session.as_ref().is_some_and(|s| s.same_channel(&tx)) {
            session.take();
        }
    });
}

/// The servers, each with the instance it last said it is.
fn voice_seen(desktop: &Desktop) -> Vec<voicemain::Seen> {
    let targets = desktop.voice_targets.lock().unwrap();
    let store = desktop.store.lock().unwrap();
    store.state.servers.iter().map(|s| voicemain::Seen { server: s.id.clone(), instance: targets.get(&s.id).cloned() }).collect()
}

fn voice_choice(desktop: &Desktop) -> voicemain::Choice {
    let saved = desktop.store.lock().unwrap().state.voice.server.clone();
    voicemain::choose(&voice_seen(desktop), saved.as_deref())
}

/// One conversation: which main, whether it is ready (asked now, within 2
/// seconds), then the session until it ends. Not ready says why and
/// records nothing.
async fn converse(app: &AppHandle, commands: tokio::sync::mpsc::UnboundedReceiver<Command>) {
    let not_ready = |reason: String, setting: Option<String>| tell_widget(app, Ui::State { state: "not_ready".into(), reason: Some(reason), setting });
    let desktop = app.state::<Desktop>();
    let server = match voice_choice(&desktop) {
        voicemain::Choice::Server(id) => id,
        voicemain::Choice::Ask => return not_ready("Choose the Reeve to talk to in Servers & keys".into(), None),
        voicemain::Choice::None => return not_ready("Pair a Legato server first".into(), None),
    };
    let (name, client) = {
        let store = desktop.store.lock().unwrap();
        let name = store.server(&server).map(|s| s.name.clone()).unwrap_or_default();
        (name, client(&store, &server))
    };
    let client = match client {
        Ok(c) => c,
        Err(e) => return not_ready(e, None),
    };
    tell_widget(app, Ui::state("connecting", Some(&name)));
    let answer = client
        .get_within("/api/reeve/voice", voicemain::STATUS_TIMEOUT)
        .await
        .and_then(|v| serde_json::from_value::<voicemain::Readiness>(v).map_err(|e| e.to_string()));
    if let Ok(r) = &answer {
        desktop.voice_targets.lock().unwrap().insert(server.clone(), r.instance.clone());
    }
    let recorder = voice::system_recorder();
    if let Some(reason) = voicemain::not_ready(&answer, &name, recorder.is_some()) {
        let setting = answer.ok().map(|r| r.setting).filter(|s| !s.is_empty());
        return not_ready(reason, setting);
    }
    let (program, args) = recorder.expect("checked");
    let socket = match client.websocket("/api/reeve/voice/session").await {
        Ok(s) => s,
        Err(e) => {
            println!("[voice] {name}: the session did not open: {e}");
            return not_ready(format!("Can't reach {name}"), None);
        }
    };
    let (audio_tx, audio) = tokio::sync::mpsc::unbounded_channel();
    let _recorder = match voice::Recorder::stream(program, args, move |bytes| {
        let _ = audio_tx.send(bytes.to_vec());
    }) {
        Ok(r) => r,
        Err(e) => return not_ready(e, None),
    };
    let lines = voiceclient::system_player().map(|(program, args)| std::sync::Arc::new(PlayLines { client: client.clone(), program, args }) as std::sync::Arc<dyn voiceclient::Lines>);
    println!("[voice] {name}: talking, recording with {program}, playing with {}", voiceclient::system_player().map(|p| p.0).unwrap_or("nothing"));
    voiceclient::run(socket, &device_name(), lines, commands, audio, |ui| tell_widget(app, ui)).await;
    println!("[voice] {name}: the conversation ended");
}

/// Plays a line: fetched over the pinned TLS with the token, written to a
/// file of its own, and played by the system's player to the end. Stopping
/// drops the future, which ends the player and removes the file.
struct PlayLines {
    client: Client,
    program: &'static str,
    args: &'static [&'static str],
}

struct Removed(std::path::PathBuf);

impl Drop for Removed {
    fn drop(&mut self) {
        let _ = std::fs::remove_file(&self.0);
    }
}

impl voiceclient::Lines for PlayLines {
    fn play(&self, url: String) -> futures_util::future::BoxFuture<'static, Result<(), String>> {
        let (client, program, args) = (self.client.clone(), self.program, self.args);
        Box::pin(async move {
            let audio = client.bytes(&url).await?;
            let mut name = [0u8; 8];
            rand::rngs::OsRng.fill_bytes(&mut name);
            let file = Removed(std::env::temp_dir().join(format!("legato-voice-{}", hex::encode(name))));
            std::fs::write(&file.0, audio).map_err(|e| e.to_string())?;
            let status = tokio::process::Command::new(program)
                .args(args)
                .arg(&file.0)
                .kill_on_drop(true)
                .status()
                .await
                .map_err(|e| format!("{program}: {e}"))?;
            if !status.success() {
                return Err(format!("{program} ended with {status}"));
            }
            Ok(())
        })
    }
}

/// Asks every server which instance it is, so the main is known before the
/// key is pressed: at start, when the servers change, and every 10 minutes.
async fn refresh_voice_targets(app: &AppHandle) {
    let desktop = app.state::<Desktop>();
    let clients: Vec<(String, Result<Client, String>)> = {
        let store = desktop.store.lock().unwrap();
        store.state.servers.iter().map(|s| (s.id.clone(), client(&store, &s.id))).collect()
    };
    let mut answered = HashMap::new();
    for (id, c) in &clients {
        let Ok(c) = c else { continue };
        if let Ok(r) = c.get_within("/api/reeve/voice", std::time::Duration::from_secs(5)).await.and_then(|v| serde_json::from_value::<voicemain::Readiness>(v).map_err(|e| e.to_string())) {
            answered.insert(id.clone(), r.instance);
        }
    }
    {
        let mut targets = desktop.voice_targets.lock().unwrap();
        // A server that did not answer keeps what it said last.
        targets.retain(|id, _| clients.iter().any(|(c, _)| c == id));
        targets.extend(answered);
    }
    let _ = app.emit_filter("voice-targets-changed", voice_targets(desktop), for_the_app);
}

fn refresh_voice_targets_soon(app: &AppHandle) {
    let app = app.clone();
    tauri::async_runtime::spawn(async move { refresh_voice_targets(&app).await });
}

#[derive(Clone, Serialize)]
pub struct VoiceTargets {
    /// Each server, with the instance it last said it is.
    servers: Vec<VoiceTarget>,
    /// The server the key talks to now (the app's ID), if one is known.
    talking_to: Option<String>,
    /// The roles do not decide, and the person has not chosen.
    must_choose: bool,
    /// The instance ID the person chose.
    chosen: Option<String>,
}

#[derive(Clone, Serialize)]
pub struct VoiceTarget {
    server: String,
    name: String,
    instance: Option<voicemain::Instance>,
}

/// "The Reeve to talk to", for Servers & keys.
#[tauri::command]
fn voice_targets(desktop: State<'_, Desktop>) -> VoiceTargets {
    let seen = voice_seen(&desktop);
    let chosen = desktop.store.lock().unwrap().state.voice.server.clone();
    let choice = voicemain::choose(&seen, chosen.as_deref());
    let names: HashMap<String, String> = desktop.store.lock().unwrap().state.servers.iter().map(|s| (s.id.clone(), s.name.clone())).collect();
    VoiceTargets {
        servers: seen.into_iter().map(|s| VoiceTarget { name: names.get(&s.server).cloned().unwrap_or_default(), server: s.server, instance: s.instance }).collect(),
        talking_to: match &choice {
            voicemain::Choice::Server(id) => Some(id.clone()),
            _ => None,
        },
        must_choose: choice == voicemain::Choice::Ask,
        chosen,
    }
}

/// Keeps the person's choice of Reeve, by instance ID; None forgets it.
#[tauri::command]
fn set_voice_server(app: AppHandle, desktop: State<'_, Desktop>, instance: Option<String>) -> Result<(), String> {
    {
        let mut store = desktop.store.lock().unwrap();
        store.state.voice.server = instance;
        store.save()?;
    }
    let _ = app.emit_filter("voice-targets-changed", voice_targets(desktop), for_the_app);
    Ok(())
}

/// The widget's buttons: `stop`, `reply`, `send`, `cancel`, `end`; and
/// `talk`, which is the key.
#[tauri::command]
fn voice_action(app: AppHandle, action: String) -> Result<(), String> {
    let command = match action.as_str() {
        "talk" => Command::Key(VoiceKey::Talk),
        other => Command::named(other).ok_or_else(|| format!("no voice action {other}"))?,
    };
    voice_command(&app, command);
    Ok(())
}

/// Starts listening for the key the way this computer has it.
fn start_voice_key(app: &AppHandle) {
    let backend = app.state::<Desktop>().key.lock().unwrap().backend;
    match backend {
        Backend::Link => println!("[voice key] no global key here; {VOICE_LINK} does what it would"),
        Backend::Shortcut => {
            use tauri_plugin_global_shortcut::GlobalShortcutExt;
            // The plugin is added only here, where the app grabs the key.
            let plugin = tauri_plugin_global_shortcut::Builder::new()
                .with_handler(|app, _shortcut, event| {
                    let input = match event.state() {
                        tauri_plugin_global_shortcut::ShortcutState::Pressed => Input::Pressed(voicekey::Shortcut::Talk),
                        tauri_plugin_global_shortcut::ShortcutState::Released => Input::Released(voicekey::Shortcut::Talk),
                    };
                    voice_key_input(app, input, "shortcut");
                })
                .build();
            if let Err(e) = app.plugin(plugin) {
                println!("[voice key] the global-shortcut plugin did not start: {e}");
                return key_changed(app, |k| k.problem = Some(format!("Could not grab a global key: {e}")));
            }
            let accelerator = voicekey::shortcut_accelerator(std::env::consts::OS);
            match app.global_shortcut().register(accelerator) {
                Ok(()) => key_changed(app, |k| k.key = voicekey::key_text(accelerator, cfg!(target_os = "macos"))),
                Err(e) => {
                    println!("[voice key] could not take {accelerator}: {e}");
                    key_changed(app, |k| k.problem = Some(format!("Could not take {accelerator}: {e}")));
                }
            }
        }
        #[cfg(target_os = "linux")]
        Backend::Portal => {
            let app = app.clone();
            tauri::async_runtime::spawn(async move {
                let (on_input, on_text, on_lost) = (app.clone(), app.clone(), app.clone());
                let opened = crate::voicekey_portal::Portal::open(
                    move |input| voice_key_input(&on_input, input, "portal"),
                    move |text| key_changed(&on_text, |k| k.key = text),
                    move || {
                        println!("[voice key] the portal went away");
                        on_lost.state::<Desktop>().portal.lock().unwrap().take();
                        key_changed(&on_lost, |k| {
                            k.key = None;
                            k.can_change = false;
                            k.problem = Some(format!("The desktop's shortcut service stopped, so the key no longer works. Restart the app, or bind {VOICE_LINK} in the desktop's own shortcut settings."));
                        });
                    },
                )
                .await;
                match opened {
                    Ok((portal, text)) => {
                        let can_change = portal.can_change();
                        *app.state::<Desktop>().portal.lock().unwrap() = Some(std::sync::Arc::new(portal));
                        key_changed(&app, |k| {
                            k.key = text;
                            k.can_change = can_change;
                        });
                    }
                    Err(e) => {
                        println!("[voice key] the portal did not give a key: {e}");
                        key_changed(&app, |k| {
                            k.backend = Backend::Link;
                            k.problem = Some(format!("The desktop gave no global key ({e}). Bind {VOICE_LINK} in its own shortcut settings."));
                        });
                    }
                }
            });
        }
        #[cfg(not(target_os = "linux"))]
        Backend::Portal => {}
    }
}

#[tauri::command]
fn voice_key_info(desktop: State<'_, Desktop>) -> KeyInfo {
    desktop.key.lock().unwrap().clone()
}

/// "Change the key…": the desktop's own dialog, where the portal has one.
#[tauri::command]
async fn change_voice_key(desktop: State<'_, Desktop>) -> Result<(), String> {
    #[cfg(target_os = "linux")]
    {
        let portal = desktop.portal.lock().unwrap().clone();
        if let Some(portal) = portal {
            return portal.change().await;
        }
    }
    let _ = desktop;
    Err("The key is not changed from here on this computer.".into())
}

#[tauri::command]
fn set_hold_to_talk(app: AppHandle, desktop: State<'_, Desktop>, on: bool) -> Result<(), String> {
    {
        let mut store = desktop.store.lock().unwrap();
        store.state.voice.hold_to_talk = on;
        store.save()?;
    }
    desktop.keys.lock().unwrap().set_hold_to_talk(on);
    key_changed(&app, |k| k.hold_to_talk = on);
    Ok(())
}

// ---- Tickets the browser extension hands the app ----

/// Takes a link the system handed the app: an import is kept for the shell,
/// which is told, and the window comes to the front. Other links are ignored.
fn handle_link(app: &AppHandle, url: &str) {
    if let Some(key) = voicekey::read_link(url) {
        return voice_key(app, key, "link");
    }
    let Some(link) = applink::read(url) else {
        println!("[link] ignored a link the app does not read");
        return;
    };
    println!("[link] import {} from {}", link.key, link.provider);
    app.state::<Desktop>().imports.lock().unwrap().push(link);
    let _ = app.emit_to("shell", "import-request", ());
    bring_to_front(app);
}

fn bring_to_front(app: &AppHandle) {
    if let Some(window) = app.get_window("main") {
        let _ = window.unminimize();
        let _ = window.show();
        let _ = window.set_focus();
    }
}

/// The imports waiting for the shell, which takes them.
#[tauri::command]
fn take_import_requests(desktop: State<'_, Desktop>) -> Vec<ImportLink> {
    std::mem::take(&mut *desktop.imports.lock().unwrap())
}

#[tauri::command]
fn import_preference(desktop: State<'_, Desktop>) -> ImportTarget {
    desktop.store.lock().unwrap().state.import_target
}

#[tauri::command]
fn set_import_preference(desktop: State<'_, Desktop>, target: ImportTarget) -> Result<(), String> {
    let mut store = desktop.store.lock().unwrap();
    store.state.import_target = target;
    store.save()
}

/// Imports a ticket into a server (POST /api/remote/import), and gives the
/// task it made, or the one it already had.
#[tauri::command]
async fn import_ticket(desktop: State<'_, Desktop>, server: String, provider: String, key: String) -> Result<Value, String> {
    let c = {
        let store = desktop.store.lock().unwrap();
        client(&store, &server)?
    };
    let card = c.post("/api/remote/import", json!({ "ticket_id": key, "provider": provider })).await?;
    Ok(json!({ "id": card.get("id").cloned().unwrap_or(Value::Null), "title": card.get("title").cloned().unwrap_or(Value::Null) }))
}

/// Opens a server's board with a task picked out.
#[tauri::command]
fn open_task_in_page(app: AppHandle, desktop: State<'_, Desktop>, server: String, task: String) -> Result<(), String> {
    open_in_page(&app, &desktop, server, "/board", ("highlight_task", &task))
}

// ---- A page the system must trust (macOS) ----

/// Whether the web view will load a server's page. On a Mac it checks the
/// system's trust, not the pin (see `systrust`), so a server with Legato's
/// own CA loads only once the Mac trusts that CA. Elsewhere, and for http,
/// always.
async fn check_page(desktop: &Desktop, id: &str) -> Result<bool, String> {
    if !cfg!(target_os = "macos") || desktop.page_trusted.lock().unwrap().contains(id) {
        return Ok(true);
    }
    let server = desktop.store.lock().unwrap().server(id).cloned().ok_or("no such server")?;
    let (Some(host), Some(ca)) = (systrust::page_host(&server.url), server.ca_pem) else { return Ok(true) };
    let leaf = legato::certificate_pem(&legato::presented_certificate(&server.url, &ca).await?);
    let trusted = tauri::async_runtime::spawn_blocking(move || systrust::trusts(&leaf, &ca, &host)).await.map_err(|e| e.to_string())??;
    if trusted {
        desktop.page_trusted.lock().unwrap().insert(id.to_string());
    }
    Ok(trusted)
}

#[tauri::command]
async fn page_trusted(desktop: State<'_, Desktop>, id: String) -> Result<bool, String> {
    check_page(&desktop, &id).await
}

/// Trusts a server's pinned CA on this Mac, for TLS to its host only (macOS
/// asks for the password), then checks again and reloads its page.
#[tauri::command]
async fn trust_server_certificate(app: AppHandle, desktop: State<'_, Desktop>, id: String) -> Result<bool, String> {
    let server = desktop.store.lock().unwrap().server(&id).cloned().ok_or("no such server")?;
    let host = systrust::page_host(&server.url).ok_or("This server's page is not served over HTTPS.")?;
    let ca = server.ca_pem.ok_or("This server has no pinned certificate; pair with it again.")?;
    tauri::async_runtime::spawn_blocking(move || systrust::trust(&ca, &host)).await.map_err(|e| e.to_string())??;
    let trusted = check_page(&desktop, &id).await?;
    if trusted {
        if let Some(view) = app.get_webview(&webview_label(&id)) {
            let _ = view.reload();
        }
    }
    Ok(trusted)
}

/// Saves a server's pinned CA to Downloads, to install by hand, and shows
/// it in the file manager. Says where.
#[tauri::command]
fn save_server_certificate(desktop: State<'_, Desktop>, id: String) -> Result<String, String> {
    let server = desktop.store.lock().unwrap().server(&id).cloned().ok_or("no such server")?;
    let ca = server.ca_pem.ok_or("This server has no pinned certificate.")?;
    let dir = dirs::download_dir().or_else(dirs::home_dir).ok_or("no Downloads folder")?;
    let path = dir.join(systrust::certificate_file_name(&server.name));
    std::fs::write(&path, ca).map_err(|e| format!("{}: {e}", path.display()))?;
    #[cfg(target_os = "macos")]
    let _ = std::process::Command::new("/usr/bin/open").arg("-R").arg(&path).status();
    Ok(path.display().to_string())
}

/// Opens a saved certificate, which on a Mac adds it in Keychain Access.
#[tauri::command]
fn open_certificate(path: String) -> Result<(), String> {
    if !path.ends_with(" Legato CA.cer") {
        return Err("not a certificate this app saved".into());
    }
    #[cfg(target_os = "macos")]
    std::process::Command::new("/usr/bin/open").arg(&path).status().map_err(|e| e.to_string())?;
    Ok(())
}

#[tauri::command]
fn set_content_visible(app: AppHandle, desktop: State<'_, Desktop>, visible: bool) {
    *desktop.content_visible.lock().unwrap() = visible;
    show_only_active(&app, &desktop);
}

#[tauri::command]
fn reload_server(app: AppHandle, id: String) {
    if let Some(view) = app.get_webview(&webview_label(&id)) {
        let _ = view.eval("location.reload()");
    }
}

/// Loads a server's page again after it did not load (see
/// `page-load-failed`): at the address that failed when it is the server's
/// own, else at its root. Nothing when the server has no page yet.
#[tauri::command]
fn load_server_page(app: AppHandle, desktop: State<'_, Desktop>, id: String, uri: Option<String>) -> Result<(), String> {
    let Some(view) = app.get_webview(&webview_label(&id)) else { return Ok(()) };
    let base = desktop.store.lock().unwrap().server(&id).map(|s| s.url.clone()).ok_or("no such server")?;
    let url = links::page_to_load(&base, uri.as_deref()).parse::<tauri::Url>().map_err(|e| e.to_string())?;
    view.navigate(url).map_err(|e| e.to_string())
}

/// Pairs by phrase, keeps the server, and enrolls each key the app holds.
#[tauri::command]
async fn pair_server(app: AppHandle, desktop: State<'_, Desktop>, url: String, phrase: String) -> Result<ServerView, String> {
    let url = url.trim().trim_end_matches('/').to_string();
    if !(url.starts_with("https://") || url.starts_with("http://")) {
        return Err("The address starts with https:// (or http://).".into());
    }
    let paired = legato::pair(&url, &phrase, &device_name()).await?;
    let server = {
        let mut store = desktop.store.lock().unwrap();
        let s = store.add_server(&paired.name, &url, paired.ca_pem, &paired.token)?;
        store.save()?;
        s
    };
    let keys = desktop.store.lock().unwrap().state.keys.clone();
    for key in keys {
        let _ = enroll(&desktop, &key.id, &server.id).await;
    }
    refresh_voice_targets_soon(&app);
    let store = desktop.store.lock().unwrap();
    Ok(view(store.server(&server.id).unwrap()))
}

/// Legato servers announcing themselves on the local network, except those
/// already paired.
#[tauri::command]
fn discovered(desktop: State<'_, Desktop>) -> Vec<Found> {
    desktop.browser.start();
    let store = desktop.store.lock().unwrap();
    desktop.browser.found().into_iter().filter(|f| !store.state.servers.iter().any(|s| s.url == f.url)).collect()
}

#[tauri::command]
fn rename_server(desktop: State<'_, Desktop>, id: String, name: String) -> Result<(), String> {
    let mut store = desktop.store.lock().unwrap();
    store.server_mut(&id).ok_or("no such server")?.name = name.trim().to_string();
    store.save()
}

#[tauri::command]
fn reorder_servers(desktop: State<'_, Desktop>, ids: Vec<String>) -> Result<(), String> {
    let mut store = desktop.store.lock().unwrap();
    store.reorder_servers(&ids);
    store.save()
}

#[tauri::command]
fn remove_server(app: AppHandle, desktop: State<'_, Desktop>, id: String) -> Result<(), String> {
    desktop.recording.lock().unwrap().take_if(|r| r.server == id);
    if let Some(view) = app.get_webview(&webview_label(&id)) {
        let _ = view.close();
    }
    {
        let mut store = desktop.store.lock().unwrap();
        store.remove_server(&id)?;
        store.save()?;
    }
    refresh_voice_targets_soon(&app);
    Ok(())
}

#[derive(Serialize)]
pub struct Status {
    id: String,
    online: bool,
    error: Option<String>,
    version: Option<String>,
    tint: Option<String>,
    approvals: Vec<Value>,
}

/// Each server's state: answering, its tint, and its open approvals that
/// need a signature.
#[tauri::command]
async fn poll(desktop: State<'_, Desktop>) -> Result<Vec<Status>, String> {
    let clients: Vec<(String, Result<Client, String>)> = {
        let store = desktop.store.lock().unwrap();
        store.state.servers.iter().map(|s| (s.id.clone(), client(&store, &s.id))).collect()
    };
    let mut out = Vec::new();
    for (id, c) in clients {
        let status = match c {
            Err(e) => Status { id, online: false, error: Some(e), version: None, tint: None, approvals: vec![] },
            Ok(c) => match c.get("/api/meta").await {
                Err(e) => Status { id, online: false, error: Some(e), version: None, tint: None, approvals: vec![] },
                Ok(meta) => {
                    let approvals = c.get("/api/approvals?status=pending").await.ok().and_then(|v| v["approvals"].as_array().cloned()).unwrap_or_default();
                    Status {
                        id,
                        online: true,
                        error: None,
                        version: meta["version"].as_str().map(String::from),
                        tint: meta["tint"].as_str().filter(|t| !t.is_empty()).map(String::from),
                        approvals,
                    }
                }
            },
        };
        out.push(status);
    }
    // Keep each tint, so the dock has it while a server is down.
    let mut store = desktop.store.lock().unwrap();
    let mut changed = false;
    for st in out.iter().filter(|s| s.online) {
        if let Some(s) = store.server_mut(&st.id) {
            if s.tint != st.tint {
                s.tint = st.tint.clone();
                changed = true;
            }
        }
    }
    if changed {
        let _ = store.save();
    }
    Ok(out)
}

// ---- Keys ----

#[derive(Serialize)]
pub struct KeyView {
    id: String,
    name: String,
    /// "security-key" or "touch-id".
    kind: &'static str,
    /// A security key is plugged in; Touch ID is set up on this Mac.
    plugged: bool,
    /// By server ID: "active", "pending", or absent.
    servers: HashMap<String, String>,
}

/// The credentials of the security keys, which are asked over USB; a Touch
/// ID key is not.
fn security_key_credentials(keys: &[crate::store::Key]) -> Vec<Vec<u8>> {
    keys.iter().filter(|k| !k.is_touch_id()).map(|k| URL_SAFE_NO_PAD.decode(&k.credential_id).unwrap_or_default()).collect()
}

/// The keys, whether each is plugged in, and how each server has it.
#[tauri::command]
async fn keys(desktop: State<'_, Desktop>) -> Result<Vec<KeyView>, String> {
    let (keys, servers) = {
        let store = desktop.store.lock().unwrap();
        let servers: Vec<(Server, Result<Client, String>)> = store.state.servers.iter().map(|s| (s.clone(), client(&store, &s.id))).collect();
        (store.state.keys.clone(), servers)
    };
    let credentials = security_key_credentials(&keys);
    let plugged = tauri::async_runtime::spawn_blocking(move || fido::held(RP_ID, &credentials)).await.map_err(|e| e.to_string())?;
    let touch_id = touchid::this_mac().is_ok();

    let mut statuses: HashMap<(String, String), String> = HashMap::new();
    let mut gone: Vec<(String, String)> = Vec::new();
    for (server, c) in &servers {
        let Ok(c) = c else { continue };
        let Ok(list) = c.get("/api/devices").await else { continue };
        let devices = list.as_array().cloned().or_else(|| list["devices"].as_array().cloned()).unwrap_or_default();
        for (key_id, device_id) in &server.devices {
            match devices.iter().find(|d| d["id"] == device_id.as_str()) {
                Some(d) if d["status"] == "active" || d["status"] == "pending" => {
                    statuses.insert((key_id.clone(), server.id.clone()), d["status"].as_str().unwrap().to_string());
                }
                // Removed on the server (or refused): no longer enrolled there.
                _ => gone.push((server.id.clone(), key_id.clone())),
            }
        }
    }
    if !gone.is_empty() {
        let mut store = desktop.store.lock().unwrap();
        for (sid, kid) in gone {
            if let Some(s) = store.server_mut(&sid) {
                s.devices.remove(&kid);
            }
        }
        let _ = store.save();
    }
    Ok(keys
        .iter()
        .map(|k| KeyView {
            id: k.id.clone(),
            name: k.name.clone(),
            kind: if k.is_touch_id() { "touch-id" } else { "security-key" },
            plugged: if k.is_touch_id() { touch_id } else { plugged.iter().any(|c| URL_SAFE_NO_PAD.encode(c) == k.credential_id) },
            servers: servers.iter().filter_map(|(s, _)| statuses.get(&(k.id.clone(), s.id.clone())).map(|st| (s.id.clone(), st.clone()))).collect(),
        })
        .collect())
}

/// Enrolls a key's credential on a server: no touch, since the credential
/// exists. A server with keys already makes this an approval that one of
/// them signs.
async fn enroll(desktop: &Desktop, key_id: &str, server_id: &str) -> Result<String, String> {
    let (c, key) = {
        let store = desktop.store.lock().unwrap();
        (client(&store, server_id)?, store.state.keys.iter().find(|k| k.id == key_id).cloned().ok_or("no such key")?)
    };
    let options = c.post("/api/devices/passkey/options", json!({ "origin": ORIGIN })).await?;
    let challenge = options["publicKey"]["challenge"].as_str().ok_or("the server gave no challenge")?;
    let client_data = webauthn::client_data("webauthn.create", challenge, ORIGIN);
    let credential = URL_SAFE_NO_PAD.decode(&key.credential_id).map_err(|e| e.to_string())?;
    let auth_data = URL_SAFE_NO_PAD.decode(&key.auth_data).map_err(|e| e.to_string())?;
    let attachment = if key.is_touch_id() { Attachment::TouchId } else { Attachment::SecurityKey };
    let device = c.post("/api/devices", webauthn::registration(&key.name, &credential, &auth_data, &client_data, attachment)).await?;
    let id = device["id"].as_str().ok_or("the server did not say which device")?.to_string();
    let mut store = desktop.store.lock().unwrap();
    if let Some(s) = store.server_mut(server_id) {
        s.devices.insert(key_id.to_string(), id);
    }
    store.save()?;
    Ok(device["status"].as_str().unwrap_or("").to_string())
}

/// Adds the one plugged-in key: it makes a credential for the app (a touch,
/// and its PIN when it insists), then it is enrolled on every server.
#[tauri::command]
async fn add_key(desktop: State<'_, Desktop>, name: String, pin: Option<String>) -> Result<Value, String> {
    let mut challenge = [0u8; 32];
    let mut user = [0u8; 16];
    rand::rngs::OsRng.fill_bytes(&mut challenge);
    rand::rngs::OsRng.fill_bytes(&mut user);
    let client_data = webauthn::client_data("webauthn.create", &URL_SAFE_NO_PAD.encode(challenge), ORIGIN);
    let (credential, auth_data) =
        tauri::async_runtime::spawn_blocking(move || fido::make_credential(RP_ID, &client_data, &user, "Legato desktop", pin.as_deref()))
            .await
            .map_err(|e| e.to_string())??;
    let key = {
        let mut store = desktop.store.lock().unwrap();
        if store.state.keys.iter().any(|k| k.credential_id == URL_SAFE_NO_PAD.encode(&credential)) {
            return Err("This key is already added.".into());
        }
        let k = store.add_key(name.trim(), &URL_SAFE_NO_PAD.encode(&credential), &URL_SAFE_NO_PAD.encode(&auth_data))?;
        store.save()?;
        k
    };
    Ok(enroll_everywhere(&desktop, &key.id).await)
}

/// Enrolls a new key on every server, and says how each went.
async fn enroll_everywhere(desktop: &Desktop, key_id: &str) -> Value {
    let servers: Vec<String> = desktop.store.lock().unwrap().state.servers.iter().map(|s| s.id.clone()).collect();
    let mut results = serde_json::Map::new();
    for sid in servers {
        let r = enroll(desktop, key_id, &sid).await;
        results.insert(sid, match r { Ok(status) => json!({ "status": status }), Err(e) => json!({ "error": e }) });
    }
    json!({ "key": key_id, "servers": results })
}

/// Whether this computer can sign with Touch ID, and if not, why.
#[tauri::command]
fn touch_id() -> Value {
    match touchid::this_mac() {
        Ok(_) => json!({ "mac": true, "available": true }),
        Err(reason) => json!({ "mac": cfg!(target_os = "macos"), "available": false, "reason": reason }),
    }
}

/// Adds Touch ID as a key: a Secure Enclave key that signs only after a
/// fingerprint (none is asked for now), enrolled on every server. One per
/// Mac.
#[tauri::command]
async fn add_touch_id(desktop: State<'_, Desktop>, name: String) -> Result<Value, String> {
    if desktop.store.lock().unwrap().state.keys.iter().any(|k| k.is_touch_id()) {
        return Err("Touch ID is already added.".into());
    }
    let made = tauri::async_runtime::spawn_blocking(|| touchid::this_mac().and_then(|mac| touchid::make(mac.as_ref())))
        .await
        .map_err(|e| e.to_string())??;
    let key = {
        let mut store = desktop.store.lock().unwrap();
        let k = store.add_touch_id_key(
            name.trim(),
            &URL_SAFE_NO_PAD.encode(&made.credential_id),
            &URL_SAFE_NO_PAD.encode(&made.auth_data),
            &URL_SAFE_NO_PAD.encode(&made.blob),
        )?;
        store.save()?;
        k
    };
    Ok(enroll_everywhere(&desktop, &key.id).await)
}

#[tauri::command]
async fn enroll_key(desktop: State<'_, Desktop>, key: String, server: String) -> Result<String, String> {
    enroll(&desktop, &key, &server).await
}

/// Revokes a key everywhere: each server is asked to remove it (an
/// approval another key, or this one, signs), and the app forgets it.
#[tauri::command]
async fn revoke_key(desktop: State<'_, Desktop>, key: String) -> Result<Value, String> {
    let targets: Vec<(String, Result<Client, String>, String)> = {
        let store = desktop.store.lock().unwrap();
        store.state.servers.iter().filter_map(|s| s.devices.get(&key).map(|d| (s.id.clone(), client(&store, &s.id), d.clone()))).collect()
    };
    let mut results = serde_json::Map::new();
    for (sid, c, device) in targets {
        let r = match c {
            Ok(c) => c.delete(&format!("/api/devices/{device}")).await,
            Err(e) => Err(e),
        };
        results.insert(sid, match r { Ok(v) => v, Err(e) => json!({ "error": e }) });
    }
    let mut store = desktop.store.lock().unwrap();
    store.remove_key(&key)?;
    store.save()?;
    Ok(Value::Object(results))
}

// ---- Approvals ----

/// The key to sign with on a server, and the device the server knows it
/// as: Touch ID when this Mac has it and the server knows it, else a
/// plugged-in security key the server knows.
fn signing_key(desktop: &Desktop, server_id: &str, plugged: &[Vec<u8>], touch_id: bool) -> Option<(crate::store::Key, String)> {
    let store = desktop.store.lock().unwrap();
    let server = store.server(server_id)?;
    let usable = |k: &&crate::store::Key| {
        if k.is_touch_id() {
            touch_id
        } else {
            plugged.iter().any(|c| URL_SAFE_NO_PAD.encode(c) == k.credential_id)
        }
    };
    let mut keys: Vec<&crate::store::Key> = store.state.keys.iter().filter(usable).collect();
    keys.sort_by_key(|k| !k.is_touch_id());
    keys.into_iter().find_map(|k| server.devices.get(&k.id).map(|d| (k.clone(), d.clone())))
}

/// prepare's answer when the server knows a key and none is plugged in;
/// the shell's sheet (ApprovalSheet.tsx) waits on these words.
const NO_KEY_PLUGGED: &str = "Plug in a key this server knows.";

/// Fetches the request a key would sign for an approval, and keeps it. The
/// sheet is drawn from what this returns.
#[tauri::command]
async fn prepare(desktop: State<'_, Desktop>, server: String, approval: String) -> Result<Value, String> {
    let (c, keys) = {
        let store = desktop.store.lock().unwrap();
        (client(&store, &server)?, store.state.keys.clone())
    };
    if keys.is_empty() {
        return Err("Add a key first: Servers & keys, at the foot of the dock.".into());
    }
    let credentials = security_key_credentials(&keys);
    let plugged = tauri::async_runtime::spawn_blocking(move || fido::held(RP_ID, &credentials)).await.map_err(|e| e.to_string())?;
    let touch_id = touchid::this_mac().is_ok();
    let knows_any = desktop.store.lock().unwrap().server(&server).is_some_and(|s| keys.iter().any(|k| s.devices.contains_key(&k.id)));
    let (key, device) = signing_key(&desktop, &server, &plugged, touch_id).ok_or(if !knows_any {
        "This server knows none of your keys. Enrol one in Servers & keys, at the foot of the dock."
    } else {
        // The sheet waits on this one, and asks again until a key is plugged in.
        NO_KEY_PLUGGED
    })?;
    let enclave = match &key.enclave {
        Some(blob) => Some(URL_SAFE_NO_PAD.decode(blob).map_err(|e| e.to_string())?),
        None => None,
    };
    let given = c.post(&format!("/api/approvals/{approval}/challenge"), json!({ "device": device })).await?;
    let canonical = String::from_utf8(STANDARD.decode(given["canonical"].as_str().unwrap_or_default()).map_err(|e| e.to_string())?).map_err(|e| e.to_string())?;
    let passkey = &given["passkey"];
    let prepared = Prepared {
        device: device.clone(),
        challenge: given["challenge"].as_str().ok_or("no challenge")?.to_string(),
        rp_id: passkey["rp_id"].as_str().ok_or("the server did not name the key")?.to_string(),
        origin: passkey["origin"].as_str().unwrap_or(ORIGIN).to_string(),
        credential: URL_SAFE_NO_PAD.decode(passkey["credential_id"].as_str().unwrap_or_default()).map_err(|e| e.to_string())?,
        canonical: canonical.clone(),
        enclave,
    };
    let with = if prepared.enclave.is_some() { "touch-id" } else { "security-key" };
    desktop.prepared.lock().unwrap().insert(format!("{server}/{approval}"), prepared);
    let request: Value = serde_json::from_str(&canonical).map_err(|e| e.to_string())?;
    Ok(json!({ "request": request, "device": device, "with": with }))
}

/// What the Touch ID dialog says after "Legato is trying to".
fn touch_id_reason(request: &Value, server: &str) -> String {
    match request["title"].as_str().filter(|t| !t.is_empty()) {
        Some(title) => format!("sign \u{201c}{title}\u{201d} on {server}"),
        None => format!("sign an approval on {server}"),
    }
}

/// Signs the prepared request with one of its allow options: a touch of a
/// security key, or a fingerprint.
#[tauri::command]
async fn sign(desktop: State<'_, Desktop>, server: String, approval: String, option: String) -> Result<Value, String> {
    let p = desktop.prepared.lock().unwrap().remove(&format!("{server}/{approval}")).ok_or("Nothing is prepared for this approval; open it again.")?;
    let request: Value = serde_json::from_str(&p.canonical).map_err(|e| e.to_string())?;
    if !request["options"].as_array().into_iter().flatten().any(|o| o["id"] == option.as_str() && o["kind"] == "allow") {
        return Err(format!("{option} is not one of this request's allow options"));
    }
    let message = webauthn::signed_message(&p.challenge, &p.device, &option, &p.canonical);
    let client_data = webauthn::client_data("webauthn.get", &webauthn::decision_challenge(&message), &p.origin);
    let (cd, rp, cred) = (client_data.clone(), p.rp_id.clone(), p.credential.clone());
    let signed = match p.enclave.clone() {
        Some(blob) => {
            let name = desktop.store.lock().unwrap().server(&server).map(|s| s.name.clone()).unwrap_or_default();
            let reason = touch_id_reason(&request, &name);
            tauri::async_runtime::spawn_blocking(move || touchid::this_mac().and_then(|mac| touchid::sign(mac.as_ref(), &blob, &cd, &reason)))
                .await
                .map_err(|e| e.to_string())??
        }
        None => tauri::async_runtime::spawn_blocking(move || fido::sign(&rp, &cd, &cred)).await.map_err(|e| e.to_string())??,
    };
    let value = webauthn::assertion(&p.credential, &signed.auth_data, &signed.signature, &client_data);
    let c = {
        let store = desktop.store.lock().unwrap();
        client(&store, &server)?
    };
    c.post(
        &format!("/api/approvals/{approval}/decision"),
        json!({ "decision": "allow", "option": option, "by": "desktop", "signature": { "device": p.device, "challenge": p.challenge, "value": value } }),
    )
    .await
    .map_err(|e| webauthn::explain_refusal(&e, signed.verified))?;
    use sha2::Digest;
    Ok(json!({
        "device": p.device,
        "message_sha256": hex::encode(sha2::Sha256::digest(message.as_bytes())),
        "signature": hex::encode(&signed.signature),
        "verified": signed.verified,
        "counter": signed.counter,
    }))
}

/// A deny needs no signature.
#[tauri::command]
async fn deny(desktop: State<'_, Desktop>, server: String, approval: String) -> Result<(), String> {
    desktop.prepared.lock().unwrap().remove(&format!("{server}/{approval}"));
    let c = {
        let store = desktop.store.lock().unwrap();
        client(&store, &server)?
    };
    c.post(&format!("/api/approvals/{approval}/decision"), json!({ "decision": "deny", "by": "desktop" })).await.map(|_| ())
}

// ---- Updates ----

fn github() -> reqwest::Client {
    reqwest::Client::builder()
        .user_agent(concat!("legato-desktop/", env!("CARGO_PKG_VERSION")))
        .timeout(std::time::Duration::from_secs(120))
        .build()
        .expect("HTTP client")
}

/// The AppImage this app runs from: the AppImage runtime names it. Only an
/// AppImage updates itself.
fn appimage() -> Option<std::path::PathBuf> {
    std::env::var_os("APPIMAGE").map(Into::into).filter(|p: &std::path::PathBuf| p.is_file())
}

/// What this copy replaces to update: the AppImage on Linux, the app bundle
/// on a Mac; or why it cannot update itself.
enum Installed {
    AppImage(std::path::PathBuf),
    Bundle(std::path::PathBuf),
}

fn installed() -> Result<Installed, String> {
    if cfg!(target_os = "macos") {
        let exe = std::env::current_exe().map_err(|e| e.to_string())?;
        updates::app_bundle(&exe).map(Installed::Bundle)
    } else {
        appimage().map(Installed::AppImage).ok_or_else(|| "Only the AppImage updates itself; this copy was not started as one.".into())
    }
}

/// Whether a newer release has this platform's build, and whether this copy
/// can install it (if not, why: `reason`).
#[tauri::command]
async fn check_update() -> Result<Value, String> {
    let current = updates::version();
    let platform = updates::this_platform();
    let (Some(platform), Some(_)) = (platform, updates::parse(current)) else {
        return Ok(json!({ "version": current, "latest": null, "available": false, "can_install": false }));
    };
    let releases: Vec<Value> = github()
        .get(format!("https://api.github.com/repos/{}/releases?per_page=30", updates::REPOSITORY))
        .header("Accept", "application/vnd.github+json")
        .send()
        .await
        .map_err(|e| e.to_string())?
        .error_for_status()
        .map_err(|e| e.to_string())?
        .json()
        .await
        .map_err(|e| e.to_string())?;
    let latest = updates::newest_with(&releases, &platform);
    let available = latest.as_ref().is_some_and(|c| updates::newer(&c.version, current));
    let reason = installed().err();
    Ok(json!({ "version": current, "latest": latest, "available": available, "can_install": reason.is_none(), "reason": reason }))
}

/// Downloads the newest release's build for this platform, checks it
/// against the release's checksums, and puts it in place of this copy, the
/// old one kept beside it. Says the version installed; restart_app starts it.
#[tauri::command]
async fn apply_update() -> Result<String, String> {
    let target = installed()?;
    let platform = updates::this_platform().ok_or("No build of the app is made for this computer.")?;
    let status = check_update().await?;
    if status["available"] != true {
        return Err("Nothing newer to install.".into());
    }
    let latest: updates::Candidate = serde_json::from_value(status["latest"].clone()).map_err(|e| e.to_string())?;
    let client = github();
    let fetch = |url: String| {
        let client = client.clone();
        async move {
            let resp = client.get(url).send().await.map_err(|e| e.to_string())?.error_for_status().map_err(|e| e.to_string())?;
            resp.bytes().await.map_err(|e| e.to_string())
        }
    };
    let sums = String::from_utf8(fetch(latest.sums_url.clone()).await?.to_vec()).map_err(|e| e.to_string())?;
    let bytes = fetch(latest.asset_url.clone()).await?;
    updates::check_sum(&bytes, &sums, platform.asset)?;
    tauri::async_runtime::spawn_blocking(move || match target {
        Installed::AppImage(path) => updates::install(&path, &bytes),
        Installed::Bundle(path) => updates::install_bundle(&path, &bytes),
    })
    .await
    .map_err(|e| e.to_string())??;
    Ok(latest.version)
}

/// Starts the copy now in place, and quits this one.
#[tauri::command]
fn restart_app(app: AppHandle) -> Result<(), String> {
    match installed()? {
        Installed::AppImage(path) => {
            std::process::Command::new(path).spawn().map_err(|e| e.to_string())?;
        }
        // A new instance, as this one is still running.
        Installed::Bundle(path) => {
            std::process::Command::new("/usr/bin/open").arg("-n").arg(path).spawn().map_err(|e| e.to_string())?;
        }
    }
    app.exit(0);
    Ok(())
}

// ---- Start ----

fn state_path() -> std::path::PathBuf {
    if let Ok(p) = std::env::var("LEGATO_DESKTOP_STATE") {
        return p.into();
    }
    dirs::config_dir().unwrap_or_else(|| ".".into()).join("legato-desktop").join("state.json")
}

pub fn run() {
    let store = Store::open(state_path(), Box::new(Keyring)).expect("the app's saved state does not read");
    let hold_to_talk = store.state.voice.hold_to_talk;
    let backend = voicekey::backend(&voicekey::Platform::this());
    tauri::Builder::default()
        // A second start (the system opening a link) hands its link to the
        // app that runs and ends; the deep-link plugin reads the link.
        // `legato-desktop --voice` is the key, as the link is.
        .plugin(tauri_plugin_single_instance::init(|app, argv, _cwd| match voicekey::read_args(&argv) {
            Some(key) => voice_key(app, key, "link"),
            None => bring_to_front(app),
        }))
        .plugin(tauri_plugin_deep_link::init())
        // The confirm before one of the app's servers becomes a worker.
        .plugin(tauri_plugin_dialog::init())
        .manage(Desktop {
            store: Mutex::new(store),
            prepared: Mutex::default(),
            active: Mutex::default(),
            content_visible: Mutex::new(true),
            browser: Browser::default(),
            page_trusted: Mutex::default(),
            recording: Mutex::default(),
            recordings: Default::default(),
            imports: Mutex::default(),
            keys: Mutex::new(voicekey::Keys::new(hold_to_talk)),
            key: Mutex::new(KeyInfo { backend, key: None, hold_to_talk, can_change: false, problem: None, link: VOICE_LINK }),
            #[cfg(target_os = "linux")]
            portal: Mutex::default(),
            voice_targets: Mutex::default(),
            voice_session: Mutex::default(),
        })
        .setup(|app| {
            // Servers take a few seconds to answer on the network; listening
            // from the start has the pair form's list ready when it opens.
            app.state::<Desktop>().browser.start();
            // Links the browser extension opens: legato-desktop://import?…
            {
                use tauri_plugin_deep_link::DeepLinkExt;
                // An AppImage is not installed, so it says it handles the
                // links itself, each start, pointing at where it is now.
                #[cfg(target_os = "linux")]
                if let Err(e) = app.deep_link().register_all() {
                    println!("[link] could not register legato-desktop links: {e}");
                }
                let handle = app.handle().clone();
                app.deep_link().on_open_url(move |event| {
                    for url in event.urls() {
                        handle_link(&handle, url.as_str());
                    }
                });
                if let Ok(Some(urls)) = app.deep_link().get_current() {
                    for url in urls {
                        handle_link(app.handle(), url.as_str());
                    }
                }
            }
            start_voice_key(app.handle());
            let handle = app.handle().clone();
            tauri::async_runtime::spawn(async move {
                loop {
                    refresh_voice_targets(&handle).await;
                    tokio::time::sleep(voicemain::REFRESH).await;
                }
            });
            let window = WindowBuilder::new(app, "main")
                .title("Legato")
                .inner_size(1360.0, 880.0)
                .min_inner_size(820.0, 560.0)
                .decorations(false)
                .build()?;
            #[cfg(target_os = "linux")]
            overlay::install(&window)?;
            let size = window.inner_size()?.to_logical::<f64>(window.scale_factor()?);
            let shell = window.add_child(WebviewBuilder::new("shell", WebviewUrl::App("index.html".into())), LogicalPosition::new(0.0, 0.0), size)?;
            #[cfg(target_os = "linux")]
            shell.with_webview(|platform| overlay::adopt(&platform.inner(), None))?;
            #[cfg(not(target_os = "linux"))]
            let _ = shell;
            let handle = app.handle().clone();
            window.on_window_event(move |e| {
                if matches!(e, WindowEvent::Resized(_) | WindowEvent::ScaleFactorChanged { .. }) {
                    relayout(&handle);
                }
            });
            // Started with --voice. No page hears the voice-key event this
            // early: the shell's webview has not loaded.
            let args: Vec<String> = std::env::args().collect();
            if let Some(key) = voicekey::read_args(&args) {
                voice_key(app.handle(), key, "link");
            }
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            servers,
            show_server,
            page_approval_done,
            open_approval_in_page,
            page_trusted,
            trust_server_certificate,
            save_server_certificate,
            open_certificate,
            set_content_visible,
            reload_server,
            load_server_page,
            pair_server,
            discovered,
            rename_server,
            remove_server,
            reorder_servers,
            poll,
            keys,
            add_key,
            touch_id,
            add_touch_id,
            enroll_key,
            revoke_key,
            prepare,
            sign,
            deny,
            check_update,
            apply_update,
            restart_app,
            take_import_requests,
            import_preference,
            set_import_preference,
            import_ticket,
            open_task_in_page,
            voice_key_info,
            change_voice_key,
            set_hold_to_talk,
            voice_targets,
            set_voice_server,
            voice_action
        ])
        .run(tauri::generate_context!())
        .expect("the app did not start");
}
