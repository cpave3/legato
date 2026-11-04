//! Legato servers on the local network: each web server announces
//! `_legato._tcp` over mDNS with its port, and its scheme in a TXT record
//! (internal/engine/discovery). The pair form lists them, as the phone does.

use std::collections::BTreeMap;
use std::net::IpAddr;
use std::sync::{Arc, Mutex};

pub const SERVICE: &str = "_legato._tcp.local.";

/// A server found on the network.
#[derive(Debug, Clone, PartialEq, serde::Serialize)]
pub struct Found {
    pub name: String,
    pub url: String,
}

/// The address of an announced server: its scheme (http unless it says
/// https), an address, and its port, which is left out when it is the
/// scheme's own.
pub fn base_url(scheme: Option<&str>, addr: IpAddr, port: u16) -> String {
    let scheme = if scheme == Some("https") { "https" } else { "http" };
    let host = match addr {
        IpAddr::V4(a) => a.to_string(),
        IpAddr::V6(a) => format!("[{a}]"),
    };
    let own = (scheme == "https" && port == 443) || (scheme == "http" && port == 80);
    if own {
        format!("{scheme}://{host}")
    } else {
        format!("{scheme}://{host}:{port}")
    }
}

/// Which of a server's addresses to use: IPv4 first, since an IPv6
/// link-local one needs an interface a URL cannot name.
pub fn best_address(addrs: &[IpAddr]) -> Option<IpAddr> {
    addrs.iter().find(|a| a.is_ipv4()).or_else(|| addrs.first()).copied()
}

/// The instance name in a service's full name.
pub fn instance(fullname: &str) -> String {
    fullname.split("._legato._tcp").next().unwrap_or(fullname).to_string()
}

/// The servers announcing themselves now, kept current by a browser that
/// runs in the background once started.
#[derive(Clone, Default)]
pub struct Browser {
    found: Arc<Mutex<BTreeMap<String, Found>>>,
    started: Arc<Mutex<bool>>,
}

impl Browser {
    pub fn found(&self) -> Vec<Found> {
        self.found.lock().unwrap().values().cloned().collect()
    }

    /// Starts browsing, once. A network that blocks mDNS just finds nothing.
    pub fn start(&self) {
        let mut started = self.started.lock().unwrap();
        if *started {
            return;
        }
        *started = true;
        let found = self.found.clone();
        std::thread::spawn(move || {
            let Ok(daemon) = mdns_sd::ServiceDaemon::new() else { return };
            let started = std::time::Instant::now();
            let mut asked = std::time::Instant::now();
            let Ok(mut events) = daemon.browse(SERVICE) else { return };
            loop {
                // Ask again now and then: an answer to a query lost at start
                // would otherwise wait for the library's long backoff.
                let every = if started.elapsed().as_secs() < 60 { 5 } else { 60 };
                if asked.elapsed().as_secs() >= every {
                    let _ = daemon.stop_browse(SERVICE);
                    match daemon.browse(SERVICE) {
                        Ok(e) => events = e,
                        Err(_) => return,
                    }
                    asked = std::time::Instant::now();
                }
                match events.recv_timeout(std::time::Duration::from_millis(500)) {
                    Ok(mdns_sd::ServiceEvent::ServiceResolved(s)) => {
                        let addrs: Vec<IpAddr> = s.addresses.iter().map(|a| a.to_ip_addr()).collect();
                        if let Some(addr) = best_address(&addrs) {
                            let url = base_url(s.get_property_val_str("scheme"), addr, s.port);
                            let mut found = found.lock().unwrap();
                            if !found.contains_key(&s.fullname) {
                                println!("[discovery] {} at {url}, {:.1}s after starting", instance(&s.fullname), started.elapsed().as_secs_f32());
                            }
                            found.insert(s.fullname.clone(), Found { name: instance(&s.fullname), url });
                        }
                    }
                    Ok(mdns_sd::ServiceEvent::ServiceRemoved(_, fullname)) => {
                        found.lock().unwrap().remove(&fullname);
                    }
                    Ok(_) | Err(mdns_sd::RecvTimeoutError::Timeout) => {}
                    Err(mdns_sd::RecvTimeoutError::Disconnected) => {
                        // stop_browse closed it; the next turn asks again.
                        asked = std::time::Instant::now() - std::time::Duration::from_secs(3600);
                    }
                }
            }
        });
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // As mobile/app/src/test/java/dev/legato/mobile/DiscoveryTest.kt.
    #[test]
    fn an_announcement_becomes_an_address() {
        let v4: IpAddr = "192.0.2.10".parse().unwrap();
        assert_eq!(base_url(None, v4, 3080), "http://192.0.2.10:3080");
        assert_eq!(base_url(Some("https"), v4, 3080), "https://192.0.2.10:3080");
        assert_eq!(base_url(Some("https"), v4, 443), "https://192.0.2.10");
        assert_eq!(base_url(Some("http"), v4, 80), "http://192.0.2.10");
        assert_eq!(base_url(Some("http"), "2001:db8::1".parse().unwrap(), 3080), "http://[2001:db8::1]:3080");
    }

    #[test]
    fn ipv4_is_preferred() {
        let v6: IpAddr = "fe80::1".parse().unwrap();
        let v4: IpAddr = "192.0.2.10".parse().unwrap();
        assert_eq!(best_address(&[v6, v4]), Some(v4));
        assert_eq!(best_address(&[v6]), Some(v6));
        assert_eq!(best_address(&[]), None);
    }

    #[test]
    fn the_instance_is_the_name_before_the_service_type() {
        assert_eq!(instance("erebus._legato._tcp.local."), "erebus");
        assert_eq!(instance("my desk._legato._tcp.local."), "my desk");
    }
}
