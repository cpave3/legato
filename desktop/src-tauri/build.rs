fn main() {
    // On a Mac, the Secure Enclave and Touch ID are reached through a little
    // Swift (touchid/), built and linked here. This runs on the machine that
    // builds, which for the Mac app is always a Mac.
    #[cfg(target_os = "macos")]
    {
        swift_rs::SwiftLinker::new("12.0").with_package("LegatoTouchID", "touchid").link();
        for framework in ["CryptoKit", "LocalAuthentication", "Security", "Foundation"] {
            println!("cargo:rustc-link-lib=framework={framework}");
        }
    }
    // Each command gets allow-<command> and deny-<command>, and a webview
    // may call only those its capability allows: the voice widget gets the
    // few it uses (capabilities/voice.json), not signing or pairing. Keep
    // this list as generate_handler! in app.rs has it.
    let manifest = tauri_build::AppManifest::new().commands(COMMANDS);
    tauri_build::try_build(tauri_build::Attributes::new().app_manifest(manifest)).expect("the Tauri build step")
}

const COMMANDS: &[&str] = &[
    "servers",
    "show_server",
    "page_approval_done",
    "open_approval_in_page",
    "page_trusted",
    "trust_server_certificate",
    "save_server_certificate",
    "open_certificate",
    "set_content_visible",
    "reload_server",
    "load_server_page",
    "pair_server",
    "discovered",
    "rename_server",
    "remove_server",
    "reorder_servers",
    "poll",
    "keys",
    "add_key",
    "touch_id",
    "add_touch_id",
    "enroll_key",
    "revoke_key",
    "prepare",
    "sign",
    "deny",
    "check_update",
    "apply_update",
    "restart_app",
    "take_import_requests",
    "import_preference",
    "set_import_preference",
    "import_ticket",
    "open_task_in_page",
    "voice_key_info",
    "change_voice_key",
    "set_hold_to_talk",
    "voice_targets",
    "set_voice_server",
    "voice_action",
    "voice_status",
    "voice_open_settings",
    "voice_widget_size",
    "voice_widget_menu",
    "voice_widget_settings",
    "set_voice_widget",
];
