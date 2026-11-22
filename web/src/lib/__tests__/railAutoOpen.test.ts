import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { DEFAULT_AUTO_OPEN, readAutoOpen, resetAutoOpenMemory, writeAutoOpen } from "../railAutoOpen"

describe("railAutoOpen", () => {
  beforeEach(() => {
    window.localStorage.clear()
    resetAutoOpenMemory()
  })
  afterEach(() => vi.restoreAllMocks())

  it("opens for the agent in view and for every item by default", () => {
    expect(DEFAULT_AUTO_OPEN).toEqual({ agentInView: true, everything: true })
    expect(readAutoOpen("http://legato")).toEqual(DEFAULT_AUTO_OPEN)
  })

  it("keeps each server's choices apart, in this browser", () => {
    writeAutoOpen("http://a", { agentInView: false, everything: true })
    expect(readAutoOpen("http://a")).toEqual({ agentInView: false, everything: true })
    expect(readAutoOpen("http://b")).toEqual(DEFAULT_AUTO_OPEN)
    resetAutoOpenMemory()
    expect(readAutoOpen("http://a")).toEqual({ agentInView: false, everything: true })
  })

  it("reads what it cannot make sense of as the defaults, field by field", () => {
    window.localStorage.setItem("legato:rail-auto-open:http://a", "{nope")
    expect(readAutoOpen("http://a")).toEqual(DEFAULT_AUTO_OPEN)
    window.localStorage.setItem("legato:rail-auto-open:http://a", JSON.stringify({ everything: false, agentInView: "no" }))
    expect(readAutoOpen("http://a")).toEqual({ agentInView: true, everything: false })
  })

  it("keeps a choice for this page when storage refuses it, and reads the defaults when storage cannot be read", () => {
    const set = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("blocked") })
    writeAutoOpen("http://a", { agentInView: false, everything: false })
    expect(readAutoOpen("http://a")).toEqual({ agentInView: false, everything: false })
    set.mockRestore()
    resetAutoOpenMemory()
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("blocked") })
    expect(readAutoOpen("http://a")).toEqual(DEFAULT_AUTO_OPEN)
  })
})
