//! The key on a Wayland desktop: the XDG desktop portal's GlobalShortcuts
//! (`org.freedesktop.portal.GlobalShortcuts`, KDE's `xdg-desktop-portal-kde`
//! first), over D-Bus with `ashpd`. Only the talking to the portal is here;
//! what to bind and what a key asks are decided in `voicekey`.
//!
//! - The app is a host app (not a Flatpak), so it first says which desktop
//!   file it is (`org.freedesktop.host.portal.Registry`), having written
//!   `dev.legato.desktop.desktop`: KDE then lists the shortcuts under Legato.
//! - One session for the app's life. The first time, binding the shortcuts
//!   shows the desktop's own dialog, where the person takes the key or picks
//!   another; later, the desktop lists them and the app does not bind again.
//! - `ConfigureShortcuts` (portal version 2) shows that dialog again:
//!   "Change the key…".

use ashpd::desktop::global_shortcuts::{BindShortcutsOptions, ConfigureShortcutsOptions, GlobalShortcuts, ListShortcutsOptions, NewShortcut, Shortcut};
use ashpd::desktop::{CreateSessionOptions, Session};
use futures_util::StreamExt;

use crate::voicekey::{self, Input};

/// The open session, kept for the app's life.
pub struct Portal {
    shortcuts: GlobalShortcuts,
    session: Session<GlobalShortcuts>,
}

/// What the portal says is bound to talk, as the person reads it.
fn talk_text(shortcuts: &[Shortcut]) -> Option<String> {
    shortcuts.iter().find(|s| s.id() == voicekey::TALK).and_then(|s| voicekey::key_text(s.trigger_description(), false))
}

/// Writes the desktop file the portal reads the app's name from, naming
/// the program running now (or the AppImage).
fn write_desktop_file() -> Result<(), String> {
    let exec = std::env::var_os("APPIMAGE").map(std::path::PathBuf::from).or_else(|| std::env::current_exe().ok()).ok_or("no program path")?;
    let dir = dirs::data_dir().ok_or("no data folder")?.join("applications");
    voicekey::install_desktop_file(&dir, &exec.to_string_lossy())
}

impl Portal {
    /// Opens the session and makes sure both shortcuts are bound. Each
    /// press and release is passed to `on_input`; `on_text` hears the key
    /// whenever the desktop changes it, and `on_lost` that the portal went
    /// away (its signals ended), after which no key arrives. Returns the key
    /// bound now.
    pub async fn open(
        on_input: impl Fn(Input) + Send + Sync + 'static,
        on_text: impl Fn(Option<String>) + Send + Sync + 'static,
        on_lost: impl FnOnce() + Send + 'static,
    ) -> Result<(Portal, Option<String>), String> {
        fn fail(what: &'static str) -> impl Fn(ashpd::Error) -> String {
            move |e| format!("{what}: {e}")
        }
        if let Err(e) = write_desktop_file() {
            println!("[voice key] could not write the desktop file: {e}");
        }
        let app_id = voicekey::APP_ID.parse().map_err(|e| format!("app ID: {e}"))?;
        if let Err(e) = ashpd::register_host_app(app_id).await {
            // An older portal has no registry; it then knows the app by its
            // systemd scope, if at all.
            println!("[voice key] the portal registry refused the app: {e}");
        }
        let shortcuts = GlobalShortcuts::new().await.map_err(fail("the desktop has no GlobalShortcuts portal"))?;
        let session = shortcuts.create_session(CreateSessionOptions::default()).await.map_err(fail("opening a shortcuts session"))?;

        // Listen before binding, so a press right after the dialog is heard.
        let mut activated = shortcuts.receive_activated().await.map_err(fail("listening for the key"))?;
        let mut deactivated = shortcuts.receive_deactivated().await.map_err(fail("listening for the key"))?;
        let mut changed = shortcuts.receive_shortcuts_changed().await.map_err(fail("listening for the key"))?;
        let on_input = std::sync::Arc::new(on_input);
        let (down, up) = (on_input.clone(), on_input);
        tauri::async_runtime::spawn(async move {
            while let Some(a) = activated.next().await {
                if let Some(s) = voicekey::Shortcut::from_id(a.shortcut_id()) {
                    down(Input::Pressed(s));
                }
            }
            on_lost();
        });
        tauri::async_runtime::spawn(async move {
            while let Some(d) = deactivated.next().await {
                if let Some(s) = voicekey::Shortcut::from_id(d.shortcut_id()) {
                    up(Input::Released(s));
                }
            }
        });
        tauri::async_runtime::spawn(async move {
            while let Some(c) = changed.next().await {
                on_text(talk_text(c.shortcuts()));
            }
        });

        let listed = shortcuts
            .list_shortcuts(&session, ListShortcutsOptions::default())
            .await
            .and_then(|r| r.response())
            .map(|l| l.shortcuts().to_vec())
            .unwrap_or_default();
        let ids: Vec<&str> = listed.iter().map(|s| s.id()).collect();
        let bound = if voicekey::needs_binding(&ids) {
            let wanted = [
                NewShortcut::new(voicekey::TALK, "Talk to the Reeve").preferred_trigger(voicekey::PORTAL_TRIGGER),
                NewShortcut::new(voicekey::CANCEL, "Stop talking to the Reeve"),
            ];
            let answer = shortcuts
                .bind_shortcuts(&session, &wanted, None, BindShortcutsOptions::default())
                .await
                .and_then(|r| r.response())
                .map_err(fail("binding the key"))?;
            answer.shortcuts().to_vec()
        } else {
            listed
        };
        let text = talk_text(&bound);
        println!("[voice key] the portal's key: {}", text.as_deref().unwrap_or("none"));
        Ok((Portal { shortcuts, session }, text))
    }

    /// Whether the desktop can show its dialog again (portal version 2).
    pub fn can_change(&self) -> bool {
        self.shortcuts.version() >= 2
    }

    /// Shows the desktop's dialog for the app's shortcuts. What the person
    /// picks arrives as ShortcutsChanged.
    pub async fn change(&self) -> Result<(), String> {
        if !self.can_change() {
            return Err("This desktop cannot change the key from here. Change it in the desktop's own shortcut settings.".into());
        }
        self.shortcuts.configure_shortcuts(&self.session, None, ConfigureShortcutsOptions::default()).await.map_err(|e| format!("The desktop did not open its shortcut settings: {e}"))
    }
}
