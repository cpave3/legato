import { describe, expect, it, vi } from "vitest"
import { LegatoRequestError, unreachableMessage } from "./legato"
import { createScopeMessageHandler, isScopeMessage, type ScopeServiceDeps } from "./scope-service"
import type { ScopePin, SentPin } from "./scope-types"

const extension = { url: "chrome-extension://abc/options.html" }
const page = (url = "http://localhost:5173/settings") => ({ tab: { id: 4, url }, url })

const sentPin: SentPin = {
  id: "sp-1", batch_id: "sb-1", origin: "http://localhost:5173", url: "http://localhost:5173/settings", path: "/settings",
  comment: "Too much padding", state: "fixed", note: "Reduced to 8px", selectors: ["#save"], box: { x: 1, y: 2, width: 3, height: 4 },
  has_screenshot: false, created_at: "2026-11-19T10:00:00Z", updated_at: "2026-11-19T11:00:00Z",
}

const api = {
  sendScopeBatch: vi.fn(async () => ({ batch_id: "sb-1", pin_ids: ["sp-1"], subscribed: false, delivered: false })),
  listScopePins: vi.fn(async () => [sentPin]),
  reopenScopePin: vi.fn(async () => ({ ...sentPin, state: "sent" as const })),
  withdrawScopePin: vi.fn(async () => undefined),
}

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
    legato: vi.fn(async () => ({ baseUrl: "http://localhost:3080", api })),
    live: { watch: vi.fn(), refresh: vi.fn(async () => undefined) },
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

describe("sending pins and following them", () => {
  const tab = { tab: { id: 4, windowId: 9, url: "http://localhost:5173/settings" }, url: "http://localhost:5173/settings" }
  const pin = (overrides: Partial<ScopePin> = {}): ScopePin => ({
    comment: "Too much padding",
    page: { url: "http://localhost:5173/settings", title: "Settings", viewport: { width: 1, height: 1 }, scroll: { x: 0, y: 0 }, dpr: 1 },
    target: { selectors: ["#save"], text: "Save", box: { x: 0, y: 0, width: 1, height: 1 } },
    styles: { computed: {}, classes: [], inline: "", parent: {} },
    ...overrides,
  })

  it("posts the tab's drafts as one batch for the tab's origin", async () => {
    const d = deps()
    const answer = await createScopeMessageHandler(d)({ type: "scopeSend", pins: [pin()] }, tab)
    expect(answer).toEqual({ ok: true, result: { batch_id: "sb-1", pin_ids: ["sp-1"], subscribed: false, delivered: false } })
    expect(api.sendScopeBatch).toHaveBeenCalledWith({ origin: "http://localhost:5173", pins: [pin()] })
    expect(d.live.refresh).toHaveBeenCalledWith("http://localhost:5173")
  })

  it("refuses a batch Legato would refuse: empty, over 20 pins, a pin without a comment or from another site", async () => {
    const handle = createScopeMessageHandler(deps())
    expect(await handle({ type: "scopeSend", pins: [] }, tab)).toEqual({ ok: false, error: "There are no pins to send" })
    expect(await handle({ type: "scopeSend", pins: Array.from({ length: 21 }, () => pin()) }, tab)).toEqual({ ok: false, error: "A batch takes at most 20 pins" })
    expect(await handle({ type: "scopeSend", pins: [pin({ comment: " " })] }, tab)).toEqual({ ok: false, error: "Every pin needs a comment" })
    expect(await handle({ type: "scopeSend", pins: [pin({ page: { ...pin().page, url: "https://other.test/" } })] }, tab)).toEqual({ ok: false, error: "A pin is on another site" })
  })

  it("watches the tab's origin and answers with its sent pins", async () => {
    const d = deps()
    expect(await createScopeMessageHandler(d)({ type: "scopeWatch" }, tab)).toEqual({ ok: true, pins: [sentPin] })
    expect(d.live.watch).toHaveBeenCalledWith(4, "http://localhost:5173")
    expect(api.listScopePins).toHaveBeenCalledWith("http://localhost:5173")
  })

  it("reopens a resolved pin and withdraws an undelivered one, then tells the origin's tabs", async () => {
    const d = deps()
    const handle = createScopeMessageHandler(d)
    expect(await handle({ type: "scopeReopen", id: "sp-1", comment: "Still too much" }, tab)).toEqual({ ok: true })
    expect(api.reopenScopePin).toHaveBeenCalledWith("sp-1", "Still too much")
    expect(await handle({ type: "scopeWithdraw", id: "sp-2" }, tab)).toEqual({ ok: true })
    expect(api.withdrawScopePin).toHaveBeenCalledWith("sp-2")
    expect(d.live.refresh).toHaveBeenCalledTimes(2)
    expect(await handle({ type: "scopeReopen", id: "sp-1", comment: "" }, tab)).toEqual({ ok: false, error: "A reopened pin needs a comment" })
  })

  it("explains refusals: the token, the limits, and an unreachable Legato with its advice", async () => {
    const failing = (error: Error) => deps({ legato: vi.fn(async () => ({ baseUrl: "http://localhost:3080", api: { ...api, listScopePins: vi.fn(async () => { throw error }) } })) })
    const watch = (d: ScopeServiceDeps) => createScopeMessageHandler(d)({ type: "scopeWatch" }, tab)
    expect(await watch(failing(new LegatoRequestError("unauthorized", 401)))).toEqual({ ok: false, error: "Legato refused the extension's token (unauthorized). Open the extension options, check the bearer token, and save." })
    expect(await watch(failing(new LegatoRequestError("screenshot too large", 413)))).toEqual({ ok: false, error: "Legato refused the batch as too large (screenshot too large). A batch takes at most 20 pins, and a screenshot at most 500 KB." })
    const unreachable = unreachableMessage("http://localhost:3080")
    expect(await watch(failing(new Error(unreachable)))).toEqual({ ok: false, error: unreachable })
  })
})
