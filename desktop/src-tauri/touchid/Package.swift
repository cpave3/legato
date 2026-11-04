// swift-tools-version:5.5
// The Secure Enclave and Touch ID for the desktop app on a Mac, as three C
// functions src/touchid.rs calls. build.rs builds it with swift-rs, on a Mac
// only.
import PackageDescription

let package = Package(
    name: "LegatoTouchID",
    platforms: [.macOS(.v12)],
    products: [
        .library(name: "LegatoTouchID", type: .static, targets: ["LegatoTouchID"]),
    ],
    targets: [
        .target(name: "LegatoTouchID", path: "Sources/LegatoTouchID"),
    ]
)
