//! The key that starts talking to the Reeve from any app.
//!
//! - On a Wayland desktop (KDE Plasma first) the key is the XDG desktop
//!   portal's GlobalShortcuts (`voicekey_portal`): two shortcuts, `talk` and
//!   `cancel`, that the desktop's own dialog binds and keeps.
//! - On X11, macOS and Windows it is `tauri-plugin-global-shortcut`, which grabs the
//!   key itself (on Wayland it would see keys only while an XWayland window
//!   has focus).
//! - Everywhere, `legato-desktop://voice` (or `legato-desktop --voice`) does
//!   what the key does, for a desktop where neither works: the person binds
//!   that in the desktop's own custom shortcuts.
//!
//! What a key does is decided here, alone: `Keys` turns presses and releases
//! into `VoiceKey`s, with hold-to-talk off by default.

use serde::Serialize;

/// How this computer's key is had.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Backend {
    /// The XDG desktop portal's GlobalShortcuts (Wayland).
    Portal,
    /// `tauri-plugin-global-shortcut` (X11, macOS, Windows): talk only, no cancel.
    Shortcut,
    /// Only the `legato-desktop://voice` link.
    Link,
}

/// What `backend` needs to know of the computer.
#[derive(Debug, Clone, Default)]
pub struct Platform {
    /// `std::env::consts::OS`.
    pub os: &'static str,
    /// `XDG_SESSION_TYPE`.
    pub session_type: Option<String>,
    /// `WAYLAND_DISPLAY` is set.
    pub wayland_display: bool,
    /// `DISPLAY` is set.
    pub x_display: bool,
}

impl Platform {
    pub fn this() -> Platform {
        let set = |name: &str| std::env::var_os(name).is_some_and(|v| !v.is_empty());
        Platform {
            os: std::env::consts::OS,
            session_type: std::env::var("XDG_SESSION_TYPE").ok().filter(|v| !v.is_empty()),
            wayland_display: set("WAYLAND_DISPLAY"),
            x_display: set("DISPLAY"),
        }
    }
}

/// Which way this computer has the key. When the portal fails at run time
/// the app falls back to the link, not to grabbing the key through X11.
pub fn backend(p: &Platform) -> Backend {
    match p.os {
        "macos" | "windows" => Backend::Shortcut,
        "linux" => {
            let wayland = match p.session_type.as_deref() {
                Some("wayland") => true,
                Some("x11") => false,
                _ => p.wayland_display,
            };
            if wayland {
                Backend::Portal
            } else if p.x_display {
                Backend::Shortcut
            } else {
                Backend::Link
            }
        }
        _ => Backend::Link,
    }
}

/// The portal's shortcut IDs.
pub const TALK: &str = "talk";
pub const CANCEL: &str = "cancel";

/// The key the app asks the portal for, in the XDG shortcuts
/// specification's words (Meta+Alt+Space); the desktop may give another.
pub const PORTAL_TRIGGER: &str = "LOGO+ALT+space";

/// The key the app grabs on X11 and macOS, as the global-shortcut plugin
/// reads it. On a Mac Option+Cmd+Space is Finder's search and
/// Ctrl+Option+Space switches the input source.
pub fn shortcut_accelerator(os: &str) -> &'static str {
    if os == "macos" {
        "Ctrl+Shift+Space"
    } else {
        "Super+Alt+Space"
    }
}

/// One of the two shortcuts.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Shortcut {
    Talk,
    Cancel,
}

impl Shortcut {
    /// The portal's ID for it, or None for one the app did not bind.
    pub fn from_id(id: &str) -> Option<Shortcut> {
        match id {
            TALK => Some(Shortcut::Talk),
            CANCEL => Some(Shortcut::Cancel),
            _ => None,
        }
    }
}

/// A key going down or coming up.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Input {
    Pressed(Shortcut),
    Released(Shortcut),
}

/// What the key asks of the voice conversation, sent to the app as the
/// `voice-key` event.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum VoiceKey {
    /// Pressed, with hold-to-talk off: start talking, or send what was said.
    Talk,
    /// Pressed and held, with hold-to-talk on: listen while it is held.
    Hold,
    /// Let go of after Hold: send what was said.
    Release,
    /// The cancel shortcut: drop the turn and stop.
    Cancel,
}

/// How close together presses with no release between are taken for a key
/// repeating while held. A press later than this after the last one is a
/// new press: its release was lost (a screen lock), or the desktop never
/// sends one.
pub const REPEAT_GAP: std::time::Duration = std::time::Duration::from_secs(1);

/// Turns presses and releases into what they ask. A key that repeats while
/// held asks once; a release with no press before it asks nothing.
#[derive(Debug, Default)]
pub struct Keys {
    hold_to_talk: bool,
    /// When talk was last seen going down, while it is held.
    held: Option<std::time::Instant>,
}

impl Keys {
    pub fn new(hold_to_talk: bool) -> Keys {
        Keys { hold_to_talk, ..Keys::default() }
    }

    pub fn hold_to_talk(&self) -> bool {
        self.hold_to_talk
    }

    /// Changes the mode; a key held now is forgotten.
    pub fn set_hold_to_talk(&mut self, on: bool) {
        *self = Keys::new(on);
    }

    pub fn on(&mut self, input: Input) -> Option<VoiceKey> {
        self.on_at(input, std::time::Instant::now())
    }

    pub fn on_at(&mut self, input: Input, now: std::time::Instant) -> Option<VoiceKey> {
        match input {
            Input::Pressed(Shortcut::Talk) => {
                let repeat = self.held.is_some_and(|last| now.saturating_duration_since(last) < REPEAT_GAP);
                self.held = Some(now);
                if repeat {
                    return None;
                }
                Some(if self.hold_to_talk { VoiceKey::Hold } else { VoiceKey::Talk })
            }
            Input::Released(Shortcut::Talk) => (self.held.take().is_some() && self.hold_to_talk).then_some(VoiceKey::Release),
            // Cancel forgets a held key: its release asks nothing, and the
            // next press talks.
            Input::Pressed(Shortcut::Cancel) => {
                self.held = None;
                Some(VoiceKey::Cancel)
            }
            Input::Released(Shortcut::Cancel) => None,
        }
    }
}

/// What a `legato-desktop://voice` link asks: `…//voice` is the key,
/// `…//voice?action=cancel` the cancel shortcut. A link has no release, so
/// it is a press of the key whatever the mode. Any other link is None: a
/// page's own `voice?key=…` addresses never reach the system.
pub fn read_link(url: &str) -> Option<VoiceKey> {
    let url = reqwest::Url::parse(url).ok()?;
    if url.scheme() != crate::bridge::SCHEME || url.host_str()? != "voice" || !matches!(url.path(), "" | "/") {
        return None;
    }
    let pairs: Vec<_> = url.query_pairs().collect();
    match pairs.as_slice() {
        [] => Some(VoiceKey::Talk),
        [(k, v)] if k == "action" && v == "talk" => Some(VoiceKey::Talk),
        [(k, v)] if k == "action" && v == "cancel" => Some(VoiceKey::Cancel),
        _ => None,
    }
}

/// What the app's command line asks: `--voice` is the key, `--voice=cancel`
/// the cancel shortcut. The first argument is the program.
pub fn read_args<S: AsRef<str>>(args: &[S]) -> Option<VoiceKey> {
    args.iter().skip(1).find_map(|a| match a.as_ref() {
        "--voice" | "--voice=talk" => Some(VoiceKey::Talk),
        "--voice=cancel" => Some(VoiceKey::Cancel),
        _ => None,
    })
}

/// The key as the person reads it: the desktop's own description when it
/// gives one, a trigger in the XDG shortcuts specification's words
/// (`LOGO+ALT+space`) or the plugin's (`Super+Alt+Space`) turned into
/// `Meta+Alt+Space`, and on a Mac the symbols (`⌃⇧Space`). None when nothing
/// is bound.
pub fn key_text(trigger: &str, mac: bool) -> Option<String> {
    let trigger = trigger.trim();
    if trigger.is_empty() {
        return None;
    }
    let parts: Vec<&str> = trigger.split('+').map(str::trim).collect();
    if parts.len() < 2 || parts.iter().any(|p| p.is_empty() || p.contains(' ')) {
        return Some(trigger.to_string());
    }
    let (key, modifiers) = parts.split_last().expect("two parts or more");
    let mut out: Vec<String> = Vec::new();
    for m in modifiers {
        let name = match m.to_ascii_lowercase().as_str() {
            "logo" | "super" | "meta" | "cmd" | "command" | "commandorcontrol" if !mac => "Meta",
            "logo" | "super" | "meta" | "cmd" | "command" | "commandorcontrol" => "⌘",
            "alt" | "option" if mac => "⌥",
            "alt" | "option" => "Alt",
            "ctrl" | "control" if mac => "⌃",
            "ctrl" | "control" => "Ctrl",
            "shift" if mac => "⇧",
            "shift" => "Shift",
            "num" => "Num",
            _ => return Some(trigger.to_string()),
        };
        out.push(name.into());
    }
    let mut chars = key.chars();
    let key = match chars.next() {
        Some(first) => first.to_uppercase().chain(chars).collect::<String>(),
        None => return Some(trigger.to_string()),
    };
    Some(if mac { format!("{}{key}", out.concat()) } else { format!("{}+{key}", out.join("+")) })
}

/// Whether the portal session must bind the shortcuts: not when the desktop
/// already lists both. KDE has had bugs with binding a second time (KDE bug
/// 498575), so a session binds at most once, and only when it must.
pub fn needs_binding<S: AsRef<str>>(listed: &[S]) -> bool {
    ![TALK, CANCEL].iter().all(|id| listed.iter().any(|l| l.as_ref() == *id))
}

/// The app ID the portal knows the app by, and the name of the desktop file
/// that says what it is called (`dev.legato.desktop.desktop`).
pub const APP_ID: &str = "dev.legato.desktop";

/// The desktop file a host app needs for the portal to know its name, so
/// KDE lists its shortcuts under "Legato". exec is the program (or the
/// AppImage) to run.
pub fn desktop_entry(exec: &str) -> String {
    // A quoted argument escapes ", `, $ and \ with \, and the value then
    // escapes \ again (the Desktop Entry Specification's Exec key); % is a
    // field code, so a literal one is %%.
    let mut quoted = String::new();
    for c in exec.chars() {
        match c {
            '"' | '`' | '$' => {
                quoted.push_str("\\\\");
                quoted.push(c);
            }
            '\\' => quoted.push_str("\\\\\\\\"),
            '%' => quoted.push_str("%%"),
            _ => quoted.push(c),
        }
    }
    format!(
        "[Desktop Entry]\nType=Application\nName=Legato\nComment=Every Legato server in one window\nExec=\"{quoted}\" %u\nTerminal=false\nNoDisplay=true\n"
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    fn linux(session: Option<&str>, wayland: bool, x: bool) -> Platform {
        Platform { os: "linux", session_type: session.map(String::from), wayland_display: wayland, x_display: x }
    }

    #[test]
    fn a_wayland_desktop_uses_the_portal_and_x11_and_a_mac_grab_the_key() {
        assert_eq!(backend(&linux(Some("wayland"), true, true)), Backend::Portal);
        assert_eq!(backend(&linux(None, true, true)), Backend::Portal);
        assert_eq!(backend(&linux(Some("x11"), false, true)), Backend::Shortcut);
        // Plasma on X11 also sets WAYLAND_DISPLAY sometimes; the session type wins.
        assert_eq!(backend(&linux(Some("x11"), true, true)), Backend::Shortcut);
        assert_eq!(backend(&linux(Some("tty"), false, false)), Backend::Link);
        assert_eq!(backend(&Platform { os: "macos", ..Platform::default() }), Backend::Shortcut);
        assert_eq!(backend(&Platform { os: "freebsd", ..Platform::default() }), Backend::Link);
    }

    #[test]
    fn a_press_talks_and_its_release_asks_nothing() {
        let mut keys = Keys::new(false);
        assert_eq!(keys.on(Input::Pressed(Shortcut::Talk)), Some(VoiceKey::Talk));
        assert_eq!(keys.on(Input::Released(Shortcut::Talk)), None);
        assert_eq!(keys.on(Input::Pressed(Shortcut::Talk)), Some(VoiceKey::Talk));
    }

    #[test]
    fn a_key_that_repeats_while_held_asks_once() {
        for hold in [false, true] {
            let mut keys = Keys::new(hold);
            assert!(keys.on(Input::Pressed(Shortcut::Talk)).is_some());
            assert_eq!(keys.on(Input::Pressed(Shortcut::Talk)), None);
            assert_eq!(keys.on(Input::Pressed(Shortcut::Talk)), None);
        }
    }

    #[test]
    fn a_press_long_after_a_release_that_never_came_is_a_new_press() {
        // A portal that sends no Deactivated, or a release lost to a screen
        // lock: the key keeps working.
        let t0 = std::time::Instant::now();
        for hold in [false, true] {
            let mut keys = Keys::new(hold);
            let first = keys.on_at(Input::Pressed(Shortcut::Talk), t0);
            assert!(first.is_some());
            assert_eq!(keys.on_at(Input::Pressed(Shortcut::Talk), t0 + REPEAT_GAP * 2), first, "hold {hold}");
            assert_eq!(keys.on_at(Input::Pressed(Shortcut::Talk), t0 + REPEAT_GAP * 4), first, "hold {hold}");
        }
    }

    #[test]
    fn repeats_close_together_stay_one_press_however_long_the_key_is_held() {
        let t0 = std::time::Instant::now();
        let mut keys = Keys::new(false);
        assert!(keys.on_at(Input::Pressed(Shortcut::Talk), t0).is_some());
        let step = REPEAT_GAP / 2;
        for i in 1..10 {
            assert_eq!(keys.on_at(Input::Pressed(Shortcut::Talk), t0 + step * i), None, "repeat {i}");
        }
    }

    #[test]
    fn cancel_lets_the_next_press_talk_with_no_release_between() {
        for hold in [false, true] {
            let mut keys = Keys::new(hold);
            let first = keys.on(Input::Pressed(Shortcut::Talk));
            assert_eq!(keys.on(Input::Pressed(Shortcut::Cancel)), Some(VoiceKey::Cancel));
            assert_eq!(keys.on(Input::Pressed(Shortcut::Talk)), first, "hold {hold}");
        }
    }

    #[test]
    fn with_hold_to_talk_the_press_listens_and_the_release_sends() {
        let mut keys = Keys::new(true);
        assert_eq!(keys.on(Input::Pressed(Shortcut::Talk)), Some(VoiceKey::Hold));
        assert_eq!(keys.on(Input::Released(Shortcut::Talk)), Some(VoiceKey::Release));
        // A release the app never saw pressed (the key was down when it started).
        assert_eq!(keys.on(Input::Released(Shortcut::Talk)), None);
    }

    #[test]
    fn cancel_while_holding_drops_the_turn_and_the_release_sends_nothing() {
        let mut keys = Keys::new(true);
        keys.on(Input::Pressed(Shortcut::Talk));
        assert_eq!(keys.on(Input::Pressed(Shortcut::Cancel)), Some(VoiceKey::Cancel));
        assert_eq!(keys.on(Input::Released(Shortcut::Cancel)), None);
        assert_eq!(keys.on(Input::Released(Shortcut::Talk)), None);
        assert_eq!(keys.on(Input::Pressed(Shortcut::Talk)), Some(VoiceKey::Hold));
    }

    #[test]
    fn changing_the_mode_forgets_a_held_key() {
        let mut keys = Keys::new(false);
        keys.on(Input::Pressed(Shortcut::Talk));
        keys.set_hold_to_talk(true);
        assert!(keys.hold_to_talk());
        assert_eq!(keys.on(Input::Released(Shortcut::Talk)), None);
        assert_eq!(keys.on(Input::Pressed(Shortcut::Talk)), Some(VoiceKey::Hold));
    }

    #[test]
    fn the_voice_link_is_the_key_and_its_cancel() {
        assert_eq!(read_link("legato-desktop://voice"), Some(VoiceKey::Talk));
        assert_eq!(read_link("legato-desktop://voice/"), Some(VoiceKey::Talk));
        assert_eq!(read_link("legato-desktop://voice?action=talk"), Some(VoiceKey::Talk));
        assert_eq!(read_link("legato-desktop://voice?action=cancel"), Some(VoiceKey::Cancel));
        for url in [
            // A page's own address, which never reaches the system.
            "legato-desktop://voice?key=k1&action=start",
            "legato-desktop://voice?action=start",
            "legato-desktop://voice/more",
            "legato-desktop://import?provider=jira&key=AG-1",
            "https://example.com/voice",
            "not a link",
        ] {
            assert_eq!(read_link(url), None, "{url}");
        }
    }

    #[test]
    fn the_voice_argument_is_the_key() {
        assert_eq!(read_args(&["legato-desktop", "--voice"]), Some(VoiceKey::Talk));
        assert_eq!(read_args(&["legato-desktop", "--voice=cancel"]), Some(VoiceKey::Cancel));
        assert_eq!(read_args(&["legato-desktop"]), None);
        // The program's own name is not an argument.
        assert_eq!(read_args(&["--voice"]), None);
        assert_eq!(read_args(&["legato-desktop", "legato-desktop://voice"]), None);
    }

    #[test]
    fn the_key_reads_as_the_person_knows_it() {
        assert_eq!(key_text("LOGO+ALT+space", false).as_deref(), Some("Meta+Alt+Space"));
        assert_eq!(key_text("Super+Alt+Space", false).as_deref(), Some("Meta+Alt+Space"));
        assert_eq!(key_text("CTRL+SHIFT+a", false).as_deref(), Some("Ctrl+Shift+A"));
        assert_eq!(key_text("Ctrl+Shift+Space", true).as_deref(), Some("⌃⇧Space"));
        // The desktop's own description is kept as it is.
        assert_eq!(key_text("Meta+Alt+Space", false).as_deref(), Some("Meta+Alt+Space"));
        assert_eq!(key_text("Press Meta and Space", false).as_deref(), Some("Press Meta and Space"));
        assert_eq!(key_text("F9", false).as_deref(), Some("F9"));
        assert_eq!(key_text("  ", false), None);
    }

    #[test]
    fn a_session_binds_only_when_the_desktop_lacks_a_shortcut() {
        assert!(needs_binding::<&str>(&[]));
        assert!(needs_binding(&["talk"]));
        assert!(!needs_binding(&["cancel", "talk"]));
        assert!(!needs_binding(&["talk", "cancel", "other"]));
    }

    #[test]
    fn the_desktop_file_names_the_app_and_quotes_its_program() {
        let entry = desktop_entry("/home/c/Apps/Legato desktop.AppImage");
        assert!(entry.starts_with("[Desktop Entry]\n"), "{entry}");
        assert!(entry.contains("\nName=Legato\n"), "{entry}");
        assert!(entry.contains("\nExec=\"/home/c/Apps/Legato desktop.AppImage\" %u\n"), "{entry}");
        assert!(entry.contains("\nNoDisplay=true\n"), "{entry}");
        assert!(desktop_entry("/a/$b\"c").contains("Exec=\"/a/\\\\$b\\\\\"c\" %u"));
        // % is a field code in Exec, so a literal one is doubled.
        assert!(desktop_entry("/a/50%b").contains("Exec=\"/a/50%%b\" %u"));
    }
}
