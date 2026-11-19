import { describe, expect, it, vi } from "vitest"
import { createScopeMessageHandler, isScopeMessage, type ScopeServiceDeps } from "./scope-service"

const extension = { url: "chrome-extension://abc/options.html" }
const page = (url = "http://localhost:5173/settings") => ({ tab: { id: 4, url }, url })

function deps(overrides: Partial<ScopeServiceDeps> = {}): ScopeServiceDeps {
  return {
    extensionUrl: "chrome-extension://abc/",
    origins: {
      list: vi.fn(async () => [{ origin: "http://localhost:5173", allowed: true }]),
      remove: vi.fn(async () => []),
      isAllowed: vi.fn(async (origin: string) => origin === "http://localhost:5173"),
    },
    releasePermission: vi.fn(async () => undefined),
    captureTab: vi.fn(async () => "data:image/png;base64,PNG"),
    crop: vi.fn(async () => "data:image/jpeg;base64,JPEG"),
    ...overrides,
  }
}

describe("Scope messages", () => {
  it("recognises its own messages", () => {
    expect(isScopeMessage({ type: "listScopeOrigins" })).toBe(true)
    expect(isScopeMessage({ type: "scopeCapture" })).toBe(true)
    expect(isScopeMessage({ type: "importTicket" })).toBe(false)
    expect(isScopeMessage(null)).toBe(false)
  })

  it("lists the allowed origins for the options page", async () => {
    const d = deps()
    expect(await createScopeMessageHandler(d)({ type: "listScopeOrigins" }, extension)).toEqual({ ok: true, origins: [{ origin: "http://localhost:5173", allowed: true }] })
  })

  it("removes an origin and gives Chrome's access back, from the options page only", async () => {
    const d = deps()
    const handle = createScopeMessageHandler(d)
    expect(await handle({ type: "removeScopeOrigin", origin: "http://localhost:5173" }, page())).toEqual({ ok: false, error: "Only the extension's own pages may do that" })
    expect(d.origins.remove).not.toHaveBeenCalled()
    expect(await handle({ type: "removeScopeOrigin", origin: "http://localhost:5173" }, extension)).toEqual({ ok: true, origins: [] })
    expect(d.origins.remove).toHaveBeenCalledWith("http://localhost:5173")
    expect(d.releasePermission).toHaveBeenCalledWith("http://localhost:5173")
  })

  it("refuses an origin that is not one", async () => {
    const handle = createScopeMessageHandler(deps())
    expect(await handle({ type: "removeScopeOrigin", origin: "http://localhost:5173/x" }, extension)).toEqual({ ok: false, error: "Invalid origin" })
  })
})

describe("a pin's screenshot", () => {
  const box = { x: 10, y: 20, width: 30, height: 40 }
  const viewport = { width: 1280, height: 800 }
  const tab = { tab: { id: 4, windowId: 9, url: "http://localhost:5173/settings" }, url: "http://localhost:5173/settings" }

  it("captures the sender's visible tab and crops it around the element", async () => {
    const d = deps()
    const answer = await createScopeMessageHandler(d)({ type: "scopeCapture", box, viewport }, tab)
    expect(answer).toEqual({ ok: true, screenshot: "data:image/jpeg;base64,JPEG" })
    expect(d.captureTab).toHaveBeenCalledWith(9)
    expect(d.crop).toHaveBeenCalledWith("data:image/png;base64,PNG", box, viewport)
  })

  it("answers without a screenshot when the element is not in view", async () => {
    const d = deps({ crop: vi.fn(async () => null) })
    expect(await createScopeMessageHandler(d)({ type: "scopeCapture", box, viewport }, tab)).toEqual({ ok: true })
  })

  it("refuses a page Scope is not allowed on, and a malformed box", async () => {
    const d = deps()
    const handle = createScopeMessageHandler(d)
    const other = { tab: { id: 4, windowId: 9, url: "https://other.test/" }, url: "https://other.test/" }
    expect(await handle({ type: "scopeCapture", box, viewport }, other)).toEqual({ ok: false, error: "Scope is not allowed on https://other.test" })
    expect(await handle({ type: "scopeCapture", box: { x: "1" }, viewport }, tab)).toEqual({ ok: false, error: "Invalid box" })
    expect(d.captureTab).not.toHaveBeenCalled()
  })
})
