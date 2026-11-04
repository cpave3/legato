//! Touch ID as a signer, on a Mac. The app is the authenticator: a key in
//! the Secure Enclave that signs only after a fingerprint, wrapped in the
//! WebAuthn messages a platform authenticator gives, for the app's own
//! relying party. To Legato it is one more passkey, enrolled with a "none"
//! attestation as a security key's credential is.
//!
//! The Secure Enclave is behind `Enclave`, so everything here but the chip
//! itself is checked on any machine.

use sha2::{Digest, Sha256};

use crate::fido::Signed;
use crate::webauthn::RP_ID;

/// A Secure Enclave, or a stand-in for one in tests.
pub trait Enclave: Send + Sync {
    /// Makes a P-256 key that signs only after a fingerprint: its blob (the
    /// key wrapped so only this Mac's chip can use it) and its public key,
    /// SEC1 uncompressed (65 bytes).
    fn create(&self) -> Result<(Vec<u8>, Vec<u8>), String>;
    /// Asks for a fingerprint, naming reason in the dialog, and signs data:
    /// ECDSA P-256 over its SHA-256, DER encoded.
    fn sign(&self, blob: &[u8], data: &[u8], reason: &str) -> Result<Vec<u8>, String>;
}

/// A Touch ID key, made: what a server's enrolment carries, and the blob.
pub struct Made {
    pub credential_id: Vec<u8>,
    pub auth_data: Vec<u8>,
    pub blob: Vec<u8>,
}

// Authenticator data flags (WebAuthn § 6.1).
const USER_PRESENT: u8 = 0x01;
const USER_VERIFIED: u8 = 0x04;
const ATTESTED: u8 = 0x40;

/// rpIdHash, flags and a signature counter of 0: the chip keeps none, and
/// WebAuthn takes 0 as "no counter".
fn header(flags: u8) -> Vec<u8> {
    let mut out = Sha256::digest(RP_ID.as_bytes()).to_vec();
    out.push(flags);
    out.extend([0, 0, 0, 0]);
    out
}

/// A P-256 public key as a COSE_Key, its entries in CTAP2's canonical order.
fn cose_key(public: &[u8]) -> Result<Vec<u8>, String> {
    if public.len() != 65 || public[0] != 0x04 {
        return Err("the Secure Enclave gave a public key that is not an uncompressed P-256 point".into());
    }
    let mut out = vec![0xa5, 0x01, 0x02, 0x03, 0x26, 0x20, 0x01, 0x21, 0x58, 0x20];
    out.extend(&public[1..33]);
    out.extend([0x22, 0x58, 0x20]);
    out.extend(&public[33..]);
    Ok(out)
}

/// Makes a Touch ID key. No fingerprint is asked for: the chip only makes
/// the key. Its credential ID is the SHA-256 of its public key.
pub fn make(enclave: &dyn Enclave) -> Result<Made, String> {
    let (blob, public) = enclave.create()?;
    let credential_id = Sha256::digest(&public).to_vec();
    let mut auth_data = header(USER_PRESENT | USER_VERIFIED | ATTESTED);
    auth_data.extend([0u8; 16]);
    auth_data.extend((credential_id.len() as u16).to_be_bytes());
    auth_data.extend(&credential_id);
    auth_data.extend(cose_key(&public)?);
    Ok(Made { credential_id, auth_data, blob })
}

/// Signs client data with a Touch ID key: the dialog names reason, and the
/// key signs only after a fingerprint, so the person was both present and
/// verified.
pub fn sign(enclave: &dyn Enclave, blob: &[u8], client_data: &str, reason: &str) -> Result<Signed, String> {
    let auth_data = header(USER_PRESENT | USER_VERIFIED);
    let mut data = auth_data.clone();
    data.extend(Sha256::digest(client_data.as_bytes()));
    let signature = enclave.sign(blob, &data, reason)?;
    Ok(Signed { auth_data, signature, verified: true, counter: 0 })
}

/// This Mac's Secure Enclave, when it has one and Touch ID is set up; else
/// why not, in words.
pub fn this_mac() -> Result<Box<dyn Enclave>, String> {
    #[cfg(target_os = "macos")]
    {
        mac::available()?;
        Ok(Box::new(mac::SecureEnclave))
    }
    #[cfg(not(target_os = "macos"))]
    {
        Err("Touch ID is only on a Mac.".into())
    }
}

/// The Swift in touchid/, which build.rs links on a Mac.
#[cfg(target_os = "macos")]
mod mac {
    use std::ffi::{c_char, CStr, CString};

    extern "C" {
        fn legato_touchid_available(err: *mut u8, err_cap: isize) -> i32;
        fn legato_touchid_create(blob: *mut u8, blob_cap: isize, blob_len: *mut isize, public: *mut u8, err: *mut u8, err_cap: isize) -> i32;
        #[allow(clippy::too_many_arguments)]
        fn legato_touchid_sign(
            blob: *const u8,
            blob_len: isize,
            data: *const u8,
            data_len: isize,
            reason: *const c_char,
            sig: *mut u8,
            sig_cap: isize,
            sig_len: *mut isize,
            err: *mut u8,
            err_cap: isize,
        ) -> i32;
    }

    const CANCELLED: i32 = 2;

    fn said(code: i32, err: &[u8]) -> String {
        if code == CANCELLED {
            return "Touch ID was cancelled.".into();
        }
        CStr::from_bytes_until_nul(err).map(|c| c.to_string_lossy().into_owned()).unwrap_or_else(|_| "Touch ID failed.".into())
    }

    pub fn available() -> Result<(), String> {
        let mut err = [0u8; 512];
        match unsafe { legato_touchid_available(err.as_mut_ptr(), err.len() as isize) } {
            0 => Ok(()),
            code => Err(said(code, &err)),
        }
    }

    pub struct SecureEnclave;

    impl super::Enclave for SecureEnclave {
        fn create(&self) -> Result<(Vec<u8>, Vec<u8>), String> {
            let (mut blob, mut public, mut err) = ([0u8; 4096], [0u8; 65], [0u8; 512]);
            let mut len: isize = 0;
            let code = unsafe { legato_touchid_create(blob.as_mut_ptr(), blob.len() as isize, &mut len, public.as_mut_ptr(), err.as_mut_ptr(), err.len() as isize) };
            if code != 0 {
                return Err(said(code, &err));
            }
            Ok((blob[..len as usize].to_vec(), public.to_vec()))
        }

        fn sign(&self, blob: &[u8], data: &[u8], reason: &str) -> Result<Vec<u8>, String> {
            let reason = CString::new(reason.replace('\0', "")).expect("no NUL is left");
            let (mut sig, mut err) = ([0u8; 128], [0u8; 512]);
            let mut len: isize = 0;
            let code = unsafe {
                legato_touchid_sign(
                    blob.as_ptr(),
                    blob.len() as isize,
                    data.as_ptr(),
                    data.len() as isize,
                    reason.as_ptr(),
                    sig.as_mut_ptr(),
                    sig.len() as isize,
                    &mut len,
                    err.as_mut_ptr(),
                    err.len() as isize,
                )
            };
            if code != 0 {
                return Err(said(code, &err));
            }
            Ok(sig[..len as usize].to_vec())
        }
    }
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use p256::ecdsa::SigningKey;
    use sha2::{Digest, Sha256};

    /// A Secure Enclave in software: the blob is the private key.
    pub struct SoftEnclave(pub Option<[u8; 32]>);

    impl Enclave for SoftEnclave {
        fn create(&self) -> Result<(Vec<u8>, Vec<u8>), String> {
            let key = match self.0 {
                Some(bytes) => SigningKey::from_slice(&bytes).unwrap(),
                None => SigningKey::random(&mut rand::rngs::OsRng),
            };
            let public = key.verifying_key().to_encoded_point(false).as_bytes().to_vec();
            Ok((key.to_bytes().to_vec(), public))
        }

        fn sign(&self, blob: &[u8], data: &[u8], _reason: &str) -> Result<Vec<u8>, String> {
            use p256::ecdsa::signature::Signer;
            let sig: p256::ecdsa::Signature = SigningKey::from_slice(blob).unwrap().sign(data);
            Ok(sig.to_der().as_bytes().to_vec())
        }
    }

    // WebAuthn § 6.1: rpIdHash (32), flags (1), signCount (4), then the
    // attested credential data: AAGUID (16), credential ID length (2, big
    // endian), credential ID, and the public key as a COSE_Key.
    #[test]
    fn a_touch_id_key_is_enrolled_as_a_verified_platform_credential_for_the_apps_relying_party() {
        let enclave = SoftEnclave(None);
        let made = make(&enclave).unwrap();
        let a = &made.auth_data;

        assert_eq!(&a[..32], &Sha256::digest(b"legato-desktop.invalid")[..], "rpIdHash");
        assert_eq!(a[32], 0x45, "flags: user present, user verified, attested credential data");
        assert_eq!(&a[33..37], &[0, 0, 0, 0], "no signature counter");
        assert_eq!(&a[37..53], &[0u8; 16], "AAGUID");
        let len = u16::from_be_bytes([a[53], a[54]]) as usize;
        assert_eq!(&a[55..55 + len], made.credential_id.as_slice());
        assert!(len >= 16, "a credential ID of {len} bytes");

        let cose: ciborium::Value = ciborium::from_reader(&a[55 + len..]).unwrap();
        let entries: Vec<(i128, ciborium::Value)> =
            cose.as_map().unwrap().iter().map(|(k, v)| (k.as_integer().unwrap().into(), v.clone())).collect();
        let get = |k: i128| entries.iter().find(|(key, _)| *key == k).map(|(_, v)| v.clone()).unwrap();
        assert_eq!(i128::from(get(1).as_integer().unwrap()), 2, "kty: EC2");
        assert_eq!(i128::from(get(3).as_integer().unwrap()), -7, "alg: ES256");
        assert_eq!(i128::from(get(-1).as_integer().unwrap()), 1, "crv: P-256");
        let public = SigningKey::from_slice(&made.blob).unwrap().verifying_key().to_encoded_point(false);
        assert_eq!(get(-2).as_bytes().unwrap().as_slice(), &public.x().unwrap()[..]);
        assert_eq!(get(-3).as_bytes().unwrap().as_slice(), &public.y().unwrap()[..]);
        assert_eq!(entries.len(), 5);
    }

    // WebAuthn § 6.3.3: the signature is over the authenticator data and
    // the SHA-256 of the client data.
    #[test]
    fn a_touch_id_signature_is_over_the_authenticator_data_and_the_client_datas_hash() {
        use p256::ecdsa::signature::Verifier;
        let enclave = SoftEnclave(None);
        let made = make(&enclave).unwrap();
        let client_data = r#"{"type":"webauthn.get","challenge":"abc","origin":"https://legato-desktop.invalid","crossOrigin":false}"#;

        let signed = sign(&enclave, &made.blob, client_data, "sign an approval").unwrap();

        let a = &signed.auth_data;
        assert_eq!(a.len(), 37, "no attested credential data in an assertion");
        assert_eq!(&a[..32], &Sha256::digest(b"legato-desktop.invalid")[..]);
        assert_eq!(a[32], 0x05, "flags: user present, user verified");
        assert!(signed.verified);
        assert_eq!(signed.counter, 0);

        let mut data = a.clone();
        data.extend(Sha256::digest(client_data.as_bytes()));
        let key = SigningKey::from_slice(&made.blob).unwrap();
        let sig = p256::ecdsa::Signature::from_der(&signed.signature).unwrap();
        assert!(key.verifying_key().verify(&data, &sig).is_ok());
    }

    // internal/service's TestADesktopTouchIDKeyEnrollsAndSignsAsVerified
    // enrolls and signs with these bytes through Legato's own checks.
    #[test]
    fn the_recorded_touch_id_key_is_what_the_app_builds() {
        use base64::engine::general_purpose::URL_SAFE_NO_PAD;
        use base64::Engine;
        let private: [u8; 32] = std::array::from_fn(|i| i as u8 + 1);
        let enclave = SoftEnclave(Some(private));
        let made = make(&enclave).unwrap();
        let signed = sign(&enclave, &made.blob, "{}", "").unwrap();
        let built = serde_json::json!({
            "private_key": hex::encode(private),
            "credential_id": URL_SAFE_NO_PAD.encode(&made.credential_id),
            "made_auth_data": URL_SAFE_NO_PAD.encode(&made.auth_data),
            "signed_auth_data": URL_SAFE_NO_PAD.encode(&signed.auth_data),
        });
        let path = concat!(env!("CARGO_MANIFEST_DIR"), "/tests/fixtures/touch-id-key.json");
        if std::env::var_os("LEGATO_UPDATE_FIXTURES").is_some() {
            std::fs::write(path, serde_json::to_string_pretty(&built).unwrap() + "\n").unwrap();
        }
        let recorded: serde_json::Value = serde_json::from_slice(&std::fs::read(path).expect("no recording: run with LEGATO_UPDATE_FIXTURES=1")).unwrap();
        assert_eq!(recorded, built, "the app builds other bytes than recorded: run with LEGATO_UPDATE_FIXTURES=1, then go test ./internal/service");
    }
}
