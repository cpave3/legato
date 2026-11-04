//! The security keys plugged into this computer, over CTAP2. A key makes a
//! credential once (it may want its PIN for that), and signs with a touch.
//! Several keys may be plugged in at once: the one that holds a credential
//! is found by asking each without a touch.

use ctap_hid_fido2::fidokey::{GetAssertionArgsBuilder, MakeCredentialArgsBuilder};
use ctap_hid_fido2::public_key_credential_user_entity::PublicKeyCredentialUserEntity;
use ctap_hid_fido2::{Cfg, FidoKeyHid, FidoKeyHidFactory, HidParam};

fn cfg() -> Cfg {
    let mut c = Cfg::init();
    c.enable_keep_alive_msg = false;
    c
}

fn plugged() -> Vec<HidParam> {
    ctap_hid_fido2::get_fidokey_devices().into_iter().map(|d| d.param).collect()
}

fn open(param: &HidParam) -> Result<FidoKeyHid, String> {
    FidoKeyHidFactory::create_by_params(&[param.clone()], &cfg()).map_err(|e| format!("the key would not open: {e}"))
}

/// Why a key refused, in words; "PIN" in it means the key wants its PIN.
fn said(e: impl std::fmt::Display) -> String {
    let text = e.to_string();
    if text.contains("0x36") || text.contains("PIN_REQUIRED") || text.contains("PinRequired") {
        "The key wants its PIN to make a credential.".into()
    } else if text.contains("0x31") || text.contains("PIN_INVALID") {
        "That PIN is not the key's. Each wrong PIN uses up one of its tries.".into()
    } else if text.contains("0x32") || text.contains("PIN_BLOCKED") {
        "The key's PIN is blocked after too many wrong tries.".into()
    } else if text.contains("0x2f") || text.contains("0x27") || text.contains("TIMEOUT") || text.contains("NOT_ALLOWED") {
        "The key was not touched in time.".into()
    } else {
        text
    }
}

/// Makes a credential on the one plugged-in key: a touch, and the PIN when
/// given (some keys insist on it to make one).
pub fn make_credential(rp_id: &str, client_data: &str, user_id: &[u8], user_name: &str, pin: Option<&str>) -> Result<(Vec<u8>, Vec<u8>), String> {
    let keys = plugged();
    let param = match keys.as_slice() {
        [] => return Err("No security key is plugged in.".into()),
        [one] => one.clone(),
        _ => return Err("More than one security key is plugged in. Leave in only the one to add.".into()),
    };
    let user = PublicKeyCredentialUserEntity::new(Some(user_id), Some(user_name), Some(user_name));
    // The library hashes what it is given: the client data, as WebAuthn does.
    let builder = MakeCredentialArgsBuilder::new(rp_id, client_data.as_bytes()).user_entity(&user);
    let args = match pin {
        Some(p) => builder.pin(p).build(),
        None => builder.without_pin_and_uv().build(),
    };
    let made = open(&param)?.make_credential_with_args(&args).map_err(said)?;
    Ok((made.credential_descriptor.id, made.auth_data))
}

/// What a key gave for a signature.
pub struct Signed {
    pub auth_data: Vec<u8>,
    pub signature: Vec<u8>,
    pub verified: bool,
    pub counter: u32,
}

/// Which plugged-in key holds credential, asked without a touch.
fn holder(rp_id: &str, credential: &[u8]) -> Option<HidParam> {
    plugged().into_iter().find(|param| {
        let Ok(key) = open(param) else { return false };
        let args = GetAssertionArgsBuilder::new(rp_id, b"which key").credential_id(credential).without_pin_and_uv().without_up().build();
        key.get_assertion_with_args(&args).is_ok()
    })
}

/// Which of credentials the plugged-in keys hold.
pub fn held(rp_id: &str, credentials: &[Vec<u8>]) -> Vec<Vec<u8>> {
    credentials.iter().filter(|c| holder(rp_id, c).is_some()).cloned().collect()
}

/// Signs client_data with the plugged-in key that holds credential: a touch.
pub fn sign(rp_id: &str, client_data: &str, credential: &[u8]) -> Result<Signed, String> {
    let param = holder(rp_id, credential).ok_or("The key for this server is not plugged in.")?;
    let args = GetAssertionArgsBuilder::new(rp_id, client_data.as_bytes()).credential_id(credential).without_pin_and_uv().build();
    let a = open(&param)?.get_assertion_with_args(&args).map_err(said)?.into_iter().next().ok_or("the key gave no signature")?;
    Ok(Signed { auth_data: a.auth_data, signature: a.signature, verified: a.flags.user_verified_result, counter: a.sign_count })
}
