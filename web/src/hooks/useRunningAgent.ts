import { useCallback, useEffect, useState } from "react"
import { useServer } from "./useServer"
import { useWebSocket, type AgentInfo, type WSMessage } from "./useWebSocket"
import { apiFetch } from "../lib/api"

interface RunningAgentState {
  agent: AgentInfo | null
  loading: boolean
}

/**
 * Returns the running agent for a given task ID, or null if no agent is
 * running for that task. Refetches on agents_changed and agent_list
 * WebSocket messages so the result stays live while the page is open.
 */
export function useRunningAgent(taskId: string): RunningAgentState {
  const { baseUrl } = useServer()
  const ws = useWebSocket()
  const subscribe = ws?.subscribe
  const [agent, setAgent] = useState<AgentInfo | null>(null)
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async () => {
    if (!taskId) {
      setAgent(null)
      setLoading(false)
      return
    }
    try {
      const res = await apiFetch(baseUrl, "/api/agents")
      if (!res.ok) return
      const agents: AgentInfo[] = await res.json()
      setAgent(agents.find((a) => a.task_id === taskId && a.status === "running") ?? null)
    } catch {
      setAgent(null)
    } finally {
      setLoading(false)
    }
  }, [baseUrl, taskId])

  useEffect(() => { void refresh() }, [refresh])

  useEffect(() => {
    if (!subscribe) return
    return subscribe((message: WSMessage) => {
      if (message.type === "agents_changed") void refresh()
      if (message.type === "agent_list" && message.agents) {
        setAgent(message.agents.find((a) => a.task_id === taskId && a.status === "running") ?? null)
      }
    })
  }, [refresh, subscribe, taskId])

  return { agent, loading }
}
