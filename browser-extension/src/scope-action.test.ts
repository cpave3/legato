import { describe, expect, it, vi } from "vitest"
import { handleScopeAction, type ScopeActionDeps } from "./scope-action"

function deps(options: { granted?: boolean; listed?: string[] } = {}) {
  const listed = new Set(options.listed ?? [])
  return {
    request: vi.fn(async () => options.granted ?? true),
    isListed: vi.fn(async (origin: string) => listed.has(origin)),
    add: vi.fn(async (origin: string) => { listed.add(origin) }),
    toggle: vi.fn(async () => undefined),
  } satisfies ScopeActionDeps
}

const tab = (url: string) => ({ id: 7, url })

describe("the toolbar button", () => {
  it("asks Chrome for the page's origin before it awaits anything, as Chrome grants only from the click", () => {
    const d = deps()
    void handleScopeAction(tab("http://localhost:5173/settings"), d)
    expect(d.request).toHaveBeenCalledWith("http://localhost:5173/*")
    expect(d.isListed).not.toHaveBeenCalled()
  })

  it("on an origin Scope is not on yet, allows it and turns pin mode on", async () => {
    const d = deps()
    const result = await handleScopeAction(tab("http://localhost:5173/settings"), d)
    expect(result).toEqual({ kind: "allowed", origin: "http://localhost:5173" })
    expect(d.add).toHaveBeenCalledWith("http://localhost:5173")
    expect(d.toggle).toHaveBeenCalledWith(7, true)
  })

  it("on an allowed origin, toggles pin mode", async () => {
    const d = deps({ listed: ["http://localhost:5173"] })
    const result = await handleScopeAction(tab("http://localhost:5173/a"), d)
    expect(result).toEqual({ kind: "toggled", origin: "http://localhost:5173" })
    expect(d.add).not.toHaveBeenCalled()
    expect(d.toggle).toHaveBeenCalledWith(7, undefined)
  })

  it("does nothing more when the person refuses Chrome's prompt", async () => {
    const d = deps({ granted: false })
    expect(await handleScopeAction(tab("https://app.test/"), d)).toEqual({ kind: "denied", origin: "https://app.test" })
    expect(d.add).not.toHaveBeenCalled()
    expect(d.toggle).not.toHaveBeenCalled()
  })

  it("ignores pages that are not on an http(s) site", async () => {
    const d = deps()
    expect(await handleScopeAction(tab("chrome://extensions"), d)).toEqual({ kind: "unsupported" })
    expect(await handleScopeAction({ url: "https://app.test/" }, d)).toEqual({ kind: "unsupported" })
    expect(d.request).not.toHaveBeenCalled()
  })

  it("reports a failure to reach the page", async () => {
    const d = deps({ listed: ["https://app.test"] })
    d.toggle.mockRejectedValueOnce(new Error("Cannot access contents of the page"))
    expect(await handleScopeAction(tab("https://app.test/"), d)).toEqual({ kind: "error", origin: "https://app.test", message: "Cannot access contents of the page" })
  })
})
