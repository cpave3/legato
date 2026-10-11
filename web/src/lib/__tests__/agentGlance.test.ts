import { describe, expect, it } from "vitest"
import type { AgentInfo } from "../../hooks/useWebSocket"
import {
  GLANCE_ENTRIES,
  contextShare,
  ensembleSection,
  glanceItems,
  orderForEnsemble,
  plainText,
  type ConversationEntry,
} from "../agentGlance"

const tool = (text: string, values: Partial<ConversationEntry> = {}): ConversationEntry =>
  ({ kind: "tool", tool: "bash", text, state: "done", ...values })

const agent = (taskId: string, activity: string): AgentInfo => ({
  id: 1, task_id: taskId, task_title: taskId, tmux_session: `legato-${taskId}`, command: "claude", agent_kind: "claude-code",
  status: "running", activity, started_at: "now", working_seconds: 0, waiting_seconds: 0, error_seconds: 0,
})

describe("what a tile shows of a conversation", () => {
  it("keeps what was asked, what was said and what was done in order", () => {
    const items = glanceItems([
      { kind: "user", text: "add a --json flag" },
      { kind: "assistant", text: "Let me look." },
      tool("list.ts", { tool: "read" }),
      tool("Run the list tests", { command: "pnpm vitest", state: "failed" }),
      { kind: "assistant", text: "One test fails." },
    ], "working")

    expect(items).toEqual([
      { kind: "person", text: "add a --json flag" },
      { kind: "said", text: "Let me look.", newest: false },
      { kind: "tool", tool: "read", text: "list.ts", state: "done" },
      { kind: "tool", tool: "bash", text: "Run the list tests", state: "failed" },
      { kind: "said", text: "One test fails.", newest: true },
    ])
  })

  it("cuts a long run of tool calls to its newest and counts the rest", () => {
    const names = ["one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"]
    const items = glanceItems([
      { kind: "user", text: "do it" },
      ...names.map((name) => tool(name)),
      { kind: "assistant", text: "All done." },
    ], "")

    expect(items.map((item) => item.kind === "earlier" ? `earlier:${item.count}` : item.text)).toEqual([
      "do it", "earlier:7", "eight", "nine", "ten", "All done.",
    ])
  })

  it("shows a short run whole", () => {
    const items = glanceItems([tool("one"), tool("two"), tool("three")], "")

    expect(items.map((item) => item.kind)).toEqual(["tool", "tool", "tool"])
  })

  it("does not count the calls of a run that began before what was read", () => {
    const entries = Array.from({ length: GLANCE_ENTRIES }, (_, index) => tool(`call ${index}`))

    expect(glanceItems(entries, "")[0]).toEqual({ kind: "earlier", count: null })
  })

  it("names a plugin's tool by its own name", () => {
    const [item] = glanceItems([tool("Build status", { tool: "mcp__plugin_legato-tools_legato__show_card" })], "")

    expect(item).toMatchObject({ tool: "show_card" })
  })

  it("says what an unfinished call is waiting for", () => {
    const unfinished = tool("Clean and rebuild", { command: "rm -rf dist", state: "running" })

    // Running it, waiting to be allowed to (then the command is what matters),
    // or cut short because nothing runs it any more.
    expect(glanceItems([unfinished], "working")[0]).toMatchObject({ state: "running", text: "Clean and rebuild" })
    expect(glanceItems([unfinished], "background")[0]).toMatchObject({ state: "running" })
    expect(glanceItems([unfinished], "waiting")[0]).toMatchObject({ state: "waiting", text: "rm -rf dist" })
    expect(glanceItems([unfinished], "")[0]).toMatchObject({ state: "stopped" })
  })

  it("tells what Legato or another agent sent from what the person typed", () => {
    const items = glanceItems([
      { kind: "user", text: "[legato peer fekz3ove] I am editing server.go\n\n---\nAgent-to-agent message from `fekz3ove`." },
      { kind: "user", text: "[legato review] Question on step 3" },
    ], "")

    expect(items).toEqual([
      { kind: "relayed", from: "Agent fekz3ove", text: "I am editing server.go" },
      { kind: "relayed", from: "Legato review", text: "Question on step 3" },
    ])
  })

  it("shows what was said without its markup", () => {
    expect(plainText("## Result\n\n**Done.** Use `yg list --json`:\n\n```sh\nyg list --json\n```\n")).toBe(
      "Result\nDone. Use yg list --json:\nyg list --json",
    )
  })
})

describe("the ensemble's order", () => {
  it("puts the agents that need the person first, then the ones at work", () => {
    const agents = [agent("idle-1", ""), agent("busy-1", "working"), agent("ask-1", "waiting"), agent("back-1", "background"), agent("fail-1", "error")]

    expect(orderForEnsemble(agents).map((a) => a.task_id)).toEqual(["ask-1", "fail-1", "busy-1", "back-1", "idle-1"])
    expect(ensembleSection(agent("fail-1", "error"))).toBe("needs-you")
  })

  it("keeps the order agents came in within one state", () => {
    const agents = [agent("b", ""), agent("a", ""), agent("c", "")]

    expect(orderForEnsemble(agents).map((a) => a.task_id)).toEqual(["b", "a", "c"])
  })
})

describe("how full a context is", () => {
  it("is a share of the window, and unknown without one", () => {
    expect(contextShare({ context_used: 138_000, context_window: 200_000 })).toBe(69)
    expect(contextShare({ context_used: 300_000, context_window: 200_000 })).toBe(100)
    expect(contextShare({ context_used: 0, context_window: 0 })).toBeNull()
  })
})
