//! Keeping the AppImage current from GitHub releases, as Legato keeps its
//! own binary current (internal/engine/selfupdate). The desktop app shares
//! Legato's version, but not every release carries a desktop build: the
//! release workflow builds one only when desktop/ changed. So the newest
//! release that has the AppImage is the one to compare with.
//!
//! A build from source is `dev` and never updates itself.

use std::path::Path;

use serde_json::Value;

pub const REPOSITORY: &str = "cpave3/legato";
pub const APPIMAGE: &str = "legato-desktop-x86_64.AppImage";
pub const SUMS: &str = "legato-desktop-SHA256SUMS";

/// This build's version: vX.Y.Z when the release workflow built it, else dev.
pub fn version() -> &'static str {
    option_env!("LEGATO_DESKTOP_VERSION").unwrap_or("dev")
}

/// A release that carries the AppImage.
#[derive(Debug, Clone, PartialEq, serde::Serialize, serde::Deserialize)]
pub struct Candidate {
    pub version: String,
    pub appimage_url: String,
    pub sums_url: String,
    pub page: String,
}

/// vX.Y.Z as numbers; anything else (dev, a prerelease) is None.
pub fn parse(version: &str) -> Option<(u64, u64, u64)> {
    let mut parts = version.strip_prefix('v')?.split('.');
    let mut next = || parts.next()?.parse::<u64>().ok();
    let v = (next()?, next()?, next()?);
    parts.next().is_none().then_some(v)
}

/// Whether a is a newer release than b. A dev build is never older.
pub fn newer(a: &str, b: &str) -> bool {
    match (parse(a), parse(b)) {
        (Some(a), Some(b)) => a > b,
        _ => false,
    }
}

/// The newest stable release, of GitHub's list, that carries asset and the
/// desktop checksums.
pub fn newest_with(releases: &[Value], asset: &str) -> Option<Candidate> {
    let url = |r: &Value, name: &str| {
        r["assets"].as_array()?.iter().find(|a| a["name"] == name)?["browser_download_url"].as_str().map(String::from)
    };
    releases
        .iter()
        .filter(|r| r["draft"] != true && r["prerelease"] != true)
        .filter_map(|r| {
            let version = r["tag_name"].as_str()?;
            parse(version)?;
            Some(Candidate {
                version: version.to_string(),
                appimage_url: url(r, asset)?,
                sums_url: url(r, SUMS)?,
                page: r["html_url"].as_str().unwrap_or_default().to_string(),
            })
        })
        .max_by_key(|c| parse(&c.version))
}

/// Checks bytes against the line for name in a SHA256SUMS file.
pub fn check_sum(bytes: &[u8], sums: &str, name: &str) -> Result<(), String> {
    use sha2::{Digest, Sha256};
    let want = sums
        .lines()
        .find_map(|l| {
            let (sum, file) = l.split_once(char::is_whitespace)?;
            (file.trim().trim_start_matches('*') == name).then(|| sum.to_ascii_lowercase())
        })
        .ok_or_else(|| format!("the release's checksums do not list {name}"))?;
    if hex::encode(Sha256::digest(bytes)) == want {
        Ok(())
    } else {
        Err(format!("{name} does not match the release's checksum"))
    }
}

/// Puts bytes in place of the file at target: written beside it first and
/// made executable, the old one kept as `<target>.prev`, then renamed over.
/// A failure leaves target as it was.
pub fn install(target: &Path, bytes: &[u8]) -> Result<(), String> {
    let fail = |e: std::io::Error| format!("{}: {e}", target.display());
    let with = |suffix: &str| {
        let mut p = target.as_os_str().to_owned();
        p.push(suffix);
        std::path::PathBuf::from(p)
    };
    let (next, prev) = (with(".new"), with(".prev"));
    std::fs::write(&next, bytes).map_err(fail)?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        if let Err(e) = std::fs::set_permissions(&next, std::fs::Permissions::from_mode(0o755)) {
            let _ = std::fs::remove_file(&next);
            return Err(fail(e));
        }
    }
    if let Err(e) = std::fs::copy(target, &prev) {
        let _ = std::fs::remove_file(&next);
        return Err(fail(e));
    }
    std::fs::rename(&next, target).map_err(|e| {
        let _ = std::fs::remove_file(&next);
        fail(e)
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use sha2::{Digest, Sha256};

    fn release(tag: &str, assets: &[&str]) -> Value {
        json!({
            "tag_name": tag, "draft": false, "prerelease": false,
            "html_url": format!("https://github.com/{REPOSITORY}/releases/tag/{tag}"),
            "assets": assets.iter().map(|a| json!({ "name": a, "browser_download_url": format!("https://example.test/{tag}/{a}") })).collect::<Vec<_>>(),
        })
    }

    #[test]
    fn versions_compare_as_numbers() {
        assert_eq!(parse("v1.10.2"), Some((1, 10, 2)));
        assert_eq!(parse("dev"), None);
        assert_eq!(parse("v1.2.3-rc1"), None);
        assert!(newer("v0.10.0", "v0.9.9"));
        assert!(!newer("v0.9.9", "v0.10.0"));
        assert!(!newer("v1.0.0", "v1.0.0"));
        assert!(!newer("v9.9.9", "dev"), "a build from source never updates");
    }

    #[test]
    fn the_newest_release_with_the_appimage_is_chosen() {
        let releases = vec![
            release("v0.9.0", &["legato-linux-amd64", "SHA256SUMS"]), // no desktop build
            release("v0.8.1", &["legato-linux-amd64", APPIMAGE, SUMS]),
            release("v0.7.0", &[APPIMAGE, SUMS]),
        ];
        let c = newest_with(&releases, APPIMAGE).unwrap();
        assert_eq!(c.version, "v0.8.1");
        assert_eq!(c.appimage_url, format!("https://example.test/v0.8.1/{APPIMAGE}"));
        assert_eq!(c.sums_url, format!("https://example.test/v0.8.1/{SUMS}"));
    }

    #[test]
    fn drafts_prereleases_and_releases_without_checksums_are_passed_over() {
        let mut draft = release("v1.0.0", &[APPIMAGE, SUMS]);
        draft["draft"] = json!(true);
        let mut pre = release("v0.9.0", &[APPIMAGE, SUMS]);
        pre["prerelease"] = json!(true);
        let releases = vec![draft, pre, release("v0.8.0", &[APPIMAGE]), release("v0.7.0", &[APPIMAGE, SUMS])];
        assert_eq!(newest_with(&releases, APPIMAGE).unwrap().version, "v0.7.0");
        assert_eq!(newest_with(&[], APPIMAGE), None);
    }

    #[test]
    fn a_download_must_match_its_checksum() {
        let bytes = b"an appimage";
        let sum = hex::encode(Sha256::digest(bytes));
        let sums = format!("{sum}  {APPIMAGE}\n0000  other\n");
        assert!(check_sum(bytes, &sums, APPIMAGE).is_ok());
        assert!(check_sum(b"something else", &sums, APPIMAGE).is_err());
        assert!(check_sum(bytes, "0000  other\n", APPIMAGE).is_err(), "no line for it");
    }

    #[cfg(unix)]
    #[test]
    fn installing_keeps_the_old_file_and_makes_the_new_one_executable() {
        use std::os::unix::fs::PermissionsExt;
        let dir = tempfile::tempdir().unwrap();
        let target = dir.path().join("Legato.AppImage");
        std::fs::write(&target, b"old").unwrap();
        install(&target, b"new").unwrap();
        assert_eq!(std::fs::read(&target).unwrap(), b"new");
        assert_eq!(std::fs::read(dir.path().join("Legato.AppImage.prev")).unwrap(), b"old");
        assert_eq!(std::fs::metadata(&target).unwrap().permissions().mode() & 0o111, 0o111);
        assert!(!dir.path().join("Legato.AppImage.new").exists());
    }
}
