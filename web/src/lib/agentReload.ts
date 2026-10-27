import type { AgentInfo } from "../hooks/useWebSocket"
import { apiFetch } from "./api"

// Whether an agent can be reloaded: stopped and started again with its agent
// tool, directory and resources, resuming its conversation, so it reads its
// MCP servers, plugins and hooks afresh. A swarm or a workflow starts its own
// agents, so theirs are not offered.
export function canReloadAgent(agent: AgentInfo | undefined): boolean {
  if (!agent || agent.status !== "running") return false
  if (agent.role || agent.parent_task_id || agent.subtask_id) return false
  return !agent.workflow_run_id && agent.execution_kind !== "workflow_agent"
}

// Reloads the running agent of a task. Fails with the server's reason when
// it cannot, in which case the agent is left running.
export async function reloadAgent(baseUrl: string, taskId: string): Promise<void> {
  const response = await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(taskId)}/restart`, { method: "POST" })
  if (response.ok) return
  const body = await response.text()
  let reason = body.trim()
  try {
    reason = (JSON.parse(body) as { error?: string }).error ?? reason
  } catch {
    // Not JSON: the text is the reason.
  }
  throw new Error(reason || "The agent could not be reloaded")
}
