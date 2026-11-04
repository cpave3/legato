//! The WebAuthn messages the app sends Legato, built as a browser would
//! build them, so internal/service/approval_passkeys.go checks them as it
//! checks a page's. WebKitGTK has no WebAuthn, so the app is the client: it
//! asks the security key over CTAP2 (see `fido`) and wraps the answers here.
//!
//! Every key the app holds is made once, for the app's own origin, and the
//! same credential is enrolled on each server: a server keeps the origin a
//! passkey was made at and checks it, whichever server it is.

use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use base64::Engine;
use serde_json::{json, Value};
use sha2::{Digest, Sha256};

fn b64url(bytes: &[u8]) -> String {
    URL_SAFE_NO_PAD.encode(bytes)
}

fn quoted(s: &str) -> String {
    serde_json::to_string(s).expect("a string encodes")
}

/// The origin, and relying party, of every key the app makes.
pub const ORIGIN: &str = "https://legato-desktop.invalid";
pub const RP_ID: &str = "legato-desktop.invalid";

/// The client data a browser hashes for the key: type, challenge, origin.
pub fn client_data(kind: &str, challenge: &str, origin: &str) -> String {
    format!(r#"{{"type":{},"challenge":{},"origin":{},"crossOrigin":false}}"#, quoted(kind), quoted(challenge), quoted(origin))
}

/// The message a decision signs, as the phone and the server build it.
pub fn signed_message(challenge: &str, device: &str, option: &str, canonical: &str) -> String {
    format!(r#"{{"challenge":{},"device":{},"option":{},"request":{canonical},"v":1}}"#, quoted(challenge), quoted(device), quoted(option))
}

/// The WebAuthn challenge of a decision: the SHA-256 of its message.
pub fn decision_challenge(message: &str) -> String {
    b64url(&Sha256::digest(message.as_bytes()))
}

/// An attestation object in the "none" format, as a browser sends when the
/// relying party asks for no attestation (Legato does).
pub fn none_attestation(auth_data: &[u8]) -> Vec<u8> {
    // A CBOR map of three: "fmt": "none", "attStmt": {}, "authData": bytes.
    let mut out = vec![0xa3, 0x63];
    out.extend(b"fmt");
    out.push(0x64);
    out.extend(b"none");
    out.push(0x67);
    out.extend(b"attStmt");
    out.push(0xa0);
    out.push(0x68);
    out.extend(b"authData");
    match auth_data.len() {
        n if n < 24 => out.push(0x40 | n as u8),
        n if n < 256 => out.extend([0x58, n as u8]),
        n => out.extend([0x59, (n >> 8) as u8, n as u8]),
    }
    out.extend(auth_data);
    out
}

/// Where a credential lives, as a browser reports it.
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum Attachment {
    /// A security key, plugged in over USB.
    SecurityKey,
    /// Touch ID: built into the Mac.
    TouchId,
}

/// The body of POST /api/devices that enrolls a key's credential.
pub fn registration(name: &str, credential_id: &[u8], auth_data: &[u8], client_data: &str, attachment: Attachment) -> Value {
    let id = b64url(credential_id);
    let (attached, transport) = match attachment {
        Attachment::SecurityKey => ("cross-platform", "usb"),
        Attachment::TouchId => ("platform", "internal"),
    };
    json!({
        "kind": "passkey", "name": name, "origin": ORIGIN,
        "credential": {
            "id": id, "rawId": id, "type": "public-key", "authenticatorAttachment": attached,
            "response": {
                "clientDataJSON": b64url(client_data.as_bytes()),
                "attestationObject": b64url(&none_attestation(auth_data)),
                "transports": [transport],
            },
            "clientExtensionResults": {},
        },
    })
}

/// The signature value of a decision: the assertion, as JSON text.
pub fn assertion(credential_id: &[u8], auth_data: &[u8], signature: &[u8], client_data: &str) -> String {
    let id = b64url(credential_id);
    json!({
        "id": id, "rawId": id, "type": "public-key",
        "response": {
            "clientDataJSON": b64url(client_data.as_bytes()),
            "authenticatorData": b64url(auth_data),
            "signature": b64url(signature),
        },
        "clientExtensionResults": {},
    })
    .to_string()
}

/// What Legato said when it refused a signature, and what it most likely
/// means when the key signed with a touch alone: Legato reports a missing
/// PIN or fingerprint only as "Error validating the authenticator
/// response", and a Legato from before touch-only signing requires one.
pub fn explain_refusal(said: &str, verified: bool) -> String {
    if verified || !said.contains("Error validating the authenticator response") {
        return said.to_string();
    }
    format!("{said}. The key signed with a touch alone: this server may run a Legato that also wants a PIN or fingerprint. Restart it on a newer build (update it) to sign with a touch.")
}

#[cfg(test)]
mod tests {
    use super::*;
    use base64::engine::general_purpose::URL_SAFE_NO_PAD;
    use base64::Engine;

    #[test]
    fn client_data_has_the_fields_a_browser_sends_in_its_order() {
        let cd = client_data("webauthn.get", "Y2hhbGxlbmdl", ORIGIN);
        assert_eq!(cd, r#"{"type":"webauthn.get","challenge":"Y2hhbGxlbmdl","origin":"https://legato-desktop.invalid","crossOrigin":false}"#);
    }

    // The same text internal/service/approval_devices.go signedMessage makes.
    #[test]
    fn the_signed_message_is_the_phones() {
        let m = signed_message("ch4l", "dev-1", "once", r#"{"approval":"ap-1","v":1}"#);
        assert_eq!(m, r#"{"challenge":"ch4l","device":"dev-1","option":"once","request":{"approval":"ap-1","v":1},"v":1}"#);
        // SHA-256 of "abc", base64url without padding.
        assert_eq!(decision_challenge("abc"), "ungWv48Bz-pBQUDeXa4iI7ADYaOWF3qctBD_YfIAFa0");
    }

    #[test]
    fn a_none_attestation_holds_the_authenticator_data() {
        for len in [10usize, 164, 300] {
            let auth: Vec<u8> = (0..len).map(|i| i as u8).collect();
            let v: ciborium::Value = ciborium::from_reader(&none_attestation(&auth)[..]).unwrap();
            let map = v.as_map().unwrap();
            let get = |k: &str| map.iter().find(|(key, _)| key.as_text() == Some(k)).map(|(_, v)| v.clone()).unwrap();
            assert_eq!(get("fmt").as_text(), Some("none"));
            assert!(get("attStmt").as_map().unwrap().is_empty());
            assert_eq!(get("authData").as_bytes().unwrap(), &auth, "{len} bytes");
        }
    }

    #[test]
    fn a_registration_is_a_passkey_at_the_apps_origin() {
        let body = registration("YubiKey 1", &[1, 2, 3], &[9; 40], "{}", Attachment::SecurityKey);
        assert_eq!(body["kind"], "passkey");
        assert_eq!(body["name"], "YubiKey 1");
        assert_eq!(body["origin"], ORIGIN);
        let c = &body["credential"];
        assert_eq!(c["id"], "AQID");
        assert_eq!(c["rawId"], "AQID");
        assert_eq!(c["type"], "public-key");
        assert_eq!(c["response"]["clientDataJSON"], URL_SAFE_NO_PAD.encode("{}"));
        let att = URL_SAFE_NO_PAD.decode(c["response"]["attestationObject"].as_str().unwrap()).unwrap();
        assert_eq!(att, none_attestation(&[9; 40]));
    }

    #[test]
    fn a_security_key_is_registered_as_a_usb_key_and_touch_id_as_built_in() {
        let usb = registration("YubiKey 1", &[1], &[9; 40], "{}", Attachment::SecurityKey);
        assert_eq!(usb["credential"]["authenticatorAttachment"], "cross-platform");
        assert_eq!(usb["credential"]["response"]["transports"], json!(["usb"]));
        let touch = registration("Touch ID", &[1], &[9; 40], "{}", Attachment::TouchId);
        assert_eq!(touch["credential"]["authenticatorAttachment"], "platform");
        assert_eq!(touch["credential"]["response"]["transports"], json!(["internal"]));
    }

    // go-webauthn reports a missing user verification only as its
    // verification error; a Legato from before touch-only signing requires it.
    #[test]
    fn a_touch_alone_refused_by_a_server_says_the_server_may_want_more() {
        let said = "the decision's signature does not hold: the passkey's signature is not over this request, option and challenge, from its page: Error validating the authenticator response";
        let explained = explain_refusal(said, false);
        assert!(explained.starts_with(said), "keeps what the server said");
        assert!(explained.contains("update it"), "{explained}");
        assert_eq!(explain_refusal(said, true), said, "a verified signature was refused for another reason");
        assert_eq!(explain_refusal("the server does not answer", false), "the server does not answer");
    }

    #[test]
    fn an_assertion_carries_the_keys_answer() {
        let v: Value = serde_json::from_str(&assertion(&[1, 2, 3], &[7; 37], &[5; 70], "{}")).unwrap();
        assert_eq!(v["id"], "AQID");
        assert_eq!(v["response"]["authenticatorData"], URL_SAFE_NO_PAD.encode([7u8; 37]));
        assert_eq!(v["response"]["signature"], URL_SAFE_NO_PAD.encode([5u8; 70]));
        assert_eq!(v["response"]["clientDataJSON"], URL_SAFE_NO_PAD.encode("{}"));
    }
}
