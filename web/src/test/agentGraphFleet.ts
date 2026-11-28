// Fleets for the agents graph's tests and its browser preview (LEG-615).
// screenshotFleet is the fleet of the screenshot that started LEG-615: the
// Reeve, three repositories, six agents, a review loop and four watch lines.
// busyFleet adds what a busier day brings: a peer message between boxes, a
// swarm of three, and a run whose two agents work in different repositories.
import type { AgentInfo, BackgroundJob } from "../lib/agentWire"
import type { AgentGraphData, GraphRun } from "../lib/agentGraph"

export interface Fleet {
  agents: AgentInfo[]
  data: AgentGraphData
}

export const fleetAgent = (taskId: string, values: Partial<AgentInfo> = {}): AgentInfo => ({
  id: 1, task_id: taskId, card_task_id: taskId, slot: "main", task_title: taskId, tmux_session: `legato-${taskId}`, command: "claude",
  agent_kind: "claude-code", status: "running", activity: "working", started_at: "2026-11-23T09:00:00Z",
  working_seconds: 0, waiting_seconds: 0, error_seconds: 0, ...values,
})

export const fleetSub = (id: string, parent?: string, state = "working"): BackgroundJob =>
  ({ kind: "subagent", id, label: `sub ${id}`, started_at: "2026-11-23T09:30:00Z", state, ...(parent ? { parent } : {}) })

export function screenshotFleet(): Fleet {
  const agents = [
    fleetAgent("reeve-1", { reeve: true, task_title: "Tessitura", started_at: "2026-11-23T07:00:00Z" }),
    fleetAgent("AG-12", { task_title: "Proposal stack for the planner", stage: "implementing", started_at: "2026-11-23T08:10:00Z" }),
    fleetAgent("CHIM-97", { task_title: "Stream tool output in chunks", stage: "reviewing", started_at: "2026-11-23T08:00:00Z" }),
    fleetAgent("CHIM-101", { task_title: "Retry a dropped model stream", stage: "exploring", activity: "idle", started_at: "2026-11-23T08:30:00Z" }),
    fleetAgent("CHIM-97_reviewer", { card_task_id: "CHIM-97", slot: "reviewer", task_title: "Stream tool output in chunks", stage: "reviewing", started_at: "2026-11-23T09:40:00Z" }),
    fleetAgent("LEG-612", { task_title: "Workflow runs on the agents graph", stage: "implementing", activity: "waiting", started_at: "2026-11-23T08:20:00Z" }),
    fleetAgent("LEG-613", { task_title: "Reeve waits and run names", stage: "planning", started_at: "2026-11-23T08:40:00Z" }),
  ]
  const data: AgentGraphData = {
    agents: {
      "reeve-1": {},
      "AG-12": { repo: "cpave3/autopilot" },
      "CHIM-97": { repo: "cpave3/chimera" },
      "CHIM-101": { repo: "cpave3/chimera" },
      "CHIM-97_reviewer": { repo: "cpave3/chimera" },
      "LEG-612": { repo: "cpave3/legato" },
      "LEG-613": { repo: "cpave3/legato" },
    },
    links: [
      { kind: "review_loop", from: "CHIM-97_reviewer", to: "CHIM-97", label: "pass 4" },
      { kind: "reeve_watch", from: "reeve-1", to: "AG-12" },
      { kind: "reeve_watch", from: "reeve-1", to: "CHIM-97" },
      { kind: "reeve_watch", from: "reeve-1", to: "LEG-612" },
      { kind: "reeve_watch", from: "reeve-1", to: "LEG-613" },
    ],
    peer_window_seconds: 1800,
  }
  return { agents, data }
}

const fleetRun = (): GraphRun => ({
  id: "wr-9", workflow_name: "one-off-task", workflow_title: "AG-12 one-off", status: "running", current_step_id: "implement", created_at: "2026-11-23 08:00:00",
  steps: [
    { step_id: "triage", title: "Triage", status: "succeeded" },
    { step_id: "implement", title: "Implement", status: "waiting_agent" },
  ],
})

// busyFleet is screenshotFleet with more going on; now dates the peer
// message.
export function busyFleet(now: number): Fleet {
  const { agents, data } = screenshotFleet()
  agents.push(
    fleetAgent("LEG-600", { role: "conductor", parent_task_id: "LEG-600", task_title: "Swarm: settings rework", started_at: "2026-11-23T08:50:00Z" }),
    fleetAgent("st-1", { role: "parser", parent_task_id: "LEG-600", subtask_id: "st-1", task_title: "Parse the settings", started_at: "2026-11-23T08:55:00Z" }),
    fleetAgent("st-2", { role: "writer", parent_task_id: "LEG-600", subtask_id: "st-2", task_title: "Write the settings", started_at: "2026-11-23T08:56:00Z", background_jobs: [fleetSub("s1"), fleetSub("s2", "s1")] }),
    fleetAgent("AG-12-triage", { task_title: "Triage AG-12", started_at: "2026-11-23T07:50:00Z" }),
  )
  data.agents["LEG-600"] = { repo: "cpave3/legato" }
  data.agents["st-1"] = { repo: "cpave3/legato" }
  data.agents["st-2"] = { repo: "cpave3/legato" }
  data.agents["AG-12-triage"] = { repo: "cpave3/chimera", workflows: [{ run_id: "wr-9", step_id: "triage", role: "agent" }] }
  data.agents["AG-12"] = { repo: "cpave3/autopilot", workflows: [{ run_id: "wr-9", step_id: "implement", role: "agent" }] }
  data.runs = { "wr-9": fleetRun() }
  data.links.push({ kind: "peer_message", from: "AG-12", to: "LEG-612", count: 2, last_at: new Date(now - 120_000).toISOString() })
  return { agents, data }
}
