import { afterEach, describe, expect, it, vi } from "vitest"
import { fromBase64Url, passkeyForThisPage, passkeysSupported, signedMessage, toBase64Url } from "../passkeys"

describe("passkeys", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("builds the message a phone and the server build, byte for byte", () => {
    const canonical = '{"approval":"ap-1","v":1}'
    expect(signedMessage("Q2hh", "dev-1", "once", canonical))
      .toBe('{"challenge":"Q2hh","device":"dev-1","option":"once","request":{"approval":"ap-1","v":1},"v":1}')
  })

  it("converts base64url both ways", () => {
    const bytes = new Uint8Array([0, 250, 251, 252, 253, 254, 255])
    const text = toBase64Url(bytes)
    expect(text).not.toMatch(/[+/=]/)
    expect(Array.from(new Uint8Array(fromBase64Url(text)))).toEqual(Array.from(bytes))
  })

  it("needs a secure page at a host name", () => {
    vi.stubGlobal("PublicKeyCredential", function PublicKeyCredential() {})
    expect(passkeysSupported({ secure: true, hostname: "erebus.pavey.network" })).toBe(true)
    expect(passkeysSupported({ secure: true, hostname: "localhost" })).toBe(true)
    expect(passkeysSupported({ secure: false, hostname: "erebus.pavey.network" })).toBe(false)
    expect(passkeysSupported({ secure: true, hostname: "192.168.1.10" })).toBe(false)
  })

  it("finds the active passkey made on this page", () => {
    const devices = [
      { id: "dev-a", kind: "android", name: "Pixel", status: "active", created_at: "" },
      { id: "dev-b", kind: "passkey", name: "Old", status: "removed", origin: "https://erebus.example", created_at: "" },
      { id: "dev-c", kind: "passkey", name: "Other page", status: "active", origin: "https://localhost:3080", created_at: "" },
      { id: "dev-d", kind: "passkey", name: "Desktop", status: "active", origin: "https://erebus.example", created_at: "" },
    ]
    expect(passkeyForThisPage(devices, "https://erebus.example")?.id).toBe("dev-d")
    expect(passkeyForThisPage(devices, "https://elsewhere.example")).toBeUndefined()
  })
})
