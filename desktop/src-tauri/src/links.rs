//! Where a link in a server's page belongs. The page's webview stays on the
//! Legato server it was opened for; a link to any other web address (a Jira
//! ticket, a Burnlite task, a pull request) goes to the system's default
//! browser. The Android app does the same (`allowNavigation` in the mobile
//! app's `PageContract.kt`).

use std::process::Command;
#[cfg(target_os = "linux")]
use std::{
    ffi::OsString,
    os::unix::ffi::{OsStrExt, OsStringExt},
    process::Stdio,
    time::{Duration, Instant},
};

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

/// The address a page that did not load starts again at: the one that failed
/// when it is a page of the server at `server_url`, else the server's root.
pub fn page_to_load(server_url: &str, failed: Option<&str>) -> String {
    failed
        .and_then(|f| Url::parse(f).ok())
        .filter(|u| matches!(u.scheme(), "http" | "https") && classify(server_url, u) == Link::Stay)
        .map(|u| u.to_string())
        .unwrap_or_else(|| server_url.to_string())
}

/// Hands `url` to the system's default handler for it, from a thread of its
/// own: for a page's link, whose handler runs on the main thread. The outcome
/// is logged as `[links]` lines.
pub fn open_in_background(url: Url) {
    let spawned = std::thread::Builder::new().name("open-link".into()).spawn(move || {
        let _ = open_in_browser(&url);
    });
    if let Err(e) = spawned {
        eprintln!("[links] could not start opening a link: {e}");
    }
}

/// Hands `url` to the system's default handler for it. Never through a shell.
/// On Linux it waits for the opener up to `OPENER_WAIT` and asks the desktop
/// portal when the opener fails, so an error means neither could open it.
pub fn open_in_browser(url: &Url) -> Result<(), String> {
    #[cfg(target_os = "linux")]
    {
        // Only the origin: an authorization address carries its state.
        let origin = url.origin().ascii_serialization();
        let outcome = Opener::new("xdg-open", std::env::vars_os(), OPENER_WAIT).open(url, portal_open);
        match &outcome {
            Ok(Opened::ByOpener) => println!("[links] {origin}: opened with xdg-open"),
            Ok(Opened::StillOpening) => println!("[links] {origin}: xdg-open still running after {}s, taken as opened", OPENER_WAIT.as_secs()),
            Ok(Opened::ByFallback(why)) => println!("[links] {origin}: xdg-open failed ({why}); opened by the desktop portal"),
            Err(e) => eprintln!("[links] {origin}: not opened: {e}"),
        }
        outcome.map(|_| ())
    }
    #[cfg(not(target_os = "linux"))]
    {
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
        cmd.spawn().map(|_| ()).map_err(|e| e.to_string())
    }
}

/// How long the opener has to say whether it opened a link. kde-open and
/// gio answer within a second; some of xdg-open's other ways run the browser
/// itself and only end with it.
#[cfg(target_os = "linux")]
const OPENER_WAIT: Duration = Duration::from_secs(5);

/// How long the desktop portal has to open a link.
#[cfg(target_os = "linux")]
const PORTAL_WAIT: Duration = Duration::from_secs(30);

/// Variables an AppImage's launcher sets that are not lists of its paths:
/// AppImageKit's AppRun (`APPDIR`, `APPIMAGE`, `ARGV0`, `OWD`,
/// `PYTHONDONTWRITEBYTECODE`) and linuxdeploy's GTK hook (`GTK_THEME`, which
/// it sets to Adwaita whatever the person had).
#[cfg(target_os = "linux")]
const LAUNCHER_ONLY: &[&str] = &["APPDIR", "APPIMAGE", "ARGV0", "OWD", "GTK_THEME", "PYTHONDONTWRITEBYTECODE"];

/// The environment for a program of the host's that the app starts, such as
/// the browser's opener: the app's own, with what its AppImage's launcher
/// added taken out again (LEG-221). The launcher puts the AppImage's folders
/// first in `LD_LIBRARY_PATH`, `PATH`, `XDG_DATA_DIRS`, `QT_PLUGIN_PATH` and
/// others, so the host's `kde-open` loaded the AppImage's older `libcrypto`
/// and died before it opened anything. Each entry inside `APPDIR` is removed
/// from every variable, a variable left with nothing is removed, and so are
/// `LAUNCHER_ONLY`. The launcher keeps no copy of what it replaced, so a
/// variable it overwrote (`GTK_THEME`) cannot be given back. Outside an
/// AppImage (no `APPDIR`, or one that is not an absolute folder) the
/// environment is passed on as it is.
#[cfg(target_os = "linux")]
pub fn host_environment(vars: impl IntoIterator<Item = (OsString, OsString)>) -> Vec<(OsString, OsString)> {
    let vars: Vec<(OsString, OsString)> = vars.into_iter().collect();
    let appdir = vars.iter().find(|(k, _)| k == "APPDIR").map(|(_, v)| v.as_bytes().to_vec());
    let Some(appdir) = appdir.map(|mut d| {
        while d.len() > 1 && d.ends_with(b"/") {
            d.pop();
        }
        d
    }) else {
        return vars;
    };
    if !appdir.starts_with(b"/") || appdir == b"/" {
        return vars;
    }
    let inside = |entry: &[u8]| entry.starts_with(&appdir) && (entry.len() == appdir.len() || entry[appdir.len()] == b'/');
    vars.into_iter()
        .filter(|(k, _)| !LAUNCHER_ONLY.iter().any(|name| k == name))
        .filter_map(|(k, v)| {
            let entries: Vec<&[u8]> = v.as_bytes().split(|b| *b == b':').collect();
            if !entries.iter().any(|e| inside(e)) {
                return Some((k, v));
            }
            let kept: Vec<&[u8]> = entries.into_iter().filter(|e| !e.is_empty() && !inside(e)).collect();
            (!kept.is_empty()).then(|| (k, OsString::from_vec(kept.join(&b':'))))
        })
        .collect()
}

#[cfg(target_os = "linux")]
#[derive(Debug, PartialEq, Eq)]
pub enum Opened {
    /// The opener ended saying it opened the link.
    ByOpener,
    /// The opener was still running when the wait ended.
    StillOpening,
    /// The opener could not, for the reason given, and the fallback did.
    ByFallback(String),
}

/// A program that opens a link in the person's browser (`xdg-open`), run
/// with the host's environment (`host_environment`).
#[cfg(target_os = "linux")]
pub struct Opener {
    program: OsString,
    env: Vec<(OsString, OsString)>,
    wait: Duration,
}

#[cfg(target_os = "linux")]
impl Opener {
    /// `program` as a bare name is found on the host's `PATH`, never on the
    /// AppImage's.
    pub fn new(program: impl Into<OsString>, vars: impl IntoIterator<Item = (OsString, OsString)>, wait: Duration) -> Opener {
        Opener { program: program.into(), env: host_environment(vars), wait }
    }

    /// Runs the opener on `url` (one argument, no shell) and waits for it up
    /// to the wait. When it cannot start or ends with a failure, `fallback`
    /// is given the link. A child still running is waited for in a thread of
    /// its own, so it never lingers as a zombie.
    pub fn open(&self, url: &Url, fallback: impl FnOnce(&Url) -> Result<(), String>) -> Result<Opened, String> {
        let why = match self.run(url) {
            Ok(None) => return Ok(Opened::ByOpener),
            Ok(Some(why)) => why,
            Err(e) if e == STILL_OPENING => return Ok(Opened::StillOpening),
            Err(e) => e,
        };
        match fallback(url) {
            Ok(()) => Ok(Opened::ByFallback(why)),
            Err(e) => Err(format!("{}: {why}; the desktop portal: {e}", self.program.to_string_lossy())),
        }
    }

    /// `Ok(None)`: opened; `Ok(Some(why))`: it ended in failure; `Err`: it
    /// did not start, or `STILL_OPENING`.
    fn run(&self, url: &Url) -> Result<Option<String>, String> {
        let mut child = Command::new(&self.program)
            .env_clear()
            .envs(self.env.iter().map(|(k, v)| (k, v)))
            .arg(url.as_str())
            .stdin(Stdio::null())
            .spawn()
            .map_err(|e| format!("could not start: {e}"))?;
        let deadline = Instant::now() + self.wait;
        loop {
            match child.try_wait() {
                Ok(Some(status)) if status.success() => return Ok(None),
                Ok(Some(status)) => return Ok(Some(status.to_string())),
                Ok(None) if Instant::now() >= deadline => {
                    std::thread::spawn(move || child.wait());
                    return Err(STILL_OPENING.into());
                }
                Ok(None) => std::thread::sleep(Duration::from_millis(20)),
                Err(e) => return Ok(Some(e.to_string())),
            }
        }
    }
}

#[cfg(target_os = "linux")]
const STILL_OPENING: &str = "still opening";

/// Asks the desktop portal (`org.freedesktop.portal.OpenURI`) to open `url`.
/// The portal starts the handler from its own environment, so nothing of the
/// app's reaches it. On a thread of its own, since a caller may already be on
/// the async runtime.
#[cfg(target_os = "linux")]
fn portal_open(url: &Url) -> Result<(), String> {
    let uri = ashpd::Uri::parse(url.as_str()).map_err(|e| e.to_string())?;
    std::thread::spawn(move || {
        tauri::async_runtime::block_on(async move {
            let ask = async {
                let request = ashpd::desktop::open_uri::OpenFileRequest::default().ask(false).send_uri(&uri).await.map_err(|e| e.to_string())?;
                request.response().map_err(|e| e.to_string())
            };
            tokio::time::timeout(PORTAL_WAIT, ask).await.map_err(|_| format!("no answer in {}s", PORTAL_WAIT.as_secs()))?
        })
    })
    .join()
    .map_err(|_| "the portal call panicked".to_string())?
}

#[cfg(test)]
mod tests {
    use super::*;

    fn link(server: &str, target: &str) -> Link {
        classify(server, &Url::parse(target).unwrap())
    }

    const SERVER: &str = "https://legato.local:8443";

    #[test]
    fn a_page_that_failed_loads_again_where_it_was_only_on_its_own_server() {
        assert_eq!(page_to_load(SERVER, Some("https://legato.local:8443/board?highlight_task=a1")), "https://legato.local:8443/board?highlight_task=a1");
        assert_eq!(page_to_load(SERVER, None), SERVER);
        assert_eq!(page_to_load(SERVER, Some("")), SERVER);
        assert_eq!(page_to_load(SERVER, Some("https://evil.example/board")), SERVER);
        assert_eq!(page_to_load(SERVER, Some("about:blank")), SERVER);
        assert_eq!(page_to_load(SERVER, Some("javascript:alert(1)")), SERVER);
    }

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

/// The opener on Linux (LEG-221): what the program the app starts for a link
/// is given of the app's environment, and what happens when it fails.
#[cfg(all(test, target_os = "linux"))]
mod opener_tests {
    use super::*;
    use std::ffi::OsString;
    use std::os::unix::fs::PermissionsExt;
    use std::path::{Path, PathBuf};
    use std::time::{Duration, Instant};

    const APPDIR: &str = "/tmp/.mount_legatoLkGIOb";

    fn vars(pairs: &[(&str, &str)]) -> Vec<(OsString, OsString)> {
        pairs.iter().map(|(k, v)| (OsString::from(k), OsString::from(v))).collect()
    }

    fn get<'a>(env: &'a [(OsString, OsString)], key: &str) -> Option<&'a str> {
        env.iter().find(|(k, _)| k == key).map(|(_, v)| v.to_str().unwrap())
    }

    /// The environment of the v0.25.0 AppImage on erebus (KDE Plasma on
    /// Wayland), as /proc/<pid>/environ had it, trimmed to what matters.
    fn appimage_env() -> Vec<(OsString, OsString)> {
        vars(&[
            ("APPDIR", APPDIR),
            ("APPIMAGE", "/var/home/cameron/AppImages/legato.appimage"),
            ("ARGV0", "/home/cameron/AppImages/legato.appimage"),
            ("OWD", "/var/home/cameron"),
            ("LD_LIBRARY_PATH", "/tmp/.mount_legatoLkGIOb/usr/lib/:/tmp/.mount_legatoLkGIOb/usr/lib/x86_64-linux-gnu/:/tmp/.mount_legatoLkGIOb/lib64/:"),
            ("PATH", "/tmp/.mount_legatoLkGIOb/usr/bin/:/tmp/.mount_legatoLkGIOb/usr/sbin/:/tmp/.mount_legatoLkGIOb/bin/:/usr/local/bin:/usr/bin:/bin"),
            ("XDG_DATA_DIRS", "/tmp/.mount_legatoLkGIOb/usr/share/:/tmp/.mount_legatoLkGIOb/usr/share:/usr/share:/home/cameron/.local/share/flatpak/exports/share:/var/lib/flatpak/exports/share"),
            ("GSETTINGS_SCHEMA_DIR", "/tmp/.mount_legatoLkGIOb/usr/share/glib-2.0/schemas/:/tmp/.mount_legatoLkGIOb//usr/share/glib-2.0/schemas"),
            ("GIO_MODULE_DIR", "/tmp/.mount_legatoLkGIOb//usr/lib/gio/modules"),
            ("GI_TYPELIB_PATH", "/tmp/.mount_legatoLkGIOb//usr/lib/girepository-1.0"),
            ("GDK_PIXBUF_MODULE_FILE", "/tmp/.mount_legatoLkGIOb//usr/lib/gdk-pixbuf-2.0/2.10.0/loaders.cache"),
            ("GTK_DATA_PREFIX", APPDIR),
            ("GTK_EXE_PREFIX", "/tmp/.mount_legatoLkGIOb//usr"),
            ("GTK_PATH", "/tmp/.mount_legatoLkGIOb//usr/lib/gtk-3.0"),
            ("GTK_IM_MODULE_FILE", "/tmp/.mount_legatoLkGIOb//usr/lib/gtk-3.0/3.0.0/immodules.cache"),
            ("GTK_THEME", "Adwaita:dark"),
            ("PYTHONHOME", "/tmp/.mount_legatoLkGIOb/usr/"),
            ("PYTHONPATH", "/tmp/.mount_legatoLkGIOb/usr/share/pyshared/:"),
            ("PYTHONDONTWRITEBYTECODE", "1"),
            ("PERLLIB", "/tmp/.mount_legatoLkGIOb/usr/share/perl5/:/tmp/.mount_legatoLkGIOb/usr/lib/perl5/:"),
            ("QT_PLUGIN_PATH", "/tmp/.mount_legatoLkGIOb/usr/lib/qt5/plugins/:/tmp/.mount_legatoLkGIOb/usr/lib64/qt5/plugins/:"),
            ("GTK_RC_FILES", "/etc/gtk/gtkrc:/home/cameron/.gtkrc:/home/cameron/.config/gtkrc"),
            ("DBUS_SESSION_BUS_ADDRESS", "unix:path=/run/user/1000/bus"),
            ("DISPLAY", ":0"),
            ("WAYLAND_DISPLAY", "wayland-0"),
            ("XDG_RUNTIME_DIR", "/run/user/1000"),
            ("XDG_CURRENT_DESKTOP", "KDE"),
            ("XDG_SESSION_TYPE", "wayland"),
            ("KDE_FULL_SESSION", "true"),
            ("HOME", "/var/home/cameron"),
        ])
    }

    #[test]
    fn the_appimages_libraries_never_reach_the_opener() {
        let env = host_environment(appimage_env());
        // The cause of LEG-221: kde-open loaded the AppImage's libcrypto.
        assert_eq!(get(&env, "LD_LIBRARY_PATH"), None);
        for key in ["GSETTINGS_SCHEMA_DIR", "GIO_MODULE_DIR", "GI_TYPELIB_PATH", "GDK_PIXBUF_MODULE_FILE", "GTK_DATA_PREFIX", "GTK_EXE_PREFIX", "GTK_PATH", "GTK_IM_MODULE_FILE", "PYTHONHOME", "PYTHONPATH", "PERLLIB", "QT_PLUGIN_PATH"] {
            assert_eq!(get(&env, key), None, "{key}");
        }
    }

    #[test]
    fn path_lists_keep_the_hosts_own_entries_in_order() {
        let env = host_environment(appimage_env());
        assert_eq!(get(&env, "PATH"), Some("/usr/local/bin:/usr/bin:/bin"));
        assert_eq!(get(&env, "XDG_DATA_DIRS"), Some("/usr/share:/home/cameron/.local/share/flatpak/exports/share:/var/lib/flatpak/exports/share"));
        assert_eq!(get(&env, "GTK_RC_FILES"), Some("/etc/gtk/gtkrc:/home/cameron/.gtkrc:/home/cameron/.config/gtkrc"));
    }

    #[test]
    fn what_only_the_appimages_launcher_sets_is_gone() {
        let env = host_environment(appimage_env());
        for key in ["APPDIR", "APPIMAGE", "ARGV0", "OWD", "GTK_THEME", "PYTHONDONTWRITEBYTECODE"] {
            assert_eq!(get(&env, key), None, "{key}");
        }
    }

    #[test]
    fn the_session_is_passed_on_as_it_is() {
        let env = host_environment(appimage_env());
        for (key, value) in [("DBUS_SESSION_BUS_ADDRESS", "unix:path=/run/user/1000/bus"), ("DISPLAY", ":0"), ("WAYLAND_DISPLAY", "wayland-0"), ("XDG_RUNTIME_DIR", "/run/user/1000"), ("XDG_CURRENT_DESKTOP", "KDE"), ("XDG_SESSION_TYPE", "wayland"), ("KDE_FULL_SESSION", "true"), ("HOME", "/var/home/cameron")] {
            assert_eq!(get(&env, key), Some(value), "{key}");
        }
    }

    #[test]
    fn outside_an_appimage_nothing_changes() {
        let own = vars(&[("LD_LIBRARY_PATH", "/opt/mine/lib"), ("GTK_THEME", "Breeze"), ("PATH", "/usr/bin:/bin"), ("PYTHONDONTWRITEBYTECODE", "1")]);
        assert_eq!(host_environment(own.clone()), own);
        // An APPDIR that names no folder of its own is not an AppImage's.
        for appdir in ["", "/", "relative/dir"] {
            let mut with = own.clone();
            with.push((OsString::from("APPDIR"), OsString::from(appdir)));
            assert_eq!(host_environment(with.clone()), with, "APPDIR={appdir:?}");
        }
    }

    #[test]
    fn a_folder_beside_the_appimages_is_not_inside_it() {
        let env = host_environment(vars(&[("APPDIR", "/tmp/.mount_abc"), ("LD_LIBRARY_PATH", "/tmp/.mount_abc/usr/lib:/tmp/.mount_abcd/lib:/tmp/.mount_abc")]));
        assert_eq!(get(&env, "LD_LIBRARY_PATH"), Some("/tmp/.mount_abcd/lib"));
    }

    // ---- Running the opener ----

    /// A folder holding a stand-in `xdg-open` that writes its arguments and
    /// environment beside itself and exits with `status` (after `sleep`).
    struct Stand {
        dir: tempfile::TempDir,
    }

    impl Stand {
        fn new(status: i32, sleep: &str) -> Stand {
            let dir = tempfile::tempdir().unwrap();
            Stand::script(dir.path(), status, sleep);
            Stand { dir }
        }

        fn script(bin: &Path, status: i32, sleep: &str) {
            std::fs::create_dir_all(bin).unwrap();
            let path = bin.join("xdg-open");
            let out = bin.join("seen");
            std::fs::write(&path, format!("#!/bin/sh\nprintf '%s\\n' \"$#\" \"$1\" > '{o}'\nenv >> '{o}'\nsleep {sleep}\nexit {status}\n", o = out.display())).unwrap();
            std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o755)).unwrap();
        }

        fn program(&self) -> PathBuf {
            self.dir.path().join("xdg-open")
        }

        fn seen(&self) -> Option<String> {
            std::fs::read_to_string(self.dir.path().join("seen")).ok()
        }
    }

    fn url() -> Url {
        Url::parse("https://auth.example/authorize?client_id=a&state=$(id);x&redirect_uri=http%3A%2F%2F127.0.0.1%3A4711%2Fcallback").unwrap()
    }

    fn with_path(path: &str) -> Vec<(OsString, OsString)> {
        let mut env: Vec<_> = appimage_env().into_iter().filter(|(k, _)| k != "PATH").collect();
        env.push((OsString::from("PATH"), OsString::from(path)));
        env
    }

    #[test]
    fn the_opener_gets_the_link_as_one_argument_and_none_of_the_appimage() {
        let stand = Stand::new(0, "0");
        let mut asked = false;
        let opened = Opener::new(stand.program(), appimage_env(), Duration::from_secs(5)).open(&url(), |_| {
            asked = true;
            Ok(())
        });
        assert_eq!(opened, Ok(Opened::ByOpener));
        assert!(!asked, "the portal is not asked when the opener opened it");
        let seen = stand.seen().expect("the opener ran");
        let mut lines = seen.lines();
        assert_eq!(lines.next(), Some("1"));
        assert_eq!(lines.next(), Some(url().as_str()));
        assert!(!seen.contains(APPDIR), "the opener saw the AppImage:\n{seen}");
        assert!(seen.contains("DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus"));
    }

    #[test]
    fn a_bare_xdg_open_is_the_hosts_not_one_the_appimage_carries() {
        let root = tempfile::tempdir().unwrap();
        let appdir = root.path().join("mount");
        let host = root.path().join("host");
        Stand::script(&appdir.join("usr/bin"), 0, "0");
        Stand::script(&host, 0, "0");
        let mut env = with_path(&format!("{}/usr/bin:{}:/usr/bin:/bin", appdir.display(), host.display()));
        env.retain(|(k, _)| k != "APPDIR");
        env.push((OsString::from("APPDIR"), appdir.clone().into_os_string()));
        let opened = Opener::new("xdg-open", env, Duration::from_secs(5)).open(&url(), |_| Ok(()));
        assert_eq!(opened, Ok(Opened::ByOpener));
        assert!(host.join("seen").exists(), "the host's xdg-open ran");
        assert!(!appdir.join("usr/bin/seen").exists(), "the AppImage's xdg-open ran");
    }

    #[test]
    fn an_opener_that_fails_hands_the_link_to_the_portal() {
        let stand = Stand::new(4, "0");
        let mut given = None;
        let opened = Opener::new(stand.program(), appimage_env(), Duration::from_secs(5)).open(&url(), |u| {
            given = Some(u.clone());
            Ok(())
        });
        assert!(matches!(&opened, Ok(Opened::ByFallback(why)) if why.contains("exit status: 4")), "{opened:?}");
        assert_eq!(given, Some(url()));
    }

    #[test]
    fn a_missing_opener_hands_the_link_to_the_portal() {
        let dir = tempfile::tempdir().unwrap();
        let mut given = None;
        let opened = Opener::new(dir.path().join("no-such-opener"), appimage_env(), Duration::from_secs(5)).open(&url(), |u| {
            given = Some(u.clone());
            Ok(())
        });
        assert!(matches!(opened, Ok(Opened::ByFallback(_))), "{opened:?}");
        assert_eq!(given, Some(url()));
    }

    #[test]
    fn when_neither_opens_it_the_error_says_why_for_both() {
        let stand = Stand::new(1, "0");
        let opened = Opener::new(stand.program(), appimage_env(), Duration::from_secs(5)).open(&url(), |_| Err("no portal on the session bus".into()));
        let error = opened.unwrap_err();
        assert!(error.contains("exit status: 1"), "{error}");
        assert!(error.contains("no portal on the session bus"), "{error}");
    }

    #[test]
    fn an_opener_still_running_after_the_wait_counts_as_opening() {
        // Some of xdg-open's ways run the browser itself and wait for it.
        let stand = Stand::new(0, "3");
        let started = Instant::now();
        let mut asked = false;
        let opened = Opener::new(stand.program(), appimage_env(), Duration::from_millis(300)).open(&url(), |_| {
            asked = true;
            Ok(())
        });
        assert_eq!(opened, Ok(Opened::StillOpening));
        assert!(!asked);
        assert!(started.elapsed() < Duration::from_secs(2), "waited {:?}", started.elapsed());
    }
}
