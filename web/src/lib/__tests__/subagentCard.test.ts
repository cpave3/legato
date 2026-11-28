import { describe, expect, it } from "vitest"
import { subagentCallView, subagentCounts, subagentNoActivity, subagentStatusClass } from "../subagentCard"
import type { NativeToolItem } from "../nativeAgentSync"

describe("subagentCard", () => {
  it("reads a spawn call, with defaults for what it leaves out", () => {
    const bare: NativeToolItem = { key: "k", toolName: "spawn_agent", args: {} }
    expect(subagentCallView(bare)).toEqual({
      purpose: "Subagent", prompt: "", agent: "default", model: "inherited", mode: "default", steps: undefined, toolCalls: undefined,
    })
    const full: NativeToolItem = {
      key: "k", toolName: "spawn_agent",
      args: { purpose: "From args", prompt: "Do it", agent: "explore", model: "small", mode: "fast" },
      result: { steps: 3, tool_calls_count: 1 },
    }
    expect(subagentCallView(full)).toEqual({
      purpose: "From args", prompt: "Do it", agent: "explore", model: "small", mode: "fast", steps: 3, toolCalls: 1,
    })
  })

  it("takes the purpose the subagent reports over its call's", () => {
    const tool = { key: "k", toolName: "spawn_agent", args: { purpose: "From args" }, subagent: { purpose: "Live" } } as unknown as NativeToolItem
    expect(subagentCallView(tool).purpose).toBe("Live")
  })

  it("colours the status by how the subagent stands", () => {
    expect(subagentStatusClass("error")).toBe("text-red-400")
    expect(subagentStatusClass("finished")).toBe("text-green-400")
    expect(subagentStatusClass("running")).toBe("text-indigo-300")
    expect(subagentStatusClass("background")).toBe("text-indigo-300")
  })

  it("waits for activity only while the subagent starts or runs", () => {
    expect(subagentNoActivity("spawning")).toBe("Waiting for activity…")
    expect(subagentNoActivity("running")).toBe("Waiting for activity…")
    expect(subagentNoActivity("finished")).toBe("No live activity captured")
  })

  it("counts steps and tool calls, in the singular for one", () => {
    expect(subagentCounts(1, 1)).toBe("1 step · 1 tool call")
    expect(subagentCounts(2, undefined)).toBe("2 steps")
    expect(subagentCounts(undefined, 0)).toBe("0 tool calls")
    expect(subagentCounts(undefined, undefined)).toBe("")
  })
})
