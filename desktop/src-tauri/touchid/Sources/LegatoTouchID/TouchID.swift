// A Secure Enclave key that signs only after a fingerprint. The key is
// never in the Keychain: CryptoKit gives it as a blob only this Mac's chip
// can use, which the app keeps in its state file, so no entitlement or
// developer account is needed and the key works from every build of the
// app. Buffers are the caller's; each function answers 0 when it worked,
// 2 when the person cancelled, and 1 with words in err otherwise.

import CryptoKit
import Foundation
import LocalAuthentication
import Security

private let worked: Int32 = 0
private let failed: Int32 = 1
private let cancelled: Int32 = 2

/// Writes text into out as UTF-8, cut to fit, and a NUL.
private func put(_ text: String, _ out: UnsafeMutablePointer<UInt8>, _ cap: Int) {
    let bytes = Array(text.utf8.prefix(max(cap - 1, 0)))
    for (i, b) in bytes.enumerated() { out[i] = b }
    if cap > 0 { out[bytes.count] = 0 }
}

private func copy(_ data: Data, _ out: UnsafeMutablePointer<UInt8>) {
    for (i, b) in data.enumerated() { out[i] = b }
}

/// Whether error is the person, or the system, calling Touch ID off.
private func isCancel(_ error: Error) -> Bool {
    let ns = error as NSError
    switch ns.domain {
    case LAErrorDomain:
        // userCancel, systemCancel, appCancel
        return [-2, -4, -9].contains(ns.code)
    case "CryptoTokenKit":
        // TKError.canceledByUser
        return ns.code == -4
    default:
        return false
    }
}

private func refuse(_ error: Error, _ err: UnsafeMutablePointer<UInt8>, _ errCap: Int) -> Int32 {
    if isCancel(error) { return cancelled }
    let ns = error as NSError
    put("\(ns.localizedDescription) [\(ns.domain) \(ns.code)]", err, errCap)
    return failed
}

@_cdecl("legato_touchid_available")
public func legatoTouchIDAvailable(_ err: UnsafeMutablePointer<UInt8>, _ errCap: Int) -> Int32 {
    guard SecureEnclave.isAvailable else {
        put("This Mac has no Secure Enclave.", err, errCap)
        return failed
    }
    var error: NSError?
    if LAContext().canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: &error) {
        return worked
    }
    put(error?.localizedDescription ?? "Touch ID is not set up on this Mac.", err, errCap)
    return failed
}

/// Makes a key: its blob into blob (blobLen bytes) and its public key, 65
/// bytes, uncompressed, into publicKey. No fingerprint is asked for.
@_cdecl("legato_touchid_create")
public func legatoTouchIDCreate(
    _ blob: UnsafeMutablePointer<UInt8>, _ blobCap: Int, _ blobLen: UnsafeMutablePointer<Int>,
    _ publicKey: UnsafeMutablePointer<UInt8>,
    _ err: UnsafeMutablePointer<UInt8>, _ errCap: Int
) -> Int32 {
    var cfError: Unmanaged<CFError>?
    guard let access = SecAccessControlCreateWithFlags(
        kCFAllocatorDefault,
        kSecAttrAccessibleWhenUnlockedThisDeviceOnly,
        [.privateKeyUsage, .biometryCurrentSet],
        &cfError
    ) else {
        put("The Secure Enclave refused the access control: \(String(describing: cfError?.takeRetainedValue()))", err, errCap)
        return failed
    }
    do {
        let key = try SecureEnclave.P256.Signing.PrivateKey(accessControl: access)
        let data = key.dataRepresentation
        let point = key.publicKey.x963Representation
        guard data.count <= blobCap, point.count == 65 else {
            put("The Secure Enclave gave a \(data.count)-byte key and a \(point.count)-byte public key.", err, errCap)
            return failed
        }
        copy(data, blob)
        blobLen.pointee = data.count
        copy(point, publicKey)
        return worked
    } catch {
        return refuse(error, err, errCap)
    }
}

/// Asks for a fingerprint, the dialog naming reason, and signs data with
/// the key in blob: ECDSA P-256 over its SHA-256, DER, into sig.
@_cdecl("legato_touchid_sign")
public func legatoTouchIDSign(
    _ blob: UnsafePointer<UInt8>, _ blobLen: Int,
    _ data: UnsafePointer<UInt8>, _ dataLen: Int,
    _ reason: UnsafePointer<CChar>,
    _ sig: UnsafeMutablePointer<UInt8>, _ sigCap: Int, _ sigLen: UnsafeMutablePointer<Int>,
    _ err: UnsafeMutablePointer<UInt8>, _ errCap: Int
) -> Int32 {
    do {
        let context = LAContext()
        context.localizedReason = String(cString: reason)
        let key = try SecureEnclave.P256.Signing.PrivateKey(
            dataRepresentation: Data(bytes: blob, count: blobLen),
            authenticationContext: context
        )
        let signature = try key.signature(for: Data(bytes: data, count: dataLen)).derRepresentation
        guard signature.count <= sigCap else {
            put("The signature is \(signature.count) bytes.", err, errCap)
            return failed
        }
        copy(signature, sig)
        sigLen.pointee = signature.count
        return worked
    } catch {
        return refuse(error, err, errCap)
    }
}
