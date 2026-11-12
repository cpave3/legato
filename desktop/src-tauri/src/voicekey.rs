//! The key that starts talking to the Reeve from any app.
//!
//! - Everywhere it can, the app grabs the key itself with
//!   `tauri-plugin-global-shortcut` (`Backend::Grab`): a key that talks and
//!   one that cancels, which the person can change in Servers & keys. On
//!   Linux the plugin grabs through X11, so on a Wayland desktop it is
//!   XWayland's: KDE passes X11 apps keys typed with Ctrl, Alt or Meta held
//!   (its default "Legacy X11 App Support"), whatever window has focus.
//! - On a Wayland desktop the person may choose the XDG desktop portal's
//!   GlobalShortcuts instead (`voicekey_portal`, `voice.key_backend:
//!   portal`): two shortcuts, `talk` and `cancel`, that the desktop's own
//!   dialog binds and keeps. Never both: one press would count twice.
//! - Everywhere, `legato-desktop://voice` (or `legato-desktop --voice`) does
//!   what the key does, for a desktop where neither works: the person binds
//!   that in the desktop's own custom shortcuts.
//!
//! What a key does is decided here, alone: `Keys` turns presses and releases
//! into `VoiceKey`s, with hold-to-talk off by default.

use serde::Serialize;

pub use crate::store::KeyBackend;

/// How this computer's key is had.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Backend {
    /// `tauri-plugin-global-shortcut`, which grabs the key (X11 or XWayland,
    /// macOS, Windows): talk and cancel.
    Grab,
    /// The XDG desktop portal's GlobalShortcuts (Wayland, when chosen).
    Portal,
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
    /// `DISPLAY` is set: an X server, or on Wayland XWayland.
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

    /// A Linux Wayland session (the session type wins over the variables:
    /// Plasma on X11 sometimes sets WAYLAND_DISPLAY too).
    fn wayland(&self) -> bool {
        self.os == "linux"
            && match self.session_type.as_deref() {
                Some("wayland") => true,
                Some("x11") => false,
                _ => self.wayland_display,
            }
    }
}

/// Which way this computer has the key, given the person's choice. A Wayland
/// desktop grabs it through XWayland unless the portal is chosen, or there
/// is no XWayland. When the way fails at run time the app falls back to the
/// link, never to the other way.
pub fn backend(p: &Platform, choice: KeyBackend) -> Backend {
    match p.os {
        "macos" | "windows" => Backend::Grab,
        "linux" if p.wayland() => {
            if choice == KeyBackend::Portal || !p.x_display {
                Backend::Portal
            } else {
                Backend::Grab
            }
        }
        "linux" if p.x_display => Backend::Grab,
        _ => Backend::Link,
    }
}

/// Whether the person can choose between grabbing the key and the portal:
/// a Wayland desktop with XWayland.
pub fn can_choose(p: &Platform) -> bool {
    p.wayland() && p.x_display
}

/// The keys the app grabs unless the person chose others, as the plugin
/// reads them. Not Super: KWin and GNOME keep many Meta keys for
/// themselves. On a Mac Option+Cmd+Space is Finder's search and
/// Ctrl+Option+Space switches the input source.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct GrabKeys {
    pub talk: &'static str,
    pub cancel: &'static str,
}

pub fn default_keys(os: &str) -> GrabKeys {
    if os == "macos" {
        GrabKeys { talk: "Ctrl+Shift+Space", cancel: "Ctrl+Alt+Shift+Space" }
    } else {
        GrabKeys { talk: "Ctrl+Alt+Space", cancel: "Ctrl+Alt+Shift+Space" }
    }
}

/// A key the person chose, as the page records it (`Ctrl+Alt+KeyV`, the
/// DOM's code for the key), written as the plugin reads it: the modifiers in
/// one order (Ctrl, Alt, Shift, Super), then the key (`Ctrl+Alt+V`). Refused
/// when it is not modifiers and one key, or has no Ctrl, Alt or Super: such
/// a key would be taken from every app, and KDE passes X11 apps only keys
/// typed with Ctrl, Alt or Meta.
pub fn accelerator(raw: &str) -> Result<String, String> {
    let tokens: Vec<&str> = raw.split('+').map(str::trim).collect();
    if tokens.iter().any(|t| t.is_empty()) {
        return Err(format!("“{}” is not a key.", raw.trim()));
    }
    let (key, modifiers) = tokens.split_last().ok_or_else(|| "No key was given.".to_string())?;
    let (mut ctrl, mut alt, mut shift, mut sup) = (false, false, false, false);
    for m in modifiers {
        match m.to_ascii_lowercase().as_str() {
            "ctrl" | "control" => ctrl = true,
            "alt" | "option" => alt = true,
            "shift" => shift = true,
            "super" | "meta" | "cmd" | "command" | "logo" => sup = true,
            _ => return Err(format!("“{}” is not one key with modifiers.", raw.trim())),
        }
    }
    let lower = key.to_ascii_lowercase();
    let modifier_key = ["control", "ctrl", "alt", "option", "shift", "super", "meta", "cmd", "command", "logo", "os"]
        .iter()
        .any(|m| lower == *m || lower.strip_prefix(m).is_some_and(|rest| rest == "left" || rest == "right"));
    if modifier_key {
        return Err("Press a key with the modifiers, not only modifiers.".into());
    }
    let key = if let Some(letter) = key.strip_prefix("Key").filter(|k| k.len() == 1) {
        letter.to_ascii_uppercase()
    } else if let Some(digit) = key.strip_prefix("Digit").filter(|k| k.len() == 1) {
        digit.to_string()
    } else if key.len() == 1 {
        key.to_ascii_uppercase()
    } else {
        let mut chars = key.chars();
        chars.next().map(|c| c.to_ascii_uppercase().to_string() + chars.as_str()).unwrap_or_default()
    };
    if !(ctrl || alt || sup) {
        return Err(format!("{} needs Ctrl, Alt or Super with it, or it would be taken from every app.", if shift { format!("Shift+{key}") } else { key }));
    }
    let mut parts: Vec<&str> = Vec::new();
    for (on, name) in [(ctrl, "Ctrl"), (alt, "Alt"), (shift, "Shift"), (sup, "Super")] {
        if on {
            parts.push(name);
        }
    }
    parts.push(&key);
    let written = parts.join("+");
    written
        .parse::<tauri_plugin_global_shortcut::Shortcut>()
        .map_err(|e| format!("“{}” is not a key the app can take: {e}", raw.trim()))?;
    Ok(written)
}

/// The talk and cancel keys the person chose, written as the plugin reads
/// them; refused when either is, or when they are the same key.
pub fn chosen_keys(talk: &str, cancel: &str) -> Result<(String, String), String> {
    let (talk, cancel) = (accelerator(talk)?, accelerator(cancel)?);
    if talk == cancel {
        return Err(format!("{talk} cannot both talk and cancel."));
    }
    Ok((talk, cancel))
}

/// The portal's shortcut IDs.
pub const TALK: &str = "talk";
pub const CANCEL: &str = "cancel";

/// The key the app asks the portal for, in the XDG shortcuts
/// specification's words (Meta+Alt+Space); the desktop may give another.
pub const PORTAL_TRIGGER: &str = "LOGO+ALT+space";

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
    fn a_wayland_desktop_grabs_the_key_through_xwayland_unless_the_portal_is_chosen() {
        use KeyBackend::{Grab, Portal};
        assert_eq!(backend(&linux(Some("wayland"), true, true), Grab), Backend::Grab);
        assert_eq!(backend(&linux(None, true, true), Grab), Backend::Grab);
        assert_eq!(backend(&linux(Some("wayland"), true, true), Portal), Backend::Portal);
        assert_eq!(backend(&linux(None, true, true), Portal), Backend::Portal);
        // No XWayland to grab through: the portal.
        assert_eq!(backend(&linux(Some("wayland"), true, false), Grab), Backend::Portal);
        // The portal is for Wayland; X11 always grabs.
        assert_eq!(backend(&linux(Some("x11"), false, true), Portal), Backend::Grab);
        // Plasma on X11 also sets WAYLAND_DISPLAY sometimes; the session type wins.
        assert_eq!(backend(&linux(Some("x11"), true, true), Portal), Backend::Grab);
        assert_eq!(backend(&linux(Some("tty"), false, false), Grab), Backend::Link);
        assert_eq!(backend(&Platform { os: "macos", ..Platform::default() }, Portal), Backend::Grab);
        assert_eq!(backend(&Platform { os: "freebsd", ..Platform::default() }, Grab), Backend::Link);
    }

    #[test]
    fn only_a_wayland_desktop_can_choose_the_portal() {
        assert!(can_choose(&linux(Some("wayland"), true, true)));
        assert!(!can_choose(&linux(Some("wayland"), true, false)));
        assert!(!can_choose(&linux(Some("x11"), false, true)));
        assert!(!can_choose(&Platform { os: "macos", ..Platform::default() }));
    }

    #[test]
    fn the_grabbed_keys_are_ctrl_alt_space_and_with_shift_to_cancel() {
        let linux_keys = default_keys("linux");
        assert_eq!((linux_keys.talk, linux_keys.cancel), ("Ctrl+Alt+Space", "Ctrl+Alt+Shift+Space"));
        assert_eq!(default_keys("windows"), linux_keys);
        let mac = default_keys("macos");
        assert_eq!((mac.talk, mac.cancel), ("Ctrl+Shift+Space", "Ctrl+Alt+Shift+Space"));
        for keys in [linux_keys, mac] {
            assert_eq!(accelerator(keys.talk).as_deref(), Ok(keys.talk));
            assert_eq!(accelerator(keys.cancel).as_deref(), Ok(keys.cancel));
        }
    }

    #[test]
    fn a_chosen_key_is_written_as_the_plugin_reads_it() {
        // As the page records a key: the DOM's code for it.
        assert_eq!(accelerator("Ctrl+Alt+KeyV").as_deref(), Ok("Ctrl+Alt+V"));
        assert_eq!(accelerator("Alt+Ctrl+Digit1").as_deref(), Ok("Ctrl+Alt+1"));
        assert_eq!(accelerator("Meta+Shift+Space").as_deref(), Ok("Shift+Super+Space"));
        assert_eq!(accelerator("control+option+space").as_deref(), Ok("Ctrl+Alt+Space"));
        assert_eq!(accelerator("Ctrl+Alt+F5").as_deref(), Ok("Ctrl+Alt+F5"));
        assert_eq!(accelerator(" Ctrl + Alt + KeyP ").as_deref(), Ok("Ctrl+Alt+P"));
    }

    #[test]
    fn a_key_with_no_ctrl_alt_or_super_is_refused() {
        // It would be taken from every app, and KDE passes X11 apps only
        // keys typed with Ctrl, Alt or Meta.
        for raw in ["KeyA", "Space", "Shift+KeyA", "Shift+Space", "F5"] {
            assert!(accelerator(raw).is_err(), "{raw}");
        }
    }

    #[test]
    fn a_key_that_is_not_one_key_is_refused() {
        for raw in ["", "Ctrl+Alt", "Ctrl+ControlLeft", "Ctrl+Alt+ShiftLeft", "Ctrl+A+B", "Ctrl+Alt+Nonsense", "Ctrl++A"] {
            assert!(accelerator(raw).is_err(), "{raw:?}");
        }
    }

    #[test]
    fn talk_and_cancel_must_be_different_keys() {
        assert!(chosen_keys("Ctrl+Alt+KeyV", "Ctrl+Alt+Shift+KeyV").is_ok());
        assert_eq!(chosen_keys("Ctrl+Alt+KeyV", "Ctrl+Alt+KeyB").unwrap(), ("Ctrl+Alt+V".to_string(), "Ctrl+Alt+B".to_string()));
        assert!(chosen_keys("Ctrl+Alt+KeyV", "Alt+Ctrl+V").is_err());
        assert!(chosen_keys("KeyV", "Ctrl+Alt+KeyB").is_err());
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
