//! Pairing by phrase, as the server's internal/engine/pairing does it:
//! SPAKE2 (RFC 9382, the P-256/SHA-256/HKDF/HMAC suite) on four words, then
//! the server's token, name and certificate sealed with the agreed key.
//! The app is party A, as the phone is; tests/fixtures/pairing-exchange.json
//! (written by `go test ./internal/engine/pairing -update`) holds both sides
//! to the same values.

// aes-gcm 0.10 and p256 0.13 take keys and nonces through generic-array
// 0.14's from_slice, which it now marks deprecated.
#![allow(deprecated)]

use std::collections::HashMap;
use std::fmt;
use std::sync::OnceLock;

use aes_gcm::aead::{Aead, KeyInit};
use aes_gcm::{Aes256Gcm, Nonce};
use hkdf::Hkdf;
use hmac::{Hmac, Mac};
use num_bigint::BigUint;
use p256::elliptic_curve::group::Group;
use p256::elliptic_curve::sec1::{FromEncodedPoint, ToEncodedPoint};
use p256::elliptic_curve::{Field, PrimeField};
use p256::{AffinePoint, EncodedPoint, FieldBytes, ProjectivePoint, Scalar};
use rand::RngCore;
use sha2::{Digest, Sha256};

pub const LABEL_PHONE: &str = "legato pair phone";
pub const LABEL_SERVER: &str = "legato pair server";
pub const WORDS: usize = 4;

// The protocol's identities and associated data, as the server has them.
const IDENTITY_A: &[u8] = b"legato-mobile";
const IDENTITY_B: &[u8] = b"legato-server";
const AAD: &[u8] = b"legato-pair-v1";

// RFC 9382's M and N for P-256.
const POINT_M: &str = "02886e2f97ace46e55ba9dd7242579f2993b64e16ef3dcab95afd497333d8fa12f";
const POINT_N: &str = "03d8bbd6c639c62937b04d997f38c3770719c629d7014d49a24b4f98baa1292b49";
const ORDER: &str = "ffffffff00000000ffffffffffffffffbce6faada7179e84f3b9cac2fc632551";

#[derive(Debug, PartialEq, Eq)]
pub struct PairingError(pub String);

impl fmt::Display for PairingError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

impl std::error::Error for PairingError {}

fn err(msg: &str) -> PairingError {
    PairingError(msg.to_string())
}

fn point(bytes: &[u8]) -> Result<ProjectivePoint, PairingError> {
    let encoded = EncodedPoint::from_bytes(bytes).map_err(|_| err("not a point"))?;
    Option::<AffinePoint>::from(AffinePoint::from_encoded_point(&encoded))
        .map(ProjectivePoint::from)
        .ok_or_else(|| err("not a point"))
}

fn constant_point(hex_point: &str) -> ProjectivePoint {
    point(&hex::decode(hex_point).unwrap()).unwrap()
}

fn scalar(bytes: &[u8; 32]) -> Option<Scalar> {
    Option::from(Scalar::from_repr(*FieldBytes::from_slice(bytes)))
}

fn encode(p: &ProjectivePoint) -> Vec<u8> {
    p.to_affine().to_encoded_point(false).as_bytes().to_vec()
}

fn hkdf(ikm: &[u8], salt: Option<&[u8]>, info: &[u8], length: usize) -> Vec<u8> {
    let mut out = vec![0u8; length];
    Hkdf::<Sha256>::new(salt, ikm).expand(info, &mut out).expect("HKDF length");
    out
}

fn hmac(key: &[u8], message: &[u8]) -> Vec<u8> {
    let mut mac = <Hmac<Sha256> as Mac>::new_from_slice(key).expect("HMAC key");
    mac.update(message);
    mac.finalize().into_bytes().to_vec()
}

/// Each part with its length as 8 bytes, little-endian, before it.
fn transcript(parts: &[&[u8]]) -> Vec<u8> {
    let mut out = Vec::new();
    for part in parts {
        out.extend((part.len() as u64).to_le_bytes());
        out.extend(*part);
    }
    out
}

/// The password scalar of a phrase's words.
pub fn phrase_scalar(words: &[&str]) -> [u8; 32] {
    let wide = hkdf(words.join(" ").as_bytes(), Some(b"legato pair v1"), b"spake2 w", 40);
    let order = BigUint::parse_bytes(ORDER.as_bytes(), 16).unwrap();
    let reduced = (BigUint::from_bytes_be(&wide) % order).to_bytes_be();
    let mut out = [0u8; 32];
    out[32 - reduced.len()..].copy_from_slice(&reduced);
    out
}

/// Party A's side of one exchange.
pub struct Exchange {
    w: Scalar,
    w_bytes: [u8; 32],
    x: Scalar,
    pub share: Vec<u8>,
}

/// The keys an exchange agrees: the key that seals, this side's
/// confirmation, and the one the server must send.
pub struct Keys {
    pub key: Vec<u8>,
    pub confirmation: Vec<u8>,
    peer_confirmation: Vec<u8>,
}

impl Keys {
    /// Whether the server's confirmation is the one this exchange expects,
    /// compared in constant time.
    pub fn confirms(&self, peer: &[u8]) -> bool {
        peer.len() == self.peer_confirmation.len()
            && peer.iter().zip(&self.peer_confirmation).fold(0u8, |acc, (a, b)| acc | (a ^ b)) == 0
    }
}

impl Exchange {
    /// Starts an exchange with the password scalar w and the secret x.
    pub fn start_with(w: &[u8; 32], x: &[u8; 32]) -> Result<Exchange, PairingError> {
        let ws = scalar(w).ok_or_else(|| err("the password scalar is not below the group order"))?;
        let xs = scalar(x).filter(|s| !bool::from(s.is_zero())).ok_or_else(|| err("not a usable secret"))?;
        let share = encode(&(ProjectivePoint::GENERATOR * xs + constant_point(POINT_M) * ws));
        Ok(Exchange { w: ws, w_bytes: *w, x: xs, share })
    }

    /// Starts an exchange with a random secret.
    pub fn start(w: &[u8; 32]) -> Result<Exchange, PairingError> {
        let mut x = [0u8; 32];
        for _ in 0..64 {
            rand::rngs::OsRng.fill_bytes(&mut x);
            if let Ok(e) = Exchange::start_with(w, &x) {
                return Ok(e);
            }
        }
        Err(err("no usable random secret"))
    }

    /// Finishes with the server's share.
    pub fn finish(&self, peer_share: &[u8]) -> Result<Keys, PairingError> {
        if peer_share.len() != 65 {
            return Err(err("the server's share is not a point"));
        }
        let peer = point(peer_share).map_err(|_| err("the server's share is not a point"))?;
        let k = (peer - constant_point(POINT_N) * self.w) * self.x;
        if bool::from(k.is_identity()) {
            return Err(err("the exchange gave no key"));
        }
        let tt = transcript(&[IDENTITY_A, IDENTITY_B, &self.share, peer_share, &encode(&k), &self.w_bytes]);
        let digest = Sha256::digest(&tt);
        let (ke, ka) = digest.split_at(16);
        let info = [b"ConfirmationKeys".as_slice(), AAD].concat();
        let kc = hkdf(ka, None, &info, 32);
        Ok(Keys { key: ke.to_vec(), confirmation: hmac(&kc[..16], &tt), peer_confirmation: hmac(&kc[16..], &tt) })
    }
}

fn cipher(key: &[u8], label: &str) -> Aes256Gcm {
    Aes256Gcm::new_from_slice(&hkdf(key, None, label.as_bytes(), 32)).expect("AES key")
}

/// AES-256-GCM under a key derived from the exchange's key and the label;
/// the nonce first.
pub fn seal_with(key: &[u8], label: &str, plaintext: &[u8], nonce: [u8; 12]) -> Vec<u8> {
    let mut out = nonce.to_vec();
    out.extend(cipher(key, label).encrypt(Nonce::from_slice(&nonce), plaintext).expect("AES-GCM"));
    out
}

pub fn seal(key: &[u8], label: &str, plaintext: &[u8]) -> Vec<u8> {
    let mut nonce = [0u8; 12];
    rand::rngs::OsRng.fill_bytes(&mut nonce);
    seal_with(key, label, plaintext, nonce)
}

pub fn open(key: &[u8], label: &str, sealed: &[u8]) -> Result<Vec<u8>, PairingError> {
    if sealed.len() < 28 {
        return Err(err("sealed text is too short"));
    }
    cipher(key, label)
        .decrypt(Nonce::from_slice(&sealed[..12]), &sealed[12..])
        .map_err(|_| err("sealed text does not open"))
}

/// The EFF short word list 2.0 (CC BY 3.0 US), as the server draws phrases.
pub fn words() -> Vec<&'static str> {
    include_str!("../resources/pairing-words.txt").lines().filter(|l| !l.trim().is_empty()).collect()
}

fn by_prefix() -> &'static HashMap<String, &'static str> {
    static MAP: OnceLock<HashMap<String, &'static str>> = OnceLock::new();
    MAP.get_or_init(|| words().into_iter().map(|w| (w[..3].to_string(), w)).collect())
}

/// The four list words a typed phrase stands for: each word by its first
/// three letters, case and punctuation ignored.
pub fn read_phrase(text: &str) -> Result<Vec<&'static str>, PairingError> {
    let typed: Vec<String> = text
        .split(|c: char| !c.is_ascii_alphabetic())
        .filter(|t| !t.is_empty())
        .map(|t| t.to_ascii_lowercase())
        .collect();
    if typed.len() != WORDS {
        return Err(PairingError(format!("A phrase is {WORDS} words; this is {}.", typed.len())));
    }
    typed
        .iter()
        .map(|t| {
            if t.len() < 3 {
                return Err(PairingError(format!("\"{t}\" is too short to be a phrase word.")));
            }
            by_prefix().get(&t[..3]).copied().ok_or_else(|| PairingError(format!("\"{t}\" is not a phrase word.")))
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::Value;
    use sha2::{Digest, Sha256};

    fn fixture() -> Value {
        serde_json::from_str(include_str!("../tests/fixtures/pairing-exchange.json")).unwrap()
    }

    fn hexed(v: &Value, k: &str) -> Vec<u8> {
        hex::decode(v[k].as_str().unwrap()).unwrap()
    }

    #[test]
    fn the_phrase_gives_the_servers_password_scalar() {
        let f = fixture();
        let words: Vec<&str> = f["phrase"].as_str().unwrap().split(' ').collect();
        assert_eq!(phrase_scalar(&words).to_vec(), hexed(&f, "w"));
    }

    #[test]
    fn the_exchange_reaches_the_servers_key_and_confirmations() {
        let f = fixture();
        let w: [u8; 32] = hexed(&f, "w").try_into().unwrap();
        let x: [u8; 32] = hexed(&f, "x").try_into().unwrap();
        let a = Exchange::start_with(&w, &x).unwrap();
        assert_eq!(a.share, hexed(&f, "share_a"));
        let keys = a.finish(&hexed(&f, "share_b")).unwrap();
        assert_eq!(keys.key, hexed(&f, "key"));
        assert_eq!(keys.confirmation, hexed(&f, "confirm_a"));
        assert!(keys.confirms(&hexed(&f, "confirm_b")));
        assert!(!keys.confirms(&hexed(&f, "confirm_a")));
    }

    #[test]
    fn a_share_that_is_not_a_point_is_refused() {
        let f = fixture();
        let w: [u8; 32] = hexed(&f, "w").try_into().unwrap();
        let a = Exchange::start(&w).unwrap();
        assert!(a.finish(&[4u8; 65]).is_err());
        assert!(a.finish(&[]).is_err());
    }

    #[test]
    fn sealing_matches_the_server_both_ways() {
        let f = fixture();
        let key = hexed(&f, "key");
        let sealed = seal_with(&key, LABEL_PHONE, f["phone_plaintext"].as_str().unwrap().as_bytes(), [1; 12]);
        assert_eq!(sealed, hexed(&f, "phone_sealed"));
        let opened = open(&key, LABEL_SERVER, &hexed(&f, "server_sealed")).unwrap();
        assert_eq!(opened, f["server_plaintext"].as_str().unwrap().as_bytes());
        // The wrong label, or a changed byte, does not open.
        assert!(open(&key, LABEL_PHONE, &hexed(&f, "server_sealed")).is_err());
        let mut changed = hexed(&f, "server_sealed");
        *changed.last_mut().unwrap() ^= 1;
        assert!(open(&key, LABEL_SERVER, &changed).is_err());
        // A random nonce still opens.
        assert_eq!(open(&key, LABEL_PHONE, &seal(&key, LABEL_PHONE, b"hi")).unwrap(), b"hi");
    }

    // internal/engine/pairing/phrase_test.go holds the server's copy to this.
    #[test]
    fn the_word_list_is_the_shared_one() {
        let joined = words().join("\n") + "\n";
        assert_eq!(hex::encode(Sha256::digest(joined.as_bytes())), "79fb63e0b81dfda90fea676b7080a352ce560178360dce20a18e8f49f7570dcf");
        assert_eq!(words().len(), 1296);
    }

    #[test]
    fn a_typed_phrase_reads_by_first_three_letters() {
        assert_eq!(read_phrase("Riverboat canis-GLEEFUL  nul").unwrap(), vec!["riverboat", "canister", "gleeful", "nullify"]);
        assert!(read_phrase("riverboat canister gleeful").is_err(), "three words");
        assert!(read_phrase("riverboat canister gleeful nullify nullify").is_err(), "five words");
        assert!(read_phrase("riverboat canister gleeful zzzz").is_err(), "not a list word");
        assert!(read_phrase("riverboat canister gleeful nu").is_err(), "too short to tell");
    }
}
