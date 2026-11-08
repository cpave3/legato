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

// The menu entry that reloads an agent on the other side of the sandbox, as
// the TUI's U does, or undefined when it is not offered: the agent cannot be
// reloaded, or the sandbox is off (neither sandboxed nor opted out of it).
export function sandboxSwitchLabel(agent: AgentInfo | undefined): string | undefined {
  if (!canReloadAgent(agent)) return undefined
  if (agent?.sandboxed) return "Reload without sandbox"
  if (agent?.unsandboxed) return "Reload in sandbox"
  return undefined
}

// A menu entry that reloads an agent on the other side of the sandbox.
export interface SandboxSwitch {
  label: string
  onSelect: () => void
}

// Reloads the running agent of a task. Fails with the server's reason when
// it cannot, in which case the agent is left running.
export async function reloadAgent(baseUrl: string, taskId: string): Promise<void> {
  await restart(baseUrl, taskId)
}

// Reloads the running agent of a task out of the sandbox when it ran in it,
// and in it when it ran outside, resuming its conversation. Resolves to
// whether it now runs in the sandbox.
export async function switchAgentSandbox(baseUrl: string, taskId: string): Promise<boolean> {
  const answer = await restart(baseUrl, taskId, { switch_sandbox: true })
  return answer.sandboxed === true
}

async function restart(baseUrl: string, taskId: string, body?: { switch_sandbox: boolean }): Promise<{ sandboxed?: boolean }> {
  const response = await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(taskId)}/restart`, {
    method: "POST",
    ...(body ? { headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) } : {}),
  })
  const text = await response.text()
  if (response.ok) {
    try {
      return JSON.parse(text) as { sandboxed?: boolean }
    } catch {
      return {}
    }
  }
  let reason = text.trim()
  try {
    reason = (JSON.parse(text) as { error?: string }).error ?? reason
  } catch {
    // Not JSON: the text is the reason.
  }
  throw new Error(reason || "The agent could not be reloaded")
}
