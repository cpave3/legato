import type { AgentInfo } from "../hooks/useWebSocket"
import { agentSections, type AgentListSection } from "./agentList"

// One group of the agent list: its agents in the order the list shows them,
// under what they are doing. name is null for the agents in no group.
export interface AgentGroupBlock {
  name: string | null
  agents: AgentInfo[]
  sections: AgentListSection[]
  // How many of its agents need the person: waiting on them, or in error.
  needsYou: number
}

// agentGroupLayout lists agents by group: the known groups (the defaults,
// then those made here) even when empty, then the other groups in use in
// name order, then the agents in no group. Inside a group they keep the
// status order of agentSections.
export function agentGroupLayout(agents: AgentInfo[], known: string[], options: { withUngrouped?: boolean } = {}): AgentGroupBlock[] {
  const names = [...new Set(known.map((name) => name.trim()).filter(Boolean))]
  const inUse = [...new Set(agents.map((agent) => agent.group?.trim() ?? "").filter((name) => name && !names.includes(name)))]
  inUse.sort((a, b) => a.localeCompare(b))
  const block = (name: string | null): AgentGroupBlock => {
    const members = agents.filter((agent) => (agent.group?.trim() || null) === name)
    const sections = agentSections(members)
    return {
      name,
      agents: sections.flatMap((section) => section.agents),
      sections,
      needsYou: sections.find((section) => section.section === "needs-you")?.agents.length ?? 0,
    }
  }
  const blocks = [...names, ...inUse].map(block)
  const ungrouped = block(null)
  if (ungrouped.agents.length > 0 || options.withUngrouped) blocks.push(ungrouped)
  return blocks
}

// groupedListOrder is the order a grouped list shows its agents in, the one
// the number keys switch by: the agents of collapsed groups are not shown.
export function groupedListOrder(blocks: AgentGroupBlock[], isCollapsed: (name: string | null) => boolean): AgentInfo[] {
  return blocks.filter((block) => !isCollapsed(block.name)).flatMap((block) => block.agents)
}

// hasGroups says whether any agent is in a group, which makes the grouped
// list the default.
export function hasGroups(agents: AgentInfo[]): boolean {
  return agents.some((agent) => Boolean(agent.group?.trim()))
}
