import { apiFetch } from "./api"

// An agent on the other side of a review loop from the agent shown: its
// reviewer, its author, or (for a run no agent authors) the pull request's
// board task. GET /api/agents/{id}/related.
export interface RelatedAgent {
  // The agent to switch to; absent for a linked task with no agent.
  agent_id?: string
  task_id: string
  slot: string
  title: string
  role: "reviewer" | "author" | "linked_task"
  pass?: number
  status: "running" | "ended" | "none"
  activity?: string
  agent_kind?: string
  loop: { kind: "handoff" | "workflow"; task_id: string; phase: string; run_id?: string; link: string }
}

export async function fetchRelatedAgents(baseUrl: string, agentId: string): Promise<RelatedAgent[]> {
  const response = await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(agentId)}/related`)
  if (!response.ok) throw new Error(await response.text())
  const body = (await response.json()) as { related_agents?: RelatedAgent[] | null }
  return body.related_agents ?? []
}

// The row's name: "Reviewer · pass 2", "Author", "Linked task".
export function relatedAgentLabel(agent: RelatedAgent): string {
  if (agent.role === "reviewer") return agent.pass ? `Reviewer · pass ${agent.pass}` : "Reviewer"
  if (agent.role === "author") return "Author"
  return "Linked task"
}

// The loop's state, as words: "waiting for author".
export function relatedLoopState(agent: RelatedAgent): string {
  return agent.loop.phase.replace(/_/g, " ") || "author"
}
