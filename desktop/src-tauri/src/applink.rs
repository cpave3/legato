//! Links the system hands the app: `legato-desktop://import?provider=jira&key=AG-1`,
//! which the browser extension opens to import a ticket into one of the
//! app's servers. Any web page can open such a link, so the app reads only
//! this one shape, and imports nothing without the person's choice of server
//! unless they chose to always use the server in front.
//!
//! The scheme is the one a server's page uses to reach the app (`bridge`),
//! but those addresses never leave the page: its web view reads and cancels
//! them, so none reaches the system.

/// A ticket to import: its provider (`jira`, `burnlite`) and its key.
#[derive(Debug, PartialEq, Clone, serde::Serialize)]
pub struct ImportLink {
    pub provider: String,
    pub key: String,
}

/// The import a link asks for, or None for any other link.
pub fn read(url: &str) -> Option<ImportLink> {
    let url = reqwest::Url::parse(url).ok()?;
    if url.scheme() != crate::bridge::SCHEME || url.host_str()? != "import" || !matches!(url.path(), "" | "/") {
        return None;
    }
    let param = |name: &str| url.query_pairs().find(|(k, _)| k == name).map(|(_, v)| v.into_owned());
    let provider = param("provider")?;
    let key = param("key")?.to_uppercase();
    let provider_ok = provider.len() <= 32
        && provider.starts_with(|c: char| c.is_ascii_lowercase())
        && provider.chars().all(|c| c.is_ascii_lowercase() || c.is_ascii_digit() || c == '-');
    let (project, number) = key.rsplit_once('-')?;
    let key_ok = key.len() <= 64
        && project.starts_with(|c: char| c.is_ascii_uppercase())
        && project.chars().all(|c| c.is_ascii_uppercase() || c.is_ascii_digit() || c == '_')
        && !number.is_empty()
        && number.chars().all(|c| c.is_ascii_digit());
    (provider_ok && key_ok).then_some(ImportLink { provider, key })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn link(provider: &str, key: &str) -> Option<ImportLink> {
        Some(ImportLink { provider: provider.into(), key: key.into() })
    }

    #[test]
    fn an_import_link_names_a_provider_and_a_key() {
        assert_eq!(read("legato-desktop://import?provider=jira&key=AG-9804"), link("jira", "AG-9804"));
        assert_eq!(read("legato-desktop://import/?key=est-42&provider=burnlite"), link("burnlite", "EST-42"));
    }

    #[test]
    fn anything_else_is_not_read() {
        for url in [
            "legato-desktop://import?provider=jira",
            "legato-desktop://import?key=AG-1",
            "legato-desktop://import?provider=Jira&key=AG-1",
            "legato-desktop://import?provider=jira&key=AG",
            "legato-desktop://import?provider=jira&key=AG-1x",
            "legato-desktop://import?provider=jira&key=../AG-1",
            "legato-desktop://import/more?provider=jira&key=AG-1",
            "legato-desktop://approval?key=k1&id=ap-1",
            "legato-desktop://voice?key=k1&action=start",
            "https://example.com/import?provider=jira&key=AG-1",
            "not a link",
        ] {
            assert_eq!(read(url), None, "{url}");
        }
    }
}
