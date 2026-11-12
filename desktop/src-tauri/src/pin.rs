//! The certificate a server sealed when it paired, pinned: Legato's own CA,
//! or the certificate set in web.tls.cert. A server is trusted when the
//! certificate it presents is the pinned one, or is issued by it for the
//! host the app asked for. Nothing else is trusted for that server.

use std::sync::Arc;

use rustls::client::danger::ServerCertVerifier;
use rustls::pki_types::{CertificateDer, ServerName, UnixTime};

#[derive(Clone)]
pub struct Pin {
    der: Vec<u8>,
}

impl Pin {
    pub fn from_pem(pem: &str) -> Result<Pin, String> {
        let first = rustls_pemfile::certs(&mut pem.as_bytes())
            .next()
            .ok_or("the server sent no certificate to pin")?
            .map_err(|e| format!("the server's certificate does not read: {e}"))?;
        Ok(Pin { der: first.to_vec() })
    }

    pub fn der(&self) -> &[u8] {
        &self.der
    }

    /// Whether leaf, presented for host, is the pinned certificate or is
    /// issued by it.
    pub fn trusts(&self, leaf: &[u8], host: &str) -> bool {
        leaf == self.der.as_slice() || self.issued(leaf, host).is_ok()
    }

    fn issued(&self, leaf: &[u8], host: &str) -> Result<(), String> {
        let mut roots = rustls::RootCertStore::empty();
        roots.add(CertificateDer::from(self.der.clone())).map_err(|e| e.to_string())?;
        let provider = Arc::new(rustls::crypto::ring::default_provider());
        let verifier = rustls::client::WebPkiServerVerifier::builder_with_provider(Arc::new(roots), provider)
            .build()
            .map_err(|e| e.to_string())?;
        let name = ServerName::try_from(host.to_string()).map_err(|e| e.to_string())?;
        verifier
            .verify_server_cert(&CertificateDer::from(leaf.to_vec()), &[], &name, &[], UnixTime::now())
            .map(|_| ())
            .map_err(|e| e.to_string())
    }
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use rcgen::{BasicConstraints, Certificate, CertificateParams, DnType, ExtendedKeyUsagePurpose, IsCa, KeyPair};

    pub(crate) struct Ca {
        pub cert: Certificate,
        key: KeyPair,
    }

    /// A CA as Legato makes one, and server certificates it issues.
    pub(crate) fn ca() -> Ca {
        let key = KeyPair::generate().unwrap();
        let mut p = CertificateParams::new(Vec::<String>::new()).unwrap();
        p.is_ca = IsCa::Ca(BasicConstraints::Unconstrained);
        p.distinguished_name.push(DnType::CommonName, "Legato Local CA");
        Ca { cert: p.self_signed(&key).unwrap(), key }
    }

    impl Ca {
        pub(crate) fn issue(&self, names: &[&str]) -> Certificate {
            let key = KeyPair::generate().unwrap();
            let mut p = CertificateParams::new(names.iter().map(|n| n.to_string()).collect::<Vec<_>>()).unwrap();
            p.extended_key_usages = vec![ExtendedKeyUsagePurpose::ServerAuth];
            p.signed_by(&key, &self.cert, &self.key).unwrap()
        }
    }

    #[test]
    fn a_pinned_ca_trusts_the_server_certificates_it_issued() {
        let ca = ca();
        let pin = Pin::from_pem(&ca.cert.pem()).unwrap();
        assert!(pin.trusts(ca.issue(&["localhost", "legato.test"]).der(), "legato.test"));
    }

    #[test]
    fn a_certificate_from_another_ca_is_refused() {
        let (ours, theirs) = (ca(), ca());
        let pin = Pin::from_pem(&ours.cert.pem()).unwrap();
        assert!(!pin.trusts(theirs.issue(&["legato.test"]).der(), "legato.test"));
    }

    #[test]
    fn a_certificate_for_another_host_is_refused() {
        let ca = ca();
        let pin = Pin::from_pem(&ca.cert.pem()).unwrap();
        assert!(!pin.trusts(ca.issue(&["legato.test"]).der(), "elsewhere.test"));
    }

    #[test]
    fn a_pinned_server_certificate_trusts_itself() {
        let ca = ca();
        let leaf = ca.issue(&["legato.test"]);
        let pin = Pin::from_pem(&leaf.pem()).unwrap();
        assert!(pin.trusts(leaf.der(), "legato.test"));
        assert!(!pin.trusts(ca.issue(&["legato.test"]).der(), "legato.test"), "a sibling is not the pinned certificate");
    }

    #[test]
    fn text_that_is_not_a_certificate_is_refused() {
        assert!(Pin::from_pem("not a certificate").is_err());
    }
}
