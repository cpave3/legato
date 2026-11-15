//! The app-wide appearance choice, shared by the local picker and keyed
//! page bridge. Remote pages gain no Tauri command capability.
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "kebab-case")]
pub enum AppTheme {
    #[default]
    Legato,
    Dracula,
    OcularLight,
    OcularDark,
    OcularLightVivid,
    OcularDarkVivid,
}

// Tolerant only at the persisted-state boundary; bridge/IPC IDs stay strict.
pub fn saved_theme<'de, D: serde::Deserializer<'de>>(d: D) -> Result<AppTheme, D::Error> {
    let value = serde_json::Value::deserialize(d)?;
    Ok(serde_json::from_value(value).unwrap_or_default())
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(try_from = "String", into = "String")]
pub enum Choice { Local(AppTheme), Server }
impl From<AppTheme> for Choice { fn from(theme: AppTheme) -> Self { Self::Local(theme) } }
impl TryFrom<String> for Choice {
    type Error = String;
    fn try_from(id: String) -> Result<Self, String> {
        if id == "server" { Ok(Self::Server) } else {
            serde_json::from_value(serde_json::json!(id)).map(Self::Local).map_err(|e| e.to_string())
        }
    }
}
impl From<Choice> for String {
    fn from(choice: Choice) -> Self {
        match choice { Choice::Server => "server".into(), Choice::Local(theme) => serde_json::to_value(theme).unwrap().as_str().unwrap().into() }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Preference { pub id: String, pub revision: u64 }

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
pub struct Appearance { pub id: Choice, pub effective: AppTheme }

pub fn choice(state: &crate::store::State) -> Choice {
    if state.follow_server_theme { Choice::Server } else { state.app_theme.into() }
}
pub fn effective(state: &crate::store::State, server: Option<&str>) -> AppTheme {
    if state.follow_server_theme {
        if let Some(preference) = server.and_then(|id| state.servers.iter().find(|s| s.id == id)).and_then(|s| s.theme_preference.as_ref()) {
            if let Ok(theme) = serde_json::from_value(serde_json::json!(preference.id)) { return theme; }
        }
    }
    state.app_theme
}
pub fn appearance(state: &crate::store::State, active: Option<&str>) -> Appearance {
    Appearance { id: choice(state), effective: effective(state, active) }
}
pub fn page_script(origin: &str, state: &crate::store::State, server: Option<&str>) -> String {
    let origin = serde_json::to_string(origin).expect("origin encodes");
    let local = serde_json::to_string(&state.app_theme).unwrap();
    let theme = serde_json::to_string(&effective(state, server)).unwrap();
    let mode = if state.follow_server_theme { "server" } else { "local" };
    format!(r#"(() => {{
  if (window !== window.top || location.origin !== {origin}) return;
  localStorage.setItem("legato:theme-mode", "{mode}");
  localStorage.setItem("legato:app-theme", {local});
  localStorage.setItem("legato:effective-app-theme", {theme});
  window.dispatchEvent(new CustomEvent("legato:theme-changed", {{ detail: {theme} }}));
}})();"#)
}

pub fn result_script(origin: &str, theme: impl Into<Choice>, error: Option<&str>) -> String {
    let origin = serde_json::to_string(origin).expect("origin encodes");
    let mut detail = serde_json::json!({ "id": theme.into() });
    if let Some(error) = error { detail["error"] = error.into(); }
    format!(r#"(() => {{
  if (window !== window.top || location.origin !== {origin}) return;
  window.dispatchEvent(new CustomEvent("legato:shell-theme", {{ detail: {detail} }}));
}})();"#)
}

/// A retained page cannot change application preferences after switching away.
pub fn select_page(
    store: &mut crate::store::Store, selection: Choice, source: &str, active: Option<&str>,
    notify: impl FnMut(Appearance) -> Result<(), String>,
    eval: impl FnMut(&str, String) -> Result<(), String>,
) -> Result<(), String> {
    if active != Some(source) { return Err("Open this server before changing the app theme.".into()); }
    select(store, selection, active, notify, eval)
}

/// Both the local picker and keyed page bridge use this save-before-delivery path.
pub fn select(
    store: &mut crate::store::Store, selection: Choice, active: Option<&str>,
    notify: impl FnMut(Appearance) -> Result<(), String>,
    eval: impl FnMut(&str, String) -> Result<(), String>,
) -> Result<(), String> {
    let previous = (store.state.app_theme, store.state.follow_server_theme);
    match selection {
        Choice::Server => store.state.follow_server_theme = true,
        Choice::Local(theme) => { store.state.app_theme = theme; store.state.follow_server_theme = false; }
    }
    if let Err(error) = store.save() {
        (store.state.app_theme, store.state.follow_server_theme) = previous;
        return Err(error);
    }
    publish(&store.state, active, notify, eval)
}

/// Resolve when the response arrives, not when its network request began.
/// Hold the active lock through publication so a switch cannot be overtaken.
pub fn refresh_current(
    store: &std::sync::Mutex<crate::store::Store>, active: &std::sync::Mutex<Option<String>>,
    server: &str, meta: &serde_json::Value,
    notify: impl FnMut(Appearance) -> Result<(), String>,
    eval: impl FnMut(&str, String) -> Result<(), String>,
) -> Result<(), String> {
    let active = active.lock().unwrap();
    refresh(&mut store.lock().unwrap(), server, meta, active.as_deref(), notify, eval)
}

/// Called only with metadata read by the pinned native client. Network failure
/// does not call this path, retaining only this saved connection's own cache.
pub fn refresh(
    store: &mut crate::store::Store, server: &str, meta: &serde_json::Value, active: Option<&str>,
    notify: impl FnMut(Appearance) -> Result<(), String>,
    eval: impl FnMut(&str, String) -> Result<(), String>,
) -> Result<(), String> {
    let preference = match meta.get("theme_preference") {
        None => None,
        Some(value) => Some(serde_json::from_value::<Preference>(value.clone()).map_err(|e| format!("Invalid server theme preference: {e}"))?),
    };
    let Some(saved) = store.server_mut(server) else { return Ok(()) };
    if saved.theme_preference == preference { return Ok(()) }
    // Ignore out-of-order replies, but allow an older server to clear its cache.
    if let (Some(old), Some(new)) = (&saved.theme_preference, &preference) {
        if new.revision < old.revision { return Ok(()) }
    }
    let previous = std::mem::replace(&mut saved.theme_preference, preference);
    if let Err(error) = store.save() {
        store.server_mut(server).unwrap().theme_preference = previous;
        return Err(error);
    }
    publish(&store.state, active, notify, eval)
}

pub fn switch(state: &crate::store::State, active: &mut Option<String>, server: Option<String>, notify: impl FnMut(Appearance) -> Result<(), String>, eval: impl FnMut(&str, String) -> Result<(), String>) -> Result<(), String> {
    *active = server;
    publish(state, active.as_deref(), notify, eval)
}

pub fn publish(state: &crate::store::State, active: Option<&str>, mut notify: impl FnMut(Appearance) -> Result<(), String>, eval: impl FnMut(&str, String) -> Result<(), String>) -> Result<(), String> {
    let notified = notify(appearance(state, active));
    let delivered = deliver(state, eval);
    match (notified, delivered) {
        (Ok(()), result) | (result, Ok(())) => result,
        (Err(a), Err(b)) => Err(format!("{a}; {b}")),
    }
}

pub fn deliver(state: &crate::store::State, mut eval: impl FnMut(&str, String) -> Result<(), String>) -> Result<(), String> {
    let mut errors = Vec::new();
    for server in &state.servers {
        let result = reqwest::Url::parse(&server.url).map_err(|e| e.to_string()).and_then(|url| {
            eval(&server.id, page_script(&url.origin().ascii_serialization(), state, Some(&server.id)))
        });
        if let Err(error) = result { errors.push(format!("{}: {error}", server.name)); }
    }
    if errors.is_empty() { Ok(()) } else { Err(errors.join("; ")) }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn only_the_active_pages_keyed_picker_can_change_native_appearance() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("state.json");
        let mut store = crate::store::Store::open(path.clone(), Box::new(crate::store::tests::Memory::default())).unwrap();
        for active in [None, Some("other")] {
            assert!(select_page(&mut store, Choice::Server, "requester", active,
                |_| panic!("inactive page notified shell"), |_, _| panic!("inactive page delivered theme")).is_err());
            assert!(!store.state.follow_server_theme);
        }
        select_page(&mut store, Choice::Server, "requester", Some("requester"), |_| Ok(()), |_, _| Ok(())).unwrap();
        let reopened = crate::store::Store::open(path, Box::new(crate::store::tests::Memory::default())).unwrap();
        assert!(reopened.state.follow_server_theme);
        assert_eq!(reopened.state.app_theme, AppTheme::Legato);
    }

    #[test]
    fn every_theme_crosses_the_page_bridge_and_survives_restart() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("state.json");
        let mut store = crate::store::Store::open(path.clone(), Box::new(crate::store::tests::Memory::default())).unwrap();
        store.add_server("one", "https://one.local", None, "token").unwrap();
        let source: serde_json::Value = serde_json::from_str(include_str!("../../../themes/palettes.json")).unwrap();
        let mut ids = vec!["legato".to_string()];
        ids.extend(source["themes"].as_array().unwrap().iter().map(|entry| entry["id"].as_str().unwrap().to_string()));
        ids.push("server".into());
        for id in ids {
            let script = crate::bridge::script("secret", false, "https://one.local");
            let program = format!(r#"
const location = {{ origin: "https://one.local", href: "" }};
const window = {{}}; window.top = window;
{script}
window.ReactNativeWebView.postMessage(JSON.stringify({{type: "theme", key: "secret", id: {id:?}}}));
console.log(location.href);"#);
            let out = std::process::Command::new("node").arg("-e").arg(program).output().expect("Node runs theme request");
            assert!(out.status.success(), "{}", String::from_utf8_lossy(&out.stderr));
            let url = String::from_utf8(out.stdout).unwrap();
            let Some(crate::bridge::PageMessage::Theme(theme)) = crate::bridge::read(url.trim(), "secret") else {
                panic!("{id} did not cross the keyed page bridge: {url}");
            };
            assert_eq!(serde_json::to_value(theme).unwrap(), id);
            assert!(crate::bridge::read(url.trim(), "wrong").is_none());
            let local = store.state.app_theme;
            let mut notified = Vec::new();
            let mut delivered = Vec::new();
            select(&mut store, theme, None, |theme| { notified.push(theme); Ok(()) }, |server, script| {
                delivered.push((server.to_string(), script)); Ok(())
            }).unwrap();
            assert_eq!(notified, [Appearance { id: theme, effective: match theme { Choice::Local(value) => value, Choice::Server => local } }]);
            assert_eq!(delivered, [(store.state.servers[0].id.clone(), page_script("https://one.local", &store.state, None))]);
            let reopened = crate::store::Store::open(path.clone(), Box::new(crate::store::tests::Memory::default())).unwrap();
            assert_eq!(choice(&reopened.state), theme);
            if theme == Choice::Server { assert_eq!(reopened.state.app_theme, local); }
        }
    }

    #[test]
    fn a_page_selection_is_saved_broadcast_and_reported_even_when_delivery_fails() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("state.json");
        let mut store = crate::store::Store::open(path.clone(), Box::new(crate::store::tests::Memory::default())).unwrap();
        store.add_server("one", "https://one.local", None, "token").unwrap();
        store.add_server("two", "https://two.local", None, "token").unwrap();
        let mut notified = Vec::new();
        let mut delivered = Vec::new();
        let result = select(&mut store, AppTheme::Dracula.into(), None, |theme| { notified.push(theme); Ok(()) }, |id, script| {
            delivered.push((id.to_string(), script));
            if delivered.len() == 1 { Err("page unavailable".into()) } else { Ok(()) }
        });
        assert!(result.unwrap_err().contains("page unavailable"));
        assert_eq!(notified, [Appearance { id: AppTheme::Dracula.into(), effective: AppTheme::Dracula }]);
        assert_eq!(delivered.len(), 2);
        let reopened = crate::store::Store::open(path, Box::new(crate::store::tests::Memory::default())).unwrap();
        assert_eq!(reopened.state.app_theme, AppTheme::Dracula);
        assert_eq!(reopened.state.servers.len(), 2);
    }

    #[test]
    fn a_failed_save_does_not_change_or_broadcast_the_theme() {
        let dir = tempfile::tempdir().unwrap();
        let valid = dir.path().join("state.json");
        let mut store = crate::store::Store::open(valid.clone(), Box::new(crate::store::tests::Memory::default())).unwrap();
        std::fs::create_dir(valid.with_extension("json.tmp")).unwrap();
        assert!(select(&mut store, AppTheme::OcularDark.into(), None, |_| panic!("must not notify"), |_, _| panic!("must not deliver")).is_err());
        assert_eq!(store.state.app_theme, AppTheme::Legato);
        assert!(select(&mut store, Choice::Server, None, |_| panic!("must not notify"), |_, _| panic!("must not deliver")).is_err());
        assert_eq!(choice(&store.state), Choice::Local(AppTheme::Legato));
    }

    #[test]
    fn every_retained_page_is_updated_even_if_another_page_fails() {
        let servers = vec![
            crate::store::Server { id: "one".into(), url: "https://one.local/path".into(), ..Default::default() },
            crate::store::Server { id: "two".into(), url: "https://two.local".into(), ..Default::default() },
        ];
        let mut delivered = Vec::new();
        let result = deliver(&crate::store::State { app_theme: AppTheme::Dracula, servers, ..Default::default() }, |id, script| {
            delivered.push((id.to_string(), script));
            if id == "one" { Err("page unavailable".into()) } else { Ok(()) }
        });
        assert!(result.unwrap_err().contains("page unavailable"));
        assert_eq!(delivered.iter().map(|(id, _)| id.as_str()).collect::<Vec<_>>(), ["one", "two"]);
        assert!(delivered[1].1.contains("https://two.local"));
    }

    #[test]
    fn the_requester_receives_success_or_error_only_on_its_own_top_frame() {
        for selection in [Choice::Local(AppTheme::Dracula), Choice::Server] {
            for error in [None, Some("Could not write \"state.json\" </script>")] {
                for (origin, top, allowed) in [("https://one.local", true, true), ("https://other.local", true, false), ("https://one.local", false, false)] {
                    let script = result_script("https://one.local", selection, error);
                    let program = format!(r#"
const calls = [];
const location = {{ origin: {origin:?} }};
const window = {{ dispatchEvent(e) {{ calls.push([e.type, e.detail]); }} }};
window.top = {top} ? window : {{}};
class CustomEvent {{ constructor(type, options) {{ this.type = type; this.detail = options.detail; }} }}
{script}
console.log(JSON.stringify(calls));"#);
                    let out = std::process::Command::new("node").arg("-e").arg(program).output().expect("Node runs theme response");
                    assert!(out.status.success(), "{}", String::from_utf8_lossy(&out.stderr));
                    let calls: serde_json::Value = serde_json::from_slice(&out.stdout).unwrap();
                    let mut detail = serde_json::json!({ "id": selection });
                    if let Some(error) = error { detail["error"] = error.into(); }
                    assert_eq!(calls, if allowed { serde_json::json!([["legato:shell-theme", detail]]) } else { serde_json::json!([]) });
                }
            }
        }
    }

    #[test]
    fn pages_receive_storage_and_public_event_only_on_their_own_top_frame() {
        for id in ["legato", "dracula", "ocular-light", "ocular-dark", "ocular-light-vivid", "ocular-dark-vivid"] {
            let theme = serde_json::from_value(serde_json::json!(id)).unwrap();
            for (origin, top, expected) in [("https://one.local", true, true), ("https://other.local", true, false), ("https://one.local", false, false)] {
                let script = page_script("https://one.local", &crate::store::State { app_theme: theme, ..Default::default() }, None);
                let program = format!(r#"
    const calls = [];
    global.location = {{ origin: {origin:?} }};
    global.window = {{ dispatchEvent(e) {{ calls.push([e.type, e.detail]); }} }};
    window.top = {top} ? window : {{}};
    global.localStorage = {{ setItem(k, v) {{ calls.push([k, v]); }} }};
    global.CustomEvent = class {{ constructor(type, options) {{ this.type = type; this.detail = options.detail; }} }};
    {script}
    console.log(JSON.stringify(calls));"#);
                let out = std::process::Command::new("node").arg("-e").arg(program).output().expect("Node runs theme delivery");
                assert!(out.status.success(), "{}", String::from_utf8_lossy(&out.stderr));
                let calls: serde_json::Value = serde_json::from_slice(&out.stdout).unwrap();
                assert_eq!(calls, if expected { serde_json::json!([["legato:theme-mode", "local"], ["legato:app-theme", id], ["legato:effective-app-theme", id], ["legato:theme-changed", id]]) } else { serde_json::json!([]) });
            }
        }
    }
}

#[cfg(test)]
mod follow_tests {
    use super::*;
    #[test]
    fn switching_uses_only_own_cache_and_late_inactive_metadata_cannot_recolor_shell() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = crate::store::Store::open(dir.path().join("state.json"), Box::new(crate::store::tests::Memory::default())).unwrap();
        let a = store.add_server("a", "https://a.local", None, "token").unwrap();
        let b = store.add_server("b", "https://b.local", None, "token").unwrap();
        select(&mut store, Choice::Server, None, |_| Ok(()), |_, _| Ok(())).unwrap();
        let mut active = None;
        switch(&store.state, &mut active, Some(a.id.clone()), |_| Ok(()), |_, _| Ok(())).unwrap();
        refresh(&mut store, &a.id, &serde_json::json!({"theme_preference":{"id":"ocular-light","revision":1}}), active.as_deref(), |_| Ok(()), |_, _| Ok(())).unwrap();
        let mut events = Vec::new();
        switch(&store.state, &mut active, Some(b.id.clone()), |value| { events.push(value.effective); Ok(()) }, |_, _| Ok(())).unwrap();
        let mut scripts = Vec::new();
        refresh(&mut store, &a.id, &serde_json::json!({"theme_preference":{"id":"dracula","revision":2}}), active.as_deref(), |value| { events.push(value.effective); Ok(()) }, |id, script| { scripts.push((id.to_string(), script)); Ok(()) }).unwrap();
        assert_eq!(events, [AppTheme::Legato, AppTheme::Legato]);
        for (id, script) in scripts {
            let origin = if id == a.id { "https://a.local" } else { "https://b.local" };
            let expected = if id == a.id { "dracula" } else { "legato" };
            assert_page(&script, origin, true, serde_json::json!([
                ["legato:theme-mode", "server"], ["legato:app-theme", "legato"],
                ["legato:effective-app-theme", expected], ["legato:theme-changed", expected]
            ]));
            assert_page(&script, "https://other.local", true, serde_json::json!([]));
            assert_page(&script, origin, false, serde_json::json!([]));
        }
        switch(&store.state, &mut active, Some(a.id.clone()), |value| { events.push(value.effective); Ok(()) }, |_, _| Ok(())).unwrap();
        assert_eq!(events.last(), Some(&AppTheme::Dracula));
        switch(&store.state, &mut active, None, |value| { events.push(value.effective); Ok(()) }, |_, _| Ok(())).unwrap();
        assert_eq!(events.last(), Some(&AppTheme::Legato));
    }

    fn assert_page(script: &str, origin: &str, top: bool, expected: serde_json::Value) {
        let program = format!(r#"
const calls = [];
const location = {{ origin: {origin:?} }};
const window = {{ dispatchEvent(e) {{ calls.push([e.type, e.detail]); }} }};
window.top = {top} ? window : {{}};
const localStorage = {{setItem(k,v) {{calls.push([k,v]);}}}};
class CustomEvent {{ constructor(type, options) {{this.type=type; this.detail=options.detail;}} }}
{script}
console.log(JSON.stringify(calls));"#);
        let out = std::process::Command::new("node").arg("-e").arg(program).output().unwrap();
        assert!(out.status.success(), "{}", String::from_utf8_lossy(&out.stderr));
        assert_eq!(serde_json::from_slice::<serde_json::Value>(&out.stdout).unwrap(), expected);
    }

    #[test]
    fn invalid_or_unsaved_refresh_keeps_the_previous_snapshot_and_stale_replies_are_ignored() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("state.json");
        let mut store = crate::store::Store::open(path.clone(), Box::new(crate::store::tests::Memory::default())).unwrap();
        let server = store.add_server("a", "https://a.local", None, "token").unwrap();
        select(&mut store, Choice::Server, Some(&server.id), |_| Ok(()), |_, _| Ok(())).unwrap();
        refresh(&mut store, &server.id, &serde_json::json!({"theme_preference":{"id":"dracula","revision":2}}), Some(&server.id), |_| Ok(()), |_, _| Ok(())).unwrap();
        for meta in [serde_json::json!({"theme_preference":{"id":7,"revision":3}}), serde_json::json!({"theme_preference":{"id":"legato","revision":-1}})] {
            assert!(refresh(&mut store, &server.id, &meta, Some(&server.id), |_| panic!("must not notify"), |_, _| panic!("must not deliver")).unwrap_err().contains("Invalid server theme preference"));
        }
        refresh(&mut store, &server.id, &serde_json::json!({"theme_preference":{"id":"legato","revision":1}}), Some(&server.id), |_| panic!("stale reply"), |_, _| panic!("stale reply")).unwrap();
        std::fs::create_dir(path.with_extension("json.tmp")).unwrap();
        assert!(refresh(&mut store, &server.id, &serde_json::json!({"theme_preference":{"id":"legato","revision":3}}), Some(&server.id), |_| panic!("must not notify"), |_, _| panic!("must not deliver")).is_err());
        assert_eq!(effective(&store.state, Some(&server.id)), AppTheme::Dracula);
        assert_eq!(store.server(&server.id).unwrap().theme_preference.as_ref().unwrap().revision, 2);
    }
}
