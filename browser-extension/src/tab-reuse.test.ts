import { describe, expect, it, vi } from "vitest"
import { decideTabReuse, handleMarkedTab, stripStreamDeckMarker, type ActiveTabSnapshot } from "./tab-reuse"

const prior: ActiveTabSnapshot = {
  tabId: 10,
  windowId: 2,
  url: "http://localhost:3080/agents?task=old",
  activatedAt: 1_000,
}
const marked = "http://localhost:3080/review/rt-1?legato_source=streamdeck"

describe("stripStreamDeckMarker", () => {
  it("recognizes only the exact marker and preserves other query parameters", () => {
    expect(stripStreamDeckMarker(`${marked}&view=files`)).toEqual({
      marked: true,
      url: "http://localhost:3080/review/rt-1?view=files",
    })
    expect(stripStreamDeckMarker("http://localhost:3080/review/rt-1?legato_source=other").marked).toBe(false)
  })
})

describe("decideTabReuse", () => {
  it("reuses the last active same-window Legato tab even when Chrome is unfocused", () => {
    expect(decideTabReuse({
      destination: marked,
      newTabId: 11,
      newWindowId: 2,
      createdAt: 1_100, normalWindow: true,
      configuredOrigin: "http://localhost:3080",
      prior,
    })).toEqual({ kind: "reuse", priorTabId: 10, cleanUrl: "http://localhost:3080/review/rt-1" })
  })

  it("keeps the new tab outside a normal Chrome window", () => {
    expect(decideTabReuse({
      destination: marked, newTabId: 11, newWindowId: 2, createdAt: 1_100, normalWindow: false,
      configuredOrigin: "http://localhost:3080", prior,
    }).kind).toBe("keep")
  })

  it.each([
    ["unmarked", "http://localhost:3080/review/rt-1", prior],
    ["wrong prior origin", marked, { ...prior, url: "https://github.com/cpave3/legato" }],
    ["wrong window", marked, { ...prior, windowId: 3 }],
    ["snapshot after event", marked, { ...prior, activatedAt: 1_101 }],
    ["same tab", marked, { ...prior, tabId: 11 }],
  ])("keeps the new tab for %s", (_name, destination, snapshot) => {
    expect(decideTabReuse({
      destination,
      newTabId: 11,
      newWindowId: 2,
      createdAt: 1_100, normalWindow: true,
      configuredOrigin: "http://localhost:3080",
      prior: snapshot,
    }).kind).toBe("keep")
  })
})

describe("handleMarkedTab", () => {
  it("updates and activates the prior tab before removing the duplicate", async () => {
    const calls: string[] = []
    const chromeTabs = {
      update: vi.fn(async (id: number, info: chrome.tabs.UpdateProperties) => { calls.push(`update:${id}:${info.active}`) }),
      remove: vi.fn(async (id: number) => { calls.push(`remove:${id}`) }),
    }
    await handleMarkedTab({
      destination: marked, newTabId: 11, newWindowId: 2, createdAt: 1_100, normalWindow: true,
      configuredOrigin: "http://localhost:3080", prior,
    }, chromeTabs)
    expect(calls).toEqual(["update:10:true", "remove:11"])
  })

  it("fails open by keeping and cleaning the new tab when prior update fails", async () => {
    const chromeTabs = {
      update: vi.fn()
        .mockRejectedValueOnce(new Error("tab disappeared"))
        .mockResolvedValueOnce(undefined),
      remove: vi.fn(),
    }
    await handleMarkedTab({
      destination: marked, newTabId: 11, newWindowId: 2, createdAt: 1_100, normalWindow: true,
      configuredOrigin: "http://localhost:3080", prior,
    }, chromeTabs)
    expect(chromeTabs.remove).not.toHaveBeenCalled()
    expect(chromeTabs.update).toHaveBeenLastCalledWith(11, { url: "http://localhost:3080/review/rt-1" })
  })
})
