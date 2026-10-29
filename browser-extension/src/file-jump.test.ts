import { describe, expect, it } from "vitest"
import { showInPullRequest } from "./file-jump"

const noReceiver = new Error("Could not establish connection. Receiving end does not exist.")

describe("showInPullRequest", () => {
  it("asks the pull request's tab to show the file and line", async () => {
    const sent: unknown[] = []
    const result = await showInPullRequest(7, { path: "src/a.ts", line: 3 }, {
      sendToTab: async (tabId, message) => { sent.push([tabId, message]); return { ok: true } },
      inject: async () => { throw new Error("not needed") },
    })
    expect(sent).toEqual([[7, { type: "legato:jump", path: "src/a.ts", line: 3 }]])
    expect(result).toEqual({ ok: true })
  })

  it("puts the page script in a tab opened before the extension loaded, then asks again", async () => {
    let injected = false
    const result = await showInPullRequest(7, { path: "src/a.ts" }, {
      sendToTab: async () => {
        if (!injected) throw noReceiver
        return { ok: true }
      },
      inject: async (tabId) => { expect(tabId).toBe(7); injected = true },
    })
    expect(injected).toBe(true)
    expect(result).toEqual({ ok: true })
  })

  it("reports a page that is loading Files changed to finish the jump there", async () => {
    const result = await showInPullRequest(7, { path: "src/a.ts" }, {
      sendToTab: async () => { throw new Error("The message port closed before a response was received.") },
      inject: async () => undefined,
    })
    expect(result).toMatchObject({ ok: true, status: "navigating" })
  })
})
