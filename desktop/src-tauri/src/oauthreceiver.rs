//! The app's side of connecting an MCP server with OAuth (LEG-199, LEG-209).
//!
//! Legato, the server, is the OAuth client: it discovers the provider,
//! registers, makes the state and the PKCE verifier, exchanges the code and
//! keeps the tokens. The app only receives the browser's return: it binds an
//! IP-loopback receiver before consent, asks the page's own server to begin
//! an attempt for that receiver, opens the system browser on the
//! authorization address the server gave, takes the one callback that
//! carries the attempt's state, and hands code, state and issuer (or the
//! denial) to that same server, over its authenticated connection, with the
//! attempt's return key. The code and the return key live only in this
//! process's memory: never in the page, a log or a file.
//!
//! The server the response goes to is fixed when the attempt begins: the
//! callback cannot name another, and a page cannot either.
//!
//! The wire contract with the server (implemented there by LEG-211):
//! - `POST /api/mcp/connections/{name}/oauth/begin` `{mode: "app",
//!   redirect_uri}` → `{id, authorize_url, return_key, expires_at}`
//! - `POST /api/mcp/oauth/attempts/{id}/deliver` `{return_key, state, iss?,
//!   code | error}` → `{status, …}`, the same status again for a repeat
//! - `POST /api/mcp/oauth/attempts/{id}/cancel` `{}`

use crate::legato::Client;
use serde_json::{json, Value};
use std::time::Duration;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpListener;

/// The longest an attempt waits for the browser.
pub const LIFETIME: Duration = Duration::from_secs(10 * 60);
/// The most of a request the receiver reads.
const MAX_REQUEST: usize = 8 * 1024;
/// How long one browser connection may take to send its request.
const READ_TIMEOUT: Duration = Duration::from_secs(10);

/// What the provider sent back through the browser.
#[derive(Debug, Clone, PartialEq)]
pub enum Response {
    Code { code: String, state: String, iss: Option<String> },
    Denied { error: String, state: String, iss: Option<String> },
}

impl Response {
    pub fn state(&self) -> &str {
        match self {
            Response::Code { state, .. } | Response::Denied { state, .. } => state,
        }
    }
}

/// A server name as Legato's registry allows it.
pub fn valid_connection(name: &str) -> bool {
    let b = name.as_bytes();
    !b.is_empty() && b.len() <= 63 && (b[0].is_ascii_lowercase() || b[0].is_ascii_digit()) && b.iter().all(|c| c.is_ascii_lowercase() || c.is_ascii_digit() || *c == b'-')
}

/// A code may go to a server over HTTPS, or over HTTP only to an IP
/// loopback address: never in clear across a network, whatever other
/// features of the app accept.
pub fn destination_allowed(server_url: &str) -> Result<(), String> {
    let url = reqwest::Url::parse(server_url).map_err(|_| "the server's address is not valid".to_string())?;
    let loopback_ip = url
        .host_str()
        .map(|h| h.trim_start_matches('[').trim_end_matches(']'))
        .and_then(|h| h.parse::<std::net::IpAddr>().ok())
        .is_some_and(|ip| ip.is_loopback());
    match url.scheme() {
        "https" if url.host_str().is_some() => Ok(()),
        "http" if loopback_ip => Ok(()),
        _ => Err("Legato reaches this server without HTTPS, so the app will not send it an authorization. Connect it from a browser on that computer, or pair the app with it over HTTPS.".into()),
    }
}

/// Reads a callback's query strictly: code or error, state, and iss each at
/// most once; anything else the provider adds is ignored and never used.
pub fn parse_callback(query: &str) -> Result<Response, String> {
    let (mut code, mut error, mut state, mut iss) = (None, None, None, None);
    let parsed = reqwest::Url::parse(&format!("http://callback.invalid/?{query}")).map_err(|_| "the callback is not a valid address".to_string())?;
    for (key, value) in parsed.query_pairs() {
        let slot = match key.as_ref() {
            "code" => &mut code,
            "error" => &mut error,
            "state" => &mut state,
            "iss" => &mut iss,
            _ => continue,
        };
        if slot.is_some() {
            return Err(format!("the callback repeats {key}"));
        }
        if value.is_empty() || value.len() > 4096 {
            return Err(format!("the callback's {key} is empty or too long"));
        }
        *slot = Some(value.into_owned());
    }
    let state = state.ok_or("the callback has no state")?;
    match (code, error) {
        (Some(code), None) => Ok(Response::Code { code, state, iss }),
        (None, Some(error)) => Ok(Response::Denied { error, state, iss }),
        _ => Err("the callback has neither a code nor an error, or both".into()),
    }
}

/// The receiver: a TCP listener on 127.0.0.1, bound before consent.
pub struct Receiver {
    listener: TcpListener,
    port: u16,
}

impl Receiver {
    pub async fn bind() -> Result<Receiver, String> {
        let listener = TcpListener::bind("127.0.0.1:0").await.map_err(|e| format!("the app could not start its receiver: {e}"))?;
        let port = listener.local_addr().map_err(|e| e.to_string())?.port();
        Ok(Receiver { listener, port })
    }

    pub fn redirect_uri(&self) -> String {
        format!("http://127.0.0.1:{}/callback", self.port)
    }

    /// Waits until the browser brings back the response with state, or the
    /// deadline. Every request gets a short page that echoes nothing; a
    /// request for another address or host, or with another state, is
    /// turned away and the receiver keeps waiting.
    pub async fn wait(&self, state: &str, deadline: Duration) -> Result<Response, String> {
        let host = format!("127.0.0.1:{}", self.port);
        tokio::time::timeout(deadline, async {
            loop {
                let (mut stream, _) = match self.listener.accept().await {
                    Ok(accepted) => accepted,
                    Err(_) => continue,
                };
                let request = match tokio::time::timeout(READ_TIMEOUT, read_request(&mut stream)).await {
                    Ok(Ok(request)) => request,
                    _ => {
                        let _ = respond(&mut stream, 400, "Invalid request.").await;
                        continue;
                    }
                };
                let outcome = check_request(&request, &host, state);
                match outcome {
                    Ok(response) => {
                        let _ = respond(&mut stream, 200, "Return to Legato to check the connection. You can close this tab.").await;
                        return response;
                    }
                    Err((status, text)) => {
                        let _ = respond(&mut stream, status, text).await;
                    }
                }
            }
        })
        .await
        .map_err(|_| "the browser did not come back in time; start again".to_string())
    }
}

struct Request {
    method: String,
    target: String,
    host: String,
}

async fn read_request(stream: &mut tokio::net::TcpStream) -> Result<Request, String> {
    let mut buf = Vec::with_capacity(1024);
    let mut chunk = [0u8; 1024];
    loop {
        let n = stream.read(&mut chunk).await.map_err(|e| e.to_string())?;
        if n == 0 {
            return Err("closed".into());
        }
        buf.extend_from_slice(&chunk[..n]);
        if buf.windows(4).any(|w| w == b"\r\n\r\n") {
            break;
        }
        if buf.len() > MAX_REQUEST {
            return Err("too large".into());
        }
    }
    let text = String::from_utf8_lossy(&buf);
    let mut lines = text.split("\r\n");
    let mut first = lines.next().unwrap_or_default().split(' ');
    let (method, target) = (first.next().unwrap_or_default().to_string(), first.next().unwrap_or_default().to_string());
    let mut host = String::new();
    for line in lines {
        if let Some((name, value)) = line.split_once(':') {
            if name.eq_ignore_ascii_case("host") {
                host = value.trim().to_string();
            }
        }
    }
    Ok(Request { method, target, host })
}

fn check_request(request: &Request, host: &str, state: &str) -> Result<Response, (u16, &'static str)> {
    if request.method != "GET" || request.host != host {
        return Err((400, "Invalid callback address."));
    }
    let (path, query) = request.target.split_once('?').unwrap_or((request.target.as_str(), ""));
    if path != "/callback" {
        return Err((404, "Not found."));
    }
    let response = parse_callback(query).map_err(|_| (400, "Invalid authorization response."))?;
    // A response for another attempt (or a forged one) settles nothing here.
    if response.state() != state {
        return Err((400, "This response is not for the connection the app is waiting for."));
    }
    Ok(response)
}

async fn respond(stream: &mut tokio::net::TcpStream, status: u16, text: &str) -> std::io::Result<()> {
    let body = format!("<!doctype html><meta charset=utf-8><title>Legato authorization</title><p>{text}</p>");
    let reply = format!(
        "HTTP/1.1 {status} X\r\nContent-Type: text/html; charset=utf-8\r\nCache-Control: no-store\r\nReferrer-Policy: no-referrer\r\nContent-Security-Policy: default-src 'none'; frame-ancestors 'none'\r\nX-Content-Type-Options: nosniff\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
        body.len()
    );
    stream.write_all(reply.as_bytes()).await?;
    stream.shutdown().await
}

/// An attempt the server began for this app's receiver.
pub struct Attempt {
    pub id: String,
    pub authorize_url: String,
    state: String,
    return_key: String,
}

impl std::fmt::Debug for Attempt {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Attempt").field("id", &self.id).finish_non_exhaustive()
    }
}

fn valid_id(id: &str) -> bool {
    !id.is_empty() && id.len() <= 128 && id.bytes().all(|c| c.is_ascii_alphanumeric() || c == b'-' || c == b'_')
}

/// Asks the server to begin an attempt for redirect, and checks what it
/// answers: an HTTPS authorization address that names this receiver and a
/// state, an attempt ID and a return key.
pub async fn begin(client: &Client, connection: &str, redirect: &str) -> Result<Attempt, String> {
    if !valid_connection(connection) {
        return Err("that is not an MCP connection's name".into());
    }
    let answer = client.post(&format!("/api/mcp/connections/{connection}/oauth/begin"), json!({ "mode": "app", "redirect_uri": redirect })).await?;
    let text = |name: &str| answer[name].as_str().map(str::to_string).filter(|s| !s.is_empty());
    let (id, authorize_url, return_key) = match (text("id"), text("authorize_url"), text("return_key")) {
        (Some(id), Some(url), Some(key)) if valid_id(&id) => (id, url, key),
        _ => return Err("the server's answer is not an authorization attempt".into()),
    };
    let url = reqwest::Url::parse(&authorize_url).map_err(|_| "the server gave an invalid authorization address".to_string())?;
    if url.scheme() != "https" {
        return Err("the server gave an authorization address without HTTPS".into());
    }
    let param = |name: &str| url.query_pairs().filter(|(k, _)| k == name).map(|(_, v)| v.into_owned()).collect::<Vec<_>>();
    let state = match (param("state").as_slice(), param("redirect_uri").as_slice()) {
        ([state], [named]) if !state.is_empty() && named == redirect => state.clone(),
        _ => return Err("the server's authorization address is not for this app's receiver".into()),
    };
    Ok(Attempt { id, authorize_url, state, return_key })
}

/// Hands the response to the server the attempt began on. A server that
/// cannot be reached is asked again, a few times within the attempt's life:
/// a repeat is answered with the first one's status, never a second exchange.
pub async fn deliver(client: &Client, attempt: &Attempt, response: &Response) -> Result<Value, String> {
    let mut body = json!({ "return_key": attempt.return_key, "state": response.state() });
    match response {
        Response::Code { code, iss, .. } => {
            body["code"] = json!(code);
            if let Some(iss) = iss {
                body["iss"] = json!(iss);
            }
        }
        Response::Denied { error, iss, .. } => {
            body["error"] = json!(error);
            if let Some(iss) = iss {
                body["iss"] = json!(iss);
            }
        }
    }
    let path = format!("/api/mcp/oauth/attempts/{}/deliver", attempt.id);
    let mut last = String::new();
    for wait in [0u64, 1, 3, 8] {
        tokio::time::sleep(Duration::from_secs(wait)).await;
        match client.reach(reqwest::Method::POST, &path, Some(body.clone())).await {
            Ok(answer) => return answer,
            Err(unreachable) => last = unreachable,
        }
    }
    Err(last)
}

/// Tells the server the attempt is over.
pub async fn cancel(client: &Client, attempt: &Attempt) {
    let _ = client.post(&format!("/api/mcp/oauth/attempts/{}/cancel", attempt.id), json!({})).await;
}

/// Connects an MCP server through the page's server: the whole of the app's
/// part. open shows the authorization address in the system's browser.
pub async fn connect(client: &Client, server_url: &str, connection: &str, open: impl FnOnce(&str) -> Result<(), String>, lifetime: Duration) -> Result<Value, String> {
    destination_allowed(server_url)?;
    let receiver = Receiver::bind().await?;
    let attempt = begin(client, connection, &receiver.redirect_uri()).await?;
    if let Err(e) = open(&attempt.authorize_url) {
        cancel(client, &attempt).await;
        return Err(format!("the browser could not be opened: {e}"));
    }
    let response = match receiver.wait(&attempt.state, lifetime).await {
        Ok(response) => response,
        Err(e) => {
            cancel(client, &attempt).await;
            return Err(e);
        }
    };
    drop(receiver);
    deliver(client, &attempt, &response).await
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::{BufRead, BufReader, Read, Write};
    use std::sync::{Arc, Mutex};

    #[derive(Debug, Clone)]
    struct Seen {
        path: String,
        auth: Option<String>,
        body: Value,
    }

    /// A stand-in for Legato's API: begin answers with an attempt for the
    /// redirect it is given; deliver and cancel are recorded.
    fn legato(state: &'static str) -> (String, Arc<Mutex<Vec<Seen>>>) {
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let address = format!("http://{}", listener.local_addr().unwrap());
        let seen = Arc::new(Mutex::new(Vec::new()));
        let record = seen.clone();
        std::thread::spawn(move || {
            for stream in listener.incoming() {
                let Ok(stream) = stream else { return };
                let mut reader = BufReader::new(stream);
                let mut line = String::new();
                if reader.read_line(&mut line).is_err() {
                    continue;
                }
                let path = line.split_whitespace().nth(1).unwrap_or_default().to_string();
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
                let body: Value = serde_json::from_slice(&raw).unwrap_or(Value::Null);
                record.lock().unwrap().push(Seen { path: path.clone(), auth, body: body.clone() });
                let reply = if path.ends_with("/oauth/begin") {
                    let redirect = body["redirect_uri"].as_str().unwrap_or_default();
                    let url = reqwest::Url::parse_with_params("https://provider.example.test/authorize", &[("client_id", "c1"), ("redirect_uri", redirect), ("state", state)]).unwrap();
                    json!({ "id": "att-1", "authorize_url": url.as_str(), "return_key": "rk-secret", "expires_at": 0 }).to_string()
                } else {
                    json!({ "status": "awaiting_confirmation" }).to_string()
                };
                let mut stream = reader.into_inner();
                let _ = stream.write_all(format!("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{reply}", reply.len()).as_bytes());
            }
        });
        (address, seen)
    }

    /// The browser: a GET with the Host it was given, returning status and body.
    async fn browse(port: u16, host: &str, target: &str) -> (u16, String) {
        let mut stream = tokio::net::TcpStream::connect(("127.0.0.1", port)).await.unwrap();
        stream.write_all(format!("GET {target} HTTP/1.1\r\nHost: {host}\r\nConnection: close\r\n\r\n").as_bytes()).await.unwrap();
        let mut out = String::new();
        stream.read_to_string(&mut out).await.unwrap();
        let status = out.split_whitespace().nth(1).and_then(|s| s.parse().ok()).unwrap_or(0);
        (status, out)
    }

    fn port_of(authorize: &str) -> u16 {
        let url = reqwest::Url::parse(authorize).unwrap();
        let redirect = url.query_pairs().find(|(k, _)| k == "redirect_uri").unwrap().1.into_owned();
        reqwest::Url::parse(&redirect).unwrap().port().unwrap()
    }

    #[test]
    fn a_callback_is_read_strictly() {
        assert_eq!(parse_callback("code=c&state=s&iss=https%3A%2F%2Fas"), Ok(Response::Code { code: "c".into(), state: "s".into(), iss: Some("https://as".into()) }));
        assert_eq!(parse_callback("error=access_denied&state=s"), Ok(Response::Denied { error: "access_denied".into(), state: "s".into(), iss: None }));
        assert_eq!(
            parse_callback("code=c&state=s&return_to=https%3A%2F%2Fevil&server=x"),
            Ok(Response::Code { code: "c".into(), state: "s".into(), iss: None }),
            "other parameters are ignored, never used as a destination"
        );
        for bad in ["code=c", "state=s", "code=c&error=e&state=s", "code=c&code=d&state=s", "code=c&state=s&state=t", "code=&state=s", ""] {
            assert!(parse_callback(bad).is_err(), "{bad}");
        }
    }

    #[test]
    fn a_code_never_crosses_a_network_without_https() {
        assert!(destination_allowed("https://legato.lan:3080").is_ok());
        assert!(destination_allowed("http://127.0.0.1:3080").is_ok());
        assert!(destination_allowed("http://[::1]:3080").is_ok());
        for bad in ["http://192.168.1.20:3080", "http://legato.lan:3080", "http://localhost:3080", "ftp://x"] {
            assert!(destination_allowed(bad).is_err(), "{bad}");
        }
    }

    #[test]
    fn the_response_goes_to_the_server_the_attempt_began_on_with_its_return_key() {
        let (url, seen) = legato("st-1");
        let client = Client::new(&url, None, Some("tok-1".into())).unwrap();
        let result = tauri::async_runtime::block_on(async {
            let (tx, rx) = tokio::sync::oneshot::channel::<String>();
            let browser = tokio::spawn(async move {
                let authorize = rx.await.unwrap();
                let port = port_of(&authorize);
                let host = format!("127.0.0.1:{port}");
                // Unrelated requests and a forged response settle nothing.
                assert_eq!(browse(port, &host, "/favicon.ico").await.0, 404);
                assert_eq!(browse(port, "evil.example.test", "/callback?code=x&state=st-1").await.0, 400);
                assert_eq!(browse(port, &host, "/callback?code=x&state=other").await.0, 400);
                browse(port, &host, "/callback?code=code-1&state=st-1&iss=https%3A%2F%2Fas.example.test&return_to=https%3A%2F%2Fevil").await
            });
            let outcome = connect(&client, &url, "notion", |authorize| tx.send(authorize.to_string()).map_err(|_| "no browser".to_string()), Duration::from_secs(10)).await;
            (outcome, browser.await.unwrap())
        });
        let (outcome, (status, page)) = result;
        assert_eq!(outcome.unwrap()["status"], "awaiting_confirmation");
        assert_eq!(status, 200);
        assert!(page.contains("Cache-Control: no-store") && !page.contains("code-1") && !page.contains("st-1"), "the page echoes the callback: {page}");
        let seen = seen.lock().unwrap().clone();
        assert_eq!(seen.len(), 2, "{seen:?}");
        assert_eq!(seen[0].path, "/api/mcp/connections/notion/oauth/begin");
        assert_eq!(seen[0].body["mode"], "app");
        assert!(seen[0].body["redirect_uri"].as_str().unwrap().starts_with("http://127.0.0.1:"));
        assert_eq!(seen[1].path, "/api/mcp/oauth/attempts/att-1/deliver");
        assert_eq!(seen[1].auth.as_deref(), Some("Bearer tok-1"));
        assert_eq!(seen[1].body, json!({ "return_key": "rk-secret", "state": "st-1", "code": "code-1", "iss": "https://as.example.test" }));
    }

    #[test]
    fn a_denial_is_handed_over_as_a_denial() {
        let (url, seen) = legato("st-2");
        let client = Client::new(&url, None, Some("tok-1".into())).unwrap();
        tauri::async_runtime::block_on(async {
            let (tx, rx) = tokio::sync::oneshot::channel::<String>();
            tokio::spawn(async move {
                let port = port_of(&rx.await.unwrap());
                browse(port, &format!("127.0.0.1:{port}"), "/callback?error=access_denied&state=st-2").await
            });
            connect(&client, &url, "notion", |a| tx.send(a.to_string()).map_err(|_| "no".to_string()), Duration::from_secs(10)).await.unwrap();
        });
        let seen = seen.lock().unwrap().clone();
        assert_eq!(seen[1].body, json!({ "return_key": "rk-secret", "state": "st-2", "error": "access_denied" }));
    }

    #[test]
    fn an_attempt_the_browser_never_returns_to_is_cancelled() {
        let (url, seen) = legato("st-3");
        let client = Client::new(&url, None, Some("tok-1".into())).unwrap();
        let outcome = tauri::async_runtime::block_on(connect(&client, &url, "notion", |_| Ok(()), Duration::from_millis(300)));
        assert!(outcome.unwrap_err().contains("did not come back"));
        let seen = seen.lock().unwrap().clone();
        assert_eq!(seen.last().unwrap().path, "/api/mcp/oauth/attempts/att-1/cancel");
    }

    #[test]
    fn a_server_without_https_is_never_asked() {
        let client = Client::new("http://192.168.1.20:3080", None, Some("tok".into())).unwrap();
        let outcome = tauri::async_runtime::block_on(connect(&client, "http://192.168.1.20:3080", "notion", |_| panic!("opened a browser"), Duration::from_secs(1)));
        assert!(outcome.unwrap_err().contains("HTTPS"));
    }

    #[test]
    fn a_begin_answer_for_another_receiver_or_without_https_is_refused() {
        // The server's answer must name this receiver: a proxy or a rogue
        // server cannot send the browser elsewhere.
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let url = format!("http://{}", listener.local_addr().unwrap());
        std::thread::spawn(move || {
            for (i, stream) in listener.incoming().enumerate() {
                let mut stream = stream.unwrap();
                let mut buf = [0u8; 4096];
                let _ = stream.read(&mut buf);
                let body = if i == 0 {
                    json!({ "id": "a", "authorize_url": "https://p.example.test/authorize?state=s&redirect_uri=http%3A%2F%2F127.0.0.1%3A1%2Fcallback", "return_key": "k" })
                } else {
                    json!({ "id": "a", "authorize_url": "http://p.example.test/authorize?state=s", "return_key": "k" })
                }
                .to_string();
                let _ = stream.write_all(format!("HTTP/1.1 200 OK\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}", body.len()).as_bytes());
            }
        });
        let client = Client::new(&url, None, Some("tok".into())).unwrap();
        for _ in 0..2 {
            let outcome = tauri::async_runtime::block_on(begin(&client, "notion", "http://127.0.0.1:34567/callback"));
            assert!(outcome.is_err(), "{outcome:?}");
        }
        assert!(tauri::async_runtime::block_on(begin(&client, "Not A Name", "http://127.0.0.1:1/callback")).is_err());
    }
}
