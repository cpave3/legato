import { useCallback, useEffect, useState } from "react"
import { useServer } from "./useServer"
import { useWebSocket, type AgentInfo, type WSMessage } from "./useWebSocket"
import { apiFetch } from "../lib/api"

interface RunningAgentState {
  agent: AgentInfo | null
  agents: AgentInfo[]
  loading: boolean
}

function relatedRunningAgents(agents: AgentInfo[], taskId: string): AgentInfo[] {
  return agents.filter((agent) =>
    agent.status === "running" && (agent.attribution_task_id || agent.task_id) === taskId)
}

function preferredAgent(agents: AgentInfo[], taskId: string): AgentInfo | null {
  const primary = agents.find((agent) => agent.task_id === taskId)
  if (primary) return primary
  return agents.length === 1 ? agents[0] : null
}

/** Returns running agents attributed to a task and its primary-first destination. */
export function useRunningAgent(taskId: string): RunningAgentState {
  const { baseUrl } = useServer()
  const ws = useWebSocket()
  const subscribe = ws?.subscribe
  const [agents, setAgents] = useState<AgentInfo[]>([])
  const [loading, setLoading] = useState(true)

  const update = useCallback((allAgents: AgentInfo[]) => {
    setAgents(relatedRunningAgents(allAgents, taskId))
  }, [taskId])

  const refresh = useCallback(async () => {
    if (!taskId) {
      setAgents([])
      setLoading(false)
      return
    }
    try {
      const res = await apiFetch(baseUrl, "/api/agents")
      if (!res.ok) return
      update(await res.json() as AgentInfo[])
    } catch {
      setAgents([])
    } finally {
      setLoading(false)
    }
  }, [baseUrl, taskId, update])

  useEffect(() => { void refresh() }, [refresh])

  useEffect(() => {
    if (!subscribe) return
    return subscribe((message: WSMessage) => {
      if (message.type === "agents_changed") void refresh()
      if (message.type === "agent_list" && message.agents) update(message.agents)
    })
  }, [refresh, subscribe, update])

  return { agent: preferredAgent(agents, taskId), agents, loading }
}
