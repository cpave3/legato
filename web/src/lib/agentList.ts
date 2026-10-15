import type { AgentInfo } from "../hooks/useWebSocket"
import { ensembleSection, type EnsembleSection } from "./agentGlance"

export interface AgentListSection {
  section: EnsembleSection
  agents: AgentInfo[]
}

const order: EnsembleSection[] = ["needs-you", "running", "background", "idle"]

export const sectionLabels: Record<EnsembleSection, string> = {
  "needs-you": "Needs you",
  running: "Running",
  background: "Background",
  idle: "Idle",
}

// Within one state, a swarm's agents stay together with its conductor first,
// swarms in the order they came, then the agents that belong to no swarm.
function groupSwarms(agents: AgentInfo[]): AgentInfo[] {
  const swarms = new Map<string, AgentInfo[]>()
  const solo: AgentInfo[] = []
  for (const agent of agents) {
    if (!agent.parent_task_id) {
      solo.push(agent)
      continue
    }
    const group = swarms.get(agent.parent_task_id) ?? []
    group.push(agent)
    swarms.set(agent.parent_task_id, group)
  }
  const grouped = [...swarms.values()].flatMap((group) => [
    ...group.filter((agent) => agent.role === "conductor"),
    ...group.filter((agent) => agent.role !== "conductor"),
  ])
  return [...grouped, ...solo]
}

// agentSections lists agents under what they are doing, as the TUI's agent
// list does: those that need the person first. A state with no agents is left out.
export function agentSections(agents: AgentInfo[]): AgentListSection[] {
  return order
    .map((section) => ({ section, agents: groupSwarms(agents.filter((agent) => ensembleSection(agent) === section)) }))
    .filter((entry) => entry.agents.length > 0)
}

// agentListOrder is the order the agent list shows: the one the number keys
// switch by.
export function agentListOrder(agents: AgentInfo[]): AgentInfo[] {
  return agentSections(agents).flatMap((entry) => entry.agents)
}

// The time that goes with what an agent is doing: how long it has worked,
// waited, or been in error.
export function stateSeconds(agent: AgentInfo): number {
  switch (agent.activity) {
    case "waiting":
      return agent.waiting_seconds
    case "error":
      return agent.error_seconds
    default:
      return agent.working_seconds
  }
}

export function formatAgentTime(seconds: number): string {
  if (seconds < 60) return seconds > 0 ? "<1m" : ""
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`
}
