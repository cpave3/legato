import { describe, expect, it } from "vitest"
import { cardFrame, cardMetaParts } from "../boardCardFrame"
import type { BoardCard } from "../board-types"

const card = (more: Partial<BoardCard> = {}): BoardCard => ({
  id: "T-1", title: "t", priority: "", issue_type: "", status: "Doing", provider: "", has_warning: false,
  agent_active: true, agent_state: "working", working_seconds: 0, waiting_seconds: 0, error_seconds: 0,
  workspace_id: null, workspace_name: "", workspace_color: "", ...more,
})

describe("cardFrame", () => {
  it("shows a running agent's strip with no edge", () => {
    expect(cardFrame(card())).toEqual({ away: null, needs: [], agentStrip: true, edge: "" })
  })
  it("gives an agent in error a red edge and one that waits an amber one", () => {
    expect(cardFrame(card({ agent_state: "error" })).edge).toBe("error")
    expect(cardFrame(card({ agent_state: "waiting" })).edge).toBe("needs")
    expect(cardFrame(card({ agent_active: false, agent_state: "error" })).edge).toBe("")
  })
  it("lets what waits stand in for a quiet agent's strip, but not a working one's", () => {
    const plans = { needs_you: { plans: { count: 1, plan_id: "p" } } } as Partial<BoardCard>
    const quiet = cardFrame(card({ ...plans, agent_state: "idle" }))
    expect(quiet.needs).toHaveLength(1)
    expect(quiet.agentStrip).toBe(false)
    expect(quiet.edge).toBe("needs")
    expect(cardFrame(card({ ...plans, agent_state: "working" })).agentStrip).toBe(true)
  })
  it("says an away task's state in one strip, with its edge", () => {
    const away = { worker: "vm-1", state: "working" } as BoardCard["away"]
    const frame = cardFrame(card({ away, agent_state: "error" }))
    expect(frame.away).not.toBeNull()
    expect(frame.needs).toEqual([])
    expect(frame.agentStrip).toBe(false)
    expect(frame.edge).toBe(frame.away?.edge)
  })
})

describe("cardMetaParts", () => {
  it("says what the task is, then how it is going", () => {
    const parts = cardMetaParts(card({
      priority: "High", issue_type: "Bug", workspace_name: "Web",
      swarm_stats: { total: 4, done: 1, in_review: 0, building: 0, queued: 3, rejected: 0 },
      blocked_by: ["A-1", "A-2"], milestone: "M1", labels: ["ui"],
    } as Partial<BoardCard>), true)
    expect(parts.map(p => p.text)).toEqual(["High", "Bug", "Web", "1/4 workers", "Blocked by A-1, A-2", "M1", "ui"])
    expect(parts[0].className).toBe("text-lw-error")
    expect(parts[1].className).toBeUndefined()
  })
  it("leaves out a Milestone's type, a hidden workspace, one named after the main, and an empty swarm", () => {
    expect(cardMetaParts(card({ issue_type: "Milestone", provider: "burnlite", workspace_name: "Web" }), false)).toEqual([])
    expect(cardMetaParts(card({ workspace_name: "erebus", handoff_from: { main: "erebus", main_task_id: "K" } } as Partial<BoardCard>), true)).toEqual([])
    expect(cardMetaParts(card({ swarm_stats: { total: 0, done: 0, in_review: 0, building: 0, queued: 0, rejected: 0 }, blocked_by: [] } as Partial<BoardCard>), true)).toEqual([])
  })
})
