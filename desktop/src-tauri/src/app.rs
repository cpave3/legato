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
use crate::store::{Anchor, ImportTarget, Keyring, Offset, ScreenId, Server, Store};
use crate::updates;
use crate::webauthn::{self, Attachment, ORIGIN, RP_ID};
use crate::discovery::{Browser, Found};
use crate::links;
use crate::bridge::{self, PageMessage, VoiceAction, VoiceEvent};
use crate::{fido, screens, systrust, touchid, voice, voiceclient, voicekey, voicemain, voicewidget, widgetapprovals, workers};
use crate::voiceclient::{Command, Ui};
use crate::voicekey::{Backend, Input, KeyBackend, VoiceKey};

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
    /// The keys the app grabbed (`Backend::Grab`), to tell talk from cancel.
    grabbed: Mutex<Vec<(tauri_plugin_global_shortcut::Shortcut, voicekey::Shortcut, String)>>,
    #[cfg(target_os = "linux")]
    portal: Mutex<Option<std::sync::Arc<crate::voicekey_portal::Portal>>>,
    /// The instance each server last said it is (`/api/reeve/voice`), by
    /// the app's server ID: which one is the main.
    voice_targets: Mutex<HashMap<String, voicemain::Instance>>,
    /// The conversation with the Reeve, while there is one.
    voice_session: Mutex<Option<tokio::sync::mpsc::UnboundedSender<Command>>>,
    /// Every server has been asked which instance it is, once.
    voice_targets_known: tokio::sync::watch::Sender<bool>,
    /// The voice widget: what it has to show, and how it keeps its place.
    widget: Mutex<WidgetState>,
    widget_placement: Mutex<voicewidget::Placement>,
    /// The screens, as last read on GTK's main thread (`refresh_screens`).
    screens: Mutex<Vec<screens::Screen>>,
    #[cfg(target_os = "linux")]
    layer_shell: Mutex<Option<std::sync::Arc<voicewidget::layer_shell::LayerShell>>>,
    /// Every server's pending approvals, as the widget shows them.
    widget_approvals: Mutex<Vec<widgetapprovals::Listed>>,
    /// Asks the servers again now (a decision was made).
    approvals_asked: tokio::sync::Notify,
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
        // Files dropped on the page are the page's: an image dropped on an
        // agent's chat. Tauri's own handler would finish the drop itself
        // (wry returns true to GTK), and WebKit would never fire `drop`.
        .disable_drag_drop_handler()
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
    /// The way the key is had now.
    backend: Backend,
    /// The way chosen for a Wayland desktop (`voice.key_backend`), which
    /// applies from the next start.
    choice: KeyBackend,
    /// This desktop can choose: Wayland with XWayland.
    can_choose: bool,
    /// The bound key as the person reads it; None when none is bound.
    key: Option<String>,
    /// The grabbed keys, as the plugin reads them (`Backend::Grab`).
    talk: Option<String>,
    cancel: Option<String>,
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
/// `{key: "talk" | "hold" | "release" | "cancel", from: "grab" | "portal" | "link"}`.
/// The press is also handed to the conversation, which it starts when
/// there is none.
fn voice_key(app: &AppHandle, key: VoiceKey, from: &'static str) {
    println!("[voice key] {key:?} from the {from}");
    let _ = app.emit("voice-key", json!({ "key": key, "from": from }));
    voice_command(app, Command::Key(key));
}

// The voice events go to every webview, but only the app's own pages can
// listen: a server's page has no capability, so no event:listen, and is
// never told what is said to the Reeve. (In Tauri 2 a target or filter would
// not keep them out: a JS listener's target is Any, which every emit
// matches.)

// ---- Talking to the Reeve ----

fn tell_widget(app: &AppHandle, ui: Ui) {
    // The log has states and line IDs, never what was said.
    match &ui {
        Ui::State { state, .. } => println!("[voice] {state}"),
        Ui::Line { id, status, .. } => println!("[voice] line {id} {status}"),
        _ => {}
    }
    match &ui {
        Ui::Caption { kind: "written", .. } => app.state::<Desktop>().widget.lock().unwrap().written = true,
        Ui::State { state, reason, .. } => {
            let showing = {
                let desktop = app.state::<Desktop>();
                let mut widget = desktop.widget.lock().unwrap();
                if state == "connecting" {
                    widget.written = false;
                }
                widget.showing = voicewidget::showing(state, reason.as_deref(), widget.written, std::time::Instant::now());
                widget.showing
            };
            show_widget_as_it_should(app);
            // A hidden widget that showed to say something goes again after.
            if let voicewidget::Showing::Until(until) = showing {
                let app = app.clone();
                tauri::async_runtime::spawn(async move {
                    tokio::time::sleep_until(until.into()).await;
                    show_widget_as_it_should(&app);
                });
            }
        }
        _ => {}
    }
    let _ = app.emit(ui.name(), ui.payload());
}

// ---- The voice widget ----

/// What a layer surface was last given: its screen (GDK's index), anchor,
/// margins and size.
pub type Applied = (Option<usize>, Anchor, Offset, (f64, f64));

pub struct WidgetState {
    showing: voicewidget::Showing,
    /// The Reeve answered in writing this conversation.
    written: bool,
    /// The logical size the page last asked for.
    size: (f64, f64),
    /// The anchor and margins a layer surface has now.
    applied: Option<Applied>,
    /// The widget being dragged.
    drag: Option<Drag>,
    /// The layout the page was last told.
    layout: Option<WidgetLayout>,
}

/// What the page needs to lay itself out: the most it may be (logical,
/// `voicewidget::limits` of the monitor it is on), and the anchor, whose
/// side it aligns to.
#[derive(Debug, Clone, Copy, PartialEq, Serialize)]
pub struct WidgetLayout {
    width: f64,
    height: f64,
    anchor: Anchor,
}

fn widget_layout(app: &AppHandle) -> WidgetLayout {
    let anchor = app.state::<Desktop>().store.lock().unwrap().state.voice.anchor;
    let area = app.get_webview_window(voicewidget::LABEL).and_then(|w| widget_area(&w));
    let (width, height) = area.map(|(area, scale)| voicewidget::limits(logical(area, scale))).unwrap_or(voicewidget::MAX_SIZE);
    WidgetLayout { width, height, anchor }
}

/// Tells the page its layout (`voice-widget-layout`) when it changed.
fn tell_layout(app: &AppHandle) {
    let layout = widget_layout(app);
    {
        let desktop = app.state::<Desktop>();
        let mut widget = desktop.widget.lock().unwrap();
        if widget.layout == Some(layout) {
            return;
        }
        widget.layout = Some(layout);
    }
    let _ = app.emit_to(voicewidget::LABEL, "voice-widget-layout", layout);
}

/// The page asks its layout at start.
#[tauri::command]
fn voice_widget_layout(app: AppHandle) -> WidgetLayout {
    let layout = widget_layout(&app);
    app.state::<Desktop>().widget.lock().unwrap().layout = Some(layout);
    layout
}

/// A drag of the widget: its top-left within the work area it started on,
/// logical, that area, and the corner a layer surface keeps to meanwhile.
#[derive(Clone, Copy)]
struct Drag {
    pos: (f64, f64),
    area: voicewidget::Rect,
    scale: f64,
    corner: Anchor,
}

/// Makes the `voice` window: the app's own page, transparent, undecorated,
/// out of the taskbar, above other windows, never focused; on Wayland a
/// layer surface when gtk-layer-shell is there (see voicewidget).
fn make_voice_widget(app: &AppHandle) -> tauri::Result<()> {
    use tauri::WebviewWindowBuilder;
    let (w, h) = voicewidget::FIRST_SIZE;
    let builder = WebviewWindowBuilder::new(app, voicewidget::LABEL, WebviewUrl::App("voice.html".into()))
        .title("Legato widget")
        .inner_size(w, h)
        .decorations(false)
        .transparent(true)
        .shadow(false)
        .resizable(false)
        .skip_taskbar(true)
        .always_on_top(true)
        .visible_on_all_workspaces(true)
        .focused(false)
        .focusable(false)
        .visible(false);
    let window = builder.build()?;
    // GTK's own display, not the environment: GDK_BACKEND=x11 on a Wayland
    // session gives an X11 window, which the app places.
    #[cfg(target_os = "linux")]
    let wayland = window.gtk_window().map(|w| { use gtk::prelude::*; WidgetExt::display(&w).type_().name() == "GdkWaylandDisplay" }).unwrap_or(false);
    #[cfg(not(target_os = "linux"))]
    let wayland = false;
    #[allow(unused_mut)]
    let mut layered = false;
    #[cfg(target_os = "linux")]
    if wayland {
        layered = match voicewidget::layer_shell::LayerShell::load() {
            Ok(shell) => match window.gtk_window().map_err(|e| e.to_string()).and_then(|gtk| shell.attach(&gtk, voicewidget::FIRST_SIZE)) {
                Ok(()) => {
                    *app.state::<Desktop>().layer_shell.lock().unwrap() = Some(std::sync::Arc::new(shell));
                    true
                }
                Err(e) => {
                    println!("[voice widget] layer shell refused the window: {e}");
                    false
                }
            },
            Err(e) => {
                println!("[voice widget] no layer shell ({e}); the widget is an ordinary window");
                false
            }
        };
    }
    let placement = voicewidget::placement(wayland, layered);
    println!("[voice widget] placed as {placement:?}");
    *app.state::<Desktop>().widget_placement.lock().unwrap() = placement;
    // A layer surface is anchored before it is first shown.
    place_widget(app);
    show_widget_as_it_should(app);
    Ok(())
}

/// Shows or hides the widget as the person's choice and the conversation say.
fn show_widget_as_it_should(app: &AppHandle) {
    let Some(window) = app.get_webview_window(voicewidget::LABEL) else { return };
    let desktop = app.state::<Desktop>();
    let hidden = desktop.store.lock().unwrap().state.voice.widget_hidden;
    let showing = desktop.widget.lock().unwrap().showing;
    let waiting = !desktop.widget_approvals.lock().unwrap().is_empty();
    if voicewidget::shows(hidden, showing, waiting, std::time::Instant::now()) {
        let _ = window.show();
        place_widget(app);
    } else {
        let _ = window.hide();
    }
}

/// The screen the widget goes on, when the person chose one: that one
/// while it is here, else the primary, else the first (`screens::screen_for`).
fn widget_screen(app: &AppHandle) -> Option<screens::Screen> {
    let desktop = app.state::<Desktop>();
    let chosen = desktop.store.lock().unwrap().state.voice.screen.clone();
    let list = desktop.screens.lock().unwrap();
    screens::screen_for(chosen.as_ref(), &list).map(|i| list[i].clone())
}

/// The work area (physical) and scale of the screen the widget goes on:
/// the chosen one, else the monitor it is on. On Wayland GTK knows no
/// panels, so it is the whole monitor; a layer surface's margins count from
/// inside the panels.
fn widget_area(window: &tauri::WebviewWindow) -> Option<(voicewidget::Rect, f64)> {
    if let Some(screen) = widget_screen(window.app_handle()) {
        return Some((screen.area, screen.scale));
    }
    let monitor = window
        .current_monitor()
        .ok()
        .flatten()
        .or_else(|| window.primary_monitor().ok().flatten())
        .or_else(|| window.available_monitors().ok().and_then(|m| m.into_iter().next()))?;
    let area = monitor.work_area();
    let rect = voicewidget::Rect { x: area.position.x as f64, y: area.position.y as f64, width: area.size.width as f64, height: area.size.height as f64 };
    Some((rect, monitor.scale_factor()))
}

/// The area's size in logical pixels.
fn logical(area: voicewidget::Rect, scale: f64) -> (f64, f64) {
    (area.width / scale, area.height / scale)
}

/// Puts the widget at its anchor, offset as saved and kept on the screen:
/// the app places it (X11, macOS), a layer surface is anchored with margins
/// (Wayland). Not during a drag, which places it itself.
fn place_widget(app: &AppHandle) {
    let Some(window) = app.get_webview_window(voicewidget::LABEL) else { return };
    let desktop = app.state::<Desktop>();
    let (anchor, offset) = {
        let voice = &desktop.store.lock().unwrap().state.voice;
        (voice.anchor, voice.offset)
    };
    tell_layout(app);
    let placement = *desktop.widget_placement.lock().unwrap();
    let (size, dragging) = {
        let widget = desktop.widget.lock().unwrap();
        (widget.size, widget.drag.is_some())
    };
    if dragging {
        return;
    }
    match placement {
        voicewidget::Placement::Placed => {
            let Some((area, scale)) = widget_area(&window) else { return };
            let (x, y) = voicewidget::place(anchor, offset, area, size, scale);
            let _ = window.set_position(tauri::PhysicalPosition::new(x.round() as i32, y.round() as i32));
        }
        voicewidget::Placement::LayerShell => {
            let m = match widget_area(&window) {
                Some((area, scale)) => voicewidget::margins(anchor, offset, logical(area, scale), size),
                None => voicewidget::margins(anchor, offset, (f64::MAX, f64::MAX), size),
            };
            anchor_layer(app, &window, anchor, m, false);
        }
        voicewidget::Placement::Floating => {}
    }
}

/// Anchors the layer surface with these margins, at the widget's size,
/// when either changed; on GTK's main thread, where this may not be (a
/// conversation runs on tokio's). Only the last placement asked is applied:
/// one overtaken while it waited for the main thread is skipped. With
/// `tell`, a change returns a receiver told once the frame carrying it is
/// committed (`voicewidget::moved`).
#[cfg(target_os = "linux")]
fn anchor_layer(app: &AppHandle, window: &tauri::WebviewWindow, anchor: Anchor, m: Offset, tell: bool) -> Option<tokio::sync::oneshot::Receiver<()>> {
    let monitor = widget_screen(app).map(|s| s.index);
    let desktop = app.state::<Desktop>();
    let (applied, size) = {
        let mut widget = desktop.widget.lock().unwrap();
        let applied = Some((monitor, anchor, m, widget.size));
        if widget.applied == applied {
            return None;
        }
        widget.applied = applied;
        (applied, widget.size)
    };
    let shell = desktop.layer_shell.lock().unwrap().clone();
    let window = window.clone();
    let handle = app.clone();
    let (moved, moving) = tokio::sync::oneshot::channel();
    let _ = app.run_on_main_thread(move || {
        if handle.state::<Desktop>().widget.lock().unwrap().applied != applied {
            return;
        }
        if let (Some(shell), Ok(gtk)) = (shell, window.gtk_window()) {
            shell.place(&gtk, monitor, anchor, m, size);
            if tell {
                voicewidget::layer_shell::after_next_paint(&gtk, move || {
                    let _ = moved.send(());
                });
            }
        }
    });
    tell.then_some(moving)
}

#[cfg(not(target_os = "linux"))]
fn anchor_layer(_app: &AppHandle, _window: &tauri::WebviewWindow, _anchor: Anchor, _m: Offset, _tell: bool) -> Option<tokio::sync::oneshot::Receiver<()>> {
    None
}

/// The page's size, so the window is the size of what it draws.
#[tauri::command]
fn voice_widget_size(app: AppHandle, width: f64, height: f64) {
    let Some(window) = app.get_webview_window(voicewidget::LABEL) else { return };
    let layout = widget_layout(&app);
    let (w, h) = voicewidget::clamp_size(width, height, (layout.width, layout.height));
    app.state::<Desktop>().widget.lock().unwrap().size = (w, h);
    let _ = window.set_size(tauri::LogicalSize::new(w, h));
    place_widget(&app);
}

/// The page's pointer moved far enough with a button down to be a drag,
/// not a click: the widget follows it from where it is now. An ordinary
/// window on Wayland is moved by the compositor instead, and not kept.
#[tauri::command]
fn voice_widget_drag_start(app: AppHandle) {
    let Some(window) = app.get_webview_window(voicewidget::LABEL) else { return };
    let desktop = app.state::<Desktop>();
    let placement = *desktop.widget_placement.lock().unwrap();
    if placement == voicewidget::Placement::Floating {
        let _ = window.start_dragging();
        return;
    }
    let Some((area, scale)) = widget_area(&window) else { return };
    let (anchor, offset) = {
        let voice = &desktop.store.lock().unwrap().state.voice;
        (voice.anchor, voice.offset)
    };
    let mut widget = desktop.widget.lock().unwrap();
    let span = logical(area, scale);
    let pos = voicewidget::origin(anchor, voicewidget::margins(anchor, offset, span, widget.size), span, widget.size);
    widget.drag = Some(Drag { pos, area, scale, corner: voicewidget::drag_corner(anchor) });
}

/// The pointer moved by dx, dy (logical) from where it took hold of the
/// widget: the widget moves as much, kept on the screen. The page measures
/// its next step from where the window is when this answers, so it answers
/// once a layer surface has moved (`voicewidget::moved`, LEG-148), and an
/// X11 window once its move was asked on the main thread, as before.
#[tauri::command]
async fn voice_widget_drag_move(app: AppHandle, dx: f64, dy: f64) {
    voicewidget::moved(drag_step(&app, dx, dy)).await;
}

/// Moves the dragged widget by dx, dy; the receiver told when the move is
/// done (see `voice_widget_drag_move`).
fn drag_step(app: &AppHandle, dx: f64, dy: f64) -> Option<tokio::sync::oneshot::Receiver<()>> {
    let window = app.get_webview_window(voicewidget::LABEL)?;
    let desktop = app.state::<Desktop>();
    let placement = *desktop.widget_placement.lock().unwrap();
    let (drag, size) = {
        let mut widget = desktop.widget.lock().unwrap();
        let size = widget.size;
        let drag = widget.drag.as_mut()?;
        drag.pos = voicewidget::drag_to(drag.pos, (dx, dy), logical(drag.area, drag.scale), size);
        (*drag, size)
    };
    match placement {
        voicewidget::Placement::Placed => {
            let (x, y) = (drag.area.x + drag.pos.0 * drag.scale, drag.area.y + drag.pos.1 * drag.scale);
            let at = tauri::PhysicalPosition::new(x.round() as i32, y.round() as i32);
            let (moved, moving) = tokio::sync::oneshot::channel();
            let _ = app.run_on_main_thread(move || {
                let _ = window.set_position(at);
                let _ = moved.send(());
            });
            Some(moving)
        }
        voicewidget::Placement::LayerShell => {
            let m = voicewidget::margins_for(drag.corner, drag.pos, logical(drag.area, drag.scale), size);
            anchor_layer(app, &window, drag.corner, m, true)
        }
        voicewidget::Placement::Floating => None,
    }
}

/// The pointer let go: the widget keeps to the nearest corner or edge, as
/// far from it as it was dropped, and that is saved.
#[tauri::command]
fn voice_widget_drag_end(app: AppHandle) -> Result<(), String> {
    let desktop = app.state::<Desktop>();
    let (drag, size) = {
        let mut widget = desktop.widget.lock().unwrap();
        (widget.drag.take(), widget.size)
    };
    let Some(drag) = drag else { return Ok(()) };
    let (anchor, offset) = voicewidget::drop_at(drag.pos, size, logical(drag.area, drag.scale));
    println!("[voice widget] dropped: {anchor:?}");
    {
        let mut store = desktop.store.lock().unwrap();
        store.state.voice.anchor = anchor;
        store.state.voice.offset = Some(offset);
        store.save()?;
    }
    place_widget(&app);
    let _ = app.emit("voice-widget-changed", voice_widget_settings(app.state::<Desktop>()));
    Ok(())
}

/// Reads the screens (GDK's monitors, with Wayland's connectors), keeps
/// them, says them on the output, and puts the widget again. On GTK's main
/// thread. Elsewhere than Linux there is no list, and the widget is on the
/// monitor it is on.
fn refresh_screens(app: &AppHandle) {
    #[cfg(target_os = "linux")]
    {
        let list = read_screens();
        let names = screens::names(&list);
        let said: Vec<String> = list.iter().zip(&names).map(|(s, n)| format!("{n} [{}]", s.connector.as_deref().unwrap_or("-"))).collect();
        println!("[voice widget] screens: {}", said.join(", "));
        *app.state::<Desktop>().screens.lock().unwrap() = list;
        if let Some(screen) = widget_screen(app) {
            let chosen = app.state::<Desktop>().store.lock().unwrap().state.voice.screen.clone();
            let here = chosen.as_ref().is_some_and(|c| screens::find(c, &app.state::<Desktop>().screens.lock().unwrap()).is_some());
            println!("[voice widget] on screen {} ({})", screen.index, if here { "the one chosen" } else { "the chosen one is missing" });
        }
        place_widget(app);
        let _ = app.emit("voice-widget-changed", voice_widget_settings(app.state::<Desktop>()));
    }
    #[cfg(not(target_os = "linux"))]
    let _ = app;
}

/// GDK's monitors, in its order, with each one's connector when Wayland
/// says it (`wloutputs`).
#[cfg(target_os = "linux")]
fn read_screens() -> Vec<screens::Screen> {
    use gtk::prelude::*;
    let Some(display) = gtk::gdk::Display::default() else { return Vec::new() };
    let monitors: Vec<gtk::gdk::Monitor> = (0..display.n_monitors()).filter_map(|i| display.monitor(i)).collect();
    let outputs = if display.type_().name() == "GdkWaylandDisplay" {
        crate::wloutputs::read().unwrap_or_else(|e| {
            println!("[voice widget] could not read the screens' connectors: {e}");
            Vec::new()
        })
    } else {
        Vec::new()
    };
    let places: Vec<(i32, i32)> = monitors.iter().map(|m| (m.geometry().x(), m.geometry().y())).collect();
    let matched = crate::wloutputs::match_monitors(&places, &outputs);
    let text = |s: Option<gtk::glib::GString>| s.map(|s| s.trim().to_string()).filter(|s| !s.is_empty());
    monitors
        .iter()
        .zip(matched)
        .enumerate()
        .map(|(index, (m, out))| {
            let scale = m.scale_factor() as f64;
            let w = m.workarea();
            screens::Screen {
                index,
                maker: text(m.manufacturer()).or_else(|| out.as_ref().map(|o| o.maker.clone())).unwrap_or_default(),
                model: text(m.model()).or_else(|| out.as_ref().map(|o| o.model.clone())).unwrap_or_default(),
                connector: out.and_then(|o| o.connector),
                area: voicewidget::Rect { x: w.x() as f64 * scale, y: w.y() as f64 * scale, width: w.width() as f64 * scale, height: w.height() as f64 * scale },
                scale,
                primary: m.is_primary(),
            }
        })
        .collect()
}

/// Reads the screens again whenever one comes or goes, a moment later, so
/// the compositor has said all about it.
#[cfg(target_os = "linux")]
fn watch_screens(app: &AppHandle) {
    let Some(display) = gtk::gdk::Display::default() else { return };
    let later = |app: AppHandle| {
        gtk::glib::timeout_add_local_once(std::time::Duration::from_millis(500), move || refresh_screens(&app));
    };
    let (added, removed) = (app.clone(), app.clone());
    display.connect_monitor_added(move |_, _| later(added.clone()));
    display.connect_monitor_removed(move |_, _| later(removed.clone()));
}

/// The screen the person chose, or none for where the desktop puts it.
fn set_screen(app: &AppHandle, screen: Option<ScreenId>) -> Result<(), String> {
    {
        let desktop = app.state::<Desktop>();
        let mut store = desktop.store.lock().unwrap();
        store.state.voice.screen = screen;
        store.save()?;
    }
    place_widget(app);
    let _ = app.emit("voice-widget-changed", voice_widget_settings(app.state::<Desktop>()));
    Ok(())
}

/// Servers & keys: the screen the widget goes on.
#[tauri::command]
fn set_voice_screen(app: AppHandle, screen: Option<ScreenId>) -> Result<(), String> {
    set_screen(&app, screen)
}

/// The widget's menu (a right click): Hide, Place, Screen, Settings.
#[tauri::command]
fn voice_widget_menu(app: AppHandle) -> Result<(), String> {
    use tauri::menu::{CheckMenuItem, Menu, MenuItem, PredefinedMenuItem, Submenu};
    let window = app.get_webview_window(voicewidget::LABEL).ok_or("no widget")?;
    let anchor = app.state::<Desktop>().store.lock().unwrap().state.voice.anchor;
    let e = |e: tauri::Error| e.to_string();
    let hide = MenuItem::with_id(&app, "voice-hide", "Hide", true, None::<&str>).map_err(e)?;
    let places = voicewidget::PLACES
        .iter()
        .map(|(a, id, label)| CheckMenuItem::with_id(&app, *id, *label, true, *a == anchor, None::<&str>))
        .collect::<Result<Vec<_>, _>>()
        .map_err(e)?;
    // Corners, then edges.
    let between = PredefinedMenuItem::separator(&app).map_err(e)?;
    let mut items: Vec<&dyn tauri::menu::IsMenuItem<tauri::Wry>> = places.iter().map(|c| c as &dyn tauri::menu::IsMenuItem<tauri::Wry>).collect();
    items.insert(4, &between);
    let place_menu = Submenu::with_items(&app, "Place", true, &items).map_err(e)?;
    // The screens, when there is a choice to make.
    let (list, chosen) = {
        let desktop = app.state::<Desktop>();
        let list = desktop.screens.lock().unwrap().clone();
        let chosen = desktop.store.lock().unwrap().state.voice.screen.clone();
        (list, chosen)
    };
    let on = screens::screen_for(chosen.as_ref(), &list);
    let auto = CheckMenuItem::with_id(&app, "screen-auto", "Where the desktop puts it", true, chosen.is_none(), None::<&str>).map_err(e)?;
    let screen_items = screens::names(&list)
        .iter()
        .enumerate()
        .map(|(i, name)| CheckMenuItem::with_id(&app, format!("screen-{i}"), name, true, chosen.is_some() && on == Some(i), None::<&str>))
        .collect::<Result<Vec<_>, _>>()
        .map_err(e)?;
    let mut items: Vec<&dyn tauri::menu::IsMenuItem<tauri::Wry>> = vec![&auto];
    items.extend(screen_items.iter().map(|c| c as &dyn tauri::menu::IsMenuItem<tauri::Wry>));
    let screen_menu = Submenu::with_items(&app, "Screen", true, &items).map_err(e)?;
    let settings = MenuItem::with_id(&app, "voice-settings", "Settings…", true, None::<&str>).map_err(e)?;
    let menu = if list.len() > 1 || chosen.is_some() {
        Menu::with_items(&app, &[&hide, &place_menu, &screen_menu, &settings])
    } else {
        Menu::with_items(&app, &[&hide, &place_menu, &settings])
    }
    .map_err(e)?;
    window.popup_menu(&menu).map_err(e)
}

/// What the widget's menu asked.
fn voice_widget_menu_chosen(app: &AppHandle, id: &str) {
    let desktop = app.state::<Desktop>();
    if id == "voice-hide" {
        let _ = set_widget(app, &desktop, Some(true), None);
    } else if id == "voice-settings" {
        bring_to_front(app);
        let _ = app.emit_to("shell", "shell-open-panel", json!({ "kind": "manage" }));
    } else if let Some((anchor, _, _)) = voicewidget::PLACES.iter().find(|(_, pid, _)| *pid == id) {
        let _ = set_widget(app, &desktop, None, Some(*anchor));
    } else if id == "screen-auto" {
        let _ = set_screen(app, None);
    } else if let Some(i) = id.strip_prefix("screen-").and_then(|i| i.parse::<usize>().ok()) {
        let screen = desktop.screens.lock().unwrap().get(i).map(|s| s.id());
        if let Some(screen) = screen {
            let _ = set_screen(app, Some(screen));
        }
    }
}

/// Saves the person's choices; an anchor chosen (not dropped) forgets where
/// the widget was dropped.
fn set_widget(app: &AppHandle, desktop: &Desktop, hidden: Option<bool>, anchor: Option<Anchor>) -> Result<(), String> {
    {
        let mut store = desktop.store.lock().unwrap();
        if let Some(h) = hidden {
            store.state.voice.widget_hidden = h;
        }
        if let Some(a) = anchor {
            if store.state.voice.anchor != a || store.state.voice.offset.is_some() {
                store.state.voice.anchor = a;
                store.state.voice.offset = None;
            }
        }
        store.save()?;
    }
    show_widget_as_it_should(app);
    let _ = app.emit("voice-widget-changed", voice_widget_settings(app.state::<Desktop>()));
    Ok(())
}

#[derive(Clone, Serialize)]
pub struct WidgetSettings {
    shown: bool,
    anchor: Anchor,
    /// Dragged to a spot of its own, rather than at the anchor's margin.
    dropped: bool,
    /// The screens there are, named (`screens::names`).
    screens: Vec<ScreenChoice>,
    /// The screen chosen; none is where the desktop puts it.
    screen: Option<ScreenId>,
    /// Where in `screens` the chosen one is, while it is connected.
    chosen: Option<usize>,
    /// The chosen screen's name when it is not connected now (the widget
    /// is on the primary or the first meanwhile).
    missing: Option<String>,
}

#[derive(Clone, Serialize)]
pub struct ScreenChoice {
    id: ScreenId,
    name: String,
}

#[tauri::command]
fn voice_widget_settings(desktop: State<'_, Desktop>) -> WidgetSettings {
    let v = desktop.store.lock().unwrap().state.voice.clone();
    let list = desktop.screens.lock().unwrap().clone();
    let screens: Vec<ScreenChoice> = list.iter().zip(screens::names(&list)).map(|(s, name)| ScreenChoice { id: s.id(), name }).collect();
    let chosen = v.screen.as_ref().and_then(|c| screens::find(c, &list));
    let missing = v.screen.as_ref().filter(|_| chosen.is_none()).map(|c| {
        let alone = screens::Screen { index: 0, maker: c.maker.clone(), model: c.model.clone(), connector: c.connector.clone(), area: voicewidget::Rect { x: 0.0, y: 0.0, width: 0.0, height: 0.0 }, scale: 1.0, primary: false };
        screens::names(&[alone]).remove(0)
    });
    WidgetSettings { shown: !v.widget_hidden, anchor: v.anchor, dropped: v.offset.is_some(), screens, screen: v.screen, chosen, missing }
}

/// Servers & keys: show or hide the widget, and where it keeps to.
#[tauri::command]
fn set_voice_widget(app: AppHandle, desktop: State<'_, Desktop>, shown: bool, anchor: Anchor) -> Result<(), String> {
    set_widget(&app, &desktop, Some(!shown), Some(anchor))
}

/// Not ready: Open settings opens the main's settings page at the setting,
/// in front.
#[tauri::command]
fn voice_open_settings(app: AppHandle, desktop: State<'_, Desktop>, setting: String) -> Result<(), String> {
    let voicemain::Choice::Server(server) = voice_choice(&desktop) else {
        bring_to_front(&app);
        let _ = app.emit_to("shell", "shell-open-panel", json!({ "kind": "manage" }));
        return Ok(());
    };
    let base = desktop.store.lock().unwrap().server(&server).map(|s| s.url.clone()).ok_or("no such server")?;
    let url = voicewidget::settings_url(&base, &setting)?;
    match app.get_webview(&webview_label(&server)) {
        Some(view) => view.navigate(url).map_err(|e| e.to_string())?,
        None => {
            let path = format!("{}#{}", url.path(), url.fragment().unwrap_or_default());
            let (s, token) = {
                let store = desktop.store.lock().unwrap();
                (store.server(&server).cloned().ok_or("no such server")?, store.token(&server))
            };
            make_server_view(&app, &s, token, &path)?;
        }
    }
    *desktop.active.lock().unwrap() = Some(server.clone());
    show_only_active(&app, &desktop);
    let _ = app.emit_to("shell", "shell-show-server", json!({ "server": server }));
    bring_to_front(&app);
    Ok(())
}

#[derive(Serialize)]
pub struct VoiceStatus {
    /// The main's name, as the app calls it.
    main: String,
    ready: bool,
    reason: String,
    setting: String,
}

/// Whether the key would listen now, for the widget at rest (asked when it
/// starts, every 30 seconds and when the servers change). Records nothing.
#[tauri::command]
async fn voice_status(app: AppHandle) -> Result<VoiceStatus, String> {
    Ok(match ready_main(&app).await {
        Ok((_, name, _)) => VoiceStatus { main: name, ready: true, reason: String::new(), setting: String::new() },
        Err(n) => VoiceStatus { main: n.main, ready: false, reason: n.reason, setting: n.setting.unwrap_or_default() },
    })
}

struct NotReady {
    main: String,
    reason: String,
    setting: Option<String>,
}

/// The main to talk to, if it can listen now: its server ID, name and
/// client; else why not.
async fn ready_main(app: &AppHandle) -> Result<(String, String, Client), NotReady> {
    let desktop = app.state::<Desktop>();
    let refuse = |main: &str, reason: String, setting: Option<String>| NotReady { main: main.into(), reason, setting };
    let server = match voice_choice(&desktop) {
        voicemain::Choice::Server(id) => id,
        voicemain::Choice::Ask => return Err(refuse("", "Choose the Reeve to talk to in Servers & keys".into(), None)),
        voicemain::Choice::None => return Err(refuse("", "Pair a Legato server first".into(), None)),
    };
    let (name, client) = {
        let store = desktop.store.lock().unwrap();
        let name = store.server(&server).map(|s| s.name.clone()).unwrap_or_default();
        (name, client(&store, &server))
    };
    let client = client.map_err(|e| refuse(&name, e, None))?;
    let answer = client
        .get_within("/api/reeve/voice", voicemain::STATUS_TIMEOUT)
        .await
        .and_then(|v| serde_json::from_value::<voicemain::Readiness>(v).map_err(|e| e.to_string()));
    if let Ok(r) = &answer {
        desktop.voice_targets.lock().unwrap().insert(server.clone(), r.instance.clone());
    }
    if let Some(reason) = voicemain::not_ready(&answer, &name, voice::system_recorder().is_some()) {
        let setting = answer.ok().map(|r| r.setting).filter(|s| !s.is_empty());
        return Err(refuse(&name, reason, setting));
    }
    Ok((server, name, client))
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
    // A press at a cold start waits for the first ask of the servers, so
    // the main is known.
    {
        let mut known = app.state::<Desktop>().voice_targets_known.subscribe();
        let _ = tokio::time::timeout(std::time::Duration::from_secs(10), known.wait_for(|k| *k)).await;
    }
    let main = {
        let desktop = app.state::<Desktop>();
        match voice_choice(&desktop) {
            voicemain::Choice::Server(id) => desktop.store.lock().unwrap().server(&id).map(|s| s.name.clone()).unwrap_or_default(),
            _ => String::new(),
        }
    };
    tell_widget(app, Ui::state("connecting", Some(&main)));
    let (_, name, client) = match ready_main(app).await {
        Ok(ready) => ready,
        Err(n) => return not_ready(n.reason, n.setting),
    };
    let Some((program, args)) = voice::system_recorder() else { return not_ready("No microphone on this computer".into(), None) };
    // Recording starts before the session opens: what is said meanwhile is
    // kept for the main (voiceclient::Audio::Hold).
    let (audio_tx, audio) = tokio::sync::mpsc::unbounded_channel();
    let mut recorder = match voice::Recorder::stream(program, args, move |bytes| {
        let _ = audio_tx.send(bytes.to_vec());
    }) {
        Ok(r) => r,
        Err(e) => return not_ready(e, None),
    };
    let socket = match client.websocket("/api/reeve/voice/session").await {
        Ok(s) => s,
        Err(e) => {
            println!("[voice] {name}: the session did not open: {e}");
            return not_ready(format!("Can't reach {name}"), None);
        }
    };
    let lines = voiceclient::system_player().map(|(program, args)| std::sync::Arc::new(PlayLines { client: client.clone(), program, args }) as std::sync::Arc<dyn voiceclient::Lines>);
    println!("[voice] {name}: talking, recording with {program}, playing with {}", voiceclient::system_player().map(|p| p.0).unwrap_or("nothing"));
    voiceclient::run(socket, &device_name(), lines, commands, audio, || recorder.why_stopped(), |ui| tell_widget(app, ui)).await;
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

/// Writes a new file only its owner can read: the temporary folder is
/// shared.
fn write_private(path: &std::path::Path, bytes: &[u8]) -> std::io::Result<()> {
    use std::io::Write;
    let mut options = std::fs::OpenOptions::new();
    options.write(true).create_new(true);
    #[cfg(unix)]
    std::os::unix::fs::OpenOptionsExt::mode(&mut options, 0o600);
    options.open(path)?.write_all(bytes)
}

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
            write_private(&file.0, &audio).map_err(|e| e.to_string())?;
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
    // All at once, so the first ask (which a cold-start press waits for)
    // takes 5 seconds at most.
    let asks = clients.iter().filter_map(|(id, c)| c.as_ref().ok().map(|c| (id, c))).map(|(id, c)| async move {
        let r = c.get_within("/api/reeve/voice", std::time::Duration::from_secs(5)).await.and_then(|v| serde_json::from_value::<voicemain::Readiness>(v).map_err(|e| e.to_string()));
        r.ok().map(|r| (id.clone(), r.instance))
    });
    let answered: HashMap<String, voicemain::Instance> = futures_util::future::join_all(asks).await.into_iter().flatten().collect();
    {
        let mut targets = desktop.voice_targets.lock().unwrap();
        // A server that did not answer keeps what it said last.
        targets.retain(|id, _| clients.iter().any(|(c, _)| c == id));
        targets.extend(answered);
    }
    desktop.voice_targets_known.send_replace(true);
    let _ = app.emit("voice-targets-changed", voice_targets(desktop));
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
    let _ = app.emit("voice-targets-changed", voice_targets(desktop));
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

/// Starts listening for the key the way this computer has it, and says so
/// on the app's output.
fn start_voice_key(app: &AppHandle) {
    let (backend, choice, can_choose) = {
        let desktop = app.state::<Desktop>();
        let key = desktop.key.lock().unwrap();
        (key.backend, key.choice, key.can_choose)
    };
    let why = if can_choose { format!(" (voice.key_backend {}, set in Servers & keys)", if choice == KeyBackend::Portal { "portal" } else { "grab" }) } else { String::new() };
    match backend {
        Backend::Link => println!("[voice key] no global key here; {VOICE_LINK} does what it would"),
        Backend::Grab => {
            println!("[voice key] the app grabs the key itself{why}");
            // The plugin is added only here, where the app grabs the key, so
            // the portal and the grab are never both bound.
            let plugin = tauri_plugin_global_shortcut::Builder::new()
                .with_handler(|app, shortcut, event| {
                    let found = app.state::<Desktop>().grabbed.lock().unwrap().iter().find(|(s, ..)| s == shortcut).map(|(_, w, t)| (*w, t.clone()));
                    let Some((which, text)) = found else { return };
                    let pressed = event.state() == tauri_plugin_global_shortcut::ShortcutState::Pressed;
                    println!("[voice key] grab: {text} {}", if pressed { "pressed" } else { "released" });
                    voice_key_input(app, if pressed { Input::Pressed(which) } else { Input::Released(which) }, "grab");
                })
                .build();
            if let Err(e) = app.plugin(plugin) {
                println!("[voice key] grab: the global-shortcut plugin did not start: {e}");
                return key_changed(app, |k| k.problem = Some(format!("Could not grab a global key: {e}")));
            }
            let (talk, cancel) = {
                let desktop = app.state::<Desktop>();
                let store = desktop.store.lock().unwrap();
                (store.state.voice.talk_key.clone(), store.state.voice.cancel_key.clone())
            };
            let defaults = voicekey::default_keys(std::env::consts::OS);
            let (talk, cancel) = match voicekey::chosen_keys(talk.as_deref().unwrap_or(defaults.talk), cancel.as_deref().unwrap_or(defaults.cancel)) {
                Ok(keys) => keys,
                Err(e) => {
                    println!("[voice key] grab: the saved keys do not read ({e}); the defaults instead");
                    (defaults.talk.to_string(), defaults.cancel.to_string())
                }
            };
            if let Err(e) = grab_keys(app, &talk, &cancel) {
                key_changed(app, |k| k.problem = Some(e));
            }
        }
        #[cfg(target_os = "linux")]
        Backend::Portal => {
            println!("[voice key] the key is the desktop's GlobalShortcuts portal{why}");
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
                            k.problem = Some(format!("The desktop gave no global key ({e}). Let the app grab the key (below), or bind {VOICE_LINK} in the desktop's own shortcut settings."));
                        });
                    }
                }
            });
        }
        #[cfg(not(target_os = "linux"))]
        Backend::Portal => {}
    }
}

/// Grabs these keys (as the plugin reads them) in place of any grabbed
/// before, and says so on the output and in the settings. A key the
/// desktop or another app has already is not taken: the error says which.
fn grab_keys(app: &AppHandle, talk: &str, cancel: &str) -> Result<(), String> {
    use tauri_plugin_global_shortcut::{GlobalShortcutExt, Shortcut};
    let parse = |k: &str| k.parse::<Shortcut>().map_err(|e| format!("{k} is not a key the app can take: {e}"));
    let wanted = [(parse(talk)?, voicekey::Shortcut::Talk), (parse(cancel)?, voicekey::Shortcut::Cancel)];
    let shortcuts = app.global_shortcut();
    let old: Vec<_> = std::mem::take(&mut *app.state::<Desktop>().grabbed.lock().unwrap());
    for (s, ..) in &old {
        let _ = shortcuts.unregister(*s);
    }
    let display = std::env::var("DISPLAY").map(|d| format!(" on X display {d}")).unwrap_or_default();
    let mut problems = Vec::new();
    let mut grabbed = Vec::new();
    for ((shortcut, which), text) in wanted.into_iter().zip([talk, cancel]) {
        let what = if which == voicekey::Shortcut::Talk { "talk" } else { "cancel" };
        match shortcuts.register(shortcut) {
            Ok(()) => {
                println!("[voice key] grab: registered {text} ({what}){display}");
                grabbed.push((shortcut, which, text.to_string()));
            }
            Err(e) => {
                println!("[voice key] grab: could not register {text} ({what}){display}: {e}");
                problems.push(format!("Could not take {text}: {e}. Another app or the desktop may have it; choose another key."));
            }
        }
    }
    let talk_taken = grabbed.iter().any(|(_, w, _)| *w == voicekey::Shortcut::Talk);
    *app.state::<Desktop>().grabbed.lock().unwrap() = grabbed;
    let mac = cfg!(target_os = "macos");
    let problem = (!problems.is_empty()).then(|| problems.join(" "));
    key_changed(app, |k| {
        k.key = if talk_taken { voicekey::key_text(talk, mac) } else { None };
        k.talk = Some(talk.to_string());
        k.cancel = Some(cancel.to_string());
        k.problem = problem.clone();
    });
    problem.map_or(Ok(()), Err)
}

/// The keys the person chose in Servers & keys, as the page records them
/// (`Ctrl+Alt+KeyV`), grabbed at once and kept; none is the defaults.
#[tauri::command]
fn set_voice_keys(app: AppHandle, desktop: State<'_, Desktop>, talk: Option<String>, cancel: Option<String>) -> Result<(), String> {
    let defaults = voicekey::default_keys(std::env::consts::OS);
    let (talk_key, cancel_key) = voicekey::chosen_keys(talk.as_deref().unwrap_or(defaults.talk), cancel.as_deref().unwrap_or(defaults.cancel))?;
    let saved = |k: &String, default: &str| (k != default).then(|| k.clone());
    if desktop.key.lock().unwrap().backend == Backend::Grab {
        let before = desktop.key.lock().unwrap().clone();
        if let Err(e) = grab_keys(&app, &talk_key, &cancel_key) {
            // Back to the keys that worked.
            if let (Some(t), Some(c)) = (before.talk, before.cancel) {
                let _ = grab_keys(&app, &t, &c);
            }
            return Err(e);
        }
    } else {
        key_changed(&app, |k| {
            k.talk = Some(talk_key.clone());
            k.cancel = Some(cancel_key.clone());
        });
    }
    let mut store = desktop.store.lock().unwrap();
    store.state.voice.talk_key = saved(&talk_key, defaults.talk);
    store.state.voice.cancel_key = saved(&cancel_key, defaults.cancel);
    store.save()
}

/// Grab the key or ask the portal, on a Wayland desktop: from the next
/// start, so the two are never bound at once.
#[tauri::command]
fn set_voice_key_backend(app: AppHandle, desktop: State<'_, Desktop>, backend: KeyBackend) -> Result<(), String> {
    {
        let mut store = desktop.store.lock().unwrap();
        store.state.voice.key_backend = backend;
        store.save()?;
    }
    println!("[voice key] voice.key_backend is now {backend:?}; it applies when the app next starts");
    key_changed(&app, |k| k.choice = backend);
    Ok(())
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

// ---- Approvals in the Legato widget's feed ----

/// Asks every server for its pending approvals every widgetapprovals::EVERY
/// (or at once after a decision), keeps the list, tells the widget when it
/// changes and keeps the widget shown while any waits. A server that does
/// not answer keeps what it said last.
async fn watch_approvals(app: AppHandle) {
    let mut by_server: HashMap<String, Vec<widgetapprovals::Listed>> = HashMap::new();
    loop {
        let asks: Vec<(Server, Result<Client, String>)> = {
            let desktop = app.state::<Desktop>();
            let store = desktop.store.lock().unwrap();
            store.state.servers.iter().map(|s| (s.clone(), client(&store, &s.id))).collect()
        };
        let answers = futures_util::future::join_all(asks.into_iter().map(|(server, c)| async move {
            let body = match c {
                Ok(c) => c.get_within(widgetapprovals::PATH, widgetapprovals::WITHIN).await,
                Err(e) => Err(e),
            };
            (server, body)
        }))
        .await;
        let order: Vec<String> = answers.iter().map(|(s, _)| s.id.clone()).collect();
        by_server.retain(|id, _| order.contains(id));
        for (server, body) in answers {
            if let Ok(body) = body {
                by_server.insert(server.id.clone(), widgetapprovals::listed(&server.id, &server.name, server.tint.as_deref(), &body));
            }
        }
        let list: Vec<widgetapprovals::Listed> = order.iter().flat_map(|id| by_server.get(id).cloned().unwrap_or_default()).collect();
        let changed = {
            let desktop = app.state::<Desktop>();
            let mut kept = desktop.widget_approvals.lock().unwrap();
            let changed = !widgetapprovals::same(&kept, &list);
            if changed {
                println!("[approvals] {} waiting", list.len());
                *kept = list.clone();
            }
            changed
        };
        if changed {
            let _ = app.emit("voice-approvals", &list);
            if list.is_empty() {
                // The page says how the last one went for a moment
                // (DECIDED_FOR in Approvals.tsx) before a hidden widget goes.
                let app = app.clone();
                tauri::async_runtime::spawn(async move {
                    tokio::time::sleep(widgetapprovals::DECIDED_FOR).await;
                    show_widget_as_it_should(&app);
                });
            } else {
                show_widget_as_it_should(&app);
            }
        }
        let desktop = app.state::<Desktop>();
        tokio::select! {
            _ = tokio::time::sleep(widgetapprovals::EVERY) => {}
            _ = desktop.approvals_asked.notified() => {}
        }
    }
}

/// The approvals the widget shows now.
#[tauri::command]
fn voice_approvals(desktop: State<'_, Desktop>) -> Vec<widgetapprovals::Listed> {
    desktop.widget_approvals.lock().unwrap().clone()
}

/// Allows (with one of its options) or denies an approval from the widget,
/// as the tray does; a signed one is signed with prepare and sign instead.
/// The first decision counts: another made first is the server's 409.
#[tauri::command]
async fn voice_approval_decide(desktop: State<'_, Desktop>, server: String, approval: String, decision: String, option: Option<String>) -> Result<(), String> {
    let body = widgetapprovals::decision(&decision, option.as_deref())?;
    let c = {
        let store = desktop.store.lock().unwrap();
        client(&store, &server)?
    };
    let out = c.post(&format!("/api/approvals/{approval}/decision"), body).await.map(|_| ());
    println!("[approvals] {server}: {decision} {approval} from the widget: {}", if out.is_ok() { "done" } else { "refused" });
    desktop.approvals_asked.notify_one();
    out
}

/// Opens the main window on an approval's server page, where its tray
/// takes what the widget cannot: a note, an answer, a secret.
#[tauri::command]
fn voice_approval_open(app: AppHandle, desktop: State<'_, Desktop>, server: String, approval: String) -> Result<(), String> {
    open_in_page(&app, &desktop, server.clone(), "/", ("approval", &approval))?;
    let _ = app.emit_to("shell", "shell-show-server", json!({ "server": server }));
    bring_to_front(&app);
    Ok(())
}

/// Opens the main window's approval sheet on a signed approval.
#[tauri::command]
fn voice_approval_sheet(app: AppHandle, server: String, approval: String) {
    let _ = app.emit_to("shell", "shell-open-approval", json!({ "server": server, "id": approval }));
    bring_to_front(&app);
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
    let platform = voicekey::Platform::this();
    let choice = store.state.voice.key_backend;
    let backend = voicekey::backend(&platform, choice);
    let can_choose = voicekey::can_choose(&platform);
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
            key: Mutex::new(KeyInfo { backend, choice, can_choose, key: None, talk: None, cancel: None, hold_to_talk, can_change: false, problem: None, link: VOICE_LINK }),
            grabbed: Mutex::default(),
            #[cfg(target_os = "linux")]
            portal: Mutex::default(),
            voice_targets: Mutex::default(),
            voice_session: Mutex::default(),
            voice_targets_known: tokio::sync::watch::Sender::new(false),
            widget: Mutex::new(WidgetState { showing: voicewidget::Showing::Rest, written: false, size: voicewidget::FIRST_SIZE, applied: None, drag: None, layout: None }),
            widget_placement: Mutex::new(voicewidget::Placement::Placed),
            #[cfg(target_os = "linux")]
            layer_shell: Mutex::default(),
            screens: Mutex::default(),
            widget_approvals: Mutex::default(),
            approvals_asked: tokio::sync::Notify::new(),
        })
        .on_menu_event(|app, event| voice_widget_menu_chosen(app, event.id().as_ref()))
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
            // The screens before the widget, so it goes on the chosen one.
            #[cfg(target_os = "linux")]
            {
                refresh_screens(app.handle());
                watch_screens(app.handle());
            }
            if let Err(e) = make_voice_widget(app.handle()) {
                println!("[voice widget] the window was not made: {e}");
            }
            tauri::async_runtime::spawn(watch_approvals(app.handle().clone()));
            // Started with --voice. No page hears the voice-key event this
            // early, but the press starts the conversation, which waits for
            // the first ask of the servers.
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
            set_voice_keys,
            set_voice_key_backend,
            voice_targets,
            set_voice_server,
            voice_action,
            voice_status,
            voice_open_settings,
            voice_widget_size,
            voice_widget_menu,
            voice_widget_settings,
            set_voice_widget,
            set_voice_screen,
            voice_widget_drag_start,
            voice_widget_drag_move,
            voice_widget_drag_end,
            voice_widget_layout,
            voice_approvals,
            voice_approval_decide,
            voice_approval_open,
            voice_approval_sheet
        ])
        .run(tauri::generate_context!())
        .expect("the app did not start");
}

#[cfg(test)]
mod tests {
    /// The commands build.rs makes permissions for are generate_handler!'s,
    /// and each capability allows only commands that exist.
    #[test]
    fn every_command_has_a_permission_and_the_capabilities_name_real_ones() {
        let names = |text: &str, start: &str, end: &str| -> Vec<String> {
            let from = text.find(start).expect(start) + start.len();
            let to = from + text[from..].find(end).expect(end);
            text[from..to].split(',').map(|n| n.trim().trim_matches('"').to_string()).filter(|n| !n.is_empty()).collect()
        };
        let app = include_str!("app.rs");
        let build = include_str!("../build.rs");
        let mut handled = names(app, "generate_handler![", "]");
        let mut listed = names(build, "const COMMANDS: &[&str] = &[", "];");
        handled.sort();
        listed.sort();
        assert_eq!(handled, listed);
        for capability in [include_str!("../capabilities/shell.json"), include_str!("../capabilities/voice.json")] {
            let v: serde_json::Value = serde_json::from_str(capability).unwrap();
            for p in v["permissions"].as_array().unwrap().iter().filter_map(|p| p.as_str()?.strip_prefix("allow-")) {
                assert!(listed.contains(&p.replace('-', "_")), "{p}");
            }
        }
    }
}
