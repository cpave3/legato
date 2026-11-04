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
    tauri_build::build()
}
