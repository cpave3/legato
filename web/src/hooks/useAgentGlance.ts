import { useCallback, useEffect, useRef, useState } from "react"
import type { AgentInfo } from "./useWebSocket"
import { useServer } from "./useServer"
import { apiFetch } from "../lib/api"
import { fetchAgentConversation, type AgentConversation } from "../lib/agentGlance"

// What an agent is at, for its tile in the ensemble: the end of its
// conversation when it has a native endpoint, and of its terminal otherwise.
export interface AgentGlance {
  conversation: AgentConversation | null
  pane: string | null
  // The last read failed, and nothing was read before it.
  failed: boolean
}

const nothingRead: AgentGlance = { conversation: null, pane: null, failed: false }

// A conversation is read from the agent's own endpoint; a pane is captured
// from tmux, which costs more per agent.
const conversationInterval = 3000
const paneInterval = 5000

// useAgentGlance reads what one agent is at, again every few seconds for as
// long as its tile is on screen and the page is in front. A read that fails
// keeps what was read before it.
export function useAgentGlance(agent: Pick<AgentInfo, "task_id" | "has_chimera_endpoint">) {
  const { baseUrl } = useServer()
  const taskId = agent.task_id
  const native = Boolean(agent.has_chimera_endpoint)
  const [glance, setGlance] = useState<AgentGlance>(nothingRead)
  const onScreen = useRef(true)
  const read = useRef<() => void>(() => {})

  useEffect(() => {
    let current = true
    setGlance(nothingRead)
    read.current = async () => {
      if (document.hidden || !onScreen.current) return
      try {
        let next: AgentGlance
        if (native) {
          next = { conversation: await fetchAgentConversation(baseUrl, taskId), pane: null, failed: false }
        } else {
          const response = await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(taskId)}/preview`)
          if (!response.ok) throw new Error("Could not read the agent's terminal")
          const preview = await response.json() as { content: string }
          next = { conversation: null, pane: preview.content, failed: false }
        }
        if (current) setGlance(next)
      } catch {
        if (current) setGlance((previous) => previous.conversation || previous.pane !== null ? previous : { ...previous, failed: true })
      }
    }
    read.current()
    const timer = window.setInterval(() => read.current(), native ? conversationInterval : paneInterval)
    const onVisibility = () => { if (!document.hidden) read.current() }
    document.addEventListener("visibilitychange", onVisibility)
    return () => {
      current = false
      window.clearInterval(timer)
      document.removeEventListener("visibilitychange", onVisibility)
    }
  }, [baseUrl, taskId, native])

  // A tile that is scrolled out of view is not read until it comes back.
  const observer = useRef<IntersectionObserver | null>(null)
  const observe = useCallback((element: HTMLElement | null) => {
    observer.current?.disconnect()
    observer.current = null
    if (!element || typeof IntersectionObserver === "undefined") return
    observer.current = new IntersectionObserver(([entry]) => {
      const returned = entry.isIntersecting && !onScreen.current
      onScreen.current = entry.isIntersecting
      if (returned) read.current()
    })
    observer.current.observe(element)
  }, [])

  return { ...glance, observe }
}
