//! The always-on voice widget: a second window, `voice`, drawn from the
//! app's own bundle (`voice.html`), over every other window in a corner of
//! the screen, never taking keyboard focus.
//!
//! - **Wayland**: a client may not place itself or keep itself above other
//!   windows, so the widget is a layer-shell surface (`zwlr_layer_shell_v1`,
//!   which KWin has), through `gtk-layer-shell` loaded at run time
//!   (`layer_shell`): layer Top, anchored to its corner 16 px in, exclusive
//!   zone 0, keyboard None. Without the library (or a compositor without
//!   layer shell) it is an ordinary undecorated window the person places.
//! - **X11**: always on top, placed in its corner, not focusable.
//! - **macOS**: an ordinary always-on-top window for now. TODO: a
//!   non-activating NSPanel (`tauri-nspanel`) at floating level, on every
//!   space.
//!
//! Only the geometry and the choices are decided here, and tested; the
//! window itself is made in `app.rs` (`make_voice_widget`).

use crate::store::Corner;

/// The window's label, which its capability and events name.
pub const LABEL: &str = "voice";

/// How far the widget keeps from the screen's edges, in logical pixels.
pub const MARGIN: f64 = 16.0;

/// How the widget keeps its place.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Placement {
    /// A layer-shell surface anchored to its corner (Wayland).
    LayerShell,
    /// The app places the window in its corner, always on top (X11, macOS).
    Placed,
    /// An ordinary window the compositor places (Wayland without layer shell).
    Floating,
}

/// How the widget keeps its place on this computer: wayland is the
/// session's kind, layer_shell whether `gtk-layer-shell` loaded and the
/// compositor has the protocol.
pub fn placement(wayland: bool, layer_shell: bool) -> Placement {
    match (wayland, layer_shell) {
        (true, true) => Placement::LayerShell,
        (true, false) => Placement::Floating,
        (false, _) => Placement::Placed,
    }
}

/// A rectangle on the screen, in physical pixels.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Rect {
    pub x: f64,
    pub y: f64,
    pub width: f64,
    pub height: f64,
}

/// Where the widget's top-left goes: in its corner of the work area (the
/// screen less panels), MARGIN in from both edges, scaled.
pub fn place(corner: Corner, area: Rect, size: (f64, f64), scale: f64) -> (f64, f64) {
    let margin = MARGIN * scale;
    let (w, h) = size;
    let left = area.x + margin;
    let right = area.x + area.width - margin - w;
    let top = area.y + margin;
    let bottom = area.y + area.height - margin - h;
    match corner {
        Corner::BottomRight => (right, bottom),
        Corner::BottomLeft => (left, bottom),
        Corner::TopRight => (right, top),
        Corner::TopLeft => (left, top),
    }
}

/// The edges a layer surface in this corner is anchored to: (left, right,
/// top, bottom).
pub fn anchors(corner: Corner) -> (bool, bool, bool, bool) {
    match corner {
        Corner::BottomRight => (false, true, false, true),
        Corner::BottomLeft => (true, false, false, true),
        Corner::TopRight => (false, true, true, false),
        Corner::TopLeft => (true, false, true, false),
    }
}

/// Whether the widget shows: always during a conversation and while it
/// says something (not ready, connecting), else unless the person hid it.
pub fn shows(hidden: bool, state: &str) -> bool {
    !hidden || state != "idle"
}

/// The widget's size before the page says, and the limits it is kept to
/// (the card is 412 wide; the not-ready capsule 430).
pub const FIRST_SIZE: (f64, f64) = (318.0, 64.0);

pub fn clamp_size(width: f64, height: f64) -> (f64, f64) {
    (width.clamp(40.0, 480.0), height.clamp(40.0, 420.0))
}

/// The main's settings page at a setting, for Open settings:
/// `/settings#setting-<key>`, or `/settings` with none.
pub fn settings_url(base: &str, setting: &str) -> Result<reqwest::Url, String> {
    let mut url = reqwest::Url::parse(base).map_err(|e| e.to_string())?;
    url.set_path("/settings");
    url.set_query(None);
    url.set_fragment((!setting.is_empty()).then(|| format!("setting-{setting}")).as_deref());
    Ok(url)
}

/// The corners as the menu names them.
pub const CORNERS: [(Corner, &str, &str); 4] = [
    (Corner::BottomRight, "corner-bottom-right", "Bottom right"),
    (Corner::BottomLeft, "corner-bottom-left", "Bottom left"),
    (Corner::TopRight, "corner-top-right", "Top right"),
    (Corner::TopLeft, "corner-top-left", "Top left"),
];

/// `gtk-layer-shell`, loaded at run time, so a build or a computer without
/// it still runs (the widget is then an ordinary window). It is not linked:
/// the AppImage can carry `libgtk-layer-shell.so.0` (see docs/ai/desktop.md
/// § The widget), else the system's is used.
#[cfg(target_os = "linux")]
pub mod layer_shell {
    use super::{anchors, MARGIN};
    use crate::store::Corner;
    use gtk::glib::translate::ToGlibPtr;
    use gtk::prelude::*;
    use std::ffi::c_int;

    type Win = *mut gtk::ffi::GtkWindow;

    // The library's C names and values (gtk-layer-shell 0.6 or later).
    const LAYER_TOP: c_int = 2;
    const KEYBOARD_NONE: c_int = 0;
    const EDGE_LEFT: c_int = 0;
    const EDGE_RIGHT: c_int = 1;
    const EDGE_TOP: c_int = 2;
    const EDGE_BOTTOM: c_int = 3;

    pub struct LayerShell {
        _lib: libloading::Library,
        is_supported: unsafe extern "C" fn() -> c_int,
        init_for_window: unsafe extern "C" fn(Win),
        set_namespace: unsafe extern "C" fn(Win, *const std::ffi::c_char),
        set_layer: unsafe extern "C" fn(Win, c_int),
        set_anchor: unsafe extern "C" fn(Win, c_int, c_int),
        set_margin: unsafe extern "C" fn(Win, c_int, c_int),
        set_exclusive_zone: unsafe extern "C" fn(Win, c_int),
        set_keyboard_mode: unsafe extern "C" fn(Win, c_int),
    }

    impl LayerShell {
        /// The library, when it is installed (or carried) and the
        /// compositor has layer shell.
        pub fn load() -> Result<LayerShell, String> {
            // SAFETY: loading a library runs its constructors; this one is
            // GTK's own layer-shell library, which has none of note.
            // The AppImage's own copy first, else the system's.
            let carried = std::env::var_os("APPDIR").map(|d| std::path::PathBuf::from(d).join("usr/lib/libgtk-layer-shell.so.0")).filter(|p| p.is_file());
            let name = carried.map(|p| p.into_os_string()).unwrap_or_else(|| "libgtk-layer-shell.so.0".into());
            let lib = unsafe { libloading::Library::new(&name) }.map_err(|e| e.to_string())?;
            macro_rules! sym {
                ($name:literal) => {
                    // SAFETY: the names and C signatures are gtk-layer-shell's.
                    *unsafe { lib.get(concat!($name, "\0").as_bytes()) }.map_err(|e| format!("{}: {e}", $name))?
                };
            }
            let shell = LayerShell {
                is_supported: sym!("gtk_layer_is_supported"),
                init_for_window: sym!("gtk_layer_init_for_window"),
                set_namespace: sym!("gtk_layer_set_namespace"),
                set_layer: sym!("gtk_layer_set_layer"),
                set_anchor: sym!("gtk_layer_set_anchor"),
                set_margin: sym!("gtk_layer_set_margin"),
                set_exclusive_zone: sym!("gtk_layer_set_exclusive_zone"),
                set_keyboard_mode: sym!("gtk_layer_set_keyboard_mode"),
                _lib: lib,
            };
            // SAFETY: called on the GTK main thread, after GTK started.
            if unsafe { (shell.is_supported)() } == 0 {
                return Err("the compositor has no layer shell".into());
            }
            Ok(shell)
        }

        /// Makes the window a layer surface in its corner. It must not have
        /// been shown yet: layer shell is set before GTK realizes a window.
        pub fn attach(&self, window: &gtk::ApplicationWindow, corner: Corner) -> Result<(), String> {
            if window.is_realized() {
                return Err("the window was realized before layer shell".into());
            }
            // tao gives an undecorated Wayland window an empty title bar
            // (client-side decorations); a layer surface has none.
            window.set_titlebar(None::<&gtk::Widget>);
            let w: Win = window.upcast_ref::<gtk::Window>().to_glib_none().0;
            // SAFETY: w is a live, unrealized GtkWindow, on the main thread.
            unsafe {
                (self.init_for_window)(w);
                (self.set_namespace)(w, c"legato-voice".as_ptr());
                (self.set_layer)(w, LAYER_TOP);
                (self.set_exclusive_zone)(w, 0);
                (self.set_keyboard_mode)(w, KEYBOARD_NONE);
                for edge in [EDGE_LEFT, EDGE_RIGHT, EDGE_TOP, EDGE_BOTTOM] {
                    (self.set_margin)(w, edge, MARGIN as c_int);
                }
            }
            self.corner(window, corner);
            Ok(())
        }

        /// Anchors the surface to another corner.
        pub fn corner(&self, window: &gtk::ApplicationWindow, corner: Corner) {
            let w: Win = window.upcast_ref::<gtk::Window>().to_glib_none().0;
            let (left, right, top, bottom) = anchors(corner);
            // SAFETY: as in attach.
            unsafe {
                (self.set_anchor)(w, EDGE_LEFT, left as c_int);
                (self.set_anchor)(w, EDGE_RIGHT, right as c_int);
                (self.set_anchor)(w, EDGE_TOP, top as c_int);
                (self.set_anchor)(w, EDGE_BOTTOM, bottom as c_int);
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const SCREEN: Rect = Rect { x: 0.0, y: 0.0, width: 1920.0, height: 1040.0 };

    #[test]
    fn the_widget_keeps_16_pixels_from_its_corner() {
        let size = (318.0, 64.0);
        assert_eq!(place(Corner::BottomRight, SCREEN, size, 1.0), (1920.0 - 16.0 - 318.0, 1040.0 - 16.0 - 64.0));
        assert_eq!(place(Corner::BottomLeft, SCREEN, size, 1.0), (16.0, 960.0));
        assert_eq!(place(Corner::TopRight, SCREEN, size, 1.0), (1586.0, 16.0));
        assert_eq!(place(Corner::TopLeft, SCREEN, size, 1.0), (16.0, 16.0));
    }

    #[test]
    fn the_margin_scales_and_a_second_screen_keeps_its_offset() {
        let right = Rect { x: 1920.0, y: 40.0, width: 2560.0, height: 1400.0 };
        // At 2x the widget is 636x128 physical pixels and the margin 32.
        assert_eq!(place(Corner::TopLeft, right, (636.0, 128.0), 2.0), (1952.0, 72.0));
        assert_eq!(place(Corner::BottomRight, right, (636.0, 128.0), 2.0), (1920.0 + 2560.0 - 32.0 - 636.0, 40.0 + 1400.0 - 32.0 - 128.0));
    }

    #[test]
    fn a_layer_surface_is_anchored_to_the_two_edges_of_its_corner() {
        assert_eq!(anchors(Corner::BottomRight), (false, true, false, true));
        assert_eq!(anchors(Corner::TopLeft), (true, false, true, false));
        for (corner, _, _) in CORNERS {
            let (l, r, t, b) = anchors(corner);
            assert!(l != r && t != b, "{corner:?}: one horizontal and one vertical edge");
        }
    }

    #[test]
    fn wayland_without_layer_shell_falls_back_to_a_floating_window() {
        assert_eq!(placement(true, true), Placement::LayerShell);
        assert_eq!(placement(true, false), Placement::Floating);
        assert_eq!(placement(false, false), Placement::Placed);
        assert_eq!(placement(false, true), Placement::Placed);
    }

    #[test]
    fn a_hidden_widget_still_shows_during_a_conversation() {
        assert!(shows(false, "idle"));
        assert!(!shows(true, "idle"));
        for state in ["connecting", "not_ready", "listening", "thinking", "speaking"] {
            assert!(shows(true, state), "{state}");
        }
    }

    #[test]
    fn open_settings_goes_to_the_mains_setting() {
        assert_eq!(settings_url("https://erebus.local:3080", "reeve.voice.enabled").unwrap().as_str(), "https://erebus.local:3080/settings#setting-reeve.voice.enabled");
        assert_eq!(settings_url("http://10.0.0.2:3080/", "").unwrap().as_str(), "http://10.0.0.2:3080/settings");
    }

    #[test]
    fn the_page_cannot_make_the_window_huge_or_nothing() {
        assert_eq!(clamp_size(412.0, 220.0), (412.0, 220.0));
        assert_eq!(clamp_size(0.0, 9999.0), (40.0, 420.0));
    }
}
