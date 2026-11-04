//! Keeping the app current from GitHub releases, as Legato keeps its own
//! binary current (internal/engine/selfupdate): the AppImage on Linux, the
//! app bundle on a Mac. The desktop app shares Legato's version, but not
//! every release carries a desktop build: the release workflow builds one
//! only when desktop/ changed, and each platform's job can fail alone. So
//! the newest release with this platform's build is the one to compare with.
//!
//! A build from source is `dev` and never updates itself.

use std::path::Path;

use serde_json::Value;

pub const REPOSITORY: &str = "cpave3/legato";
pub const APPIMAGE: &str = "legato-desktop-x86_64.AppImage";
pub const SUMS: &str = "legato-desktop-SHA256SUMS";

/// A platform's build in a release, and the checksums that list it.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Platform {
    pub asset: &'static str,
    pub sums: &'static str,
}

pub const LINUX: Platform = Platform { asset: APPIMAGE, sums: SUMS };
/// Legato.app, as a gzipped tar, for Apple Silicon.
pub const MACOS: Platform = Platform { asset: "legato-desktop-macos-arm64.app.tar.gz", sums: "legato-desktop-macos-SHA256SUMS" };

/// The build this copy updates from, if its platform has one.
pub fn this_platform() -> Option<Platform> {
    if cfg!(target_os = "linux") {
        Some(LINUX)
    } else if cfg!(all(target_os = "macos", target_arch = "aarch64")) {
        Some(MACOS)
    } else {
        None
    }
}

/// This build's version: vX.Y.Z when the release workflow built it, else dev.
pub fn version() -> &'static str {
    option_env!("LEGATO_DESKTOP_VERSION").unwrap_or("dev")
}

/// A release that carries this platform's build.
#[derive(Debug, Clone, PartialEq, serde::Serialize, serde::Deserialize)]
pub struct Candidate {
    pub version: String,
    pub asset_url: String,
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

/// The newest stable release, of GitHub's list, that carries a platform's
/// build and its checksums.
pub fn newest_with(releases: &[Value], platform: &Platform) -> Option<Candidate> {
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
                asset_url: url(r, platform.asset)?,
                sums_url: url(r, platform.sums)?,
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

// ---- A Mac: the app bundle ----

/// The app bundle (`…/Legato.app`) the executable at exe runs from, when
/// the app can replace it.
pub fn app_bundle(exe: &Path) -> Result<std::path::PathBuf, String> {
    let bundle = exe
        .parent()
        .filter(|p| p.ends_with("Contents/MacOS"))
        .and_then(|p| p.parent()?.parent())
        .filter(|b| b.extension().is_some_and(|e| e == "app"))
        .ok_or("This copy is not an app bundle, so it does not update itself.")?;
    let path = bundle.to_string_lossy();
    if path.contains("/AppTranslocation/") || path.starts_with("/Volumes/") {
        return Err("Move Legato to your Applications folder, and open it from there, to let it update itself.".into());
    }
    Ok(bundle.to_path_buf())
}

/// Puts the Legato.app in a gzipped tar in place of bundle: unpacked beside
/// it first (the same disk, so the swap is two renames), the old app kept
/// hidden as `.Legato.app.prev` (an older one removed), then the new one
/// renamed into place. Anything wrong with the archive leaves bundle as it
/// was.
pub fn install_bundle(bundle: &Path, tarball: &[u8]) -> Result<(), String> {
    use rand::RngCore;
    let parent = bundle.parent().ok_or("the app is in no folder")?;
    let name = bundle.file_name().ok_or("the app has no name")?;
    let fail = |e: std::io::Error| {
        if e.kind() == std::io::ErrorKind::PermissionDenied {
            format!(
                "macOS did not let Legato replace itself in {}. If it asked, allow Legato in System Settings › Privacy & Security › App Management, then try again.",
                parent.display()
            )
        } else {
            format!("{}: {e}", parent.display())
        }
    };
    let mut id = [0u8; 6];
    rand::rngs::OsRng.fill_bytes(&mut id);
    let staging = parent.join(format!(".legato-update-{}", hex::encode(id)));
    std::fs::create_dir(&staging).map_err(fail)?;

    let swapped = (|| {
        let mut archive = tar::Archive::new(flate2::read::GzDecoder::new(tarball));
        archive.set_preserve_permissions(true);
        archive.unpack(&staging).map_err(|e| format!("the update does not unpack: {e}"))?;
        let new = staging.join(name);
        let has_executable = std::fs::read_dir(new.join("Contents/MacOS")).is_ok_and(|mut d| d.next().is_some());
        if !has_executable {
            return Err(format!("the update holds no {}", name.to_string_lossy()));
        }
        let prev = parent.join(format!(".{}.prev", name.to_string_lossy()));
        if prev.exists() {
            std::fs::remove_dir_all(&prev).map_err(fail)?;
        }
        std::fs::rename(bundle, &prev).map_err(fail)?;
        std::fs::rename(&new, bundle).map_err(|e| {
            let _ = std::fs::rename(&prev, bundle);
            fail(e)
        })
    })();
    let _ = std::fs::remove_dir_all(&staging);
    swapped
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
        let c = newest_with(&releases, &LINUX).unwrap();
        assert_eq!(c.version, "v0.8.1");
        assert_eq!(c.asset_url, format!("https://example.test/v0.8.1/{APPIMAGE}"));
        assert_eq!(c.sums_url, format!("https://example.test/v0.8.1/{SUMS}"));
    }

    // The Mac job can fail while the Linux one attaches its build, so a Mac
    // copy looks for its own archive and checksums.
    #[test]
    fn a_mac_updates_to_the_newest_release_with_the_mac_app() {
        let releases = vec![
            release("v0.9.0", &[APPIMAGE, SUMS]),
            release("v0.8.1", &[APPIMAGE, SUMS, MACOS.asset, MACOS.sums]),
            release("v0.8.0", &[MACOS.asset]), // no checksums
        ];
        let c = newest_with(&releases, &MACOS).unwrap();
        assert_eq!(c.version, "v0.8.1");
        assert_eq!(c.asset_url, "https://example.test/v0.8.1/legato-desktop-macos-arm64.app.tar.gz");
        assert_eq!(c.sums_url, "https://example.test/v0.8.1/legato-desktop-macos-SHA256SUMS");
    }

    #[test]
    fn drafts_prereleases_and_releases_without_checksums_are_passed_over() {
        let mut draft = release("v1.0.0", &[APPIMAGE, SUMS]);
        draft["draft"] = json!(true);
        let mut pre = release("v0.9.0", &[APPIMAGE, SUMS]);
        pre["prerelease"] = json!(true);
        let releases = vec![draft, pre, release("v0.8.0", &[APPIMAGE]), release("v0.7.0", &[APPIMAGE, SUMS])];
        assert_eq!(newest_with(&releases, &LINUX).unwrap().version, "v0.7.0");
        assert_eq!(newest_with(&[], &LINUX), None);
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

    #[test]
    fn a_mac_copy_updates_the_app_bundle_it_runs_from() {
        let bundle = app_bundle(Path::new("/Applications/Legato.app/Contents/MacOS/legato-desktop")).unwrap();
        assert_eq!(bundle, Path::new("/Applications/Legato.app"));
        let home = app_bundle(Path::new("/Users/c/Applications/Legato.app/Contents/MacOS/legato-desktop")).unwrap();
        assert_eq!(home, Path::new("/Users/c/Applications/Legato.app"));
    }

    // Opened from Downloads with the quarantine flag, macOS runs the app
    // from a read-only copy elsewhere; opened from the disk image, it runs
    // from the image. Neither can be replaced: the app must be moved first.
    #[test]
    fn a_mac_copy_that_cannot_replace_itself_says_to_move_it_to_applications() {
        for exe in [
            "/private/var/folders/x1/T/AppTranslocation/0A1B/d/Legato.app/Contents/MacOS/legato-desktop",
            "/Volumes/Legato/Legato.app/Contents/MacOS/legato-desktop",
        ] {
            let err = app_bundle(Path::new(exe)).unwrap_err();
            assert!(err.contains("Applications"), "{exe}: {err}");
        }
        assert!(app_bundle(Path::new("/Users/c/legato/desktop/src-tauri/target/debug/legato-desktop")).is_err(), "not in a bundle");
    }

    /// A gzipped tar of files, as the release workflow makes Legato.app's.
    fn tarball(files: &[(&str, &[u8], u32)]) -> Vec<u8> {
        let mut builder = tar::Builder::new(flate2::write::GzEncoder::new(Vec::new(), flate2::Compression::fast()));
        for (path, body, mode) in files {
            let mut header = tar::Header::new_gnu();
            header.set_size(body.len() as u64);
            header.set_mode(*mode);
            header.set_cksum();
            builder.append_data(&mut header, path, *body).unwrap();
        }
        builder.into_inner().unwrap().finish().unwrap()
    }

    fn old_bundle(dir: &Path) -> std::path::PathBuf {
        let bundle = dir.join("Legato.app");
        std::fs::create_dir_all(bundle.join("Contents/MacOS")).unwrap();
        std::fs::write(bundle.join("Contents/MacOS/legato-desktop"), b"old").unwrap();
        bundle
    }

    fn leftovers(dir: &Path) -> Vec<String> {
        std::fs::read_dir(dir).unwrap().map(|e| e.unwrap().file_name().to_string_lossy().into_owned()).filter(|n| n != "Legato.app" && n != ".Legato.app.prev").collect()
    }

    #[cfg(unix)]
    #[test]
    fn installing_a_bundle_puts_the_new_app_in_place_and_keeps_the_old_one_hidden() {
        use std::os::unix::fs::PermissionsExt;
        let dir = tempfile::tempdir().unwrap();
        let bundle = old_bundle(dir.path());
        let new = tarball(&[("Legato.app/Contents/MacOS/legato-desktop", b"new", 0o755), ("Legato.app/Contents/Info.plist", b"<plist/>", 0o644)]);

        install_bundle(&bundle, &new).unwrap();

        let exe = bundle.join("Contents/MacOS/legato-desktop");
        assert_eq!(std::fs::read(&exe).unwrap(), b"new");
        assert_eq!(std::fs::metadata(&exe).unwrap().permissions().mode() & 0o111, 0o111, "still executable");
        assert_eq!(std::fs::read(dir.path().join(".Legato.app.prev/Contents/MacOS/legato-desktop")).unwrap(), b"old");
        assert!(leftovers(dir.path()).is_empty(), "left behind: {:?}", leftovers(dir.path()));

        // The next update replaces the kept copy.
        install_bundle(&bundle, &tarball(&[("Legato.app/Contents/MacOS/legato-desktop", b"newer", 0o755)])).unwrap();
        assert_eq!(std::fs::read(&exe).unwrap(), b"newer");
        assert_eq!(std::fs::read(dir.path().join(".Legato.app.prev/Contents/MacOS/legato-desktop")).unwrap(), b"new");
    }

    #[test]
    fn an_archive_without_the_app_leaves_the_app_as_it_was() {
        let dir = tempfile::tempdir().unwrap();
        let bundle = old_bundle(dir.path());
        for bad in [
            tarball(&[("Other.app/Contents/MacOS/x", b"x", 0o755)]),
            tarball(&[("Legato.app/Contents/Info.plist", b"<plist/>", 0o644)]), // no executable
            b"not a tarball".to_vec(),
        ] {
            assert!(install_bundle(&bundle, &bad).is_err());
            assert_eq!(std::fs::read(bundle.join("Contents/MacOS/legato-desktop")).unwrap(), b"old");
            assert!(!dir.path().join(".Legato.app.prev").exists());
            assert!(leftovers(dir.path()).is_empty(), "left behind: {:?}", leftovers(dir.path()));
        }
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
