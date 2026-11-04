//! Whether macOS trusts a server's certificate for its pages, and making it
//! so. The app's own requests trust the pinned certificate (see `legato`),
//! and on Linux so do the pages; but on a Mac the page is WKWebView's,
//! which checks the system's trust, so a server with Legato's own CA shows
//! nothing until the Mac trusts that CA.
//!
//! Trusting it asks for the person's password, and trusts the CA only for
//! TLS to that server's host, so the CA cannot vouch for other sites.

use std::path::Path;

/// The host a server's page is loaded from, when its page needs the system
/// to trust its certificate: an https address. None for http.
pub fn page_host(url: &str) -> Option<String> {
    let rest = url.strip_prefix("https://")?;
    let authority = rest.split(['/', '?', '#']).next()?;
    let host = if let Some(v6) = authority.strip_prefix('[') {
        v6.split(']').next()?
    } else {
        authority.rsplit_once(':').map_or(authority, |(h, _)| h)
    };
    (!host.is_empty()).then(|| host.to_string())
}

fn path(p: &Path) -> String {
    p.to_string_lossy().into_owned()
}

/// `security verify-cert`: does the system trust the leaf, for TLS to
/// host? The CA goes in as a certificate to build the chain with, not as an
/// anchor, so only the system's trust counts. Exits 0 when it does.
pub fn verify_args(leaf: &Path, ca: &Path, host: &str) -> Vec<String> {
    ["verify-cert", "-c", &path(leaf), "-c", &path(ca), "-p", "ssl", "-s", host, "-L", "-q"].map(String::from).to_vec()
}

/// `security add-trusted-cert`: the CA, into the login keychain, trusted as
/// a root for TLS to host only. macOS asks for the person's password.
pub fn trust_args(ca: &Path, host: &str, keychain: &Path) -> Vec<String> {
    ["add-trusted-cert", "-r", "trustRoot", "-p", "ssl", "-s", host, "-k", &path(keychain), &path(ca)].map(String::from).to_vec()
}

/// The file the certificate is saved as for the person: a name Keychain
/// Access opens, which says whose it is.
pub fn certificate_file_name(server: &str) -> String {
    let name: String = server.chars().map(|c| if c.is_alphanumeric() || " -_.".contains(c) { c } else { '-' }).collect();
    let name = name.trim_matches(['.', ' ']);
    format!("{} Legato CA.cer", if name.is_empty() { "server" } else { name })
}

#[cfg(target_os = "macos")]
mod mac {
    use std::process::Command;

    fn temp(name: &str, contents: &[u8]) -> Result<tempfile_path::TempPath, String> {
        tempfile_path::write(name, contents)
    }

    /// A file removed when dropped, in the temporary folder.
    mod tempfile_path {
        pub struct TempPath(pub std::path::PathBuf);
        impl Drop for TempPath {
            fn drop(&mut self) {
                let _ = std::fs::remove_file(&self.0);
            }
        }
        pub fn write(name: &str, contents: &[u8]) -> Result<TempPath, String> {
            let mut id = [0u8; 6];
            rand::RngCore::fill_bytes(&mut rand::rngs::OsRng, &mut id);
            let p = std::env::temp_dir().join(format!("legato-{}-{name}", hex::encode(id)));
            std::fs::write(&p, contents).map_err(|e| format!("{}: {e}", p.display()))?;
            Ok(TempPath(p))
        }
    }

    pub fn trusts(leaf_pem: &str, ca_pem: &str, host: &str) -> Result<bool, String> {
        let leaf = temp("leaf.pem", leaf_pem.as_bytes())?;
        let ca = temp("ca.pem", ca_pem.as_bytes())?;
        let status = Command::new("/usr/bin/security")
            .args(super::verify_args(&leaf.0, &ca.0, host))
            .status()
            .map_err(|e| format!("security verify-cert did not run: {e}"))?;
        Ok(status.success())
    }

    pub fn trust(ca_pem: &str, host: &str) -> Result<(), String> {
        let ca = temp("ca.pem", ca_pem.as_bytes())?;
        let keychain = dirs::home_dir().ok_or("no home folder")?.join("Library/Keychains/login.keychain-db");
        let out = Command::new("/usr/bin/security")
            .args(super::trust_args(&ca.0, host, &keychain))
            .output()
            .map_err(|e| format!("security add-trusted-cert did not run: {e}"))?;
        if out.status.success() {
            return Ok(());
        }
        let said = String::from_utf8_lossy(&out.stderr).trim().to_string();
        Err(if said.is_empty() { "macOS did not trust the certificate.".into() } else { said })
    }
}

/// Whether the system trusts the leaf a server presents, for its host.
pub fn trusts(leaf_pem: &str, ca_pem: &str, host: &str) -> Result<bool, String> {
    #[cfg(target_os = "macos")]
    {
        mac::trusts(leaf_pem, ca_pem, host)
    }
    #[cfg(not(target_os = "macos"))]
    {
        let _ = (leaf_pem, ca_pem, host);
        Ok(true)
    }
}

/// Trusts the CA for TLS to host; macOS asks for the person's password.
pub fn trust(ca_pem: &str, host: &str) -> Result<(), String> {
    #[cfg(target_os = "macos")]
    {
        mac::trust(ca_pem, host)
    }
    #[cfg(not(target_os = "macos"))]
    {
        let _ = (ca_pem, host);
        Err("Only a Mac needs this.".into())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;

    #[test]
    fn a_page_needs_the_systems_trust_only_over_https_and_for_its_host() {
        assert_eq!(page_host("https://192.168.1.20:3080").as_deref(), Some("192.168.1.20"));
        assert_eq!(page_host("https://erebus.local:3080/board?x=1").as_deref(), Some("erebus.local"));
        assert_eq!(page_host("https://legato.example.com").as_deref(), Some("legato.example.com"));
        assert_eq!(page_host("https://[fe80::1]:3080").as_deref(), Some("fe80::1"));
        assert_eq!(page_host("http://erebus.local:3080"), None);
    }

    // man security: verify-cert takes the leaf first; -r would make the CA
    // an anchor and pass whatever the system thinks, so it is not used.
    #[test]
    fn the_check_asks_the_system_about_tls_to_the_host_without_anchoring_the_ca() {
        let args = verify_args(&PathBuf::from("/t/leaf.pem"), &PathBuf::from("/t/ca.pem"), "erebus.local");
        assert_eq!(args, ["verify-cert", "-c", "/t/leaf.pem", "-c", "/t/ca.pem", "-p", "ssl", "-s", "erebus.local", "-L", "-q"]);
        assert!(!args.contains(&"-r".to_string()));
    }

    #[test]
    fn trusting_makes_the_ca_a_root_for_tls_to_that_host_only() {
        let args = trust_args(&PathBuf::from("/t/ca.pem"), "192.168.1.20", &PathBuf::from("/Users/c/Library/Keychains/login.keychain-db"));
        assert_eq!(
            args,
            ["add-trusted-cert", "-r", "trustRoot", "-p", "ssl", "-s", "192.168.1.20", "-k", "/Users/c/Library/Keychains/login.keychain-db", "/t/ca.pem"]
        );
    }

    #[test]
    fn the_saved_certificate_is_named_for_its_server() {
        assert_eq!(certificate_file_name("erebus"), "erebus Legato CA.cer");
        assert_eq!(certificate_file_name("lab/box:1"), "lab-box-1 Legato CA.cer");
        assert_eq!(certificate_file_name(".."), "server Legato CA.cer");
    }
}
