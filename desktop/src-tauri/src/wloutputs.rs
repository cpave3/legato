//! The compositor's outputs as Wayland says them: connector (`DP-1`, from
//! `wl_output` version 4's `name`), maker, model and position. GTK 3 gives
//! maker and model but not the connector, so the app reads them over a
//! connection of its own and matches them to GDK's monitors by position
//! (`app.rs`, `read_screens`). Linux, Wayland only.

use wayland_client::protocol::{wl_output, wl_registry};
use wayland_client::{Connection, Dispatch, QueueHandle};

/// One output.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct Output {
    pub connector: Option<String>,
    pub maker: String,
    pub model: String,
    /// Its place in the compositor's space (logical on KWin and wlroots).
    pub x: i32,
    pub y: i32,
}

#[derive(Default)]
struct State {
    outputs: Vec<Output>,
}

impl Dispatch<wl_registry::WlRegistry, ()> for State {
    fn event(state: &mut State, registry: &wl_registry::WlRegistry, event: wl_registry::Event, _: &(), _: &Connection, qh: &QueueHandle<State>) {
        if let wl_registry::Event::Global { name, interface, version } = event {
            if interface == "wl_output" {
                let index = state.outputs.len();
                state.outputs.push(Output::default());
                registry.bind::<wl_output::WlOutput, _, _>(name, version.min(4), qh, index);
            }
        }
    }
}

impl Dispatch<wl_output::WlOutput, usize> for State {
    fn event(state: &mut State, _: &wl_output::WlOutput, event: wl_output::Event, index: &usize, _: &Connection, _: &QueueHandle<State>) {
        let Some(out) = state.outputs.get_mut(*index) else { return };
        match event {
            wl_output::Event::Geometry { x, y, make, model, .. } => {
                (out.x, out.y, out.maker, out.model) = (x, y, make, model);
            }
            wl_output::Event::Name { name } => out.connector = Some(name),
            _ => {}
        }
    }
}

/// The outputs, in the order the compositor announced them (GDK's order).
pub fn read() -> Result<Vec<Output>, String> {
    let conn = Connection::connect_to_env().map_err(|e| e.to_string())?;
    let mut queue = conn.new_event_queue();
    let qh = queue.handle();
    conn.display().get_registry(&qh, ());
    let mut state = State::default();
    // The globals, then each output's events.
    queue.roundtrip(&mut state).map_err(|e| e.to_string())?;
    queue.roundtrip(&mut state).map_err(|e| e.to_string())?;
    Ok(state.outputs)
}

/// Which output each of GDK's monitors is, given each monitor's position:
/// the output at the same place, else the one at the same index when there
/// are as many outputs as monitors.
pub fn match_monitors(monitors: &[(i32, i32)], outputs: &[Output]) -> Vec<Option<Output>> {
    monitors
        .iter()
        .enumerate()
        .map(|(i, &(x, y))| {
            let at: Vec<&Output> = outputs.iter().filter(|o| (o.x, o.y) == (x, y)).collect();
            match at.as_slice() {
                [one] => Some((*one).clone()),
                _ if outputs.len() == monitors.len() => outputs.get(i).cloned(),
                _ => None,
            }
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn out(connector: &str, x: i32, y: i32) -> Output {
        Output { connector: Some(connector.into()), maker: String::new(), model: String::new(), x, y }
    }

    #[test]
    fn a_monitor_is_the_output_at_its_place() {
        let outputs = [out("DP-2", 1920, 0), out("DP-1", 0, 0), out("HDMI-A-1", 3840, 0)];
        let found = match_monitors(&[(0, 0), (1920, 0), (3840, 0)], &outputs);
        let names: Vec<_> = found.iter().map(|o| o.as_ref().and_then(|o| o.connector.as_deref())).collect();
        assert_eq!(names, [Some("DP-1"), Some("DP-2"), Some("HDMI-A-1")]);
    }

    #[test]
    fn with_no_output_at_its_place_a_monitor_is_the_one_at_its_index_or_none() {
        // Positions in another space (scaled): by order, when the counts agree.
        let outputs = [out("DP-1", 0, 0), out("DP-2", 3840, 0)];
        let found = match_monitors(&[(0, 0), (1920, 0)], &outputs);
        assert_eq!(found[1].as_ref().and_then(|o| o.connector.as_deref()), Some("DP-2"));
        let found = match_monitors(&[(0, 0), (1920, 0), (5000, 0)], &outputs);
        assert_eq!(found[1], None);
    }
}
