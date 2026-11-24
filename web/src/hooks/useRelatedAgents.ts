import { useCallback, useEffect, useRef, useState } from "react"
import { useServer } from "./useServer"
import { useWebSocket, type WSMessage } from "./useWebSocket"
import { fetchRelatedAgents, type RelatedAgent } from "../lib/related-agents"
import { coalesce, isRunChange } from "../lib/querySync"

/**
 * The agents on the other side of an agent's review loops, fetched again
 * when agents start or stop (agents_changed), when a workflow run moves on
 * (workflow_changed) and when a card changes (cards_changed, which a
 * handoff's loop moving on brings): when a loop's reviewer, author or state
 * changes.
 */
export function useRelatedAgents(agentId: string): RelatedAgent[] {
  const { baseUrl } = useServer()
  const { subscribe } = useWebSocket()
  const [rows, setRows] = useState<RelatedAgent[]>([])
  const requestIdRef = useRef(0)

  const refresh = useCallback(async () => {
    const requestId = ++requestIdRef.current
    if (!agentId) {
      setRows([])
      return
    }
    try {
      const found = await fetchRelatedAgents(baseUrl, agentId)
      if (requestId === requestIdRef.current) setRows(found)
    } catch {
      if (requestId === requestIdRef.current) setRows([])
    }
  }, [agentId, baseUrl])

  useEffect(() => { void refresh() }, [refresh])

  useEffect(() => {
    const read = coalesce(() => void refresh())
    const unsubscribe = subscribe((message: WSMessage) => {
      if (message.type === "agents_changed" || isRunChange(message) || message.type === "cards_changed") read()
    })
    return () => {
      read.cancel()
      unsubscribe()
    }
  }, [refresh, subscribe])

  return rows
}
