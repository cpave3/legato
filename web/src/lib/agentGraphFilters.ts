import type { AgentGraphData } from "./agentGraph"
import { glanceSection, type WorkStage } from "./agentStage"
import type { AgentInfo } from "./agentWire"

// What the ensemble's graph view derives from the agent list besides the
// layout: the stage filter's dimming, the agents still waiting for a read,
// those that need the operator, and what an agent the address names waits
// for. See components/AgentGraphView.tsx.

export interface GraphStageView {
  // The agents the stage menu counts: never the Reeve.
  counted: AgentInfo[]
  // The agents at the filtered stage, the Reeve always among them.
  atStage: AgentInfo[]
  // The agents the filter leaves out: dimmed rather than taken off the
  // graph, so a change of stage never moves a node.
  faded: ReadonlySet<string>
}

export function graphStageView(agents: AgentInfo[], stageFilter: WorkStage | null): GraphStageView {
  const counted = agents.filter((agent) => !agent.reeve)
  if (!stageFilter) return { counted, atStage: agents, faded: new Set() }
  return {
    counted,
    atStage: agents.filter((agent) => agent.reeve || agent.stage === stageFilter),
    faded: new Set(counted.filter((agent) => agent.stage !== stageFilter).map((agent) => agent.task_id)),
  }
}

// pendingAgents are the agents listed since the graph was last read that
// it does not place yet, and no clone places: each waits for the next read
// to say where it works.
export function pendingAgents(agents: AgentInfo[], readFor: ReadonlySet<string> | null, data: AgentGraphData | null): ReadonlySet<string> {
  if (!readFor || !data) return new Set()
  return new Set(agents.filter((agent) => !readFor.has(agent.task_id) && !data.agents[agent.task_id] && !agent.clone?.repo).map((agent) => agent.task_id))
}

// graphNeedsYou lists the agents at the stage that need the operator.
export function graphNeedsYou(atStage: AgentInfo[]): AgentInfo[] {
  return atStage.filter((agent) => !agent.reeve && glanceSection(agent) === "needs-you")
}

// toggled is a set with an ID added, or taken out when it was in it.
export function toggled(set: ReadonlySet<string>, id: string): ReadonlySet<string> {
  const next = new Set(set)
  if (!next.delete(id)) next.add(id)
  return next
}

// What an agent the address names waits for: to be on the graph and not
// dimmed, then to be selected. A stage filter that dims an agent still
// listed is cleared, but only once the page has taken the agent out of the
// address: clearing the filter writes the address too, and would put it
// back.
export function linkedAgentStep(state: { onGraph: boolean; faded: boolean; filtering: boolean; stillInAddress: boolean; listed: boolean }): "select" | "clear-filter" | "wait" {
  if (state.onGraph && !state.faded) return "select"
  if (state.filtering && !state.stillInAddress && state.listed) return "clear-filter"
  return "wait"
}
