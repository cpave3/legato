//! The icon by the clock: what a click on it and its menu ask, apart from
//! Tauri, so they are tested. `app.rs` makes the icon (`make_tray`) and acts.
//!
//! A left click shows or hides the Legato widget; the menu (a right click,
//! or the Mac's menu bar) has the widget's item, Talk to the Reeve, Open
//! Legato, Servers & keys and Quit. Showing or hiding flips the person's
//! setting, the one Servers & keys › Show the Legato widget, the capsule's ×
//! and its Hide change, never only the window: a hidden widget that shows
//! because approvals wait is still hidden, so a click "shows" it (no change
//! on screen) and the next hides it, and the menu's label and the checkbox
//! always agree.
//!
//! On Linux the icon is a StatusNotifierItem, through tray-icon's KSNI
//! backend (Cargo.toml says why). With no tray host (GNOME without the
//! AppIndicator extension) the icon is not made: the app logs it once
//! (`no_host`) and carries on without one, until a restart.

use tauri::tray::{MouseButton, MouseButtonState};

/// The icon's ID.
pub const ID: &str = "legato";

/// The menu's items. They share the app's one menu handler with the
/// widget's menu (`voice-*`, `screen-*`, `voicewidget::PLACES`), so each
/// starts with `tray-`, which none of those does.
pub const WIDGET: &str = "tray-widget";
pub const TALK: &str = "tray-talk";
pub const OPEN: &str = "tray-open";
pub const SETTINGS: &str = "tray-settings";
pub const QUIT: &str = "tray-quit";

/// What the icon was asked.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Action {
    /// Show the widget if the setting hides it, else hide it.
    ToggleWidget,
    /// Talk to the Reeve, as the key does.
    Talk,
    /// The main window, in front.
    OpenLegato,
    /// Servers & keys, in front.
    Settings,
    Quit,
}

/// A click on the icon: a left button let go toggles the widget. Its press
/// does nothing (a click is both), nor does any other button: on Linux a
/// middle click is the host's secondary activation, and a right click
/// opens the menu.
pub fn on_click(button: MouseButton, state: MouseButtonState) -> Option<Action> {
    (button == MouseButton::Left && state == MouseButtonState::Up).then_some(Action::ToggleWidget)
}

/// The menu item chosen, when it is the tray's.
pub fn chosen(id: &str) -> Option<Action> {
    match id {
        WIDGET => Some(Action::ToggleWidget),
        TALK => Some(Action::Talk),
        OPEN => Some(Action::OpenLegato),
        SETTINGS => Some(Action::Settings),
        QUIT => Some(Action::Quit),
        _ => None,
    }
}

/// The setting a toggle saves: hidden when it was shown, and the other way.
pub fn toggled(hidden: bool) -> bool {
    !hidden
}

/// The widget's item, as the setting is.
pub fn widget_item_text(hidden: bool) -> &'static str {
    if hidden {
        "Show widget"
    } else {
        "Hide widget"
    }
}

/// The one line logged when the icon cannot be made.
pub fn no_host(err: &str) -> String {
    format!("[tray] no icon by the clock ({err}): the desktop has no tray host. On GNOME, turn on the AppIndicator extension and start the app again")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_left_click_toggles_the_widget_when_the_button_is_let_go() {
        assert_eq!(on_click(MouseButton::Left, MouseButtonState::Up), Some(Action::ToggleWidget));
        assert_eq!(on_click(MouseButton::Left, MouseButtonState::Down), None);
    }

    #[test]
    fn other_buttons_do_nothing() {
        for state in [MouseButtonState::Up, MouseButtonState::Down] {
            assert_eq!(on_click(MouseButton::Right, state), None);
            assert_eq!(on_click(MouseButton::Middle, state), None);
        }
    }

    #[test]
    fn each_menu_item_asks_its_action() {
        assert_eq!(chosen("tray-widget"), Some(Action::ToggleWidget));
        assert_eq!(chosen("tray-talk"), Some(Action::Talk));
        assert_eq!(chosen("tray-open"), Some(Action::OpenLegato));
        assert_eq!(chosen("tray-settings"), Some(Action::Settings));
        assert_eq!(chosen("tray-quit"), Some(Action::Quit));
    }

    #[test]
    fn the_widgets_own_menu_items_are_not_the_trays() {
        assert_eq!(chosen("tray-"), None);
        assert_eq!(chosen("something"), None);
        for id in ["voice-hide", "voice-settings", "screen-auto", "screen-0", "screen-3"] {
            assert_eq!(chosen(id), None, "{id}");
        }
        for (_, id, _) in crate::voicewidget::PLACES {
            assert_eq!(chosen(id), None, "{id}");
        }
    }

    #[test]
    fn the_widgets_menu_handler_never_takes_a_tray_item() {
        // voice_widget_menu_chosen matches PLACES ids, then screen-*.
        for id in [WIDGET, TALK, OPEN, SETTINGS, QUIT] {
            assert!(!crate::voicewidget::PLACES.iter().any(|(_, p, _)| *p == id), "{id}");
            assert!(!id.starts_with("screen-") && !id.starts_with("voice-"), "{id}");
        }
    }

    #[test]
    fn a_toggle_flips_the_setting_and_back() {
        assert!(toggled(false));
        assert!(!toggled(true));
        assert!(!toggled(toggled(false)));
    }

    #[test]
    fn the_widget_item_says_what_a_choice_of_it_does() {
        assert_eq!(widget_item_text(true), "Show widget");
        assert_eq!(widget_item_text(false), "Hide widget");
    }

    #[test]
    fn tauri_and_the_app_share_one_tray_icon_crate_so_linux_gets_ksni() {
        // Two versions in the lock would leave Tauri's tray on AppIndicator,
        // which hears no left click (Cargo.toml).
        let lock = std::fs::read_to_string(concat!(env!("CARGO_MANIFEST_DIR"), "/Cargo.lock")).unwrap();
        let copies = lock.lines().filter(|l| *l == "name = \"tray-icon\"").count();
        assert_eq!(copies, 1, "tray-icon is in Cargo.lock {copies} times: match Cargo.toml's to Tauri's");
    }

    #[test]
    fn with_no_tray_host_the_log_says_how_to_get_one_on_gnome() {
        let line = no_host("Watcher(ServiceUnknown)");
        assert!(line.starts_with("[tray] "), "{line}");
        assert!(line.contains("Watcher(ServiceUnknown)"), "{line}");
        assert!(line.contains("AppIndicator"), "{line}");
    }
}
