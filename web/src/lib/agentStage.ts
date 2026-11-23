import type { AgentInfo } from "../hooks/useWebSocket"
import { apiFetch } from "./api"

// Where an agent's work stands, as the server keeps it (see
// docs/ai/agent-states.md § Stages), in the order work moves through them.
export type WorkStage = "exploring" | "planning" | "implementing" | "reviewing" | "finished"

export const workStages: WorkStage[] = ["exploring", "planning", "implementing", "reviewing", "finished"]

const labels: Record<WorkStage, string> = {
  exploring: "Exploring",
  planning: "Planning",
  implementing: "Implementing",
  reviewing: "Reviewing",
  finished: "Finished",
}

function isStage(value: string | undefined | null): value is WorkStage {
  return workStages.includes(value as WorkStage)
}

// stageLabel is a stage's word, "" for an agent with no stage (the Reeve).
export function stageLabel(stage: string | undefined): string {
  return isStage(stage) ? labels[stage] : ""
}

// stageSteps is how many of a stage's five track bars are lit: one while
// exploring, one more at each stage, all five when finished; none for an
// agent with no stage.
export function stageSteps(stage: string | undefined): number {
  return isStage(stage) ? workStages.indexOf(stage) + 1 : 0
}

// How the operator should read an agent at a glance, in the order lists
// show them. The server works it out (glance); your-turn is an idle agent
// whose work is not finished.
export type GlanceSection = "needs-you" | "your-turn" | "working" | "finished" | "ended"

export const glanceOrder: GlanceSection[] = ["needs-you", "your-turn", "working", "finished", "ended"]

export const glanceLabels: Record<GlanceSection, string> = {
  "needs-you": "Needs you",
  "your-turn": "Your turn",
  working: "Working",
  finished: "Finished",
  ended: "Ended",
}

const fromServer: Record<string, GlanceSection> = {
  needs_you: "needs-you",
  your_turn: "your-turn",
  working: "working",
  finished: "finished",
  ended: "ended",
}

// glanceSection is the group the server put the agent in. An agent from a
// server that has no glances is grouped by its activity alone.
export function glanceSection(agent: Pick<AgentInfo, "glance" | "activity" | "status">): GlanceSection {
  const known = agent.glance ? fromServer[agent.glance] : undefined
  if (known) return known
  if (agent.status && agent.status !== "running") return "ended"
  switch (agent.activity) {
    case "waiting":
    case "error":
      return "needs-you"
    case "working":
    case "background":
      return "working"
    default:
      return "your-turn"
  }
}

// needsYouReason says why an agent needs the operator, "" when it does not.
export function needsYouReason(agent: AgentInfo): string {
  if (glanceSection(agent) !== "needs-you") return ""
  if (agent.activity === "error") return "Stopped on an error"
  if (agent.activity === "waiting") return "Waiting for your answer"
  if (agent.plan_pending) return "Plan to approve"
  const items = (agent.open_items ?? 0) - (agent.open_done_items ?? 0)
  if (items > 1) return `${items} items for you`
  if (items === 1) return "An item for you"
  return "Needs you"
}

// unreadDone says a finished agent left a done note the operator has not
// cleared: the list marks it unread.
export function unreadDone(agent: AgentInfo): boolean {
  return glanceSection(agent) === "finished" && (agent.open_done_items ?? 0) > 0
}

// stageCounts counts agents at each stage.
export function stageCounts(agents: AgentInfo[]): Record<WorkStage, number> {
  const counts = { exploring: 0, planning: 0, implementing: 0, reviewing: 0, finished: 0 }
  for (const agent of agents) if (isStage(agent.stage)) counts[agent.stage]++
  return counts
}

// filterByStage keeps the agents at stage, all of them for none.
export function filterByStage(agents: AgentInfo[], stage: WorkStage | null): AgentInfo[] {
  return stage ? agents.filter((agent) => agent.stage === stage) : agents
}

// needsYouOutside counts the agents that need the operator and that a
// filter on stage hides: a filter never hides urgent work.
export function needsYouOutside(agents: AgentInfo[], stage: WorkStage | null): number {
  if (!stage) return 0
  return agents.filter((agent) => agent.stage !== stage && glanceSection(agent) === "needs-you").length
}

// parseStageFilter reads a stage filter (?stage=, or a remembered one),
// null for all or for anything that is not a stage.
export function parseStageFilter(value: string | null | undefined): WorkStage | null {
  const stage = value?.trim().toLowerCase()
  return isStage(stage) ? stage : null
}

// The source the operator reads beside a stage: what set it.
export function stageSourceLabel(source: string | undefined): string {
  switch (source) {
    case "operator":
      return "set by you"
    case "agent":
      return "reported"
    default:
      return ""
  }
}

// One change in an agent place's stage, as GET /api/agents/{id}/stage-history
// lists them, newest first.
export interface StageEvent {
  from_stage?: string
  stage: string
  reason?: string
  source: string
  at?: string
}

// fetchStageHistory reads an agent's stage changes, newest first.
export async function fetchStageHistory(baseUrl: string, agentId: string): Promise<StageEvent[]> {
  const response = await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(agentId)}/stage-history`)
  if (!response.ok) throw new Error((await response.text()).trim() || "Could not read the stage history")
  const body = await response.json() as { events?: StageEvent[] }
  return body.events ?? []
}

// setAgentStage sets an agent's stage as the operator chose it.
export async function setAgentStage(baseUrl: string, agentId: string, stage: WorkStage): Promise<void> {
  const response = await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(agentId)}/stage`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ stage }),
  })
  if (!response.ok) throw new Error((await response.text()).trim() || "Could not set the stage")
}
