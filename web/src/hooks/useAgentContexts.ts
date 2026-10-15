import { useEffect, useMemo, useState } from "react"
import { useWebSocket, type AgentInfo } from "./useWebSocket"

export interface AgentContext {
  used: number
  window: number
  // How full the window is, 0 to 100.
  share: number
}

function reading(used: number | undefined, window: number | undefined): AgentContext | undefined {
  if (used == null || !window || window <= 0) return undefined
  return { used, window, share: Math.min(Math.max(Math.floor((used * 100) / window), 0), 100) }
}

// How full each native agent's context window is, by task ID. It starts from
// what the agent list carries and follows what the server pushes as the
// agents work (agent_context), so nothing is polled. A pushed reading is the
// newest there is and wins over the list's.
export function useAgentContexts(agents: AgentInfo[]): Record<string, AgentContext> {
  const { subscribe } = useWebSocket()
  const [pushed, setPushed] = useState<Record<string, AgentContext>>({})

  useEffect(() => subscribe((message) => {
    if (message.type !== "agent_context" || !message.agent_id) return
    const next = reading(message.context_used, message.context_window)
    if (!next) return
    setPushed((current) => ({ ...current, [message.agent_id!]: next }))
  }), [subscribe])

  return useMemo(() => {
    const contexts: Record<string, AgentContext> = {}
    for (const agent of agents) {
      const listed = reading(agent.context_used, agent.context_window)
      if (listed) contexts[agent.task_id] = listed
    }
    return { ...contexts, ...pushed }
  }, [agents, pushed])
}
