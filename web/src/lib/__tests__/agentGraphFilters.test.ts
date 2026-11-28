import { describe, expect, it } from "vitest"
import { graphNeedsYou, graphStageView, linkedAgentStep, pendingAgents, toggled } from "../agentGraphFilters"
import type { AgentGraphData } from "../agentGraph"
import type { AgentInfo } from "../agentWire"

const agent = (values: Partial<AgentInfo> & Pick<AgentInfo, "task_id">): AgentInfo => ({
  id: 1, card_task_id: values.task_id, slot: "main", task_title: "", tmux_session: "", command: "claude", agent_kind: "claude-code",
  status: "running", activity: "working", started_at: "2026-11-23T09:00:00Z", working_seconds: 0, waiting_seconds: 0, error_seconds: 0, ...values,
})

describe("graphStageView", () => {
  const agents = [agent({ task_id: "a", stage: "reviewing" }), agent({ task_id: "b", stage: "planning" }), agent({ task_id: "r", reeve: true })]
  it("counts every agent but the Reeve and dims none without a filter", () => {
    const view = graphStageView(agents, null)
    expect(view.counted.map(a => a.task_id)).toEqual(["a", "b"])
    expect(view.atStage).toBe(agents)
    expect([...view.faded]).toEqual([])
  })
  it("dims the agents at other stages, never the Reeve", () => {
    const view = graphStageView(agents, "reviewing")
    expect(view.atStage.map(a => a.task_id)).toEqual(["a", "r"])
    expect([...view.faded]).toEqual(["b"])
  })
})

describe("pendingAgents", () => {
  const data = { agents: { a: { repo: "x/y" } }, links: [], peer_window_seconds: 1800 } as unknown as AgentGraphData
  it("waits for a read for an agent listed since, unplaced and without a clone", () => {
    const agents = [agent({ task_id: "a" }), agent({ task_id: "new" }), agent({ task_id: "cloned", clone: { repo: "x/y" } as AgentInfo["clone"] }), agent({ task_id: "old" })]
    expect([...pendingAgents(agents, new Set(["a", "old"]), data)]).toEqual(["new"])
  })
  it("waits for nothing before the first read", () => {
    expect([...pendingAgents([agent({ task_id: "new" })], null, data)]).toEqual([])
    expect([...pendingAgents([agent({ task_id: "new" })], new Set(), null)]).toEqual([])
  })
})

describe("graphNeedsYou", () => {
  it("lists the agents that need the operator, never the Reeve", () => {
    expect(graphNeedsYou([agent({ task_id: "a", activity: "waiting" }), agent({ task_id: "b" }), agent({ task_id: "r", reeve: true, activity: "waiting" })]).map(a => a.task_id)).toEqual(["a"])
  })
})

describe("toggled", () => {
  it("adds an ID that is out and takes out one that is in", () => {
    expect([...toggled(new Set(["a"]), "b")]).toEqual(["a", "b"])
    expect([...toggled(new Set(["a", "b"]), "a")]).toEqual(["b"])
  })
})

describe("linkedAgentStep", () => {
  const base = { onGraph: true, faded: false, filtering: true, stillInAddress: false, listed: true }
  it("selects an agent on the graph that is not dimmed", () => {
    expect(linkedAgentStep(base)).toBe("select")
  })
  it("clears the filter that dims a listed agent once the address no longer names it", () => {
    expect(linkedAgentStep({ ...base, faded: true })).toBe("clear-filter")
    expect(linkedAgentStep({ ...base, faded: true, stillInAddress: true })).toBe("wait")
    expect(linkedAgentStep({ ...base, faded: true, filtering: false })).toBe("wait")
    expect(linkedAgentStep({ ...base, onGraph: false, listed: false })).toBe("wait")
  })
})
