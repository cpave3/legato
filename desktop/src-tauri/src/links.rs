//! Where a link in a server's page belongs. The page's webview stays on the
//! Legato server it was opened for; a link to any other web address (a Jira
//! ticket, a Burnlite task, a pull request) goes to the system's default
//! browser. The Android app does the same (`allowNavigation` in the mobile
//! app's `PageContract.kt`).

use std::process::Command;

use reqwest::Url;

#[derive(Debug, PartialEq, Eq)]
pub enum Link {
    /// The page's own server, or something that is not a web address (about:,
    /// blob:, data:, the shell bridge): the webview handles it.
    Stay,
    /// Another web address: the system's browser opens it.
    External,
}

/// What a navigation of the page of the server at `server_url` to `target` is.
pub fn classify(server_url: &str, target: &Url) -> Link {
    match target.scheme() {
        "http" | "https" => {
            let same = Url::parse(server_url).map(|s| s.origin() == target.origin()).unwrap_or(false);
            if same { Link::Stay } else { Link::External }
        }
        "mailto" | "tel" => Link::External,
        _ => Link::Stay,
    }
}

/// Hands `url` to the system's default handler for it. Never through a shell.
pub fn open_in_browser(url: &Url) -> Result<(), String> {
    #[cfg(target_os = "macos")]
    let mut cmd = {
        let mut c = Command::new("/usr/bin/open");
        c.arg(url.as_str());
        c
    };
    #[cfg(target_os = "windows")]
    let mut cmd = {
        let mut c = Command::new("rundll32");
        c.arg("url.dll,FileProtocolHandler").arg(url.as_str());
        c
    };
    #[cfg(not(any(target_os = "macos", target_os = "windows")))]
    let mut cmd = {
        let mut c = Command::new("xdg-open");
        c.arg(url.as_str());
        c
    };
    cmd.spawn().map(|_| ()).map_err(|e| e.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn link(server: &str, target: &str) -> Link {
        classify(server, &Url::parse(target).unwrap())
    }

    const SERVER: &str = "https://legato.local:8443";

    #[test]
    fn the_servers_own_pages_stay() {
        assert_eq!(link(SERVER, "https://legato.local:8443/board"), Link::Stay);
        assert_eq!(link(SERVER, "https://LEGATO.local:8443/a?b=1#c"), Link::Stay);
    }

    #[test]
    fn other_web_addresses_go_to_the_browser() {
        assert_eq!(link(SERVER, "https://acme.atlassian.net/browse/LEG-1"), Link::External);
        assert_eq!(link(SERVER, "https://burnlite.example/EST-42"), Link::External);
    }

    #[test]
    fn another_port_scheme_or_host_is_another_origin() {
        assert_eq!(link(SERVER, "https://legato.local:9000/"), Link::External);
        assert_eq!(link(SERVER, "http://legato.local:8443/"), Link::External);
        assert_eq!(link(SERVER, "https://legato.local.evil.example:8443/"), Link::External);
    }

    #[test]
    fn mail_and_phone_links_go_to_the_system() {
        assert_eq!(link(SERVER, "mailto:a@b.example"), Link::External);
        assert_eq!(link(SERVER, "tel:+15550100"), Link::External);
    }

    #[test]
    fn what_is_not_a_web_address_stays() {
        assert_eq!(link(SERVER, "about:blank"), Link::Stay);
        assert_eq!(link(SERVER, "blob:https://legato.local:8443/x"), Link::Stay);
        assert_eq!(link(SERVER, "data:text/html,hi"), Link::Stay);
    }
}
