//! When the app ends, and what a second start does: the decisions, apart
//! from Tauri, so they are tested. `app.rs` acts on them.
//!
//! The app does not run in the background: it has no tray, so a process
//! with no main window would leave the person no way back. Closing the
//! main window quits the app, and the voice widget goes with it. Tauri
//! would otherwise keep running while any window is open, and the widget
//! (a layer surface on Wayland) is one.

use crate::voicekey::VoiceKey;

/// The main window's label.
pub const MAIN: &str = "main";

/// What became of a window.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Closed {
    /// It was asked to close (its close button, the window manager).
    Requested,
    /// It is gone.
    Destroyed,
}

/// Whether the app quits when a window closes: when the main window is
/// gone, whatever else is open. Not on the request alone, so a close the
/// system refuses leaves the app as it was.
pub fn quits_when(label: &str, closed: Closed) -> bool {
    label == MAIN && closed == Closed::Destroyed
}

/// What a second start of the app asks of the one running.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Relaunch {
    /// The voice key, by its link (`legato-desktop --voice`): no window.
    Key(VoiceKey),
    /// The main window, in front.
    ShowMain,
    /// The main window is gone: make it again, in front.
    MakeMain,
}

/// A second start with these arguments (the first is the program), the
/// main window there or not. Anything but the voice key brings the main
/// window to the front, so a start never does nothing.
pub fn relaunch<S: AsRef<str>>(args: &[S], has_main: bool) -> Relaunch {
    match crate::voicekey::read_args(args) {
        Some(key) => Relaunch::Key(key),
        None if has_main => Relaunch::ShowMain,
        None => Relaunch::MakeMain,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_app_quits_when_the_main_window_is_gone() {
        assert!(quits_when(MAIN, Closed::Destroyed));
        assert!(!quits_when(MAIN, Closed::Requested));
    }

    #[test]
    fn the_widget_closing_leaves_the_app_running() {
        assert!(!quits_when(crate::voicewidget::LABEL, Closed::Destroyed));
        assert!(!quits_when(crate::voicewidget::LABEL, Closed::Requested));
    }

    #[test]
    fn a_second_start_shows_the_main_window() {
        assert_eq!(relaunch(&["legato-desktop"], true), Relaunch::ShowMain);
        // A link the deep-link plugin reads is not the voice key.
        assert_eq!(relaunch(&["legato-desktop", "legato-desktop://import?x=1"], true), Relaunch::ShowMain);
    }

    #[test]
    fn a_second_start_with_no_main_window_makes_one() {
        assert_eq!(relaunch(&["legato-desktop"], false), Relaunch::MakeMain);
    }

    #[test]
    fn the_voice_key_by_its_link_opens_no_window() {
        assert_eq!(relaunch(&["legato-desktop", "--voice"], true), Relaunch::Key(VoiceKey::Talk));
        assert_eq!(relaunch(&["legato-desktop", "--voice=cancel"], false), Relaunch::Key(VoiceKey::Cancel));
    }
}
