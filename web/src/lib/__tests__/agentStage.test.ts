import { describe, expect, it } from "vitest"
import type { AgentInfo } from "../../hooks/useWebSocket"
import {
  filterByStage,
  glanceSection,
  needsYouReason,
  needsYouOutside,
  parseStageFilter,
  stageCounts,
  stageSteps,
  stageLabel,
  unreadDone,
} from "../agentStage"

const base: AgentInfo = {
  id: 1, task_id: "LEG-1", task_title: "", tmux_session: "", command: "", agent_kind: "claude-code",
  status: "running", activity: "", started_at: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0,
}
const agent = (fields: Partial<AgentInfo>): AgentInfo => ({ ...base, ...fields })

describe("agent stages", () => {
  it("lights one more bar of a stage's track at each stage, all five when finished", () => {
    expect(["exploring", "planning", "implementing", "reviewing", "finished"].map(stageSteps)).toEqual([1, 2, 3, 4, 5])
    expect(stageSteps(undefined)).toBe(0)
    expect(stageLabel("reviewing")).toBe("Reviewing")
    expect(stageLabel(undefined)).toBe("")
  })

  it("takes the group from the server's glance", () => {
    expect(glanceSection(agent({ glance: "needs_you", activity: "working" }))).toBe("needs-you")
    expect(glanceSection(agent({ glance: "your_turn" }))).toBe("your-turn")
    expect(glanceSection(agent({ glance: "working", activity: "background" }))).toBe("working")
    expect(glanceSection(agent({ glance: "finished" }))).toBe("finished")
    expect(glanceSection(agent({ glance: "ended", status: "dead" }))).toBe("ended")
    // A done note is the server's to judge: an open item alone is no reason.
    expect(glanceSection(agent({ glance: "finished", open_items: 1, open_done_items: 1 }))).toBe("finished")
  })

  it("groups an agent from a server without glances by its activity alone", () => {
    expect(glanceSection(agent({ activity: "waiting" }))).toBe("needs-you")
    expect(glanceSection(agent({ activity: "error" }))).toBe("needs-you")
    expect(glanceSection(agent({ activity: "background" }))).toBe("working")
    expect(glanceSection(agent({ activity: "" }))).toBe("your-turn")
    expect(glanceSection(agent({ activity: "", status: "dead" }))).toBe("ended")
  })

  it("says why an agent needs the operator", () => {
    expect(needsYouReason(agent({ glance: "needs_you", activity: "error" }))).toBe("Stopped on an error")
    expect(needsYouReason(agent({ glance: "needs_you", activity: "waiting" }))).toBe("Waiting for your answer")
    expect(needsYouReason(agent({ glance: "needs_you", plan_pending: true }))).toBe("Plan to approve")
    expect(needsYouReason(agent({ glance: "needs_you", open_items: 2 }))).toBe("2 items for you")
    expect(needsYouReason(agent({ glance: "needs_you", open_items: 1 }))).toBe("An item for you")
    expect(needsYouReason(agent({ glance: "working", activity: "working" }))).toBe("")
  })

  it("marks a finished agent with a done note unread", () => {
    expect(unreadDone(agent({ glance: "finished", open_done_items: 1 }))).toBe(true)
    expect(unreadDone(agent({ glance: "finished" }))).toBe(false)
    expect(unreadDone(agent({ glance: "your_turn", open_done_items: 1 }))).toBe(false)
  })

  it("counts agents by stage and filters by one", () => {
    const agents = [
      agent({ task_id: "a", stage: "reviewing" }), agent({ task_id: "b", stage: "reviewing" }),
      agent({ task_id: "c", stage: "finished" }), agent({ task_id: "d" }),
    ]
    expect(stageCounts(agents)).toEqual({ exploring: 0, planning: 0, implementing: 0, reviewing: 2, finished: 1 })
    expect(filterByStage(agents, "reviewing").map((a) => a.task_id)).toEqual(["a", "b"])
    expect(filterByStage(agents, null)).toHaveLength(4)
  })

  it("counts those that need the operator outside a filter", () => {
    const agents = [
      agent({ task_id: "a", stage: "reviewing", glance: "needs_you" }),
      agent({ task_id: "b", stage: "implementing", glance: "needs_you" }),
      agent({ task_id: "c", stage: "implementing", glance: "working" }),
    ]
    expect(needsYouOutside(agents, "implementing")).toBe(1)
    expect(needsYouOutside(agents, null)).toBe(0)
  })

  it("reads a stage filter, ignoring what is not a stage", () => {
    expect(parseStageFilter("Reviewing")).toBe("reviewing")
    expect(parseStageFilter("all")).toBeNull()
    expect(parseStageFilter("nonsense")).toBeNull()
    expect(parseStageFilter(null)).toBeNull()
  })
})
