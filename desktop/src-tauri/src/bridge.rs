//! What a server's page and the shell say to each other, as the Legato app
//! for Android does (mobile/.../PageContract.kt, web/src/lib/mobileShell.ts):
//! the page is told it runs in an app that signs (`window.legatoShell`), and
//! its approval tray then asks the app to open its sheet on a signed approval
//! instead of signing in the page. The app reads only the approval's ID and
//! fetches the request itself, so a page cannot change what is signed.
//!
//! A server's page has no IPC. It reaches the app by navigating to a
//! `legato-desktop:` address, which the app reads and cancels; the address
//! carries a key only the page's own script knows, so a frame or a link in
//! the page cannot pass for it. The app answers with an event in the page.

/// The scheme of what the page asks of the app.
pub const SCHEME: &str = "legato-desktop";

/// What a page asks of the app.
#[derive(Debug, PartialEq)]
pub enum PageMessage {
    /// Open the sheet on this approval.
    Approval(String),
    /// Enrol this computer to sign: the app's keys.
    Enroll,
}

/// The script, run first in the page's top frame, that tells the page it
/// runs in the app and turns what it posts into a `legato-desktop:` address.
pub fn script(key: &str) -> String {
    let key = serde_json::to_string(key).expect("a string encodes");
    format!(
        r#"(() => {{
  if (window !== window.top) return;
  const key = {key};
  window.legatoShell = {{ approval: 1, enroll: 1, key }};
  window.ReactNativeWebView = {{
    postMessage(text) {{
      let m;
      try {{ m = JSON.parse(text); }} catch (e) {{ return; }}
      if (!m || m.key !== key) return;
      const ask = (kind, rest) => {{ location.href = "{SCHEME}://" + kind + "?key=" + encodeURIComponent(key) + rest; }};
      if (m.legatoShell === "approval" && typeof m.id === "string") ask("approval", "&id=" + encodeURIComponent(m.id));
      else if (m.legatoShell === "enroll") ask("enroll", "");
    }},
  }};
}})();"#
    )
}

/// What a navigation to url asks of the app, when it is the page's own (it
/// carries key). None for anything else.
pub fn read(url: &str, key: &str) -> Option<PageMessage> {
    let url = reqwest::Url::parse(url).ok()?;
    if url.scheme() != SCHEME {
        return None;
    }
    let param = |name: &str| url.query_pairs().find(|(k, _)| k == name).map(|(_, v)| v.into_owned());
    if param("key").as_deref() != Some(key) {
        return None;
    }
    match url.host_str()? {
        "approval" => param("id").filter(|id| !id.is_empty()).map(PageMessage::Approval),
        "enroll" => Some(PageMessage::Enroll),
        _ => None,
    }
}

/// Tells the page how the sheet on an approval came out, as the web UI
/// listens for it: `legato:shell-approval` with {id, status, option?, error?}.
pub fn result_script(detail: &serde_json::Value) -> String {
    format!(r#"(() => {{ try {{ window.dispatchEvent(new CustomEvent("legato:shell-approval", {{ detail: {detail} }})); }} catch (e) {{}} }})();"#)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn the_page_asks_for_the_sheet_on_an_approval_with_its_key() {
        assert_eq!(read("legato-desktop://approval?key=k1&id=ap-12", "k1"), Some(PageMessage::Approval("ap-12".into())));
        assert_eq!(read("legato-desktop://approval?key=k1&id=ap%2012", "k1"), Some(PageMessage::Approval("ap 12".into())));
        assert_eq!(read("legato-desktop://enroll?key=k1", "k1"), Some(PageMessage::Enroll));
    }

    #[test]
    fn anything_without_the_pages_key_or_an_approval_is_not_read() {
        assert_eq!(read("legato-desktop://approval?key=other&id=ap-1", "k1"), None, "another key");
        assert_eq!(read("legato-desktop://approval?id=ap-1", "k1"), None, "no key");
        assert_eq!(read("legato-desktop://approval?key=k1", "k1"), None, "no approval");
        assert_eq!(read("legato-desktop://approval?key=k1&id=", "k1"), None, "an empty approval");
        assert_eq!(read("legato-desktop://shutdown?key=k1", "k1"), None, "something else");
        assert_eq!(read("https://erebus.local:3080/?key=k1&id=ap-1", "k1"), None, "a page");
    }

    #[test]
    fn the_answer_carries_the_outcome_as_json_whatever_it_says() {
        let detail = json!({ "id": "ap-1", "status": "error", "error": "it said \"no\" </script> \u{2028}" });
        let script = result_script(&detail);
        assert!(script.contains(r#"new CustomEvent("legato:shell-approval""#));
        let start = script.find("detail: ").unwrap() + "detail: ".len();
        let end = script.rfind(" })); }").unwrap();
        let back: serde_json::Value = serde_json::from_str(&script[start..end]).unwrap();
        assert_eq!(back, detail);
    }

    /// Runs the page's script in Node with a stand-in window, posts what the
    /// web UI posts, and says where the page was sent. None without Node.
    fn in_node(key: &str, top: bool, posts: &[&str]) -> Option<Vec<String>> {
        let program = format!(
            r#"
const sent = [];
const location = {{ set href(v) {{ sent.push(v); }} }};
const window = {{}};
window.top = {top} ? window : {{}};
(function (window, location) {{ {script} }})(window, location);
for (const p of {posts}) window.ReactNativeWebView && window.ReactNativeWebView.postMessage(p);
console.log(JSON.stringify({{ sent, shell: window.legatoShell || null }}));
"#,
            top = top,
            script = script(key),
            posts = serde_json::to_string(posts).unwrap(),
        );
        let out = std::process::Command::new("node").arg("-e").arg(&program).output().ok()?;
        assert!(out.status.success(), "node: {}", String::from_utf8_lossy(&out.stderr));
        let v: serde_json::Value = serde_json::from_slice(&out.stdout).unwrap();
        if top {
            assert_eq!(v["shell"], json!({ "approval": 1, "enroll": 1, "key": key }));
        } else {
            assert!(v["shell"].is_null(), "a frame is not told");
        }
        Some(v["sent"].as_array().unwrap().iter().map(|s| s.as_str().unwrap().to_string()).collect())
    }

    // What web/src/lib/mobileShell.ts posts.
    #[test]
    fn the_pages_script_turns_what_the_web_ui_posts_into_addresses_the_app_reads() {
        let posts = [
            r#"{"legatoShell":"approval","key":"k 1","id":"ap-7"}"#,
            r#"{"legatoShell":"enroll","key":"k 1"}"#,
            r#"{"legatoShell":"approval","key":"wrong","id":"ap-8"}"#,
            r#"{"legatoShell":"restore","key":"k 1","url":"https://x/"}"#,
            "not json",
        ];
        let Some(sent) = in_node("k 1", true, &posts) else { return };
        assert_eq!(sent.len(), 2, "{sent:?}");
        assert_eq!(read(&sent[0], "k 1"), Some(PageMessage::Approval("ap-7".into())));
        assert_eq!(read(&sent[1], "k 1"), Some(PageMessage::Enroll));
    }

    #[test]
    fn a_frame_in_the_page_is_not_told_it_runs_in_the_app() {
        let Some(sent) = in_node("k1", false, &[r#"{"legatoShell":"approval","key":"k1","id":"ap-7"}"#]) else { return };
        assert!(sent.is_empty());
    }
}
