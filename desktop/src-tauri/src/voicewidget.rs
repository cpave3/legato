//! The always-on voice widget: a second window, `voice`, drawn from the
//! app's own bundle (`voice.html`), over every other window at a corner or
//! an edge of the screen, or where the person dragged it, never taking
//! keyboard focus.
//!
//! Where it keeps to is an anchor (a corner, or an edge it is centred on)
//! and an offset from the anchored edges, so a spot it was dropped on stays
//! as far from its edge when the screen changes size (`drop_at`,
//! `margins`).
//!
//! - **Wayland**: a client may not place itself or keep itself above other
//!   windows, so the widget is a layer-shell surface (`zwlr_layer_shell_v1`,
//!   which KWin has), through `gtk-layer-shell` loaded at run time
//!   (`layer_shell`; the AppImage carries it): layer Top, anchored to its
//!   anchor's edges with the offset as margins, exclusive zone 0, keyboard
//!   None. Without the library (or a compositor without layer shell) it is
//!   an ordinary undecorated window the person places.
//! - **X11**: always on top, placed by the app, not focusable.
//! - **macOS**: an ordinary always-on-top window for now. TODO: a
//!   non-activating NSPanel (`tauri-nspanel`) at floating level, on every
//!   space.
//!
//! Only the geometry and the choices are decided here, and tested; the
//! window itself is made in `app.rs` (`make_voice_widget`).

use std::time::{Duration, Instant};

pub use crate::store::{Anchor, Offset};

/// The window's label, which its capability and events name.
pub const LABEL: &str = "voice";

/// How far the widget keeps from the screen's edges, in logical pixels.
pub const MARGIN: f64 = 16.0;

/// How the widget keeps its place.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Placement {
    /// A layer-shell surface anchored to its edges (Wayland).
    LayerShell,
    /// The app places the window, always on top (X11, macOS).
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

/// The edges a layer surface with this anchor is anchored to: (left, right,
/// top, bottom). An edge anchor has one, and layer shell centres the surface
/// along it.
pub fn anchors(anchor: Anchor) -> (bool, bool, bool, bool) {
    match anchor {
        Anchor::BottomRight => (false, true, false, true),
        Anchor::BottomLeft => (true, false, false, true),
        Anchor::TopRight => (false, true, true, false),
        Anchor::TopLeft => (true, false, true, false),
        Anchor::Top => (false, false, true, false),
        Anchor::Bottom => (false, false, false, true),
        Anchor::Left => (true, false, false, false),
        Anchor::Right => (false, true, false, false),
    }
}

/// The margins the widget keeps from the edges it is anchored to, all
/// logical: the saved offset, kept so the whole widget (of size) is inside
/// the area (its width and height); MARGIN when none was saved, or when the
/// saved spot is off this screen (a monitor was unplugged). An axis the
/// widget is centred on has 0.
pub fn margins(anchor: Anchor, offset: Option<Offset>, area: (f64, f64), size: (f64, f64)) -> Offset {
    let (left, right, top, bottom) = anchors(anchor);
    let on_screen = |o: &Offset| (0.0..=area.0).contains(&o.x) && (0.0..=area.1).contains(&o.y);
    let o = offset.filter(on_screen).unwrap_or(Offset { x: MARGIN, y: MARGIN });
    let keep = |m: f64, room: f64| m.min(room.max(0.0)).max(0.0);
    Offset {
        x: if left || right { keep(o.x, area.0 - size.0) } else { 0.0 },
        y: if top || bottom { keep(o.y, area.1 - size.1) } else { 0.0 },
    }
}

/// The widget's top-left within the area, logical, for these margins.
pub fn origin(anchor: Anchor, m: Offset, area: (f64, f64), size: (f64, f64)) -> (f64, f64) {
    let (left, right, top, bottom) = anchors(anchor);
    let along = |near: bool, far: bool, margin: f64, span: f64, length: f64| {
        if near {
            margin
        } else if far {
            span - margin - length
        } else {
            (span - length) / 2.0
        }
    };
    (along(left, right, m.x, area.0, size.0), along(top, bottom, m.y, area.1, size.1))
}

/// Where the widget's top-left goes, where the app places it (X11, macOS):
/// at its anchor in the work area (the screen less panels), offset as saved
/// and kept on screen. size is the logical size the page asked for: on
/// Linux the window's own size changes only once the compositor has
/// answered, so it is not read back.
pub fn place(anchor: Anchor, offset: Option<Offset>, area: Rect, size: (f64, f64), scale: f64) -> (f64, f64) {
    let span = (area.width / scale, area.height / scale);
    let (x, y) = origin(anchor, margins(anchor, offset, span, size), span, size);
    (area.x + x * scale, area.y + y * scale)
}

/// Where a dragged widget goes next: its top-left (logical, within the
/// area) moved by delta, kept inside the area.
pub fn drag_to(pos: (f64, f64), delta: (f64, f64), area: (f64, f64), size: (f64, f64)) -> (f64, f64) {
    let keep = |v: f64, room: f64| v.min(room.max(0.0)).max(0.0);
    (keep(pos.0 + delta.0, area.0 - size.0), keep(pos.1 + delta.1, area.1 - size.1))
}

/// The corner a layer surface is anchored to while it is dragged: its own,
/// or for an edge a corner on that edge's side. Margins keep counting from
/// the edges they counted from, so the surface does not jump when the drag
/// starts (GTK on Wayland knows no panels; the compositor counts margins
/// from inside them).
pub fn drag_corner(anchor: Anchor) -> Anchor {
    match anchor {
        Anchor::Top | Anchor::Left => Anchor::TopLeft,
        Anchor::Bottom => Anchor::BottomLeft,
        Anchor::Right => Anchor::TopRight,
        corner => corner,
    }
}

/// The margins that put a widget's top-left at pos (logical, within the
/// area) with this anchor; an axis it is centred on has 0.
pub fn margins_for(anchor: Anchor, pos: (f64, f64), area: (f64, f64), size: (f64, f64)) -> Offset {
    let (left, right, top, bottom) = anchors(anchor);
    let pick = |near: bool, far: bool, a: f64, b: f64| if near { a } else if far { b } else { 0.0 };
    Offset { x: pick(left, right, pos.0, area.0 - pos.0 - size.0), y: pick(top, bottom, pos.1, area.1 - pos.1 - size.1) }
}

/// How near the middle of a side the widget's centre must be dropped to
/// keep to the middle of an edge, as a share of the side.
pub const CENTRE_BAND: f64 = 0.1;

/// What a widget dropped with its top-left at pos (logical, within the
/// area) keeps to: the nearest corner, or the edge it is near the middle
/// of, with the offset that keeps it where it was dropped. Saved as anchor
/// and offset, it stays as far from that edge when the screen changes size.
pub fn drop_at(pos: (f64, f64), size: (f64, f64), area: (f64, f64)) -> (Anchor, Offset) {
    let (x, y) = drag_to(pos, (0.0, 0.0), area, size);
    #[derive(PartialEq)]
    enum Side {
        Near,
        Middle,
        Far,
    }
    let side = |start: f64, length: f64, span: f64| {
        let centre = start + length / 2.0;
        if (centre - span / 2.0).abs() <= span * CENTRE_BAND {
            Side::Middle
        } else if centre < span / 2.0 {
            Side::Near
        } else {
            Side::Far
        }
    };
    let (gap_left, gap_right, gap_top, gap_bottom) = (x, area.0 - x - size.0, y, area.1 - y - size.1);
    let anchor = match (side(x, size.0, area.0), side(y, size.1, area.1)) {
        (Side::Near, Side::Near) => Anchor::TopLeft,
        (Side::Far, Side::Near) => Anchor::TopRight,
        (Side::Near, Side::Far) => Anchor::BottomLeft,
        (Side::Far, Side::Far) => Anchor::BottomRight,
        (Side::Middle, Side::Near) => Anchor::Top,
        (Side::Middle, Side::Far) => Anchor::Bottom,
        (Side::Near, Side::Middle) => Anchor::Left,
        (Side::Far, Side::Middle) => Anchor::Right,
        // In the middle of the screen: the nearest edge.
        (Side::Middle, Side::Middle) => {
            let nearest = gap_left.min(gap_right).min(gap_top).min(gap_bottom);
            if nearest == gap_top {
                Anchor::Top
            } else if nearest == gap_bottom {
                Anchor::Bottom
            } else if nearest == gap_left {
                Anchor::Left
            } else {
                Anchor::Right
            }
        }
    };
    (anchor, margins_for(anchor, (x, y), area, size))
}

/// How long a hidden widget shows to say why the key could not listen.
pub const NOT_READY_FOR: Duration = Duration::from_secs(8);

/// How long it shows how a conversation ended: the page's card stays as
/// long (`ENDED_FOR` in src/voice/Widget.tsx).
pub const ENDED_FOR: Duration = Duration::from_secs(6);

/// Whether the widget has something to show whatever the person chose.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Showing {
    /// Nothing: at rest.
    Rest,
    /// A conversation, or the key asking the main.
    Busy,
    /// Something to say, until then.
    Until(Instant),
}

/// What the widget has to show after a `voice-state`: reason is the
/// state's, written whether the Reeve answered in writing. Idle with no
/// reason is a conversation kept after the person's Stop. Which ends say
/// something is as the page's `surface` has it (src/voice/model.ts).
pub fn showing(state: &str, reason: Option<&str>, written: bool, now: Instant) -> Showing {
    match (state, reason.unwrap_or_default()) {
        ("not_ready", _) => Showing::Until(now + NOT_READY_FOR),
        ("idle", "") => Showing::Busy,
        ("idle", "bye" | "cancelled") => Showing::Rest,
        ("idle", "done") if !written => Showing::Rest,
        ("idle", _) => Showing::Until(now + ENDED_FOR),
        _ => Showing::Busy,
    }
}

/// Whether the widget shows: unless the person hid it, and then still
/// while it has something to show, or while the feed under the voice has
/// something (waiting; for now, approvals: see widgetapprovals).
pub fn shows(hidden: bool, showing: Showing, waiting: bool, now: Instant) -> bool {
    !hidden
        || waiting
        || match showing {
            Showing::Rest => false,
            Showing::Busy => true,
            Showing::Until(until) => now < until,
        }
}

/// The widget's size before the page says.
pub const FIRST_SIZE: (f64, f64) = (318.0, 64.0);

/// The most the widget may be, logical, whatever the screen: the card is
/// 412 wide, the not-ready capsule 430; the height holds the capsule or
/// card and the feed under it, which scrolls in what is left.
pub const MAX_SIZE: (f64, f64) = (480.0, 640.0);

/// The least the widget may be, logical.
pub const MIN_SIZE: (f64, f64) = (40.0, 40.0);

/// The most the widget may be on a work area of this logical size: MAX_SIZE,
/// less on a screen too small for it with MARGIN on both sides. The page
/// keeps itself to it (`voice_widget_limits`), so all of it is on screen.
pub fn limits(area: (f64, f64)) -> (f64, f64) {
    let fit = |max: f64, span: f64, min: f64| max.min(span - 2.0 * MARGIN).max(min);
    (fit(MAX_SIZE.0, area.0, MIN_SIZE.0), fit(MAX_SIZE.1, area.1, MIN_SIZE.1))
}

/// The size the page asked for, kept to the limits.
pub fn clamp_size(width: f64, height: f64, limits: (f64, f64)) -> (f64, f64) {
    (width.clamp(MIN_SIZE.0, limits.0), height.clamp(MIN_SIZE.1, limits.1))
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

/// The anchors as the menu names them, corners then edges.
pub const PLACES: [(Anchor, &str, &str); 8] = [
    (Anchor::BottomRight, "place-bottom-right", "Bottom right"),
    (Anchor::BottomLeft, "place-bottom-left", "Bottom left"),
    (Anchor::TopRight, "place-top-right", "Top right"),
    (Anchor::TopLeft, "place-top-left", "Top left"),
    (Anchor::Top, "place-top", "Top"),
    (Anchor::Bottom, "place-bottom", "Bottom"),
    (Anchor::Left, "place-left", "Left"),
    (Anchor::Right, "place-right", "Right"),
];

/// `gtk-layer-shell`, loaded at run time, so a build or a computer without
/// it still runs (the widget is then an ordinary window). It is not linked:
/// the AppImage can carry `libgtk-layer-shell.so.0` (see docs/ai/desktop.md
/// § The widget), else the system's is used.
#[cfg(target_os = "linux")]
pub mod layer_shell {
    use super::{anchors, Anchor, Offset};
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
        set_monitor: unsafe extern "C" fn(Win, *mut gtk::gdk::ffi::GdkMonitor),
    }

    /// A layer surface is the size the window asks for, not the size it was
    /// given: GTK sends the request after the first configure, and a window
    /// holding only a web view asks for none, so it is never drawn. The
    /// window asks for the widget's size.
    fn request(window: &gtk::ApplicationWindow, (width, height): (f64, f64)) {
        window.set_size_request(width.round() as i32, height.round() as i32);
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
                set_monitor: sym!("gtk_layer_set_monitor"),
                _lib: lib,
            };
            // SAFETY: called on the GTK main thread, after GTK started.
            if unsafe { (shell.is_supported)() } == 0 {
                return Err("the compositor has no layer shell".into());
            }
            Ok(shell)
        }

        /// Makes the window a layer surface of this size (logical). It must
        /// not have been shown yet: layer shell is set before GTK realizes
        /// a window. `place` then anchors it.
        pub fn attach(&self, window: &gtk::ApplicationWindow, size: (f64, f64)) -> Result<(), String> {
            if window.is_realized() {
                return Err("the window was realized before layer shell".into());
            }
            // tao gives an undecorated Wayland window an empty title bar
            // (client-side decorations); a layer surface has none.
            window.set_titlebar(None::<&gtk::Widget>);
            request(window, size);
            let w: Win = window.upcast_ref::<gtk::Window>().to_glib_none().0;
            // SAFETY: w is a live, unrealized GtkWindow, on the main thread.
            unsafe {
                (self.init_for_window)(w);
                (self.set_namespace)(w, c"legato-voice".as_ptr());
                (self.set_layer)(w, LAYER_TOP);
                (self.set_exclusive_zone)(w, 0);
                (self.set_keyboard_mode)(w, KEYBOARD_NONE);
            }
            Ok(())
        }

        /// Puts the surface on a screen (GDK's index of the monitor; none is
        /// the compositor's choice), sizes it (logical), anchors it and sets
        /// its margins from the edges it is anchored to; the others have
        /// none.
        pub fn place(&self, window: &gtk::ApplicationWindow, monitor: Option<usize>, anchor: Anchor, m: Offset, size: (f64, f64)) {
            request(window, size);
            let w: Win = window.upcast_ref::<gtk::Window>().to_glib_none().0;
            let monitor = monitor.and_then(|i| WidgetExt::display(window).monitor(i as i32));
            let monitor_ptr: *mut gtk::gdk::ffi::GdkMonitor = monitor.as_ref().map_or(std::ptr::null_mut(), |m| m.to_glib_none().0);
            // SAFETY: as in attach; the monitor is GDK's, alive while held.
            unsafe { (self.set_monitor)(w, monitor_ptr) };
            let (left, right, top, bottom) = anchors(anchor);
            let px = |on: bool, v: f64| if on { v.round() as c_int } else { 0 };
            // SAFETY: as in attach.
            unsafe {
                for (edge, on, margin) in [(EDGE_LEFT, left, m.x), (EDGE_RIGHT, right, m.x), (EDGE_TOP, top, m.y), (EDGE_BOTTOM, bottom, m.y)] {
                    (self.set_anchor)(w, edge, on as c_int);
                    (self.set_margin)(w, edge, px(on, margin));
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::{Duration, Instant};

    const SCREEN: Rect = Rect { x: 0.0, y: 0.0, width: 1920.0, height: 1040.0 };

    #[test]
    fn the_widget_keeps_16_pixels_from_its_corner() {
        let size = (318.0, 64.0);
        assert_eq!(place(Anchor::BottomRight, None, SCREEN, size, 1.0), (1920.0 - 16.0 - 318.0, 1040.0 - 16.0 - 64.0));
        assert_eq!(place(Anchor::BottomLeft, None, SCREEN, size, 1.0), (16.0, 960.0));
        assert_eq!(place(Anchor::TopRight, None, SCREEN, size, 1.0), (1586.0, 16.0));
        assert_eq!(place(Anchor::TopLeft, None, SCREEN, size, 1.0), (16.0, 16.0));
    }

    #[test]
    fn on_an_edge_the_widget_is_centred_along_it() {
        let size = (318.0, 64.0);
        assert_eq!(place(Anchor::Top, None, SCREEN, size, 1.0), (801.0, 16.0));
        assert_eq!(place(Anchor::Bottom, None, SCREEN, size, 1.0), (801.0, 960.0));
        assert_eq!(place(Anchor::Left, None, SCREEN, size, 1.0), (16.0, 488.0));
        assert_eq!(place(Anchor::Right, None, SCREEN, size, 1.0), (1586.0, 488.0));
    }

    #[test]
    fn the_margin_scales_and_a_second_screen_keeps_its_offset() {
        let right = Rect { x: 1920.0, y: 40.0, width: 2560.0, height: 1400.0 };
        // At 2x the widget asked for 318x64 is 636x128 physical pixels and
        // the margin 32: placed from the size asked, not the window's,
        // which a Linux window has only once the compositor has resized it.
        assert_eq!(place(Anchor::TopLeft, None, right, (318.0, 64.0), 2.0), (1952.0, 72.0));
        assert_eq!(place(Anchor::BottomRight, None, right, (318.0, 64.0), 2.0), (1920.0 + 2560.0 - 32.0 - 636.0, 40.0 + 1400.0 - 32.0 - 128.0));
        // A saved offset is logical too.
        assert_eq!(place(Anchor::TopLeft, Some(Offset { x: 100.0, y: 50.0 }), right, (318.0, 64.0), 2.0), (2120.0, 140.0));
    }

    #[test]
    fn a_layer_surface_is_anchored_to_its_corner_or_its_edge() {
        assert_eq!(anchors(Anchor::BottomRight), (false, true, false, true));
        assert_eq!(anchors(Anchor::TopLeft), (true, false, true, false));
        for (anchor, _, _) in PLACES {
            let (l, r, t, b) = anchors(anchor);
            assert!(!(l && r) && !(t && b), "{anchor:?}: never stretched between two edges");
            let corner = (l || r) && (t || b);
            let edge = [l, r, t, b].iter().filter(|e| **e).count() == 1;
            assert!(corner || edge, "{anchor:?}");
        }
    }

    #[test]
    fn a_saved_spot_stays_as_far_from_its_edge_when_the_screen_changes() {
        let dropped = Some(Offset { x: 300.0, y: 200.0 });
        let size = (318.0, 64.0);
        assert_eq!(margins(Anchor::BottomRight, dropped, (1920.0, 1040.0), size), Offset { x: 300.0, y: 200.0 });
        assert_eq!(margins(Anchor::BottomRight, dropped, (2560.0, 1400.0), size), Offset { x: 300.0, y: 200.0 });
        // An edge keeps only the distance from it.
        assert_eq!(margins(Anchor::Top, dropped, (1920.0, 1040.0), size), Offset { x: 0.0, y: 200.0 });
        assert_eq!(margins(Anchor::Left, dropped, (1920.0, 1040.0), size), Offset { x: 300.0, y: 0.0 });
    }

    #[test]
    fn the_whole_widget_stays_on_the_screen() {
        // Dropped near the bottom as a capsule, then a card 412x400 grows up
        // from the bottom edge: it is kept inside, the saved spot unchanged.
        let near_bottom = Some(Offset { x: 16.0, y: 900.0 });
        let card = (412.0, 400.0);
        let m = margins(Anchor::BottomRight, near_bottom, (1920.0, 1040.0), card);
        assert_eq!(m, Offset { x: 16.0, y: 640.0 });
        let (x, y) = origin(Anchor::BottomRight, m, (1920.0, 1040.0), card);
        assert_eq!((x, y), (1920.0 - 16.0 - 412.0, 0.0));
        for (anchor, _, _) in PLACES {
            let (x, y) = place(anchor, Some(Offset { x: 1800.0, y: 1000.0 }), SCREEN, card, 1.0);
            assert!(x >= 0.0 && y >= 0.0 && x + card.0 <= 1920.0 && y + card.1 <= 1040.0, "{anchor:?}: ({x}, {y})");
        }
    }

    #[test]
    fn a_spot_off_this_screen_falls_back_to_the_anchor() {
        // Saved on a 2560 wide monitor, now on a 1366x768 laptop.
        let far = Some(Offset { x: 2000.0, y: 100.0 });
        assert_eq!(margins(Anchor::TopRight, far, (1366.0, 768.0), (318.0, 64.0)), Offset { x: MARGIN, y: MARGIN });
        assert_eq!(margins(Anchor::TopRight, Some(Offset { x: -5.0, y: 10.0 }), (1366.0, 768.0), (318.0, 64.0)), Offset { x: MARGIN, y: MARGIN });
    }

    #[test]
    fn a_drag_moves_the_widget_but_not_off_the_screen() {
        let size = (318.0, 64.0);
        assert_eq!(drag_to((100.0, 100.0), (25.0, -40.0), (1920.0, 1040.0), size), (125.0, 60.0));
        assert_eq!(drag_to((100.0, 100.0), (-500.0, 5000.0), (1920.0, 1040.0), size), (0.0, 976.0));
    }

    #[test]
    fn a_drag_starts_from_the_margins_the_widget_had() {
        let (area, size) = ((1920.0, 1040.0), (318.0, 64.0));
        for (anchor, _, _) in PLACES {
            let corner = drag_corner(anchor);
            let (l, r, t, b) = anchors(corner);
            assert!((l || r) && (t || b), "{anchor:?}: dragged at a corner");
            // The edges it was anchored to are the corner's.
            let (al, ar, at, ab) = anchors(anchor);
            assert!((!al || l) && (!ar || r) && (!at || t) && (!ab || b), "{anchor:?}");
            let had = margins(anchor, Some(Offset { x: 40.0, y: 30.0 }), area, size);
            let pos = origin(anchor, had, area, size);
            let start = margins_for(corner, pos, area, size);
            if al || ar {
                assert_eq!(start.x, had.x, "{anchor:?}");
            }
            if at || ab {
                assert_eq!(start.y, had.y, "{anchor:?}");
            }
        }
    }

    #[test]
    fn a_drop_keeps_to_the_nearest_corner_or_edge() {
        let area = (1920.0, 1040.0);
        let size = (318.0, 64.0);
        // Near the top-left corner, 40 px from the left and 30 from the top.
        assert_eq!(drop_at((40.0, 30.0), size, area), (Anchor::TopLeft, Offset { x: 40.0, y: 30.0 }));
        // Bottom right: the offset is from the right and bottom edges.
        assert_eq!(drop_at((1500.0, 900.0), size, area), (Anchor::BottomRight, Offset { x: 102.0, y: 76.0 }));
        assert_eq!(drop_at((1500.0, 100.0), size, area).0, Anchor::TopRight);
        assert_eq!(drop_at((100.0, 900.0), size, area).0, Anchor::BottomLeft);
        // Near the middle of the top edge: centred on it, 20 px down.
        assert_eq!(drop_at((780.0, 20.0), size, area), (Anchor::Top, Offset { x: 0.0, y: 20.0 }));
        assert_eq!(drop_at((820.0, 950.0), size, area), (Anchor::Bottom, Offset { x: 0.0, y: 26.0 }));
        assert_eq!(drop_at((10.0, 480.0), size, area), (Anchor::Left, Offset { x: 10.0, y: 0.0 }));
        assert_eq!(drop_at((1590.0, 500.0), size, area), (Anchor::Right, Offset { x: 12.0, y: 0.0 }));
        // In the middle of the screen: the nearest edge, here the top.
        assert_eq!(drop_at((801.0, 420.0), size, area).0, Anchor::Top);
        // Dropped partly off the screen: kept on it.
        assert_eq!(drop_at((-50.0, -20.0), size, area), (Anchor::TopLeft, Offset { x: 0.0, y: 0.0 }));
    }

    #[test]
    fn a_drop_stays_where_it_was_dropped() {
        let area = (1920.0, 1040.0);
        let size = (318.0, 64.0);
        for pos in [(40.0, 30.0), (1500.0, 900.0), (780.0, 20.0), (10.0, 480.0), (1590.0, 500.0), (300.0, 700.0)] {
            let (anchor, offset) = drop_at(pos, size, area);
            let (x, y) = origin(anchor, margins(anchor, Some(offset), area, size), area, size);
            let (l, r, t, b) = anchors(anchor);
            // Along an edge it is centred; across, exactly where it was.
            if l || r {
                assert_eq!(x, pos.0, "{anchor:?} at {pos:?}");
            }
            if t || b {
                assert_eq!(y, pos.1, "{anchor:?} at {pos:?}");
            }
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
        let now = Instant::now();
        assert!(shows(false, Showing::Rest, false, now));
        assert!(!shows(true, Showing::Rest, false, now));
        for state in ["connecting", "listening", "thinking", "speaking"] {
            assert_eq!(showing(state, None, false, now), Showing::Busy, "{state}");
        }
        assert!(shows(true, Showing::Busy, false, now));
        // Idle with nothing said: the person stopped the Reeve and the
        // conversation is still open.
        assert_eq!(showing("idle", None, false, now), Showing::Busy);
    }

    #[test]
    fn a_hidden_widget_shows_while_approvals_wait() {
        let now = Instant::now();
        assert!(shows(true, Showing::Rest, true, now));
        assert!(!shows(true, Showing::Rest, false, now));
        // And whatever the conversation says.
        assert!(shows(true, Showing::Until(now), true, now + ENDED_FOR));
    }

    #[test]
    fn a_hidden_widget_shows_why_for_a_while() {
        let now = Instant::now();
        let not_ready = showing("not_ready", Some("Voice is off"), false, now);
        assert!(shows(true, not_ready, false, now + NOT_READY_FOR - Duration::from_millis(1)));
        assert!(!shows(true, not_ready, false, now + NOT_READY_FOR));

        // How a conversation ended, for as long as the page's card says it.
        for reason in ["no_speech", "nothing_heard", "no_answer", "failed", "taken over by Pixel 9", LOST_EXAMPLE] {
            let ended = showing("idle", Some(reason), false, now);
            assert!(shows(true, ended, false, now + ENDED_FOR - Duration::from_millis(1)), "{reason}");
            assert!(!shows(true, ended, false, now + ENDED_FOR), "{reason}");
        }
        // Answered in writing: the card says so.
        assert!(shows(true, showing("idle", Some("done"), true, now), false, now));
        // Ended by the person, or the Reeve said its piece: nothing to say.
        for reason in ["bye", "cancelled", "done"] {
            assert_eq!(showing("idle", Some(reason), false, now), Showing::Rest, "{reason}");
        }
    }

    const LOST_EXAMPLE: &str = "Lost the connection to the main";

    #[test]
    fn open_settings_goes_to_the_mains_setting() {
        assert_eq!(settings_url("https://erebus.local:3080", "reeve.voice.enabled").unwrap().as_str(), "https://erebus.local:3080/settings#setting-reeve.voice.enabled");
        assert_eq!(settings_url("http://10.0.0.2:3080/", "").unwrap().as_str(), "http://10.0.0.2:3080/settings");
    }

    #[test]
    fn the_page_cannot_make_the_window_huge_or_nothing() {
        let big = limits((1920.0, 1040.0));
        assert_eq!(big, MAX_SIZE);
        assert_eq!(clamp_size(412.0, 220.0, big), (412.0, 220.0));
        assert_eq!(clamp_size(0.0, 9999.0, big), (40.0, 640.0));
    }

    #[test]
    fn on_a_small_screen_the_widget_is_kept_inside_its_margins() {
        // 1366x768 at 1.25: 1093x614 logical.
        assert_eq!(limits((1093.0, 614.0)), (480.0, 614.0 - 32.0));
        let (w, h) = clamp_size(412.0, 9999.0, limits((1093.0, 614.0)));
        assert_eq!((w, h), (412.0, 582.0));
        // At its anchor it then fits whole, MARGIN in from both edges.
        assert_eq!(origin(Anchor::BottomRight, margins(Anchor::BottomRight, None, (1093.0, 614.0), (w, h)), (1093.0, 614.0), (w, h)), (1093.0 - 16.0 - 412.0, 16.0));
        assert_eq!(limits((10.0, 10.0)), MIN_SIZE);
    }
}
