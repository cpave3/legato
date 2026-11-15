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
}

// Tolerant only at the persisted-state boundary; bridge/IPC IDs stay strict.
pub fn saved_theme<'de, D: serde::Deserializer<'de>>(d: D) -> Result<AppTheme, D::Error> {
    let value = serde_json::Value::deserialize(d)?;
    Ok(serde_json::from_value(value).unwrap_or_default())
}

pub fn page_script(origin: &str, theme: AppTheme) -> String {
    let origin = serde_json::to_string(origin).expect("origin encodes");
    let theme = serde_json::to_string(&theme).expect("theme encodes");
    format!(r#"(() => {{
  if (window !== window.top || location.origin !== {origin}) return;
  localStorage.setItem("legato:app-theme", {theme});
  window.dispatchEvent(new CustomEvent("legato:theme-changed", {{ detail: {theme} }}));
}})();"#)
}

pub fn result_script(origin: &str, theme: AppTheme, error: Option<&str>) -> String {
    let origin = serde_json::to_string(origin).expect("origin encodes");
    let mut detail = serde_json::json!({ "id": theme });
    if let Some(error) = error { detail["error"] = error.into(); }
    format!(r#"(() => {{
  if (window !== window.top || location.origin !== {origin}) return;
  window.dispatchEvent(new CustomEvent("legato:shell-theme", {{ detail: {detail} }}));
}})();"#)
}

/// Both the local picker and keyed page bridge use this save-before-delivery path.
pub fn select(
    store: &mut crate::store::Store,
    theme: AppTheme,
    mut notify: impl FnMut(AppTheme) -> Result<(), String>,
    eval: impl FnMut(&str, String) -> Result<(), String>,
) -> Result<(), String> {
    let previous = store.state.app_theme;
    store.state.app_theme = theme;
    if let Err(error) = store.save() {
        store.state.app_theme = previous;
        return Err(error);
    }
    let notified = notify(theme);
    let delivered = deliver(theme, &store.state.servers, eval);
    match (notified, delivered) {
        (Ok(()), result) | (result, Ok(())) => result,
        (Err(a), Err(b)) => Err(format!("{a}; {b}")),
    }
}

pub fn deliver(theme: AppTheme, servers: &[crate::store::Server], mut eval: impl FnMut(&str, String) -> Result<(), String>) -> Result<(), String> {
    let mut errors = Vec::new();
    for server in servers {
        let result = reqwest::Url::parse(&server.url).map_err(|e| e.to_string()).and_then(|url| {
            eval(&server.id, page_script(&url.origin().ascii_serialization(), theme))
        });
        if let Err(error) = result { errors.push(format!("{}: {error}", server.name)); }
    }
    if errors.is_empty() { Ok(()) } else { Err(errors.join("; ")) }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_page_selection_is_saved_broadcast_and_reported_even_when_delivery_fails() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("state.json");
        let mut store = crate::store::Store::open(path.clone(), Box::new(crate::store::tests::Memory::default())).unwrap();
        store.add_server("one", "https://one.local", None, "token").unwrap();
        store.add_server("two", "https://two.local", None, "token").unwrap();
        let mut notified = Vec::new();
        let mut delivered = Vec::new();
        let result = select(&mut store, AppTheme::Dracula, |theme| { notified.push(theme); Ok(()) }, |id, script| {
            delivered.push((id.to_string(), script));
            if delivered.len() == 1 { Err("page unavailable".into()) } else { Ok(()) }
        });
        assert!(result.unwrap_err().contains("page unavailable"));
        assert_eq!(notified, [AppTheme::Dracula]);
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
        assert!(select(&mut store, AppTheme::OcularDark, |_| panic!("must not notify"), |_, _| panic!("must not deliver")).is_err());
        assert_eq!(store.state.app_theme, AppTheme::Legato);
    }

    #[test]
    fn every_retained_page_is_updated_even_if_another_page_fails() {
        let servers = vec![
            crate::store::Server { id: "one".into(), url: "https://one.local/path".into(), ..Default::default() },
            crate::store::Server { id: "two".into(), url: "https://two.local".into(), ..Default::default() },
        ];
        let mut delivered = Vec::new();
        let result = deliver(AppTheme::Dracula, &servers, |id, script| {
            delivered.push((id.to_string(), script));
            if id == "one" { Err("page unavailable".into()) } else { Ok(()) }
        });
        assert!(result.unwrap_err().contains("page unavailable"));
        assert_eq!(delivered.iter().map(|(id, _)| id.as_str()).collect::<Vec<_>>(), ["one", "two"]);
        assert!(delivered[1].1.contains("https://two.local"));
    }

    #[test]
    fn the_requester_receives_success_or_error_only_on_its_own_top_frame() {
        for error in [None, Some("Could not write \"state.json\" </script>")] {
            for (origin, top, allowed) in [("https://one.local", true, true), ("https://other.local", true, false), ("https://one.local", false, false)] {
                let script = result_script("https://one.local", AppTheme::Dracula, error);
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
                let mut detail = serde_json::json!({ "id": "dracula" });
                if let Some(error) = error { detail["error"] = error.into(); }
                assert_eq!(calls, if allowed { serde_json::json!([["legato:shell-theme", detail]]) } else { serde_json::json!([]) });
            }
        }
    }

    #[test]
    fn pages_receive_storage_and_public_event_only_on_their_own_top_frame() {
        for (origin, top, expected) in [("https://one.local", true, true), ("https://other.local", true, false), ("https://one.local", false, false)] {
            let script = page_script("https://one.local", AppTheme::OcularLight);
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
            assert_eq!(calls, if expected { serde_json::json!([["legato:app-theme", "ocular-light"], ["legato:theme-changed", "ocular-light"]]) } else { serde_json::json!([]) });
        }
    }
}
