//! Adding a worker from a server the app already knows. The page of the
//! server in front (the main) offers the app's other servers; the person
//! chooses one, the app asks natively whether to make it a worker, and opens
//! a main's pairing request on it (`POST /api/pairing {"purpose":"main"}`)
//! with the token and pinned certificate only the app holds. The page gets
//! the address and the phrase, and its server pairs with them as if they had
//! been typed into Add a worker. The page never sees the other server's
//! token. See `bridge` for how the page asks, and `web/src/lib/workers.ts`.

use serde_json::{json, Value};

use crate::legato::Client;
use crate::store::Server;

/// The longest name of the main the confirm repeats from the page.
const LONGEST_MAIN: usize = 80;

/// Every server the app knows but own (the page's), as the page lists them:
/// `{key, name, url}`, key being the app's ID for it.
pub fn candidates(servers: &[Server], own: &str) -> Value {
    Value::Array(servers.iter().filter(|s| s.id != own).map(|s| json!({ "key": s.id, "name": s.name, "url": s.url })).collect())
}

/// The server a page asked to make a worker: one the app knows, and not the
/// page's own. Refused without a confirm otherwise.
pub fn target<'a>(servers: &'a [Server], own: &str, key: &str) -> Result<&'a Server, String> {
    servers.iter().find(|s| s.id == key && s.id != own).ok_or_else(|| "This app has no such server.".into())
}

/// The error for a server whose token the app has lost.
pub fn no_token(name: &str) -> String {
    format!("This app has no token for {name}. Pair {name} with the app again.")
}

/// What the confirm calls the main: the name its page gave, else the app's
/// name for it.
pub fn main_name(asked: &str, app_name: &str) -> String {
    let asked = asked.trim();
    if asked.is_empty() {
        return app_name.to_string();
    }
    asked.chars().take(LONGEST_MAIN).collect()
}

/// The question the app asks before it opens the request.
pub fn confirm_text(server: &str, main: &str) -> String {
    format!("Make {server} a worker of {main}? {main} will be able to hand tasks to {server}, which runs their agents there.")
}

/// Opens a main's pairing request on a server (name, at url) and gives what
/// the page needs: `{address, phrase, expires_at}`, address being the app's
/// URL for it and phrase as the server gave it. A refusal is given in the
/// server's words. A Legato from before handoff ignores the purpose and
/// opens a phone's request: that is withdrawn, and the answer says the
/// server is too old. The words match the Android app's (ShellWorkers.kt).
pub async fn open_main_request(client: &Client, name: &str, url: &str) -> Result<Value, String> {
    let opened = match client.reach(reqwest::Method::POST, "/api/pairing", Some(json!({ "purpose": "main" }))).await {
        Err(unreached) => return Err(format!("Could not reach {name}: {unreached}")),
        Ok(Err(refused)) => return Err(match refused.strip_prefix("HTTP ") {
            Some(status) => format!("The server answered {status}."),
            None => refused,
        }),
        Ok(Ok(opened)) => opened,
    };
    if opened["purpose"] != "main" {
        let _ = client.delete("/api/pairing").await;
        return Err("This server is too old to be a worker. Update Legato on it, then try again.".into());
    }
    let phrase = opened["phrase"].clone();
    if !phrase.as_array().is_some_and(|words| !words.is_empty()) {
        return Err("The server opened no pairing request.".into());
    }
    let mut answer = json!({ "address": url, "phrase": phrase });
    if !opened["expires_at"].is_null() {
        answer["expires_at"] = opened["expires_at"].clone();
    }
    Ok(answer)
}

/// The detail of the `legato:shell-worker` event that answers request id:
/// what came of it, `{cancelled: true}` when the person said no, or
/// `{error}`.
pub fn answer(id: &Value, outcome: Result<Option<Value>, String>) -> Value {
    let mut detail = match outcome {
        Ok(Some(Value::Object(fields))) => Value::Object(fields),
        Ok(Some(other)) => json!({ "servers": other }),
        Ok(None) => json!({ "cancelled": true }),
        Err(error) => json!({ "error": error }),
    };
    detail["id"] = id.clone();
    detail
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::{BufRead, BufReader, Read, Write};
    use std::net::TcpListener;
    use std::sync::{Arc, Mutex};

    fn server(id: &str, name: &str, url: &str) -> Server {
        Server { id: id.into(), name: name.into(), url: url.into(), ..Default::default() }
    }

    #[derive(Debug, Clone, PartialEq)]
    struct Seen {
        method: String,
        path: String,
        auth: Option<String>,
        body: Value,
    }

    /// A server that answers each request with the next of answers (status,
    /// body) and records what it was asked. Gives its address.
    fn serve(answers: Vec<(u16, &'static str)>) -> (String, Arc<Mutex<Vec<Seen>>>) {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let address = format!("http://{}", listener.local_addr().unwrap());
        let seen = Arc::new(Mutex::new(Vec::new()));
        let record = seen.clone();
        std::thread::spawn(move || {
            for (status, reply_body) in answers {
                let Ok((stream, _)) = listener.accept() else { return };
                let mut reader = BufReader::new(stream);
                let mut line = String::new();
                reader.read_line(&mut line).unwrap();
                let mut parts = line.split_whitespace();
                let (method, path) = (parts.next().unwrap_or_default().to_string(), parts.next().unwrap_or_default().to_string());
                let (mut length, mut auth) = (0usize, None);
                loop {
                    let mut header = String::new();
                    reader.read_line(&mut header).unwrap();
                    let header = header.trim_end();
                    if header.is_empty() {
                        break;
                    }
                    let (name, value) = header.split_once(':').unwrap_or((header, ""));
                    match name.to_ascii_lowercase().as_str() {
                        "content-length" => length = value.trim().parse().unwrap_or(0),
                        "authorization" => auth = Some(value.trim().to_string()),
                        _ => {}
                    }
                }
                let mut raw = vec![0u8; length];
                reader.read_exact(&mut raw).unwrap();
                let body = serde_json::from_slice(&raw).unwrap_or(Value::Null);
                record.lock().unwrap().push(Seen { method, path, auth, body });
                let mut stream = reader.into_inner();
                let reply = format!("HTTP/1.1 {status} X\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{reply_body}", reply_body.len());
                stream.write_all(reply.as_bytes()).unwrap();
            }
        });
        (address, seen)
    }

    fn open(answers: Vec<(u16, &'static str)>) -> (Result<Value, String>, Vec<Seen>, String) {
        let (url, seen) = serve(answers);
        let client = Client::new(&url, None, Some("tok-1".into())).unwrap();
        let result = tauri::async_runtime::block_on(open_main_request(&client, "legato-1", &url));
        let seen = seen.lock().unwrap().clone();
        (result, seen, url)
    }

    #[test]
    fn the_page_is_offered_every_server_but_its_own() {
        let servers = [server("srv-a", "legato-1", "https://a:3080"), server("srv-main", "main", "https://m:3080"), server("srv-b", "legato-2", "http://b:3080")];
        assert_eq!(
            candidates(&servers, "srv-main"),
            json!([{ "key": "srv-a", "name": "legato-1", "url": "https://a:3080" }, { "key": "srv-b", "name": "legato-2", "url": "http://b:3080" }])
        );
        assert_eq!(candidates(&servers[1..2], "srv-main"), json!([]));
    }

    #[test]
    fn a_page_cannot_make_its_own_server_or_one_the_app_does_not_know_a_worker() {
        let servers = [server("srv-a", "legato-1", "https://a:3080"), server("srv-main", "main", "https://m:3080")];
        assert_eq!(target(&servers, "srv-main", "srv-a").unwrap().name, "legato-1");
        assert_eq!(target(&servers, "srv-main", "srv-main").unwrap_err(), "This app has no such server.");
        assert_eq!(target(&servers, "srv-main", "srv-gone").unwrap_err(), "This app has no such server.");
    }

    #[test]
    fn the_confirm_names_both_servers() {
        assert_eq!(
            confirm_text("legato-1", "workstation"),
            "Make legato-1 a worker of workstation? workstation will be able to hand tasks to legato-1, which runs their agents there."
        );
        assert_eq!(main_name("  workstation ", "main"), "workstation");
        assert_eq!(main_name("", "main"), "main", "the app's name when the page gives none");
        assert_eq!(main_name(&"x".repeat(500), "main").chars().count(), LONGEST_MAIN);
    }

    #[test]
    fn it_opens_a_mains_request_with_the_servers_token_and_gives_the_address_and_phrase() {
        let (result, seen, url) = open(vec![(200, r#"{"state":"open","purpose":"main","phrase":["bucket","ladle","spree","oyster"],"expires_at":"2026-11-05T09:05:00Z","attempts_left":5,"address":"https://elsewhere:3080"}"#)]);
        assert_eq!(result.unwrap(), json!({ "address": url, "phrase": ["bucket", "ladle", "spree", "oyster"], "expires_at": "2026-11-05T09:05:00Z" }));
        assert_eq!(seen, [Seen { method: "POST".into(), path: "/api/pairing".into(), auth: Some("Bearer tok-1".into()), body: json!({ "purpose": "main" }) }]);
    }

    #[test]
    fn a_server_too_old_to_pair_a_main_is_said_to_be_and_its_phone_request_cancelled() {
        let (result, seen, _) = open(vec![(200, r#"{"state":"open","phrase":["bucket","ladle","spree","oyster"],"attempts_left":5}"#), (200, r#"{"state":"cancelled"}"#)]);
        assert_eq!(result.unwrap_err(), "This server is too old to be a worker. Update Legato on it, then try again.");
        assert_eq!(seen.iter().map(|s| (s.method.as_str(), s.path.as_str())).collect::<Vec<_>>(), [("POST", "/api/pairing"), ("DELETE", "/api/pairing")]);
    }

    #[test]
    fn a_refusal_is_given_in_the_servers_words() {
        let (result, _, _) = open(vec![(404, r#"{"error":"this server does not pair by phrase"}"#)]);
        assert_eq!(result.unwrap_err(), "this server does not pair by phrase");
        let (result, _, _) = open(vec![(404, "")]);
        assert_eq!(result.unwrap_err(), "The server answered 404.");
        let (result, _, _) = open(vec![(200, r#"{"state":"open","purpose":"main","attempts_left":5}"#)]);
        assert_eq!(result.unwrap_err(), "The server opened no pairing request.");
    }

    #[test]
    fn a_server_that_does_not_answer_could_not_be_reached() {
        // A port nothing listens on.
        let url = {
            let listener = TcpListener::bind("127.0.0.1:0").unwrap();
            format!("http://{}", listener.local_addr().unwrap())
        };
        let client = Client::new(&url, None, Some("tok-1".into())).unwrap();
        let error = tauri::async_runtime::block_on(open_main_request(&client, "legato-1", &url)).unwrap_err();
        assert!(error.starts_with("Could not reach legato-1: "), "{error}");
    }

    #[test]
    fn the_answer_carries_the_requests_id() {
        assert_eq!(answer(&json!("w1"), Ok(Some(json!([{ "key": "srv-a" }])))), json!({ "id": "w1", "servers": [{ "key": "srv-a" }] }));
        assert_eq!(answer(&json!("w2"), Ok(Some(json!({ "address": "https://a", "phrase": ["x"] })))), json!({ "id": "w2", "address": "https://a", "phrase": ["x"] }));
        assert_eq!(answer(&json!(3), Ok(None)), json!({ "id": 3, "cancelled": true }), "a number stays a number");
        assert_eq!(answer(&json!("w4"), Err("no".into())), json!({ "id": "w4", "error": "no" }));
    }
}
