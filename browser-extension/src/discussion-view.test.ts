// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest"
import type { NativeConversationItem } from "../../web/src/lib/nativeAgentSync"
import { discussionStatus, renderDiscussionFeed } from "./discussion-view"

const handlers = () => ({ onPermission: vi.fn(), onOpenFile: vi.fn() })

describe("renderDiscussionFeed", () => {
  beforeEach(() => document.body.replaceChildren())

  it("draws what the person sent, the agent's replies as Markdown, and tool calls as rows", () => {
    const feed = document.createElement("div")
    const items: NativeConversationItem[] = [
      { key: "1", role: "assistant", text: "This PR adds **a discussion** workflow." },
      { key: "2", role: "user", text: "Why a new trigger?" },
      { key: "3", role: "tool", text: "", tool: { key: "c1", toolName: "bash", args: { command: "gh pr diff 12" }, summary: "gh pr diff 12" } },
      { key: "4", role: "reasoning", text: "thinking about it" },
    ]

    renderDiscussionFeed(feed, items, handlers())

    const rows = Array.from(feed.children) as HTMLElement[]
    expect(rows.map((row) => row.dataset.role)).toEqual(["assistant", "user", "tool"])
    expect(rows[0].querySelector("strong")?.textContent).toBe("a discussion")
    expect(rows[1].textContent).toBe("Why a new trigger?")
    expect(rows[2].textContent).toContain("bash")
    expect(rows[2].textContent).toContain("gh pr diff 12")
  })

  it("lets the person open a file and line the agent cites", () => {
    const feed = document.createElement("div")
    const h = handlers()
    renderDiscussionFeed(feed, [{ key: "1", role: "assistant", text: "The check is in `src/legato.ts:44`." }], h)

    feed.querySelector<HTMLButtonElement>("button.file-ref")!.click()
    expect(h.onOpenFile).toHaveBeenCalledWith({ path: "src/legato.ts", line: 44 })
  })

  it("never runs what the agent wrote", () => {
    const feed = document.createElement("div")
    renderDiscussionFeed(feed, [{ key: "1", role: "assistant", text: "<img src=x onerror=alert(1)><script>alert(2)</script>" }], handlers())

    expect(feed.querySelector("script")).toBeNull()
    expect(feed.innerHTML).not.toContain("onerror")
  })

  it("offers Allow and Deny on a pending permission request, and passes the choice on", () => {
    const feed = document.createElement("div")
    const h = handlers()
    renderDiscussionFeed(feed, [{
      key: "p", role: "system", text: "",
      permission: { requestId: "req-1", tool: "bash", target: "host", command: "gh pr checks 12", status: "pending" },
    }], h)

    expect(feed.textContent).toContain("gh pr checks 12")
    const allow = Array.from(feed.querySelectorAll("button")).find((button) => button.textContent === "Allow")!
    allow.click()
    expect(h.onPermission).toHaveBeenCalledWith("req-1", "allow")
  })

  it("shows a settled request without buttons", () => {
    const feed = document.createElement("div")
    renderDiscussionFeed(feed, [{
      key: "p", role: "system", text: "",
      permission: { requestId: "req-1", tool: "bash", target: "host", command: "rm -rf /", status: "denied" },
    }], handlers())

    expect(feed.querySelectorAll("button")).toHaveLength(0)
    expect(feed.textContent).toContain("Denied")
  })
})

describe("discussionStatus", () => {
  it("says what the agent is doing", () => {
    const base = { connected: true, loading: false, busy: false, items: [] as NativeConversationItem[] }
    expect(discussionStatus({ ...base, connected: false })).toEqual({ label: "Reconnecting…", tone: "idle" })
    expect(discussionStatus({ ...base, busy: true })).toEqual({ label: "Working", tone: "live" })
    expect(discussionStatus(base)).toEqual({ label: "Waiting for your question", tone: "idle" })
    expect(discussionStatus({ ...base, busy: true, items: [{
      key: "p", role: "system", text: "",
      permission: { requestId: "r", tool: "bash", target: "host", command: "x", status: "pending" },
    }] })).toEqual({ label: "Needs you", tone: "needs" })
  })
})
