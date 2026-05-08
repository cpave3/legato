import { useEffect, useState, useCallback, useRef } from "react"
import { useWebSocket } from "./useWebSocket"
import { useServer } from "./useServer"
import { getSwarmStatus, peekInbox, type SwarmStatusData, type InboxEntry } from "../lib/swarm"

export interface SwarmState {
  status: SwarmStatusData | null
  lastChangedAt: number
}

export function useSwarmEvents() {
  const { subscribe } = useWebSocket()
  const { baseUrl } = useServer()
  const [states, setStates] = useState<Record<string, SwarmState>>({})
  const statesRef = useRef(states)
  statesRef.current = states

  const fetchState = useCallback(async (parentID: string) => {
    try {
      const status = await getSwarmStatus(baseUrl, parentID)
      setStates((prev) => ({
        ...prev,
        [parentID]: { status, lastChangedAt: Date.now() },
      }))
    } catch {
      // ignore
    }
  }, [baseUrl])

  useEffect(() => {
    return subscribe((msg) => {
      if (msg.type === "swarm_changed" && msg.parent_task_id) {
        const pid = msg.parent_task_id
        setStates((prev) => ({
          ...prev,
          [pid]: { ...prev[pid], lastChangedAt: Date.now() },
        }))
        // Refetch the full status on the next tick.
        setTimeout(() => fetchState(pid), 0)
      }
      if (msg.type === "plan_proposed" && msg.parent_task_id) {
        const pid = msg.parent_task_id
        setStates((prev) => ({
          ...prev,
          [pid]: { ...prev[pid], lastChangedAt: Date.now() },
        }))
      }
    })
  }, [subscribe, fetchState])

  return { states, fetchState }
}

export function useSwarmInbox(parentId: string | null) {
  const { baseUrl } = useServer()
  const [entries, setEntries] = useState<InboxEntry[]>([])

  const peek = useCallback(async () => {
    if (!parentId) return
    try {
      const data = await peekInbox(baseUrl, parentId)
      setEntries(data)
    } catch {
      // ignore
    }
  }, [parentId, baseUrl])

  useEffect(() => {
    setEntries([])
    peek()
  }, [peek])

  const clear = useCallback(() => {
    setEntries([])
  }, [])

  return { entries, peek, clear, setEntries }
}
