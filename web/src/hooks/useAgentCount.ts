import { useCallback, useEffect, useRef, useState } from "react"
import { apiFetch } from "../lib/api"
import { useServer } from "./useServer"
import { useWebSocket, type AgentInfo } from "./useWebSocket"

function countNonIdleAgents(agents: AgentInfo[]): number {
  return agents.filter((agent) => Boolean(agent.activity)).length
}

export function useAgentCount(): number {
  const { baseUrl } = useServer()
  const { subscribe } = useWebSocket()
  const [count, setCount] = useState(0)
  const requestIdRef = useRef(0)

  const refresh = useCallback(async () => {
    const requestId = ++requestIdRef.current
    try {
      const response = await apiFetch(baseUrl, "/api/agents")
      if (!response.ok) return
      const agents: AgentInfo[] = await response.json()
      if (requestId === requestIdRef.current) {
        setCount(countNonIdleAgents(agents))
      }
    } catch {
      // Preserve the last confirmed count after transient failures.
    }
  }, [baseUrl])

  useEffect(() => {
    void refresh()
  }, [refresh])

  useEffect(() => subscribe((message) => {
    if (message.type === "agent_list" && message.agents) {
      requestIdRef.current += 1
      setCount(countNonIdleAgents(message.agents))
    }
    if (message.type === "agents_changed") {
      void refresh()
    }
  }), [refresh, subscribe])

  return count
}
