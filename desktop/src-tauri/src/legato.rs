//! Requests to one Legato server, over TLS that trusts its pinned
//! certificate and nothing else, and the pairing that pins it.

use std::sync::{Arc, Mutex};

use base64::engine::general_purpose::STANDARD;
use base64::Engine;
use rustls::client::danger::{HandshakeSignatureValid, ServerCertVerified, ServerCertVerifier};
use rustls::pki_types::{CertificateDer, ServerName, UnixTime};
use rustls::{DigitallySignedStruct, SignatureScheme};
use serde::Deserialize;
use serde_json::{json, Value};

use crate::pairing::{self, Exchange, LABEL_PHONE, LABEL_SERVER};
use crate::pin::Pin;
use crate::webauthn::ORIGIN;

/// Checks a server's certificate with a pin, or (while pairing, before
/// there is one) records it to check afterwards.
#[derive(Debug)]
struct PinVerifier {
    pin: Option<Pin>,
    seen: Arc<Mutex<Option<Vec<u8>>>>,
    provider: Arc<rustls::crypto::CryptoProvider>,
}

impl std::fmt::Debug for Pin {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("Pin")
    }
}

impl ServerCertVerifier for PinVerifier {
    fn verify_server_cert(
        &self,
        end_entity: &CertificateDer<'_>,
        _intermediates: &[CertificateDer<'_>],
        server_name: &ServerName<'_>,
        _ocsp: &[u8],
        _now: UnixTime,
    ) -> Result<ServerCertVerified, rustls::Error> {
        *self.seen.lock().unwrap() = Some(end_entity.to_vec());
        match &self.pin {
            None => Ok(ServerCertVerified::assertion()),
            Some(pin) if pin.trusts(end_entity, &server_name.to_str()) => Ok(ServerCertVerified::assertion()),
            Some(_) => Err(rustls::Error::General("the server's certificate is not the one pinned when it paired".into())),
        }
    }

    fn verify_tls12_signature(&self, message: &[u8], cert: &CertificateDer<'_>, dss: &DigitallySignedStruct) -> Result<HandshakeSignatureValid, rustls::Error> {
        rustls::crypto::verify_tls12_signature(message, cert, dss, &self.provider.signature_verification_algorithms)
    }

    fn verify_tls13_signature(&self, message: &[u8], cert: &CertificateDer<'_>, dss: &DigitallySignedStruct) -> Result<HandshakeSignatureValid, rustls::Error> {
        rustls::crypto::verify_tls13_signature(message, cert, dss, &self.provider.signature_verification_algorithms)
    }

    fn supported_verify_schemes(&self) -> Vec<SignatureScheme> {
        self.provider.signature_verification_algorithms.supported_schemes()
    }
}

fn http_client(pin: Option<Pin>, seen: Arc<Mutex<Option<Vec<u8>>>>) -> reqwest::Client {
    let provider = Arc::new(rustls::crypto::ring::default_provider());
    let tls = rustls::ClientConfig::builder_with_provider(provider.clone())
        .with_safe_default_protocol_versions()
        .expect("TLS versions")
        .dangerous()
        .with_custom_certificate_verifier(Arc::new(PinVerifier { pin, seen, provider }))
        .with_no_client_auth();
    reqwest::Client::builder()
        .use_preconfigured_tls(tls)
        .timeout(std::time::Duration::from_secs(15))
        .build()
        .expect("HTTP client")
}

/// One server's API, with its token.
#[derive(Clone)]
pub struct Client {
    base: String,
    token: Option<String>,
    http: reqwest::Client,
}

impl Client {
    pub fn new(url: &str, ca_pem: Option<&str>, token: Option<String>) -> Result<Client, String> {
        let pin = ca_pem.map(Pin::from_pem).transpose()?;
        if url.starts_with("https://") && pin.is_none() {
            return Err("this server has no pinned certificate; pair with it again".into());
        }
        Ok(Client { base: url.trim_end_matches('/').to_string(), token, http: http_client(pin, Arc::default()) })
    }

    pub async fn call(&self, method: reqwest::Method, path: &str, body: Option<Value>) -> Result<Value, String> {
        let mut req = self.http.request(method, format!("{}{}", self.base, path)).header("Origin", ORIGIN);
        if let Some(t) = &self.token {
            req = req.bearer_auth(t);
        }
        if let Some(b) = body {
            req = req.json(&b);
        }
        let resp = req.send().await.map_err(|e| describe(&e))?;
        read(resp).await
    }

    pub async fn get(&self, path: &str) -> Result<Value, String> {
        self.call(reqwest::Method::GET, path, None).await
    }

    pub async fn post(&self, path: &str, body: Value) -> Result<Value, String> {
        self.call(reqwest::Method::POST, path, Some(body)).await
    }

    pub async fn delete(&self, path: &str) -> Result<Value, String> {
        self.call(reqwest::Method::DELETE, path, None).await
    }
}

fn describe(e: &reqwest::Error) -> String {
    let mut text = e.to_string();
    let mut source = std::error::Error::source(e);
    while let Some(s) = source {
        text = s.to_string();
        source = s.source();
    }
    if e.is_connect() || e.is_timeout() {
        format!("the server does not answer ({text})")
    } else {
        text
    }
}

async fn read(resp: reqwest::Response) -> Result<Value, String> {
    let status = resp.status();
    let text = resp.text().await.unwrap_or_default();
    if !status.is_success() {
        let said = serde_json::from_str::<Value>(&text).ok().and_then(|v| v["error"].as_str().map(String::from));
        return Err(said.unwrap_or_else(|| format!("HTTP {}", status.as_u16())));
    }
    Ok(serde_json::from_str(&text).unwrap_or(Value::Null))
}

/// What pairing gives: the server's name, a token, and its certificate.
pub struct Paired {
    pub name: String,
    pub token: String,
    pub ca_pem: Option<String>,
}

#[derive(Deserialize)]
struct Secret {
    token: String,
    name: String,
    #[serde(default)]
    ca_certificate: Option<String>,
}

fn host_of(url: &str) -> String {
    reqwest::Url::parse(url).ok().and_then(|u| u.host_str().map(String::from)).unwrap_or_default()
}

/// Pairs with the server at url by its phrase. Over HTTPS the certificate
/// the server presents is accepted for the two pairing requests and kept;
/// afterwards it must be the certificate the server sealed, or be issued by
/// it, or something between the two answered for the server.
pub async fn pair(url: &str, phrase: &str, device: &str) -> Result<Paired, String> {
    let url = url.trim_end_matches('/');
    let words = pairing::read_phrase(phrase).map_err(|e| e.0)?;
    let exchange = Exchange::start(&pairing::phrase_scalar(&words)).map_err(|e| e.0)?;
    let seen = Arc::new(Mutex::new(None));
    let http = http_client(None, seen.clone());
    let post = |path: &str, body: Value| {
        let req = http.post(format!("{url}{path}")).json(&body);
        async move { read(req.send().await.map_err(|e| describe(&e))?).await }
    };

    let answer = post("/api/pair/exchange", json!({ "share": STANDARD.encode(&exchange.share) })).await?;
    let session = answer["session"].as_str().ok_or("the server's answer has no session")?.to_string();
    let share = STANDARD.decode(answer["share"].as_str().unwrap_or_default()).map_err(|_| "the server's share does not read")?;
    let keys = exchange.finish(&share).map_err(|e| e.0)?;
    let mine = pairing::seal(&keys.key, LABEL_PHONE, json!({ "device": device }).to_string().as_bytes());
    let answer = post(
        "/api/pair/confirm",
        json!({ "session": session, "confirmation": STANDARD.encode(&keys.confirmation), "phone": STANDARD.encode(mine) }),
    )
    .await?;
    let theirs = STANDARD.decode(answer["confirmation"].as_str().unwrap_or_default()).unwrap_or_default();
    if !keys.confirms(&theirs) {
        return Err("The server did not prove it knows the phrase. Something else may have answered for it.".into());
    }
    let sealed = STANDARD.decode(answer["sealed"].as_str().unwrap_or_default()).map_err(|_| "the server's answer does not read")?;
    let secret: Secret = serde_json::from_slice(&pairing::open(&keys.key, LABEL_SERVER, &sealed).map_err(|e| e.0)?)
        .map_err(|_| "the server's sealed answer does not read")?;

    if url.starts_with("https://") {
        let pem = secret.ca_certificate.clone().ok_or("the server sent no certificate to pin")?;
        let pin = Pin::from_pem(&pem)?;
        let presented = seen.lock().unwrap().clone().ok_or("no certificate was presented")?;
        if !pin.trusts(&presented, &host_of(url)) {
            return Err("The certificate the server presented is not the one it sealed. Something between you and the server answered for it.".into());
        }
    }
    Ok(Paired { name: secret.name, token: secret.token, ca_pem: secret.ca_certificate })
}
