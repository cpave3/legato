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
    ...overrides,
  }
}

describe("Scope messages", () => {
  it("recognises its own messages", () => {
    expect(isScopeMessage({ type: "listScopeOrigins" })).toBe(true)
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
