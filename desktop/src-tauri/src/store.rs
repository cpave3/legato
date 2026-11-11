//! What the app keeps: the servers it paired with (address, name, pinned
//! certificate, and which of its keys each has enrolled) and the security
//! keys it holds, in one JSON file readable only by the person. A server's
//! token is not in that file: it is in the desktop's keyring.

use std::collections::BTreeMap;
use std::path::PathBuf;

use rand::RngCore;
use serde::{Deserialize, Serialize};

fn new_id(prefix: &str) -> String {
    let mut b = [0u8; 5];
    rand::rngs::OsRng.fill_bytes(&mut b);
    format!("{prefix}-{}", hex::encode(b))
}

#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct Server {
    pub id: String,
    pub name: String,
    pub url: String,
    /// The certificate the server sealed when it paired, as PEM.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub ca_pem: Option<String>,
    /// The server's tint, as /api/meta last gave it.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub tint: Option<String>,
    /// The device each key is enrolled as on this server, by key ID.
    #[serde(default)]
    pub devices: BTreeMap<String, String>,
}

/// A key's credential, made once for the app's origin: a security key's,
/// or a Touch ID key's (see `touchid`).
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct Key {
    pub id: String,
    pub name: String,
    /// base64url
    pub credential_id: String,
    /// The authenticator data the key gave when it made the credential,
    /// base64url: what each server's enrollment carries.
    pub auth_data: String,
    /// A Touch ID key's Secure Enclave blob, base64url: the private key,
    /// wrapped so only this Mac's chip can use it, and only after a
    /// fingerprint. None for a security key.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub enclave: Option<String>,
}

impl Key {
    pub fn is_touch_id(&self) -> bool {
        self.enclave.is_some()
    }
}

#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct State {
    #[serde(default)]
    pub servers: Vec<Server>,
    #[serde(default)]
    pub keys: Vec<Key>,
    /// Where a ticket the browser extension hands the app is imported.
    #[serde(default)]
    pub import_target: ImportTarget,
    /// Talking to the Reeve: the key's mode and the Reeve chosen.
    #[serde(default, skip_serializing_if = "VoiceSettings::is_default")]
    pub voice: VoiceSettings,
}

/// The person's choices about talking to the Reeve.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct VoiceSettings {
    /// The instance ID of the Legato whose Reeve to talk to, when the person
    /// chose one ("The Reeve to talk to"): kept by ID, so a new address
    /// does not lose it.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub server: Option<String>,
    /// Listen while the key is held, and send when it is let go of.
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub hold_to_talk: bool,
}

impl VoiceSettings {
    fn is_default(&self) -> bool {
        self == &VoiceSettings::default()
    }
}

/// Where a ticket the browser extension hands the app is imported: the app
/// asks which server, or uses the server in front.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ImportTarget {
    #[default]
    Ask,
    Current,
}

/// Where tokens are kept: the desktop's keyring, or memory in tests.
pub trait Secrets: Send + Sync {
    fn get(&self, id: &str) -> Option<String>;
    fn set(&self, id: &str, token: &str) -> Result<(), String>;
    fn delete(&self, id: &str);
}

pub struct Store {
    path: PathBuf,
    secrets: Box<dyn Secrets>,
    pub state: State,
}

impl Store {
    /// Reads the file at path, or starts empty when there is none.
    pub fn open(path: PathBuf, secrets: Box<dyn Secrets>) -> Result<Store, String> {
        let state = match std::fs::read(&path) {
            Ok(bytes) => serde_json::from_slice(&bytes).map_err(|e| format!("{} does not read: {e}", path.display()))?,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => State::default(),
            Err(e) => return Err(format!("{}: {e}", path.display())),
        };
        Ok(Store { path, secrets, state })
    }

    /// Writes the file whole, beside it first, then over it.
    pub fn save(&self) -> Result<(), String> {
        use std::io::Write;
        let fail = |e: std::io::Error| format!("{}: {e}", self.path.display());
        if let Some(dir) = self.path.parent() {
            std::fs::create_dir_all(dir).map_err(fail)?;
        }
        let tmp = self.path.with_extension("json.tmp");
        let mut options = std::fs::OpenOptions::new();
        options.write(true).create(true).truncate(true);
        #[cfg(unix)]
        {
            use std::os::unix::fs::OpenOptionsExt;
            options.mode(0o600);
        }
        let mut file = options.open(&tmp).map_err(fail)?;
        file.write_all(&serde_json::to_vec_pretty(&self.state).expect("state encodes")).map_err(fail)?;
        file.sync_all().map_err(fail)?;
        std::fs::rename(&tmp, &self.path).map_err(fail)
    }

    pub fn add_server(&mut self, name: &str, url: &str, ca_pem: Option<String>, token: &str) -> Result<Server, String> {
        let server = Server { id: new_id("s"), name: name.into(), url: url.trim_end_matches('/').into(), ca_pem, ..Default::default() };
        self.secrets.set(&server.id, token)?;
        self.state.servers.push(server.clone());
        Ok(server)
    }

    pub fn server(&self, id: &str) -> Option<&Server> {
        self.state.servers.iter().find(|s| s.id == id)
    }

    pub fn server_mut(&mut self, id: &str) -> Option<&mut Server> {
        self.state.servers.iter_mut().find(|s| s.id == id)
    }

    /// Puts the servers in the order of `ids`. An ID that names no server is
    /// ignored, and a server left out keeps its relative place after the rest.
    pub fn reorder_servers(&mut self, ids: &[String]) {
        let mut rest = std::mem::take(&mut self.state.servers);
        let mut ordered = Vec::with_capacity(rest.len());
        for id in ids {
            if let Some(i) = rest.iter().position(|s| &s.id == id) {
                ordered.push(rest.remove(i));
            }
        }
        ordered.extend(rest);
        self.state.servers = ordered;
    }

    pub fn token(&self, id: &str) -> Option<String> {
        self.secrets.get(id)
    }

    /// Forgets a server and its token.
    pub fn remove_server(&mut self, id: &str) -> Result<(), String> {
        self.state.servers.retain(|s| s.id != id);
        self.secrets.delete(id);
        Ok(())
    }

    pub fn add_key(&mut self, name: &str, credential_id: &str, auth_data: &str) -> Result<Key, String> {
        let key = Key { id: new_id("k"), name: name.into(), credential_id: credential_id.into(), auth_data: auth_data.into(), enclave: None };
        self.state.keys.push(key.clone());
        Ok(key)
    }

    pub fn add_touch_id_key(&mut self, name: &str, credential_id: &str, auth_data: &str, enclave: &str) -> Result<Key, String> {
        let key = Key { id: new_id("k"), name: name.into(), credential_id: credential_id.into(), auth_data: auth_data.into(), enclave: Some(enclave.into()) };
        self.state.keys.push(key.clone());
        Ok(key)
    }

    /// Forgets a key, and that any server enrolled it.
    pub fn remove_key(&mut self, id: &str) -> Result<(), String> {
        self.state.keys.retain(|k| k.id != id);
        for s in &mut self.state.servers {
            s.devices.remove(id);
        }
        Ok(())
    }
}

/// The desktop's keyring: the Secret Service on Linux, the Keychain on
/// macOS, the Credential Manager on Windows.
pub struct Keyring;

impl Secrets for Keyring {
    fn get(&self, id: &str) -> Option<String> {
        keyring::Entry::new("dev.legato.desktop", id).ok()?.get_password().ok()
    }

    fn set(&self, id: &str, token: &str) -> Result<(), String> {
        keyring::Entry::new("dev.legato.desktop", id)
            .and_then(|e| e.set_password(token))
            .map_err(|e| format!("the keyring would not keep the token: {e}"))
    }

    fn delete(&self, id: &str) {
        if let Ok(e) = keyring::Entry::new("dev.legato.desktop", id) {
            let _ = e.delete_credential();
        }
    }
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use std::sync::{Arc, Mutex};

    #[derive(Clone, Default)]
    pub(crate) struct Memory(pub Arc<Mutex<BTreeMap<String, String>>>);

    impl Secrets for Memory {
        fn get(&self, id: &str) -> Option<String> {
            self.0.lock().unwrap().get(id).cloned()
        }
        fn set(&self, id: &str, token: &str) -> Result<(), String> {
            self.0.lock().unwrap().insert(id.into(), token.into());
            Ok(())
        }
        fn delete(&self, id: &str) {
            self.0.lock().unwrap().remove(id);
        }
    }

    fn open(dir: &tempfile::TempDir, secrets: &Memory) -> Store {
        Store::open(dir.path().join("legato-desktop.json"), Box::new(secrets.clone())).unwrap()
    }

    #[test]
    fn servers_and_keys_are_kept_across_opens() {
        let (dir, secrets) = (tempfile::tempdir().unwrap(), Memory::default());
        let mut store = open(&dir, &secrets);
        let s = store.add_server("erebus", "https://erebus.local:3080", Some("PEM".into()), "tok-1").unwrap();
        let k = store.add_key("YubiKey 1", "Y3JlZA", "YXV0aA").unwrap();
        store.server_mut(&s.id).unwrap().devices.insert(k.id.clone(), "dev-1".into());
        store.save().unwrap();

        let again = open(&dir, &secrets);
        assert_eq!(again.state, store.state);
        assert_eq!(again.token(&s.id).as_deref(), Some("tok-1"));
    }

    #[test]
    fn servers_keep_the_order_they_are_put_in() {
        let (dir, secrets) = (tempfile::tempdir().unwrap(), Memory::default());
        let mut store = open(&dir, &secrets);
        let a = store.add_server("a", "https://a.local", None, "t").unwrap();
        let b = store.add_server("b", "https://b.local", None, "t").unwrap();
        let c = store.add_server("c", "https://c.local", None, "t").unwrap();

        // An id the store does not know is ignored; one left out keeps its place at the end.
        store.reorder_servers(&[c.id.clone(), "s-gone".into(), a.id.clone()]);
        let ids = |s: &Store| s.state.servers.iter().map(|x| x.id.clone()).collect::<Vec<_>>();
        assert_eq!(ids(&store), [c.id.clone(), a.id.clone(), b.id.clone()]);

        store.save().unwrap();
        assert_eq!(ids(&open(&dir, &secrets)), ids(&store));
    }

    #[test]
    fn where_tickets_are_imported_is_asked_until_the_person_says_otherwise() {
        let (dir, secrets) = (tempfile::tempdir().unwrap(), Memory::default());
        // A file from before the setting existed.
        std::fs::write(dir.path().join("legato-desktop.json"), r#"{"servers":[],"keys":[]}"#).unwrap();
        let mut store = open(&dir, &secrets);
        assert_eq!(store.state.import_target, ImportTarget::Ask);

        store.state.import_target = ImportTarget::Current;
        store.save().unwrap();
        let text = std::fs::read_to_string(dir.path().join("legato-desktop.json")).unwrap();
        assert!(text.contains(r#""import_target": "current""#), "{text}");
        assert_eq!(open(&dir, &secrets).state.import_target, ImportTarget::Current);
    }

    #[test]
    fn the_voice_choices_are_kept_and_absent_until_made() {
        let (dir, secrets) = (tempfile::tempdir().unwrap(), Memory::default());
        let path = dir.path().join("legato-desktop.json");
        std::fs::write(&path, r#"{"servers":[],"keys":[]}"#).unwrap();
        let mut store = open(&dir, &secrets);
        assert_eq!(store.state.voice, VoiceSettings::default());
        store.save().unwrap();
        assert!(!std::fs::read_to_string(&path).unwrap().contains("voice"));

        store.state.voice = VoiceSettings { server: Some("inst-1".into()), hold_to_talk: true };
        store.save().unwrap();
        let text = std::fs::read_to_string(&path).unwrap();
        assert!(text.contains(r#""server": "inst-1""#), "{text}");
        assert_eq!(open(&dir, &secrets).state.voice, store.state.voice);
    }

    #[test]
    fn a_token_is_kept_in_the_keyring_not_the_file() {
        let (dir, secrets) = (tempfile::tempdir().unwrap(), Memory::default());
        let mut store = open(&dir, &secrets);
        store.add_server("erebus", "https://erebus.local:3080", None, "secret-token-xyz").unwrap();
        store.save().unwrap();
        let text = std::fs::read_to_string(dir.path().join("legato-desktop.json")).unwrap();
        assert!(!text.contains("secret-token-xyz"));
    }

    #[cfg(unix)]
    #[test]
    fn the_file_is_readable_by_its_owner_only() {
        use std::os::unix::fs::PermissionsExt;
        let (dir, secrets) = (tempfile::tempdir().unwrap(), Memory::default());
        let mut store = open(&dir, &secrets);
        store.add_server("erebus", "https://erebus.local:3080", None, "t").unwrap();
        store.save().unwrap();
        let mode = std::fs::metadata(dir.path().join("legato-desktop.json")).unwrap().permissions().mode();
        assert_eq!(mode & 0o777, 0o600);
    }

    #[test]
    fn removing_a_server_forgets_its_token() {
        let (dir, secrets) = (tempfile::tempdir().unwrap(), Memory::default());
        let mut store = open(&dir, &secrets);
        let s = store.add_server("erebus", "https://erebus.local:3080", None, "t").unwrap();
        store.remove_server(&s.id).unwrap();
        assert!(store.server(&s.id).is_none());
        assert!(secrets.get(&s.id).is_none());
    }

    #[test]
    fn removing_a_key_forgets_where_it_was_enrolled() {
        let (dir, secrets) = (tempfile::tempdir().unwrap(), Memory::default());
        let mut store = open(&dir, &secrets);
        let s = store.add_server("erebus", "https://erebus.local:3080", None, "t").unwrap();
        let k = store.add_key("YubiKey 1", "Y3JlZA", "YXV0aA").unwrap();
        store.server_mut(&s.id).unwrap().devices.insert(k.id.clone(), "dev-1".into());
        store.remove_key(&k.id).unwrap();
        assert!(store.state.keys.is_empty());
        assert!(store.server(&s.id).unwrap().devices.is_empty());
    }

    #[test]
    fn a_touch_id_key_keeps_its_secure_enclave_blob_across_opens() {
        let (dir, secrets) = (tempfile::tempdir().unwrap(), Memory::default());
        let mut store = open(&dir, &secrets);
        let k = store.add_touch_id_key("Touch ID", "Y3JlZA", "YXV0aA", "YmxvYg").unwrap();
        store.save().unwrap();

        let again = open(&dir, &secrets);
        let kept = again.state.keys.iter().find(|x| x.id == k.id).unwrap();
        assert_eq!(kept.enclave.as_deref(), Some("YmxvYg"));
        assert!(kept.is_touch_id());
    }

    // A state file saved before Touch ID keys existed.
    #[test]
    fn a_key_saved_without_a_blob_is_a_security_key() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("legato-desktop.json");
        std::fs::write(&path, r#"{"servers":[],"keys":[{"id":"k-1","name":"YubiKey","credential_id":"Y3JlZA","auth_data":"YXV0aA"}]}"#).unwrap();
        let store = Store::open(path, Box::new(Memory::default())).unwrap();
        assert!(!store.state.keys[0].is_touch_id());
    }

    #[test]
    fn ids_are_distinct() {
        let (dir, secrets) = (tempfile::tempdir().unwrap(), Memory::default());
        let mut store = open(&dir, &secrets);
        let a = store.add_server("a", "https://a.local", None, "t").unwrap();
        let b = store.add_server("b", "https://b.local", None, "t").unwrap();
        assert_ne!(a.id, b.id);
    }
}
