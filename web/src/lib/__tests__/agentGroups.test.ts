import { describe, expect, it } from "vitest"
import type { AgentInfo } from "../../hooks/useWebSocket"
import { agentGroupLayout, groupedListOrder } from "../agentGroups"

function agent(task_id: string, activity: string, group?: string): AgentInfo {
  return {
    id: 1, task_id, task_title: task_id, tmux_session: `legato-${task_id}`, command: "claude", agent_kind: "claude-code",
    status: "running", activity, started_at: "2026-11-12T00:00:00Z", working_seconds: 0, waiting_seconds: 0, error_seconds: 0,
    ...(group ? { group } : {}),
  }
}

describe("agentGroupLayout", () => {
  it("lists the known groups, then the other groups in use by name, then Ungrouped", () => {
    const layout = agentGroupLayout(
      [agent("a", "working", "Zeta"), agent("b", "working", "Alpha"), agent("c", "working"), agent("d", "working", "Tooling")],
      ["Tickets", "Tooling"],
    )
    expect(layout.map((block) => block.name)).toEqual(["Tickets", "Tooling", "Alpha", "Zeta", null])
    expect(layout[0].agents).toEqual([])
    expect(layout.at(-1)!.agents.map((a) => a.task_id)).toEqual(["c"])
  })

  it("keeps the status order inside a group and counts those that need the person", () => {
    const [block] = agentGroupLayout(
      [agent("idle", "", "G"), agent("busy", "working", "G"), agent("asks", "waiting", "G"), agent("failed", "error", "G")],
      [],
    )
    expect(block.agents.map((a) => a.task_id)).toEqual(["asks", "failed", "busy", "idle"])
    expect(block.sections.map((s) => s.section)).toEqual(["needs-you", "running", "idle"])
    expect(block.needsYou).toBe(2)
  })

  it("leaves Ungrouped out when it is empty, unless asked for it", () => {
    const agents = [agent("a", "working", "G")]
    expect(agentGroupLayout(agents, []).map((b) => b.name)).toEqual(["G"])
    expect(agentGroupLayout(agents, [], { withUngrouped: true }).map((b) => b.name)).toEqual(["G", null])
  })
})

describe("groupedListOrder", () => {
  it("is the order shown, without the agents of collapsed groups", () => {
    const layout = agentGroupLayout([agent("a", "working", "One"), agent("b", "working", "Two"), agent("c", "waiting")], ["One", "Two"])
    expect(groupedListOrder(layout, (name) => name === "Two").map((a) => a.task_id)).toEqual(["a", "c"])
  })
})
