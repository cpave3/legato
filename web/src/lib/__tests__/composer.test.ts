import { describe, expect, it } from "vitest"
import { currentStep, formatElapsed, queueMessage, shortModelName, unreadQueued } from "../composer"
import type { NativeConversationItem } from "../nativeAgentSync"

const tool = (key: string, toolName: string, args: Record<string, unknown>, done: boolean, summary?: string): NativeConversationItem => ({
  key,
  role: "tool",
  text: "",
  tool: { key, toolName, args, summary, ...(done ? { result: {} } : {}) },
})

describe("currentStep", () => {
  it("names the tool call still running, its command for a shell", () => {
    const items = [tool("a", "read", { path: "/x/a.go" }, true), tool("b", "bash", { command: "./dev check --fe=all" }, false)]
    expect(currentStep(items)).toBe("bash ./dev check --fe=all")
  })

  it("names another tool by what it is for", () => {
    expect(currentStep([tool("a", "edit", { path: "/repo/web/src/App.tsx" }, false)])).toBe("edit App.tsx")
    expect(currentStep([tool("a", "web_search", {}, false, "legato docs")])).toBe("web_search legato docs")
  })

  it("is empty when every call has finished", () => {
    expect(currentStep([tool("a", "bash", { command: "ls" }, true)])).toBeNull()
    expect(currentStep([])).toBeNull()
  })
})

describe("formatElapsed", () => {
  it("counts seconds, then minutes and seconds, then hours and minutes", () => {
    expect(formatElapsed(9)).toBe("9s")
    expect(formatElapsed(252)).toBe("4m 12s")
    expect(formatElapsed(3 * 3600 + 5 * 60 + 7)).toBe("3h 5m")
  })
})

describe("unreadQueued", () => {
  const user = (key: string, text: string): NativeConversationItem => ({ key, role: "user", text })

  it("keeps a queued message until the agent reads it", () => {
    const queued = queueMessage([], "check the tests", [user("u1", "start")])
    expect(unreadQueued(queued, [user("u1", "start")]).map((entry) => entry.text)).toEqual(["check the tests"])
    expect(unreadQueued(queued, [user("u1", "start"), user("u2", "check the tests")])).toEqual([])
  })

  it("tells a message apart from the same words sent before it", () => {
    const before = [user("u1", "go on")]
    const queued = queueMessage([], "go on", before)
    expect(unreadQueued(queued, before)).toHaveLength(1)
    expect(unreadQueued(queued, [...before, user("u2", "go on")])).toEqual([])
  })
})

describe("shortModelName", () => {
  it("leaves out the provider, which only the model menu names", () => {
    expect(shortModelName("anthropic/claude-opus-5-5")).toBe("opus-5-5")
    expect(shortModelName("manticore/glm")).toBe("glm")
    expect(shortModelName("gpt-6-astra")).toBe("gpt-6-astra")
  })
})
