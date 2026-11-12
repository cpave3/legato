//! The screens the widget can be put on: what each is called, how the
//! person's choice is kept, and which screen it is found on later.
//!
//! The app reads the screens elsewhere (GDK's monitors on Linux, with each
//! output's connector read over Wayland; see `app.rs`, `read_screens`);
//! everything decided about them is here, alone.

pub use crate::store::ScreenId;
use crate::voicewidget::Rect;

/// A screen as the app found it.
#[derive(Debug, Clone, PartialEq)]
pub struct Screen {
    /// GDK's index of the monitor, for gtk-layer-shell.
    pub index: usize,
    /// The maker and model the monitor says (its EDID), as GDK gives them;
    /// empty when it says none.
    pub maker: String,
    pub model: String,
    /// The connector (`DP-1`, `HDMI-A-1`), when Wayland says it.
    pub connector: Option<String>,
    /// The work area, physical pixels, and the scale.
    pub area: Rect,
    pub scale: f64,
    pub primary: bool,
}

impl Screen {
    /// What the choice is kept as.
    pub fn id(&self) -> ScreenId {
        ScreenId { maker: self.maker.clone(), model: self.model.clone(), connector: self.connector.clone() }
    }

    /// The maker and model as a person reads them: the model alone when it
    /// already starts with the maker's name (`Dell Inc.` and `DELL U2720Q`
    /// is `DELL U2720Q`).
    fn friendly(&self) -> String {
        let (maker, model) = (self.maker.trim(), self.model.trim());
        let first = maker.split_whitespace().next().unwrap_or("").to_lowercase();
        if maker.is_empty() || (!first.is_empty() && model.to_lowercase().starts_with(&first)) {
            model.to_string()
        } else if model.is_empty() {
            maker.to_string()
        } else {
            format!("{maker} {model}")
        }
    }
}

/// Each screen's name, in the order given: its maker and model; its
/// connector beside it when another screen has the same name, or alone when
/// it says neither; else `Screen 2`.
pub fn names(screens: &[Screen]) -> Vec<String> {
    let base: Vec<String> = screens
        .iter()
        .enumerate()
        .map(|(i, s)| {
            let f = s.friendly();
            if !f.is_empty() {
                f
            } else {
                s.connector.clone().unwrap_or_else(|| format!("Screen {}", i + 1))
            }
        })
        .collect();
    base.iter()
        .enumerate()
        .map(|(i, name)| {
            let same: Vec<usize> = (0..base.len()).filter(|&j| base[j] == *name).collect();
            if same.len() < 2 {
                return name.clone();
            }
            match &screens[i].connector {
                Some(c) if c != name => format!("{name} ({c})"),
                _ => format!("{name} ({})", same.iter().position(|&j| j == i).unwrap() + 1),
            }
        })
        .collect()
}

/// Where the screen the person chose is now, if it is here: the same maker,
/// model and connector; else the only one of that maker and model (moved to
/// another port); else, for a screen that says no maker or model, the same
/// connector.
pub fn find(chosen: &ScreenId, screens: &[Screen]) -> Option<usize> {
    if let Some(i) = screens.iter().position(|s| s.id() == *chosen) {
        return Some(i);
    }
    let named = !(chosen.maker.is_empty() && chosen.model.is_empty());
    if named {
        let same: Vec<usize> = (0..screens.len()).filter(|&i| screens[i].maker == chosen.maker && screens[i].model == chosen.model).collect();
        return (same.len() == 1).then(|| same[0]);
    }
    chosen.connector.as_ref().and_then(|c| screens.iter().position(|s| s.connector.as_ref() == Some(c)))
}

/// The screen the widget goes on: the one chosen, while it is here; else
/// the primary, else the first. None when none was chosen (the desktop's
/// choice, or the screen the widget is on) or there are no screens.
pub fn screen_for(chosen: Option<&ScreenId>, screens: &[Screen]) -> Option<usize> {
    let chosen = chosen?;
    if screens.is_empty() {
        return None;
    }
    find(chosen, screens).or_else(|| screens.iter().position(|s| s.primary)).or(Some(0))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn screen(index: usize, maker: &str, model: &str, connector: Option<&str>) -> Screen {
        Screen {
            index,
            maker: maker.into(),
            model: model.into(),
            connector: connector.map(String::from),
            area: Rect { x: index as f64 * 1920.0, y: 0.0, width: 1920.0, height: 1080.0 },
            scale: 1.0,
            primary: false,
        }
    }

    #[test]
    fn a_screen_is_called_by_its_maker_and_model() {
        let screens = [
            screen(0, "Dell Inc.", "DELL U2720Q", Some("DP-1")),
            screen(1, "Samsung Electric Company", "LS27A70", Some("HDMI-A-1")),
            screen(2, "", "Built-in display", Some("eDP-1")),
        ];
        assert_eq!(names(&screens), ["DELL U2720Q", "Samsung Electric Company LS27A70", "Built-in display"]);
    }

    #[test]
    fn two_of_the_same_model_are_told_apart_by_connector() {
        let screens = [
            screen(0, "Dell Inc.", "DELL U2720Q", Some("DP-1")),
            screen(1, "Dell Inc.", "DELL U2720Q", Some("DP-2")),
            screen(2, "LG", "27GL850", Some("HDMI-A-1")),
        ];
        assert_eq!(names(&screens), ["DELL U2720Q (DP-1)", "DELL U2720Q (DP-2)", "LG 27GL850"]);
        // With no connector, by their order.
        let screens = [screen(0, "LG", "27GL850", None), screen(1, "LG", "27GL850", None)];
        assert_eq!(names(&screens), ["LG 27GL850 (1)", "LG 27GL850 (2)"]);
    }

    #[test]
    fn a_screen_that_says_no_maker_or_model_is_called_by_connector_or_number() {
        let screens = [screen(0, "", "", Some("Virtual-0")), screen(1, "", "", None)];
        assert_eq!(names(&screens), ["Virtual-0", "Screen 2"]);
    }

    #[test]
    fn the_chosen_screen_is_found_by_maker_model_and_connector() {
        let screens = [
            screen(0, "Dell Inc.", "DELL U2720Q", Some("DP-1")),
            screen(1, "Dell Inc.", "DELL U2720Q", Some("DP-2")),
            screen(2, "LG", "27GL850", Some("HDMI-A-1")),
        ];
        assert_eq!(find(&screens[1].id(), &screens), Some(1));
        // The only LG, moved to another port.
        let moved = ScreenId { maker: "LG".into(), model: "27GL850".into(), connector: Some("DP-3".into()) };
        assert_eq!(find(&moved, &screens), Some(2));
        // One of two Dells on a port neither is on: not guessed.
        let dell = ScreenId { maker: "Dell Inc.".into(), model: "DELL U2720Q".into(), connector: Some("DP-3".into()) };
        assert_eq!(find(&dell, &screens), None);
        // A screen with no maker or model, by connector.
        let bare = [screen(0, "", "", Some("Virtual-0")), screen(1, "", "", Some("Virtual-1"))];
        let id = ScreenId { maker: String::new(), model: String::new(), connector: Some("Virtual-1".into()) };
        assert_eq!(find(&id, &bare), Some(1));
    }

    #[test]
    fn a_missing_screen_falls_back_to_the_primary_then_the_first_and_returns_when_it_does() {
        let lg = screen(2, "LG", "27GL850", Some("HDMI-A-1"));
        let mut primary = screen(1, "Dell Inc.", "DELL U2720Q", Some("DP-2"));
        primary.primary = true;
        let without = [screen(0, "Dell Inc.", "DELL U2720Q", Some("DP-1")), primary.clone()];
        assert_eq!(screen_for(Some(&lg.id()), &without), Some(1));
        let no_primary = [screen(0, "Dell Inc.", "DELL U2720Q", Some("DP-1"))];
        assert_eq!(screen_for(Some(&lg.id()), &no_primary), Some(0));
        let with = [screen(0, "Dell Inc.", "DELL U2720Q", Some("DP-1")), primary, lg.clone()];
        assert_eq!(screen_for(Some(&lg.id()), &with), Some(2));
    }

    #[test]
    fn with_no_choice_the_widget_is_where_the_desktop_puts_it() {
        let screens = [screen(0, "LG", "27GL850", None)];
        assert_eq!(screen_for(None, &screens), None);
        assert_eq!(screen_for(Some(&screens[0].id()), &[]), None);
    }
}
